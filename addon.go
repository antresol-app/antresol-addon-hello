package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/antresol-app/antresol/addonkit"
)

// Publisher talks to the core API (*addonkit.Client in production): it
// sends events and registers inputs for playback.
type Publisher interface {
	Event(ctx context.Context, name string, data any) error
	RegisterInput(ctx context.Context, in addonkit.InputRequest) (addonkit.InputCreated, error)
}

// the only files /media serves, all from the directory in
// ANTRESOL_HELLO_SAMPLES
var sampleFiles = map[string]bool{"video.mp4": true, "video.mkv": true, "video.ru.m4a": true, "video.ru.srt": true}

const noSamples = "Примеры не настроены"

func newHandler(env addonkit.Env, core Publisher, ready *addonkit.Ready) http.Handler {
	mux := http.NewServeMux()
	// the core's health check: {"ok": true, "ready": …}
	mux.HandleFunc("GET /health", ready.Health)
	// GET /api/addons/hello/x/hello in the app; X-Antresol-User says who asks
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		u, ok := addonkit.UserFrom(r)
		if !ok {
			addonkit.WriteError(w, http.StatusUnauthorized, "no user")
			return
		}
		addonkit.WriteJSON(w, http.StatusOK, map[string]string{"name": u.Name})
	})
	// POST /api/addons/hello/x/ping: «ping» goes out through the core API and
	// comes back to the app as the event addon.hello.ping
	mux.HandleFunc("POST /api/ping", func(w http.ResponseWriter, r *http.Request) {
		u, ok := addonkit.UserFrom(r)
		if !ok {
			addonkit.WriteError(w, http.StatusUnauthorized, "no user")
			return
		}
		if err := core.Event(r.Context(), "ping", map[string]string{"from": u.Name}); err != nil {
			log.Printf("ping: %v", err)
			addonkit.WriteError(w, http.StatusBadGateway, "the core refused the event")
			return
		}
		addonkit.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	// /api/admin/… (x/admin/… in the app) is for admins only: the core
	// refuses it to anyone else, RequireAdmin checks again
	mux.Handle("GET /api/admin/hello", addonkit.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := addonkit.UserFrom(r)
		addonkit.WriteJSON(w, http.StatusOK, map[string]string{"admin": u.Name})
	})))
	// the sample videos the core fetches through its relay (Range works:
	// ServeContent); only the four known names, only while the directory is set
	mux.HandleFunc("GET /media/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		dir := env.Var("SAMPLES")
		if dir == "" || !sampleFiles[name] {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
	// POST /api/sample/direct|hls: registers a sample as an input for the
	// asking profile; the answer is {id}, the page opens /play/input/<id>
	mux.HandleFunc("POST /api/sample/{which}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := addonkit.UserFrom(r)
		if !ok {
			addonkit.WriteError(w, http.StatusUnauthorized, "no user")
			return
		}
		which := r.PathValue("which")
		if which != "direct" && which != "hls" {
			http.NotFound(w, r)
			return
		}
		dir := env.Var("SAMPLES")
		if dir == "" {
			addonkit.WriteError(w, http.StatusNotFound, noSamples)
			return
		}
		in, err := sampleInput(dir, which, u.ID)
		if err != nil {
			log.Printf("sample %s: %v", which, err)
			addonkit.WriteError(w, http.StatusNotFound, noSamples)
			return
		}
		created, err := core.RegisterInput(r.Context(), in)
		if err != nil {
			log.Printf("sample %s: %v", which, err)
			addonkit.WriteError(w, http.StatusBadGateway, "the core refused the input")
			return
		}
		addonkit.WriteJSON(w, http.StatusOK, map[string]string{"id": created.ID})
	})
	// the last play.progress the core sent for the asking profile (204: none yet)
	progress := &lastProgress{m: map[int64]addonkit.PlayProgress{}}
	mux.HandleFunc("GET /api/sample/progress", func(w http.ResponseWriter, r *http.Request) {
		u, ok := addonkit.UserFrom(r)
		if !ok {
			addonkit.WriteError(w, http.StatusUnauthorized, "no user")
			return
		}
		p, ok := progress.get(u.ID)
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		addonkit.WriteJSON(w, http.StatusOK, p)
	})
	// the hooks the core calls (POST /hooks/<name>): handle exactly those
	// the manifest declares in "hooks"
	hooks := &addonkit.HookMux{}
	hooks.Handle("play.progress", func(_ context.Context, _ *addonkit.User, in json.RawMessage) (any, error) {
		var p addonkit.PlayProgress
		if err := json.Unmarshal(in, &p); err != nil {
			return nil, err
		}
		progress.set(p)
		return nil, nil
	})
	mux.Handle("POST /hooks/", hooks)
	return mux
}

// lastProgress keeps the last play.progress per profile.
type lastProgress struct {
	mu sync.Mutex
	m  map[int64]addonkit.PlayProgress
}

func (l *lastProgress) set(p addonkit.PlayProgress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[p.UserID] = p
}

func (l *lastProgress) get(user int64) (addonkit.PlayProgress, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.m[user]
	return p, ok
}

// sampleInput is the registration of sample "direct" (the mp4 with an audio
// and a subtitle sidecar) or "hls" (the mkv, which the core remuxes) for a profile.
func sampleInput(dir, which string, user int64) (addonkit.InputRequest, error) {
	size := func(name string) (int64, error) {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return 0, err
		}
		return st.Size(), nil
	}
	if which == "hls" {
		n, err := size("video.mkv")
		if err != nil {
			return addonkit.InputRequest{}, err
		}
		return addonkit.InputRequest{UserID: user, Path: "/media/video.mkv", Name: "Пример/video.mkv", Size: n}, nil
	}
	n, err := size("video.mp4")
	if err != nil {
		return addonkit.InputRequest{}, err
	}
	an, err := size("video.ru.m4a")
	if err != nil {
		return addonkit.InputRequest{}, err
	}
	if _, err := size("video.ru.srt"); err != nil {
		return addonkit.InputRequest{}, err
	}
	return addonkit.InputRequest{
		UserID: user, Path: "/media/video.mp4", Name: "Пример/video.mp4", Size: n,
		Sidecars: addonkit.InputSidecars{
			Audio:     []addonkit.AudioSidecar{{Path: "/media/video.ru.m4a", Name: "Пример/video.ru.m4a", Size: an}},
			Subtitles: []addonkit.SubtitleSidecar{{Path: "/media/video.ru.srt", Name: "Пример/video.ru.srt", Lang: "ru"}},
		},
		Meta: json.RawMessage(`{"sample":"direct"}`),
	}, nil
}
