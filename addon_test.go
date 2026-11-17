package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/antresol-app/antresol/addonkit"
)

func serve(t *testing.T, ready *addonkit.Ready, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if ready == nil {
		ready = &addonkit.Ready{}
	}
	rec := httptest.NewRecorder()
	newHandler(addonkit.Env{ID: "hello"}, &events{}, ready).ServeHTTP(rec, r)
	return rec
}

func TestHealth(t *testing.T) {
	ready := &addonkit.Ready{}
	for _, want := range []bool{false, true} {
		rec := serve(t, ready, httptest.NewRequest("GET", "/health", nil))
		var got map[string]bool
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got["ready"] != want {
			t.Fatalf("health: %d %s, want ready %v", rec.Code, rec.Body, want)
		}
		ready.Set()
	}
}

func TestHelloNamesTheUser(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/hello", nil)
	// the core escapes every non-ASCII character of the header's JSON
	req.Header.Set("X-Antresol-User", `{"id":1,"name":"Мама","isAdmin":true,"isKid":false,"lang":"ru"}`)
	rec := serve(t, nil, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got["name"] != "Мама" {
		t.Fatalf("hello: %d %s", rec.Code, rec.Body)
	}
}

func TestHelloWithoutUser(t *testing.T) {
	if rec := serve(t, nil, httptest.NewRequest("GET", "/api/hello", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no user: %d", rec.Code)
	}
}

func TestAdminOnly(t *testing.T) {
	for user, want := range map[string]int{
		`{"id":1,"name":"A","isAdmin":true}`: http.StatusOK,
		`{"id":2,"name":"B"}`:                http.StatusForbidden,
		``:                                   http.StatusForbidden,
	} {
		req := httptest.NewRequest("GET", "/api/admin/hello", nil)
		if user != "" {
			req.Header.Set("X-Antresol-User", user)
		}
		if rec := serve(t, nil, req); rec.Code != want {
			t.Fatalf("%q: %d, want %d", user, rec.Code, want)
		}
	}
}

func TestUndeclaredHookIs404(t *testing.T) {
	if rec := serve(t, nil, httptest.NewRequest("POST", "/hooks/library.pending", nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("hook: %d", rec.Code)
	}
}

type events struct {
	name string
	data any
	in   []addonkit.InputRequest
}

func (e *events) RegisterInput(_ context.Context, in addonkit.InputRequest) (addonkit.InputCreated, error) {
	e.in = append(e.in, in)
	return addonkit.InputCreated{ID: "abc"}, nil
}

func (e *events) Event(_ context.Context, name string, data any) error {
	e.name, e.data = name, data
	return nil
}

func TestPingPublishesTheEvent(t *testing.T) {
	ev := &events{}
	req := httptest.NewRequest("POST", "/api/ping", strings.NewReader("{}"))
	req.Header.Set("X-Antresol-User", `{"id":1,"name":"Mama"}`)
	rec := httptest.NewRecorder()
	newHandler(addonkit.Env{ID: "hello"}, ev, &addonkit.Ready{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || ev.name != "ping" || ev.data.(map[string]string)["from"] != "Mama" {
		t.Fatalf("ping: %d, event %q %v", rec.Code, ev.name, ev.data)
	}
}

const mama = `{"id":7,"name":"Mama","isAdmin":false,"isKid":false}`

// samples writes the four sample files and points the addon at them.
func samples(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"video.mp4": "0123456789", "video.mkv": "abcdefghijkl", "video.ru.m4a": "audio", "video.ru.srt": "1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ANTRESOL_HELLO_SAMPLES", dir)
}

func TestMediaRangeAndUnknownName(t *testing.T) {
	samples(t)
	req := httptest.NewRequest("GET", "/media/video.mp4", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := serve(t, nil, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body)
	}
	for _, name := range []string{"secret.txt", "..%2Faddon.go", "video.mp4.bak"} {
		if rec := serve(t, nil, httptest.NewRequest("GET", "/media/"+name, nil)); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", name, rec.Code)
		}
	}
}

func TestMediaWithoutSamplesDir(t *testing.T) {
	t.Setenv("ANTRESOL_HELLO_SAMPLES", "")
	if rec := serve(t, nil, httptest.NewRequest("GET", "/media/video.mp4", nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("unset: %d", rec.Code)
	}
}

func post(t *testing.T, ev *events, path, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, nil)
	if user != "" {
		req.Header.Set("X-Antresol-User", user)
	}
	rec := httptest.NewRecorder()
	newHandler(addonkit.Env{ID: "hello"}, ev, &addonkit.Ready{}).ServeHTTP(rec, req)
	return rec
}

func TestSampleDirectRegistersTheInput(t *testing.T) {
	samples(t)
	ev := &events{}
	rec := post(t, ev, "/api/sample/direct", mama)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got["id"] != "abc" {
		t.Fatalf("direct: %d %s", rec.Code, rec.Body)
	}
	want := addonkit.InputRequest{
		UserID: 7, Path: "/media/video.mp4", Name: "Пример/video.mp4", Size: 10,
		Sidecars: addonkit.InputSidecars{
			Audio:     []addonkit.AudioSidecar{{Path: "/media/video.ru.m4a", Name: "Пример/video.ru.m4a", Size: 5}},
			Subtitles: []addonkit.SubtitleSidecar{{Path: "/media/video.ru.srt", Name: "Пример/video.ru.srt", Lang: "ru"}},
		},
		Meta: json.RawMessage(`{"sample":"direct"}`),
	}
	if len(ev.in) != 1 || !reflect.DeepEqual(ev.in[0], want) {
		t.Fatalf("request: %+v\nwant %+v", ev.in, want)
	}
}

func TestSampleHLSRegistersTheMkv(t *testing.T) {
	samples(t)
	ev := &events{}
	if rec := post(t, ev, "/api/sample/hls", mama); rec.Code != http.StatusOK {
		t.Fatalf("hls: %d %s", rec.Code, rec.Body)
	}
	if len(ev.in) != 1 || ev.in[0].Path != "/media/video.mkv" || ev.in[0].Name != "Пример/video.mkv" || ev.in[0].Size != 12 || ev.in[0].UserID != 7 {
		t.Fatalf("request: %+v", ev.in)
	}
}

func TestSampleRefusals(t *testing.T) {
	samples(t)
	ev := &events{}
	if rec := post(t, ev, "/api/sample/direct", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no user: %d", rec.Code)
	}
	if rec := post(t, ev, "/api/sample/other", mama); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown sample: %d", rec.Code)
	}
	t.Setenv("ANTRESOL_HELLO_SAMPLES", "")
	rec := post(t, ev, "/api/sample/direct", mama)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Примеры не настроены") {
		t.Fatalf("unset: %d %s", rec.Code, rec.Body)
	}
	if len(ev.in) != 0 {
		t.Fatalf("registered anyway: %+v", ev.in)
	}
}

func TestProgressHookThenGet(t *testing.T) {
	h := newHandler(addonkit.Env{ID: "hello"}, &events{}, &addonkit.Ready{})
	get := func(user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/sample/progress", nil)
		req.Header.Set("X-Antresol-User", user)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get(mama); rec.Code != http.StatusNoContent {
		t.Fatalf("before: %d", rec.Code)
	}
	hook := httptest.NewRequest("POST", "/hooks/play.progress", strings.NewReader(`{"inputId":"abc","userId":7,"positionSec":12.5,"durationSec":60,"ended":false}`))
	hook.Header.Set("X-Antresol-User", mama)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, hook)
	if rec.Code >= 300 {
		t.Fatalf("hook: %d %s", rec.Code, rec.Body)
	}
	rec = get(mama)
	var got addonkit.PlayProgress
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.InputID != "abc" || got.PositionSec != 12.5 {
		t.Fatalf("after: %d %s", rec.Code, rec.Body)
	}
	if rec := get(`{"id":8,"name":"Other"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("another user: %d", rec.Code)
	}
}
