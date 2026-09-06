# Atrium

Atrium is a local-first home hub: a Mac mini runs the core service, a NAS holds the family photo library (read-only), and a TV shows an always-on dashboard with the clock, photo slideshow and device status. Everything works on the home LAN without cloud accounts, and a maintainer can change what the TV shows from the command line.

> Status: **pre-release, not yet hardware-verified**. The V1 base (V0.1 Screen → V0.2 NAS → V0.3 Realtime) is being implemented against the design documents below. Hardware verification (G0) with the real Mac mini, NAS and TV has not happened yet, so nothing here is a promise about a particular TV or NAS.

## What it does

- **Dashboard on the TV**: time and date in the home timezone, a photo slideshow, Core/NAS/connection status readable from across the room. Works in the TV's browser; no app store, no CDN.
- **Photos from the NAS, read-only**: one authorized directory is indexed into a local SQLite database; previews and thumbnails are generated on the Mac mini and cached with a size budget. Collections: recently added, captured today, random slideshow, browse all.
- **Screen control**: `navigate`, `show` and `refresh` commands with delivery confirmation (`accepted → applied | failed | expired | unknown`), issued from the CLI or plain `curl`.
- **Recovery by design**: the TV reconnects with backoff, the service survives restarts under launchd, the NAS being offline never blocks the dashboard, and scans never mass-delete on failure.
- **Security on an untrusted LAN**: paired screens and the admin use separate credentials; TLS with a locally generated CA; no NAS credentials or paths ever reach the TV; previews are re-encoded so EXIF/GPS is stripped.

AI integration (`home-mcp`) is a later, optional increment; the base system does not depend on it.

## Architecture in one picture

```
TV browser  <-- HTTPS + WebSocket -->  atrium serve (Go, Mac mini)  -- read-only -->  NAS share
                                         |  SQLite index + preview cache (local disk)
atrium admin / scripts  -- Bearer -->    |
```

- `docs/prd/2026-09/atrium-home-hub/prd.md` — product requirements (Chinese)
- `docs/tech/2026-09/atrium-home-hub/tech-design.md` — technical design: stack, data model, HTTP/WebSocket contracts, pipelines, security, deployment
- `docs/plan/2026-09/atrium-home-hub/dev-plan.md` — task-level development plan and decision log
- `docs/api/` — OpenAPI and WebSocket protocol
- `docs/ops/` — runbook, acceptance procedure, G0 record template

## Stack

Go (single static binary, `net/http`, WebSocket, SQLite via `modernc.org/sqlite`), React + TypeScript + Vite for the TV client (embedded into the binary), launchd for hosting on macOS.

## Quick start (development)

Requirements: Go 1.24+, Node 20+.

```sh
make web            # build the TV client and copy it into the embed directory
make build          # bin/atrium with the embedded UI
./bin/atrium init --config ./config.yaml --data-dir ./data --public-url https://127.0.0.1:8443
# edit config.yaml: home.timezone, sources[0].root (read-only NAS mount)
./bin/atrium serve --config ./config.yaml
```

Open `https://<mac-mini-ip>:8443` on the TV. It shows a 6-digit pairing code; approve it on the Mac mini:

```sh
./bin/atrium admin pair approve 123456 --id living_room_tv --name "Living room TV"
./bin/atrium admin screens list
./bin/atrium admin screen navigate living_room_tv --route photos --collection captured_today --wait
```

The generated admin token lives in `data/admin.token` (mode 0600); the local CA certificate for the TV is printed by `atrium tls export-ca`. See `docs/ops/runbook.md` for launchd installation, NAS mount options, diagnostics, backup and restore.

`make check` runs vet, lint, tests and the build for both tracks; `scripts/e2e/` holds a Playwright smoke test that drives the real server and client in headless Chrome (pairing, dashboard, slideshow, `navigate`/`show`/`refresh`).

## Repository layout

```
cmd/atrium          entrypoint
internal/           Go packages (config, store, auth, httpapi, ws, source, indexer, media, screen, widget, ...)
web/                Vite + React client (output copied to internal/webui/dist by `make web-sync`)
deploy/launchd      LaunchAgent template and install scripts
scripts/            curl-based control and benchmark scripts
docs/               prd, tech design, plan, api, ops
```

## Privacy and safety

- The NAS is opened read-only through an interface that has no write methods; symlinks are never followed.
- The repository contains no real photos, paths, hostnames or credentials. Local config (`config.yaml`), data directories, tokens and certificates are git-ignored.
- Logs never contain tokens, cookies or NAS paths; diagnostics reference photo IDs only.

## License

MIT — see `LICENSE`.
