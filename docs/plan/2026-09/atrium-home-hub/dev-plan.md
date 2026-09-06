# Atrium Home Hub — Development Plan

| Item | Value |
| --- | --- |
| Project | Atrium |
| Document | Development plan for V1 base (M0 scaffold, V0.1 Screen, V0.2 NAS, V0.3 Realtime, hardening) |
| Version | 0.1 |
| Status | Active |
| Created | 2026-09-05 |
| Source PRD | `docs/prd/2026-09/atrium-home-hub/prd.md` |
| Technical design | `docs/tech/2026-09/atrium-home-hub/tech-design.md` |

This plan is written to be executed by engineers or coding agents without further clarification. Every task has an ID, dependencies, deliverables (files), acceptance criteria and required tests. The technical design is the authority for contracts; when a task and the design disagree, follow the design and record the discrepancy in §11.

## 0. How to work this plan

### 0.1 Conventions

- Language: code, comments, identifiers, commit messages and documentation in English. The PRD stays in Chinese.
- Go: `gofmt`, `go vet`, `golangci-lint` clean; errors wrapped with `%w`; contexts on every I/O; no globals except `main`.
- Web: strict TypeScript, ESLint clean, no `any` in `src/core`; no external network requests at runtime.
- Tests are part of each task, not a later phase. A task is not done until its tests pass locally (`make test`).
- Commit granularity: one task ID per commit where practical, message `feat(scope): B-201 short description`. Do not commit generated `web/dist` content, data directories, tokens or certificates.
- Secrets and privacy: no real NAS paths, IPs, hostnames, emails or photos in the repository. Examples use `192.168.1.10`, `/Volumes/photos/family`, `living_room_tv`. `testdata/` images are generated programmatically.
- Keep `docs/api/openapi.yaml` and `docs/api/ws-protocol.md` in sync with any contract change; the design §8/§9 is the source.

### 0.2 Ownership boundaries for parallel work

| Track | Owns | Must not touch |
| --- | --- | --- |
| **Backend (Go)** | `cmd/`, `internal/` (incl. `internal/webui/embed.go` and `internal/webui/dist/.gitkeep`), `web/go.mod` (stub), `deploy/`, `scripts/`, `testdata/`, `Makefile`, `go.mod`, `docs/api/`, `docs/ops/`, `.golangci.yml`, `.github/workflows/` | `web/src`, `web/package.json`, `web/vite.config.ts` |
| **Web (TypeScript)** | everything under `web/` except the stub `web/go.mod`; output must land in `web/dist` (copied into the embed dir by `make web-sync`) | any Go file |
| **Integration** | wiring both, README, release checklist | — |

Contract between tracks: design §8 (HTTP), §9 (WebSocket), §10 (config keys visible to the client via `/home`). If a track needs a contract change it must update `docs/api/*` in the same change and note it in §11.

### 0.3 Definition of done per milestone

A milestone is done when all its P0 tasks are merged, `make check` (vet, lint, tests, build for both tracks) passes, the runbook covers the new behaviour, and the acceptance items in §9 for that milestone are demonstrably testable with the shipped CLI/scripts (hardware runs happen later in G0/V1 acceptance).

## 1. Milestones

| Milestone | Goal | Exit criteria | Estimate |
| --- | --- | --- | --- |
| **M0 Scaffold** | Repository, toolchain, CI, config, DB, embed pipeline | `atrium init && atrium serve` serves the built web UI over TLS; `make check` green | 1–2 person-days |
| **V0.1 Screen** | Dashboard, clock, pairing, auth, connection recovery, launchd | TV (or desktop browser) pairs, shows the dashboard, survives Core restart and network loss | 4–6 person-days |
| **V0.2 NAS** | Read-only source, identity, health, indexer, media pipeline, collections, admin scope controls | Sample library indexed; NAS offline does not block the dashboard; collections and previews correct | 6–8 person-days |
| **V0.3 Realtime** | WS sessions, heartbeat, commands, acks, change push, control CLI and scripts | Navigate/show/refresh with applied/failed/expired/unknown tracking; 7-day stability plan executable | 4–6 person-days |
| **H1 Hardening** | Diagnostics, backup/restore, retention, docs, release build | Runbook and acceptance docs complete; release artifacts built by CI | 2–3 person-days |
| **G0 Hardware** | Executed by the maintainer with real Mac mini / NAS / TV | `docs/ops/g0-record.md` filled; open items in design §16 decided | 2–4 person-days (maintainer) |
| **V0.4 AI** | Out of scope for this plan; design boundary only | — | later |

