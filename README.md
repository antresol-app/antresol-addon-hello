# Hello: a sample Antresol addon

Made from `addons/template`. It shows every part of addon contract 1
(`docs/addons/contract.md` in the core):

- a page, `/addons/hello/page`, that greets the profile by name through the
  addon's own API (`GET /api/hello`, the name from `X-Antresol-User`);
- a button that asks the addon to publish `ping` through the core API
  (`POST /api/addon-core/v1/events`); the page shows the event
  `addon.hello.ping` when it comes back;
- a tile in «Управление» that opens the page;
- an item action «Привет от расширения» whose text, with the item's title,
  shows in a notice on the item page;
- playing external inputs (permission `play`, access `adults`): with
  `ANTRESOL_HELLO_SAMPLES` naming a directory that holds `video.mp4`,
  `video.mkv`, `video.ru.m4a` and `video.ru.srt`, the addon serves them on
  its socket (`GET /media/{name}`, Range works) and the page's buttons
  «Пример видео» (the mp4 with an audio and a subtitle sidecar) and «Пример
  видео (HEVC)» (the mkv) register one as an input through
  `Client.RegisterInput` and open `/play/input/<id>`. A `playerNotes` entry
  shows a notice while it loads, and the `play.progress` hook keeps the last
  position per profile (`GET /api/sample/progress`).

`make local` builds `dist-local/hello`, which the core's e2e
(`web/e2e/addons.full.spec.ts`) installs as `local:hello`. Build and test
work as in the template's README. Unlike the template, this repository has no
release workflow: its releases are made from a checked `make build` output
attached to the tag `v<version>` by the maintainers.

## Licence

Licence: MIT

The MIT licence (see `LICENSE`), Copyright (c) 2026 Antresol Contributors. The addon
SDK it builds on is MIT too; the Antresol server stays AGPL-3.0-or-later.
