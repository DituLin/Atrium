# Changelog

All notable changes to Atrium are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Until 1.0 the HTTP contract in `docs/api/openapi.yaml` and the WebSocket
contract in `docs/api/ws-protocol.md` may change between minor versions; both
are kept in sync with `docs/tech/2026-09/atrium-home-hub/tech-design.md`.

## 0.1.0 (unreleased)

First complete V1 base: a Mac mini serves a dashboard to a TV over the LAN,
indexes a read-only NAS photo share, and can be driven and diagnosed entirely
from the command line. Nothing is exposed to the internet and no cloud service
is required.

### Screen and dashboard (V0.1)

- Pairing with a six-digit code approved on the Mac mini; the screen credential
  is minted at claim time, so no unclaimed token exists at rest.
- Separate screen and admin credentials (`atr_scr_*`, `atr_adm_*`), stored only
  as SHA-256 hashes, with cookie and Bearer transports, an Origin policy,
  cookie-only-on-GET, admin-Bearer-only and an auth-failure rate limit.
- `GET /api/v1/home` composes the dashboard snapshot from cheap in-memory and
  database reads: clock with timezone and DST transitions, photo totals, NAS
  health, and the optional weather and notice widgets.
- `atrium init`, `serve`, `tls renew|export-ca`, `token reset`, and a launchd
  LaunchAgent with install and uninstall scripts.
- TLS `auto` generates a local CA and a server certificate; `file` and `off`
  are supported, and `off` is reported as `insecure` everywhere.

### NAS and photos (V0.2)

- Read-only source access with path hardening, no symlink following, per-call
  deadlines that surface a hung share instead of blocking, and a bounded
  in-flight semaphore.
- Mount identity binding (filesystem type, hashed mount source, optional marker
  file); a mismatch stops scanning and removal until an operator rebinds.
- Health prober per source with `online` / `degraded` / `offline` / `unknown`
  and stable detail codes.
- Indexer with stability checks before indexing, a baseline-import concept so a
  first import does not read as "added today", and a two-missed-scan removal
  rule that a failed scan never advances.
- Metadata extraction (`captured_at` exact / inferred / unknown; the file mtime
  is never used as a capture time) and preview generation with orientation
  correction, size and byte budgets, and stable failure codes.
- Optional HEIC conversion through `sips`, behind an interface.
- LRU cache janitor with an eviction budget and a low-disk pause.
- Collections `recent`, `captured_today`, `random` and `all` with opaque
  cursors, and an ID-only media route that re-encodes previews, so EXIF and GPS
  never reach a client.
- Admin control of sources, scans, photos and exclusions, with an audit log.

### Realtime and control (V0.3)

- WebSocket sessions at `GET /api/v1/screens/connect`: authenticated handshake,
  one session per screen with supersede, a bounded send queue, a 64 KiB frame
  limit and the close codes of design §9.
- Heartbeat-based presence: `online` now means a live session with a recent
  heartbeat, distinct from `registered`.
- Screen commands `navigate`, `show` and `refresh` with per-kind validation, a
  per-screen monotonic sequence allocated in the insert transaction, a 10 s TTL,
  and the full `accepted → applied | failed | expired | unknown` lifecycle.
  An offline screen returns `409` with the recorded command and nothing is
  queued.
- `unknown` commands are never resolved automatically; the next heartbeat
  attaches the sequence and route the screen reports.
- Coalesced `data.changed` push (250 ms per session) and immediate session
  revocation on `DELETE /screens/{id}`.
- `atrium admin screen navigate|show|refresh [--wait]` and
  `atrium admin commands get|list`, plus `curl` equivalents in `scripts/`.
- Daily retention for commands, scan runs, the audit log, finished jobs and
  pairings.

### Operations (H1)

- `GET /api/v1/diagnostics` and `atrium admin diag` render the whole operator
  document: sources, photos, jobs, cache, screens, commands, widgets and a ring
  of the 100 most recent errors. It contains no credentials and no paths.
- Backups via `VACUUM INTO` with configuration copies, retention, a daily
  scheduler, `POST`/`GET /api/v1/backups` and `atrium backup now|list`.
- `atrium restore --from <file>`, refused while a server holds the data
  directory lock; the previous database is preserved, never deleted.
- `POST /api/v1/admin/token/rotate` and `atrium admin token rotate`; the
  previous token is revoked only after the replacement has been delivered.
- `docs/ops/`: runbook, control guide, acceptance measurements, failure matrix
  and the G0 record template.
- `scripts/bench-api.sh` and `scripts/bench-commands.sh` for the PRD §7.1
  latency targets.
- `make release` builds `atrium-darwin-arm64` and `atrium-darwin-amd64` with
  the web bundle embedded and version, commit and build date linked in.

### Known limitations

- HEIC conversion is exercised only against a fake converter in CI; a real
  `sips` run needs `ATRIUM_TEST_SIPS=1` and a sample.
- The performance targets in `docs/ops/acceptance.md` are planning targets
  until the G0 hardware baselines are recorded.
- The 24-hour offline cache limit is a client-side rule; it cannot revoke
  content already copied off a device.