Ordering: M0 → V0.1 → V0.2 → V0.3 → H1. Backend and web tracks run in parallel inside each milestone; the web track for V0.2/V0.3 can start against the contract before the backend is complete by using the mock server (W-004).

## 2. M0 — Scaffold

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| S-001 | Repository skeleton | — | `.gitignore` (Go, Node, macOS, `*.token`, `*.key`, `*.crt`, `data/`, `web/dist/*` except `.gitkeep`, `.env*`, `config.yaml`, `config.local.yaml`), `.editorconfig`, `LICENSE` (MIT), `README.md` (purpose, quick start, status), `CONTRIBUTING.md`, `SECURITY.md`, `Makefile` (`build`, `web`, `test`, `lint`, `check`, `run-dev`, `e2e`), `.golangci.yml` | `make check` runs all targets; no tracked file contains private data |
| S-002 | Go module and entrypoint | S-001 | `go.mod` (module `github.com/DituLin/Atrium`, Go 1.25), `cmd/atrium/main.go`, `internal/cli` with `version`, `serve`, `init` stubs (cobra), `internal/app` lifecycle (signal handling, graceful shutdown) | `go build ./...`; `atrium version` prints version + commit (ldflags) |
| S-003 | Config package | S-002 | `internal/config`: structs for every key in design §10, defaults, `Load(path)`, `Validate()`, `Redacted()`; `config.example.yaml` | Unit tests: defaults applied; every validation rule in design §10 rejects; `~` expansion; env overrides |
| S-004 | Store and migrations | S-002 | `internal/store`: `Open(path)` with pragmas, embedded migrations `0001_init.sql` covering design §5 fully, `Migrate()`, `schema_migrations`; repository skeletons per table; `store/testutil` temp DB helper | Tests: migrate from empty; idempotent re-run; foreign keys enforced; WAL enabled |
| S-005 | Domain package | S-002 | `internal/domain`: enums (health, photo status, preview status, command status, route names, widget types), entity structs, error codes from design §8, ID generation (ULID) | Tests: enum validation; error code table complete |
| S-006 | HTTP server base | S-003, S-004 | `internal/httpapi`: router (Go 1.22 `net/http` patterns), middleware chain (request ID, recovery, access log with redaction, JSON errors), `/health/live`, `/health/ready`; TLS from config (`auto/file/off`) via `internal/auth/tlsgen` | Tests: health routes; TLS auto generates CA + server cert with expected SANs; `off` mode logs warning and sets `insecure` |
| S-007 | Web UI embedding | S-006 | `internal/webui/embed.go` (`//go:embed all:dist`), `internal/webui/dist/.gitkeep`, `web/go.mod` stub, `make web-sync` copy step, `internal/webui` SPA handler (hashed assets immutable, `index.html` no-cache, fallback to index for non-API paths, friendly "UI not built" page when `index.html` missing) | Tests: asset caching headers; SPA fallback; missing dist handled |
| S-008 | Logging | S-003 | `slog` JSON handler to `logs/atrium.log` via lumberjack with retention; console handler for `--dev`; redaction helper rejecting keys `token`, `authorization`, `cookie`, `root_path`, `rel_path` at info level | Tests: redaction; rotation config |
| S-009 | CI | S-001 | `.github/workflows/ci.yml`: Go (vet, lint, test -race), web (ci, lint, test, build), full build with embedded UI, upload `atrium-darwin-arm64` and `-amd64` artifacts | Workflow passes on push |
| W-001 | Web project scaffold | — | `web/package.json`, `vite.config.ts` (`target es2018`, `outDir dist`, `emptyOutDir`, `postbuild: touch dist/.gitkeep`, dev proxy `/api` and `/health` to `https://127.0.0.1:8443` with self-signed accepted), `tsconfig.json` strict, ESLint, Vitest, `src/main.tsx`, `index.html` with inlined critical CSS | `npm run build` produces `web/dist/index.html`; `npm test` runs |
| W-002 | Types for contracts | W-001 | `web/src/types/api.ts`, `ws.ts` mirroring design §8/§9 (DTOs, error codes, envelope, route state) | Type-checks; a compile-time exhaustive switch over command kinds |
| W-003 | Core client libs | W-002 | `src/core/api.ts` (fetch wrapper: JSON errors, `401` → pair event, `Retry-After`), `src/core/ws.ts` (envelope codec, reconnect with full-jitter backoff 1–30 s, close-code handling), `src/core/clock.ts` (server offset, tz formatting with fallback) | Vitest: backoff sequence bounds; close codes map to actions; clock formatting with and without `Intl` tz |
| W-004 | Mock core for development | W-002 | `web/mock/server.ts` (Node http + ws) implementing pairing, `/home`, `/photos` with generated SVG/JPEG placeholders, WS commands; `npm run dev:mock` | The web app runs end to end against the mock, including a scripted `navigate` and `show` |

