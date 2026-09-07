# Third-party notices

MyTiktok depends on separately licensed third-party software. Exact versions are recorded in `go.mod`, `go.sum`, `web/package.json`, and `web/package-lock.json`.

- `github.com/SheltonZhu/115driver` v1.3.2 — MIT. Used as a Go dependency for 115 QR login, credentials, listing, file metadata and download links. [Upstream](https://github.com/SheltonZhu/115driver).
- `modernc.org/sqlite` — BSD-3-Clause Go SQLite driver and its transitive components.
- React / React DOM — MIT.
- Vite — MIT.
- lucide-react — ISC.
- FFmpeg is installed as a Debian package in the runtime image; its build-specific LGPL/GPL licensing and copyright notices are included by that package. MyTiktok invokes it as a separate process.

The 115 integration approach was researched in `nianzhibai/91` (MIT). MyTiktok's application, storage abstraction, HTTP handlers, database, scanner, and frontend were written independently rather than importing that project's backend or frontend files. The SDK's protocol implementation is consumed through Go Modules, not vendored here.

When distributing binary or container builds, retain upstream license notices. Go dependency licenses are available in the module cache; npm dependencies carry license files in their packages; Debian package notices are under `/usr/share/doc/` in the runtime image.