## 3. V0.1 — Screen

### 3.1 Backend

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| B-101 | Token and credential primitives | S-004, S-005 | `internal/auth`: token generation (`atr_scr_`, `atr_adm_`), sha256 hashing, `admin_tokens` repo, admin token file (0600) written by `atrium init`, `atrium token reset` | Tests: format, hash lookup, file permissions, reset flow |
| B-102 | Auth middleware and scopes | B-101 | Bearer + cookie extraction, scope resolution (`screen`, `admin`), Origin policy, cookie-only-on-GET rule, auth-failure rate limiter, `screens/me` | Tests: matrix of (credential type × route scope × origin) per design §6.8; rate limit returns `429` |
| B-103 | Pairing flow | B-102 | `pairings` repo; `POST /pair/start`, `GET /pair/{id}`, `POST /pair/{id}/claim`; admin `GET /pairings`, `POST /pairings/{id}/approve`, `DELETE /pairings/{id}`; cookie attributes per TLS mode; retention (1 day) | Integration test: start → approve → claim → `/home` with cookie; second claim `410`; expiry; rate limits; code uniqueness |
| B-104 | Screens registry | B-103 | `screens` repo; `GET/DELETE /screens`, `GET /screens/{id}`; revoke marks status and (later) closes sessions; `online` computed from `last_seen_at` | Tests: revoke blocks auth immediately; list shows `registered` vs `online` |
| B-105 | Home snapshot v1 | S-006 | `internal/widget`: `clock` (timezone, server time, offset, next transition via `time.Location` lookups), `photo` placeholder totals (zeros until V0.2), `nas` placeholder from config (health `unknown`); `GET /home`; `internal/clock` with `HomeDay`, `OffsetAt`, `NextTransition` | Tests: DST transition edge for a tz with DST and one without; response schema snapshot |
| B-106 | Optional widgets (P1) | B-105 | Weather via Open-Meteo with cache, refresh, stale flag; static notice; both hidden when disabled | Tests with an `httptest` provider stub: refresh interval, stale after 2 h, provider failure keeps last payload |
| B-107 | `atrium init` and `serve` | S-003, B-101 | `init`: write example config if missing, create data dir tree, TLS auto, admin token; `serve`: full startup/shutdown per design §4.2/4.3; `tls renew | export-ca` | Integration test: init in temp dir then serve on a random port; `/health/ready` OK; shutdown within 10 s |
| B-108 | launchd packaging | B-107 | `deploy/launchd/com.atrium.core.plist.tmpl`, `install.sh`, `uninstall.sh`, `deploy/README.md` (login/auto-login/FileVault notes, NAS mount options, log locations) | Shell scripts pass `shellcheck`; manual test documented |
| B-109 | Admin CLI base | B-102 | `atrium admin` root with `--url/--token/--json`, CA trust from data dir, `diag` (v1 fields), `pair`, `screens` subcommands | Tests: CLI against `httptest` server; JSON and table output |

### 3.2 Web

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| W-101 | App shell, router state machine, focus/keys | W-003 | `src/app/router.ts` (reducer, whitelist routes, `appliedSequence`), `src/ui/keys.ts` (remote key mapping incl. 461/10009), `src/ui/focus.tsx` (roving focus, visible ring), layout primitives for 1080p/4K | Vitest: router rejects unknown routes; key mapping table |
| W-102 | Pair screen | W-101 | Shows code large, instructions, polling with `poll_interval_ms`, error states (expired → restart), claim → dashboard | Runs against mock; expiry path tested |
| W-103 | Connect screen and connection state | W-101 | Connection reducer (`idle/connecting/online/reconnecting/offline`), banner on dashboard when reconnecting, full connect screen when nothing cached or auth cache expired, "another session active" for `4003` | Vitest: reducer transitions incl. `401`, `4002`, `4003` |
| W-104 | Dashboard v1 | W-103 | Clock widget (local tick, server offset, date crossing midnight, "time unverified" badge), status bar (Core/NAS/connection with icon + text), photo pane placeholder ("no photos yet" / index progress), widget registry with unknown-type hiding, weather and notice widgets (hidden when absent) | Visual check at 1920×1080 and 3840×2160 in a desktop browser; Vitest for clock reducer |
| W-105 | Auth cache expiry rule | W-103 | `src/core/authExpiry.ts`: `lastAuthOkAt` persistence, checks on boot/visibility/60 s, purge and route to connect | Vitest: boundary at 24 h; purge clears photo state |
| W-106 | Error boundaries and degraded rendering | W-104 | Per-widget error boundary (a failing widget renders a placeholder, never a white page); global boundary shows connect screen with retry | Vitest: throwing widget contained |

## 4. V0.2 — NAS

### 4.1 Backend

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| B-201 | Source FS abstraction | S-005 | `internal/source`: `FS` interface, `OSFS` with path hardening (no `..`, no symlinks, root containment), deadline wrapper with helper goroutine + `stuck_ops` accounting + inflight semaphore, `FakeFS` for tests (hang, EACCES, disappear, slow) | Tests: traversal attempts rejected; symlink skipped; deadline returns `ErrStuck` and accounts; semaphore trips `degraded` |
| B-202 | Mount identity and health prober | B-201 | `Statfs` on darwin (`syscall.Statfs_t`: `Fstypename`, `Mntfromname`, `Fsid`) with portable fallback; identity computation, binding, mismatch handling; health state machine and transitions; `data_sources` reconcile at startup; share capacity fields | Tests: local dir with `require_mount` → `unknown/not_a_mount`; `allow_local` binds; mismatch after bind → `unknown/identity_mismatch`; offline/degraded/online transitions with `FakeFS` |
| B-203 | Indexer walk and stability | B-202 | `internal/indexer`: scheduler (serial, non-overlapping, manual merge), walker with exclusions and extension allowlist, stability set with rounds, `scan_runs` accounting and progress, generation handling, unsupported tracking | Tests over a temp tree: new files indexed only after stability; changed file re-queued; excluded prefix skipped; progress counters |
| B-204 | Removal and baseline rules | B-203 | Two-missing-generations rule; failed/aborted scans do not count; revival of removed paths; baseline flagging and `baseline_completed_at`; `first_seen_at` semantics; source revoke/restore | Tests: file removed → still `ready` after one completed scan, `removed` after two; a failed scan in between does not advance; baseline flag on first import only; reappearance sets new `first_seen_at` |
| B-205 | Job queue | S-004 | `internal/jobs`: persistent queue (pick with lock, dedup index, priority, backoff schedule `1m,5m,30m,6h`, max attempts), worker pool with per-job deadline, panic recovery, stale-lock recovery on startup | Tests: dedup; backoff; locked jobs return to queue after restart; deadline cancels |
| B-206 | Metadata extraction | B-205, B-201 | `internal/media`: `MetadataReader` interface with `imagemeta` implementation (fallback `go-exif/v3` if needed); `captured_at` resolution (exact/inferred/unknown, plausibility), `captured_day`, orientation, dimensions; fingerprint; `extract_meta` job; `testutil` that writes JPEGs with EXIF (`DateTimeOriginal`, `OffsetTimeOriginal`, `Orientation`) and PNGs | Tests: all three confidence outcomes; implausible dates → unknown; PNG without EXIF → unknown; fingerprint stable across runs |
| B-207 | Preview generation | B-206 | `build_preview` job: guards (health, disk, budget), decode with `AutoOrientation`, `max_pixels`/`max_source_bytes` checks, preview/thumb sizing and byte target loop, atomic write, `preview_files` rows, failure codes and retry, `unsupported` promotion | Tests: sizes ≤ limits; bytes ≤ 1 MiB on a large synthetic image; orientation 6 rotated; corrupt file → `decode_failed` with backoff; huge dimensions → `too_large` |
| B-208 | HEIC via sips | B-207 | `heic.Converter` interface; `sips` implementation with timeout and kill; `off` mode counts HEIC as unsupported; orientation re-read from converted JPEG; `sips -g creation` used when EXIF unavailable | Tests: converter interface with a fake; real `sips` test skipped unless `ATRIUM_TEST_SIPS=1` and a sample exists (no sample committed) |
| B-209 | Cache budget janitor | B-207 | LRU eviction to 90 % of budget, `evicted` status, low-disk pause, throttled `last_access_at`, periodic and post-write runs | Tests: eviction order; index retained after eviction; pause when free below threshold (fake disk stats) |
| B-210 | Photo collections API | B-204, B-207 | `GET /photos` (recent, captured_today, random, all) with keyset/seeded cursors and `meta`; `GET /photos/{id}` with `neighbors`; DTO per design §8 | Tests: recent excludes baseline; captured_today window left-closed right-open in home tz across midnight; random round has no repeats and stable pagination for a seed; limits 50/100 |
| B-211 | Media route | B-209, B-210 | `GET /media/photos/{id}` with variant, `ETag`/`304`, `202` (enqueue on evicted), `404` for removed/excluded/revoked, `503` when NAS offline and missing; access-time touch | Tests: every status branch; no path parameter accepted; revoked source → `404` |
| B-212 | NAS status and home photo widget | B-202, B-210 | `GET /nas/status` (summary vs admin detail), `photo` widget totals/new_today/captured_today/unknown/baseline/index state with cached counters | Tests: counters match DB; admin-only fields absent for screens |
| B-213 | Admin scope controls | B-204 | Routes and CLI for `sources list/scan/revoke/restore/rebind-identity`, `scans list/get`, `photos get/exclude/include/retry/list`, `exclusions list/add/remove`; exclusions applied on subsequent scans; `audit_log` entries | Tests: exclude hides immediately and persists across a full rescan; revoke source hides all and `media` returns 404; rebind clears mismatch |
| B-214 | Change events (backend side) | B-204, B-207 | In-process event bus `internal/app/events` with topics `home/photos/nas/screen`, coalescing helper; publishers in indexer, media, prober; consumer stub until V0.3 hub | Tests: coalescing merges topics within window |

### 4.2 Web

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| W-201 | Photo pane slideshow | W-104 | Random collection round handling (new seed at end), `slideshow_interval_seconds` from `/home`, preloading next image, skip non-ready/`202`/`503`, fixed display when < 2 photos, Ken-Burns-free crossfade (cheap on TV) | Vitest: round/seed reducer; skip logic |
| W-202 | Collections browser | W-201 | `photos/:collection` screen: focusable strip with thumbs, Left/Right/Up/Down navigation, Enter opens photo, Back returns; captions with captured date (confidence shown as "estimated" when inferred), `unknown_captured_count` note on captured_today; empty states per collection with "go to slideshow" action; baseline-only empty state for recent | Runs on mock; Vitest for list navigation reducer |
| W-203 | Photo viewer | W-202 | `photo/:id` with prev/next via `neighbors`, pause slideshow, Back to previous route; handles `404` (drop and return) and `202` (spinner then skip) | Vitest: viewer reducer |
| W-204 | NAS and index status UI | W-104 | Status bar states online/offline/degraded/unknown with icon + text and last check time; index progress during baseline import; share free space when present | Visual check; snapshot test of status text |
| W-205 | `data.changed` handling (client side, against mock) | W-003 | Throttled refetch per topic; slideshow list refresh without restarting the current image | Vitest: throttle |

## 5. V0.3 — Realtime

### 5.1 Backend

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| B-301 | WS hub and sessions | B-102, B-104 | `internal/ws`: upgrade with auth + Origin, one session per screen with supersede (`4003`), bounded send queue (64) with coalescable drop, read loop with 64 KiB limit, envelope codec, close codes, `session.ready` with `pending_command` | Tests: two connections → first closed with `4003`; overflow closes `4004`; invalid JSON → `4005`; unauthenticated → `4001` |
| B-302 | Heartbeat monitor and state | B-301 | `heartbeat`/`state` handling → `screens.last_seen_at/current_route/applied_sequence`; `heartbeat.ack`; monitor marks offline after 45 s and closes stale sessions; `online` in `/screens` from session presence | Tests: offline after timeout with a fake clock; route persisted |
| B-303 | Command service | B-302 | `internal/screen`: validation per kind (route whitelist, photo eligibility), sequence in transaction, persistence, delivery through hub, `409 screen_offline` with failed command, `POST /screens/{id}/commands`, `GET /commands/{id}`, `GET /commands` | Tests: validation matrix; offline → failed + 409; sequence monotonic under concurrency |
| B-304 | Ack, expiry, unknown | B-303 | `command.ack` handling (terminal once, duplicates ignored), expirer (`expired` if never delivered, `unknown` after `expires_at + 2 s` when delivered without ack), startup `unknown/server_restart`, `result.observed` enrichment from heartbeats | Tests: each terminal path; ack after terminal ignored; restart marks unknown; observed state attached |
| B-305 | Change push | B-214, B-301 | Hub subscribes to the event bus; per-session coalescing (250 ms); `data.changed` versioning; revoke → `session.revoked` + `4002`; source revoke → `photos` topic | Tests: burst of 100 events → ≤ a few messages; revoke closes session |
| B-306 | Control CLI and scripts | B-303, B-109 | `atrium admin screen navigate/show/refresh --wait`, `commands get/list`; `scripts/*.sh` curl examples; `docs/ops/control.md` | CLI tests against `httptest`; scripts pass `shellcheck` and are exercised in `make e2e` |
| B-307 | Retention job | S-004 | Daily deletion per design §5 retention; startup run | Tests: old rows deleted, recent kept |

### 5.2 Web

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| W-301 | WS session integration | W-003, W-103 | Connect after `/home`, `session.ready` handling (apply `pending_command`), heartbeat every 15 s with route/applied_sequence/client_version, `state` on route change, close-code handling | Vitest with a fake socket: heartbeat cadence; ready → pending command applied |
| W-302 | Command executor | W-301, W-203 | Sequence guard, duplicate `command_id` ignore, `navigate/show/refresh` semantics (pause rules, refresh keeps state), ack after render (`onload` for show), error codes (`superseded`, `photo_unavailable`, `invalid_route`) | Vitest: all paths incl. stale sequence and unknown photo |
| W-303 | Change push handling | W-205, W-301 | Wire `data.changed` to refetch; `session.revoked` → clear and pair; `session.superseded` → connect screen message | Vitest |
| W-304 | Reconnect and snapshot sync | W-301 | On reconnect: `/home` first, then WS; keep current valid route and report it; cold start returns to dashboard; do not apply commands older than `applied_sequence` | Vitest with scripted reconnect |

## 6. H1 — Hardening and operations

| ID | Task | Depends | Deliverables | Acceptance / tests |
| --- | --- | --- | --- | --- |
| H-401 | Diagnostics endpoint and CLI | B-3xx | `internal/diag` aggregation per design §6.11; `GET /diagnostics`; `atrium admin diag`; `recent_errors` ring buffer (100) | Tests: shape; no paths/tokens present |
| H-402 | Backup and restore | S-004 | `internal/backup`: `VACUUM INTO`, retention, scheduler, `POST/GET /backups`, `atrium backup now/list`, `atrium restore --from` (pre-restore copy kept) | Tests: backup opens and matches row counts; retention keeps 7; restore refuses when server lock held |
| H-403 | Admin token rotation | B-101 | `POST /admin/token/rotate`, CLI `token rotate`, file rewrite | Tests: old token rejected after rotation |
| H-404 | Runbook and acceptance docs | all | `docs/ops/runbook.md` (install, pair, scan, control, diagnose, backup/restore, upgrade/rollback), `docs/ops/acceptance.md` (PRD §7 measurements with exact commands), `docs/ops/g0-record.md` template, `docs/ops/failure-matrix.md` (PRD 7.2 → command to verify) | Reviewed against PRD §7–§9 checklists |
| H-405 | Release build | S-009 | `make release` producing `atrium-darwin-arm64`, `atrium-darwin-amd64` with embedded UI and version; `CHANGELOG.md` | CI artifacts downloadable; `atrium version` shows tag |
| H-406 | E2E smoke | B-306, W-304 | `make e2e`: temp data dir, `tls.mode: off`, generated sample source (`allow_local`), Playwright: pair → dashboard → navigate → show → applied; runs in CI on macOS runner | Green in CI |
| H-407 | Performance harness | B-210 | `scripts/bench-api.sh` (1,000 requests to `/home`, `/photos`, `/nas/status`, P95 report), `scripts/bench-commands.sh` (100 commands with `--wait`, P95) | Produces a report file used by `acceptance.md` |
| H-408 | Fault injection helpers | B-201 | `atrium serve --dev-faults` (test builds only) or `FakeFS` scenarios documented for unplugging/permission/hang tests | Documented in `failure-matrix.md` |

## 7. G0 — Hardware verification (maintainer-executed)

Not code tasks; the code ships knobs and documents. Fill `docs/ops/g0-record.md`:

1. Mac mini: chip, RAM, macOS, free disk, sleep/FileVault/auto-login settings; chosen launch strategy (LaunchAgent vs LaunchDaemon).
2. NAS: protocol (SMB/NFS), read-only account, mount method and path, directory count, file count, size distribution, JPEG/PNG/HEIC ratio; `atrium admin sources list` output after first scan; scan duration vs 60 s.
3. TV: model, system, browser/WebView, viewport, full-screen behaviour, cookie persistence across power cycles, CA trust method, back-key code, standby/resume behaviour, 24 h run result.
4. Decisions on design §16 items; any config defaults changed.

## 8. V0.4 boundary (not in this plan)

`home-mcp` will wrap the admin API (`home.get_status → /diagnostics` subset, `screen.get_status → /screens`, `screen.navigate/show → /screens/{id}/commands`, `nas.get_status → /nas/status`, `nas.get_recent_photos → /photos?collection=recent`) using a dedicated admin token with a `screen.control` label. No code is added in V1 for this; the token `label` column and the audit log exist so V0.4 can attribute calls.

## 9. Verification matrix (PRD → tasks)

| PRD ID | Tasks | Automated evidence |
| --- | --- | --- |
| FR-01 | W-104, W-204 | Visual check 1080p/4K; status badges with text |
| FR-02 | B-105, W-104 | Clock tests (DST, midnight), unverified badge |
| FR-03 | W-102, W-103, W-106 | Reducer tests; error boundary test |
| FR-04 | W-101, W-202, W-203 | Key mapping and navigation tests; full-screen is G0 |
| FR-05 | B-106, W-104 | Weather stale/hidden tests |
| FR-06 | B-201, B-211 | Traversal/symlink tests; media route ID-only |
| FR-07 | B-202, B-212, W-204 | Health transition tests |
| FR-08 | B-203, B-204, B-205 | Stability, resume, failed-scan-no-removal tests |
| FR-09 | B-206, B-207, B-208 | Orientation and unsupported tests; HEIC sample is G0 |
| FR-10 | B-207, B-209, B-211 | Size/bytes limits, eviction, regeneration |
| FR-11 | B-210, W-201–W-203 | Collection query tests |
| FR-12 | B-202, W-204 | Share stats hidden when absent |
| FR-13 | B-104, B-301, B-302 | Supersede and online/registered tests |
| FR-14 | B-303, W-302 | Validation matrix; executor tests |
| FR-15 | B-304 | Terminal state tests |
| FR-16 | B-301, B-304, W-304 | Restart → unknown; reconnect snapshot |
| FR-17 | B-305, W-303 | Coalescing tests |
| FR-18 | B-306, H-406 | CLI/scripts exercised in e2e |
| PRD 8.2 | W-105, B-305 | 24 h expiry; revoke push |
| PRD 8.3 | B-108, H-402, H-404 | Backup tests; runbook |

## 10. Risks specific to this plan

| Risk | Mitigation in plan |
| --- | --- |
| `imagemeta` API friction or bugs | `MetadataReader` interface; B-206 allows switching to `go-exif/v3` without touching callers |
| `sips` orientation semantics | B-208 keeps a converter interface and re-reads orientation; G0 validates |
| TV cookie loss | Design §7.4 fallback is additive; W-003 keeps auth transport behind one module |
| Scan slower than 60 s on real library | Diagnostics expose duration; partition knob designed (design §13) |
| Parallel tracks drift on contract | `docs/api/*` is the contract; mock server (W-004) and backend tests both assert it |
| Large synthetic images slow tests | Keep test images small; mark heavy tests with `-short` skip |

## 11. Decision log and deviations

Record here any deviation from the technical design discovered during implementation, with date, task ID, reason and follow-up.

| Date | Task | Deviation | Reason | Follow-up |
| --- | --- | --- | --- | --- |
| 2026-09-06 | S-007 | Embed directory moved from `web/embed.go` to `internal/webui/embed.go`; `web/go.mod` stub added; `make web-sync` copies `web/dist` | `go build ./...` descended into `web/node_modules`, which contains Go sources (`flatted`) | Design §3 updated; CI release job unchanged (`release` depends on `web-sync`) |
| 2026-09-06 | B-103 | Screen token is minted at claim, not at approval | Avoids holding an unclaimed plaintext credential in memory/at rest for up to 5 min | Design §6.8 updated |
| 2026-09-06 | B-104 | `online` derived from `last_seen_at` until the WS hub exists | Hub is B-301/B-302 | Replace with session presence in V0.3 |
| 2026-09-06 | B-202 | `require_mount` checks the fstype of the filesystem containing the root instead of requiring `st_dev` to differ from the parent | On the real NAS the authorized root is a subdirectory of the share (`<mount>/Photos`), which the old rule rejected as `not_a_mount` | Design §6.1 updated; fix delivered in the V0.3 run |
| 2026-09-06 | S-004/B-203 | Write transactions use `BEGIN IMMEDIATE`; indexer commits in ≤ 200-row batches; short writers retry on `SQLITE_BUSY` | First scan of 17k files on the Mac mini produced `SQLITE_BUSY`/`BUSY_SNAPSHOT` job-claim failures | Design §4.2 updated; fix delivered in the V0.3 run |
| 2026-09-06 | B-102 | Cookie auth accepts `Sec-Fetch-Site: same-origin/none` or an allowed `Referer` when `Origin` is absent | Real browsers omit `Origin` on same-origin GET, so the first Playwright run against the real client got `403` on every request after pairing | Design §6.8 updated; auth matrix test extended; found by H-406 |
| 2026-09-06 | B-105/B-210 | `GET /home` emits `{schema_version, server_time, version, home{name,timezone}, widgets[{type,payload}]}`; `GET /photos/{id}` emits `{item, neighbors{collection, previous_id, next_id}}` | Backend and web tracks had chosen different envelopes for shapes the design left implicit; the web client (and PRD WidgetSnapshot) form was kept | OpenAPI updated; found by H-406 |
| 2026-09-06 | B-108 | LaunchAgent deployment requires the SMB share to be mounted in the GUI login session and a one-time TCC "Network Volumes" approval | On the real Mac mini a mount made from SSH left the service in `degraded / stuck_io`; the TCC prompt blocks reads until answered | Runbook §2 updated; `docs/ops/g0-record.md` open issues 1–2 |
| 2026-09-06 | B-207 | Follow-up: `stuck` preview failures while the source is `degraded` should be deferrals, not attempts; first-import preview priority should interleave with metadata | Observed during G0 (see g0-record open issues 3–4) | Not yet implemented |
| 2026-09-06 | B-307 | Follow-up: periodic maintenance writers (pairing expiry, retention) should use `store.RetryBusy` like the job claim | On the Mac mini the pairing-expiry tick logged `SQLITE_BUSY` every few minutes while previews were being written | Not yet implemented |
