# Backend (Go) track — progress

Scope so far: M0 (S-001..S-009), V0.1 (B-101..B-109), V0.2 (B-201..B-214),
V0.3 (B-301..B-307) and H1 (H-401..H-405, H-407, H-408; H-406 belongs to the
integrator).
Authority: `docs/tech/2026-09/atrium-home-hub/tech-design.md`.
Last updated after completing V0.3 and H1.

## M0 — Scaffold

| Task | Status | Notes |
| --- | --- | --- |
| S-001 Repository skeleton | done | `.gitignore`, `.editorconfig`, `LICENSE` (MIT), `CONTRIBUTING.md`, `SECURITY.md`, `Makefile` (`build`, `web`, `test`, `lint`, `check`, `run-dev`, `release`, `e2e`), `.golangci.yml`. `README.md` belongs to the integrator. |
| S-002 Go module and entrypoint | done | `cmd/atrium/main.go`, `internal/cli` (cobra: `version`, `init`, `serve`, `tls`, `token`, `admin`), `internal/app` with signal handling and a 10 s graceful shutdown. |
| S-003 Config package | done | The existing package already covered design §10; verified every key, default and validation rule and left it unchanged. `config.example.yaml` matches §10 verbatim. |
| S-004 Store and migrations | done | `0001_init.sql` verified line by line against design §5. Repositories added for `data_sources`, `photos`, `photo_exclusions`, `preview_files`, `jobs`, `scan_runs`, `screens`, `pairings`, `screen_commands`, `admin_tokens`, `widget_cache`, `audit_log`; `store/testutil` temp-DB helper; tests for migrate/idempotence/WAL/foreign keys and every repository. |
| S-005 Domain package | done | Enums and error codes verified complete against design §8; added a package comment and `internal/domain/domain_test.go` asserting the code table, status mapping and enum validity. |
| S-006 HTTP server base | done | Go 1.22 `net/http` patterns, middleware (request ID, logger, recovery, security headers, access log with path redaction), `/health/live`, `/health/ready`, TLS `auto\|file\|off` through `internal/auth/tlsgen`. |
| S-007 Web UI embedding | done | `web/embed.go` (`package web`, `//go:embed all:dist`, exported `Dist`), `web/dist/.gitkeep`, `internal/webui` SPA handler with immutable hashed assets, no-cache `index.html`, SPA fallback and a friendly "UI not built" page. |
| S-008 Logging | done | slog JSON to `logs/atrium.log` via lumberjack (20 MB rotation, `retain_days`, `max_total_mb`), console handler for `--dev`, redaction of `token`, `authorization`, `cookie`, `root_path`, `rel_path`, `mount_from` at info and above. |
| S-009 CI | done | `.github/workflows/ci.yml`: Go (gofmt check, vet, golangci-lint, `test -race`, build), web (ci, lint, test, build, bundle artifact), shellcheck, release job producing `atrium-darwin-arm64` and `-amd64` with the embedded UI. |

## V0.1 — Screen (backend)

| Task | Status | Notes |
| --- | --- | --- |
| B-101 Token and credential primitives | done | `atr_scr_`/`atr_adm_` with 32 random bytes as 43 base64url characters, sha256-only storage, admin token file at mode 0600, `AdminService` with `Issue`/`EnsureInitial`/`Reset`/`Rotate`, `atrium token reset` for offline recovery. |
| B-102 Auth middleware and scopes | done | Bearer and cookie extraction, scope resolution, Origin policy, cookie-only-on-GET, admin Bearer-only, auth-failure limiter (10/min per IP → 429 for 60 s), `GET /api/v1/screens/me`. Tested as a credential × route-scope × origin matrix. |
| B-103 Pairing flow | done | `POST /pair/start` (5/min per IP, 20/min global), `GET /pair/{id}` (60/min per pairing), `POST /pair/{id}/claim` with the full cookie attribute set, admin `GET /pairings`, `POST /pairings/{id}/approve`, `DELETE /pairings/{id}`; 5-minute TTL, unique six-digit codes, second claim 410, one-day retention in the background loop. |
| B-104 Screens registry | done | `GET/DELETE /screens`, `GET /screens/{id}`; revocation blocks authentication immediately and deletes the pairing history; `registered` from status, `online` from `last_seen_at` within `screens.offline_after`. |
| B-105 Home snapshot v1 | done | `GET /api/v1/home` with the `clock` widget (timezone, server time, UTC offset, next transition), the zeroed `photo` baseline, and the `nas` widget from the reconciled sources with health `unknown`. `internal/clock` verified for `HomeDay`, `DayBounds`, `OffsetAt`, `NextTransition` including both DST directions. |
| B-106 Optional widgets | done | Open-Meteo behind a `Provider` interface, cached in `widget_cache`, refresh on `refresh_interval`, `stale` past `stale_after`, a provider failure keeps the last payload; the static notice widget. Tested with an `httptest` stub. |
| B-107 `init` / `serve` / `tls` | done | `init` writes the config when missing, creates the 0700 data tree, generates TLS material and the admin token; `serve` runs the §4.2 startup sequence (reconcile sources, mark accepted commands unknown, requeue running jobs, detect a timezone change) and the §4.3 shutdown; `tls renew`, `tls export-ca`. |
| B-108 launchd packaging | done | `deploy/launchd/com.atrium.core.plist.tmpl`, `install.sh`, `uninstall.sh` (both shellcheck clean), `deploy/README.md` covering LaunchAgent vs LaunchDaemon, FileVault and auto-login, NAS mount options, logs, upgrade and rollback. |
| B-109 Admin CLI base | done | `atrium admin --url/--token/--json` with token resolution (`--token`, `ATRIUM_ADMIN_TOKEN`, data-dir file) and automatic trust of `tls/ca.crt`; `diag`, `pair list\|approve\|reject` (approve accepts the six-digit code), `screens list\|get\|revoke`. Tested against `httptest`. |

Also delivered: `docs/api/openapi.yaml` covering every route implemented so far,
and `docs/api/ws-protocol.md` carrying design §9 verbatim as the cross-track
contract for V0.3.

## V0.2 — NAS (backend)

| Task | Status | Notes |
| --- | --- | --- |
| B-201 Source FS abstraction | done | `internal/source`: the read-only `FS` interface (`Stat`, `ReadDir`, `Open`, `Statfs`), `OSFS` with path hardening (`..`, absolute paths and NUL rejected; `Lstat` first; `O_NOFOLLOW`; symlinks counted as `skipped_symlink`), a deadline wrapper that runs each syscall in a helper goroutine and returns `ErrStuck` on timeout, an inflight semaphore that trips `degraded` with a reserved probe slot, and `FakeFS` with hang / EACCES / disappear / slow injection. |
| B-202 Mount identity and health prober | done | darwin `syscall.Statfs_t` (`Fstypename`, `Mntfromname`) plus an `st_dev` mount-point check, with a portable non-darwin fallback; identity `{fstype, mount_from_hash, marker}` bound on the first success; `require_mount` / `allow_local` / `marker_file`; mismatch → `unknown/identity_mismatch` with scans and removals blocked until `rebind-identity`; health state machine with `last_check_at` / `last_success_at`; share capacity stored only for network filesystems; transitions publish `nas` and `home`. |
| B-203 Indexer walk and stability | done | `internal/indexer`: one serial, non-overlapping scan loop per source with manual merge and immediate wake, a breadth-first walk applying exclusions and the extension allowlist, a stability set (`stability_checks` / `stability_interval` / `stability_max_rounds`) that carries unresolved candidates across scans, `scan_runs` counters flushed every 200 files, generation handling, `unsupported` accounting and `index.state` / `progress` on `GET /home`. |
| B-204 Removal and baseline rules | done | `missing_generations` advanced only by a completed scan with a confirmed identity; `removed` after two; failed and aborted runs never advance the generation; revived paths get a new `first_seen_at` and lose `is_baseline`; `is_baseline` set only before `baseline_completed_at`; source revoke/restore hides and unhides through the eligibility join without touching photo rows; `recompute_day` on a timezone change. |
| B-205 Job queue | done | `internal/jobs`: `Queue` (dedup that also covers photo-less kinds, priorities), `Pool` (claim-with-lock, per-job deadline, backoff `1m,5m,30m,6h`, five attempts, `Permanent` and `Defer` outcomes, panic recovery with a restart delay, stale-lock recovery at startup, running jobs re-queued on shutdown). |
| B-206 Metadata extraction | done | `MetadataReader` interface with an `imagemeta` implementation; `captured_at` exact / inferred / unknown with the `[1970, now+1d]` plausibility bound; `captured_day` via `clock.HomeDay`; the file mtime is never used as a capture time; fingerprint `sha256(size, mtime, head 64 KiB, tail 64 KiB)`; `extract_meta` chains `build_preview`; `internal/media/testutil` writes JPEGs with a real APP1 Exif segment (`DateTimeOriginal`, `OffsetTimeOriginal`, `Orientation`), PNGs, a header-only oversized PNG and a corrupt JPEG, all in code. |
| B-207 Preview generation | done | Guards (health, `min_free_bytes`, cache budget) express themselves as deferrals so an offline share never burns a photo's retry budget; `image.DecodeConfig` checks `max_pixels` and `max_source_bytes` before any pixels are allocated; `imaging.Decode` with `AutoOrientation`; preview at `preview_max_edge` and quality 85 → 75 → 65 until `preview_max_bytes`; thumb at `thumb_max_edge` quality 80; atomic temp+rename into `cache/<variant>/<id[0:2]>/<id[2:4]>/<id>.jpg`; `preview_files` rows; the stable codes `decode_failed`, `unsupported`, `too_large`, `timeout`, `io_error`, `stuck`; retry with backoff, `unsupported` promotion and `photos` change events. |
| B-208 HEIC via sips | done | `media.Converter` interface with a `sips` implementation (`-s format jpeg --resampleHeightWidthMax`) under the job deadline with `WaitDelay` kill, an `off` implementation that lands HEIC in `unsupported`, orientation taken from the converted JPEG, and `sips -g creation` as the capture-time fallback. A fake converter covers the pipeline; the real-binary test is skipped unless `ATRIUM_TEST_SIPS=1` and `ATRIUM_TEST_HEIC` point at a sample (none committed). |
| B-209 Cache budget janitor | done | LRU eviction by `last_access_at` down to 90 % of the budget, `preview_status = evicted` with the index kept, low-disk pause reported as `cache.paused_reason = low_disk`, access-time writes throttled to once per file per hour, and both a 10-minute loop and a post-write run. Disk statistics sit behind a `DiskStats` interface so the pause is testable. |
| B-210 Photo collections API | done | `GET /photos?collection=recent\|captured_today\|random\|all` with opaque base64 keyset cursors, a seeded `math/rand/v2` PCG shuffle for `random` (cursor `seed:offset`, `next_cursor` null at round end), `meta.unknown_captured_count`, `meta.baseline_only` and `meta.day`; `GET /photos/{id}` with `?neighbors=<collection>`; the DTO of design §8; eligibility is `status = 'ready'` on an active source. |
| B-211 Media route | done | `GET /media/photos/{id}?variant=preview\|thumb`: `200 image/jpeg` with `ETag` = fingerprint and `304`; `202` with `Retry-After: 5` while pending, processing or evicted (rebuild enqueued, deduplicated); `404` for removed, excluded, revoked and unknown alike; `503 preview_unavailable` when the source is offline and nothing is cached; throttled access-time touch; the route accepts an ID and never a path. |
| B-212 NAS status and photo widget | done | `GET /nas/status` gives screens health and timestamps only, and admins `health_detail`, `stuck_ops` and `identity_confirmed`; the `photo` widget reports totals, `new_today`, `captured_today`, `unknown_captured`, the baseline state and live index state, recomputed at most every two seconds. |
| B-213 Admin scope controls | done | Routes and CLI for `sources list\|scan [--full]\|revoke\|restore\|rebind-identity`, `scans list\|get`, `photos get\|list\|exclude\|include\|retry`, `exclusions list\|add\|remove`. `GET /photos/{id}/admin` is the only route that reveals `rel_path`. Exclusions are stored as rules, so they survive a full rescan, and every admin mutation writes an `audit_log` entry. |
| B-214 Change events | done | `internal/app/events`: a bus with the `home` / `photos` / `nas` / `screen` topics, a non-blocking fan-out that merges rather than drops when a subscriber is behind, and a 250 ms coalescing helper. The indexer, media pipeline, prober and admin routes publish; the V0.2 consumer logs at debug until the V0.3 hub subscribes (`Runtime.Bus()` is the seam). |

Also delivered: `docs/api/openapi.yaml` extended with every V0.2 route and schema,
and `internal/httpapi/openapi_test.go`, which asserts that every documented path
is actually registered rather than falling through to the SPA handler.


## V0.3 — Realtime (backend)

| Task | Status | Notes |
| --- | --- | --- |
| B-301 WS hub and sessions | done | `internal/ws` on `github.com/coder/websocket`: authenticated upgrade, one session per screen with supersede (`session.superseded` then 4003), a 64-message send queue where only `data.changed` is droppable and any other overflow closes 4004, a read loop that bounds each frame at 64 KiB and closes 4005 itself, the envelope codec, per-session write and read goroutines, and `session.ready` with `pending_command`. Graceful close 1001 on shutdown. Tests cover supersede, overflow, malformed and oversized frames, the unauthenticated handshake, cookie + Origin, and Bearer without Origin. |
| B-302 Heartbeat monitor and state | done | `heartbeat` and `state` update `screens.last_seen_at / current_route / applied_sequence / client_version / last_ip` in one statement (`Screens.RecordHeartbeat`); `heartbeat.ack` carries `server_time`. The monitor ticks every 5 s and closes sessions past `offline_after`. `online` in `GET /screens` and `/screens/{id}` is now session presence through the `httpapi.Sessions` seam; `registered` is unchanged. Fake-clock tests. |
| B-303 Command service | done | `internal/screen`: per-kind validation (`navigate` route whitelist plus an optional collection whitelist; `show` requires an eligible photo with `preview_status = ready` through `Photos.GetEligible`; `refresh` is normalised to an empty payload) → `400 invalid_command`; unknown or revoked screen → `404`; offline screen persists `failed/screen_offline` and answers `409` with the command body; the sequence is allocated inside the insert transaction; TTL from `screens.command_ttl`; delivery through the hub with `failed/delivery_failed` on a closed or full queue. Routes `POST /screens/{id}/commands`, `GET /commands/{id}`, `GET /commands`; `audit_log` on every issue. Concurrency test asserts 30 parallel issues produce 30 distinct sequences. |
| B-304 Ack, expiry, unknown | done | `command.ack` sets a terminal status exactly once and logs a later ack at debug; the expirer ticks every second and marks `expired` when never delivered or `unknown/ack_timeout` past `expires_at + 2 s`; startup marks every open command `unknown/server_restart`; the next heartbeat attaches `result.observed = {applied_sequence, route}` without ever flipping the status. A test covers every terminal path. |
| B-305 Change push | done | Each session subscribes to `Runtime.Bus()` and runs its own `events.Coalesce` at 250 ms, so a burst of 100 publications arrives as one `data.changed` with the merged topics and the bus version. `DELETE /screens/{id}` sends `session.revoked` and closes 4002. Tests cover the burst and the revoke. |
| B-306 Control CLI and scripts | done | `atrium admin screen navigate\|show\|refresh [--wait]` and `commands get\|list`; `--wait` polls every 250 ms up to 15 s and exits non-zero unless `applied`, printing `result.observed` for an `unknown`. `scripts/{_common,screens-list,screen-navigate,screen-show,screen-refresh,command-get}.sh` are plain `curl` reading `ATRIUM_URL`, `ATRIUM_ADMIN_TOKEN`, `ATRIUM_CA` and `ATRIUM_DATA_DIR`; `docs/ops/control.md` documents the whole path. |
| B-307 Retention job | done | `store.DB.RunRetention` deletes `screen_commands`, `scan_runs`, `audit_log` and terminal `jobs` older than 7 days and `pairings` older than 1 day. It runs once at startup and daily after that. A test proves the old rows go and the recent ones, the queued job and every system-state row stay. |

## H1 — Hardening (backend)

| Task | Status | Notes |
| --- | --- | --- |
| H-401 Diagnostics | done | `internal/diag` builds the whole §6.11 document — `db`, `sources` (with `last_scan` and `stuck_ops`), `photos`, `jobs`, `cache`, `screens` (presence and `open_commands`), `commands.last_24h`, `widgets`, `pairings`, `recent_errors`. The ring of 100 is an `slog.Handler` wired into the logger, so media, indexer, prober and ws feed it without importing `diag`. `GET /diagnostics` and `atrium admin diag` (table and `--json`) render the same type. A test asserts no token, hash or path appears. |
| H-402 Backup and restore | done | `internal/backup`: `VACUUM INTO backup.dir/atrium-YYYYMMDD-HHMMSS.db` plus a config copy, `backup.keep` retention, a scheduler that ticks every minute against `backup.time` in home time, `POST`/`GET /api/v1/backups`, `atrium backup now\|list`, `atrium restore --from`. The runtime holds a `flock` on `data_dir/atrium.lock`; restore refuses while it is held, verifies the snapshot's integrity and schema before touching anything, and moves the live database aside as `atrium.db.pre-restore-<ts>` with its `-wal`/`-shm`. Tests cover row counts, retention, the lock refusal and a bogus source file. |
| H-403 Admin token rotation | done | `POST /api/v1/admin/token/rotate` issues the replacement, writes the response, flushes it and only then revokes the caller's token; `atrium admin token rotate` rewrites `admin.token`. A test proves the old token is rejected afterwards and the new one works. |
| H-404 Runbook and acceptance docs | done | `docs/ops/runbook.md` (install, a user LaunchAgent that mounts with `mount_smbfs -o rdonly` reading the password from the Keychain via `security find-internet-password -w`, pairing, scanning, control, diagnosis, backup/restore, token rotation, upgrade/rollback, log locations), `docs/ops/acceptance.md` (every PRD §7.1 metric with its command), `docs/ops/failure-matrix.md` (every PRD §7.2 row with a real injection step) and `docs/ops/g0-record.md` (blank template). No IPs, hostnames or real paths appear. |
| H-405 Release build | done | `make release` already depended on `web-sync`; verified it produces `bin/atrium-darwin-arm64` and `-amd64` with the version, commit and build-date ldflags. `CHANGELOG.md` added with a `0.1.0 (unreleased)` section covering V0.1 to V0.3 and H1. |
| H-406 E2E smoke | skipped | Owned by the integrator, per the brief. |
| H-407 Performance harness | done | `scripts/bench-api.sh` (P50/P95/error rate for `/home`, `/photos?collection=random` and `/nas/status`, written to a report file) and `scripts/bench-commands.sh` (navigate with `--wait`, P50/P95/max of accepted → applied). Both are shellcheck clean and documented in `acceptance.md`. |
| H-408 Fault injection | done (documented) | No fault switch was added to the binary. `docs/ops/failure-matrix.md` injects every scenario from outside the process (unmount, `chmod`, `kill -STOP`, a disk ballast file), so what is exercised is the shipping path; the same scenarios stay covered in the tests by `source.FakeFS` and fake disk statistics. The section "On fault injection built into the binary" records the rule for any future switch: behind a build tag or an explicitly documented config key, off by default. |

Also delivered: `docs/api/openapi.yaml` extended with the WebSocket upgrade, the
command routes, the backup routes and the token rotation, plus `Command`,
`CommandResult`, `Backup` and the full `Diagnostics` schema, all kept honest by
`internal/httpapi/openapi_test.go`; and a "Server behaviour notes" section in
`docs/api/ws-protocol.md` recording where the implementation is more specific
than §9.

## Coordinator-requested fixes in this run

| Item | Status | Notes |
| --- | --- | --- |
| `require_mount` rejected a subdirectory of an SMB share | fixed | `CheckIdentity` no longer requires the root to *be* the mount point. `require_mount` now means "the filesystem carrying the root is a network filesystem" (`smbfs`, `nfs`, `afpfs`, `webdav`) unless `allow_local`. The protection is unchanged in substance: a share that failed to mount leaves either nothing (ENOENT → `offline/root_missing`) or a local `apfs` directory (→ `unknown/not_a_mount`). `VolumeStats.IsMountPoint` is kept but is now informational. Identity binding (fstype + `mount_from` hash + marker) is untouched. Two tests added: a subdirectory of a fake network mount binds, a local apfs directory is still rejected. |
| `SQLITE_BUSY` / `SQLITE_BUSY_SNAPSHOT` during a large first scan | fixed | Three changes. (1) The DSN now carries `_txlock=immediate`, so every transaction takes the write lock at `BEGIN`; a deferred read-then-write upgrade was what produced `(517) SQLITE_BUSY_SNAPSHOT`, which no busy timeout can recover. (2) `store.RetryBusy` retries short writers with jittered backoff for about a second, and `DB.InWriteTx` / `DB.BeginWrite` use it; `Jobs.Claim` and `Commands.Issue` go through it, and the job pool logs a remaining busy result at debug instead of warning every second. (3) The indexer now commits in batches of 200 files (`indexer.BatchSize`, matching `ProgressFlushEvery`) through `Photos/Jobs/Previews.WithTx`, instead of one auto-commit transaction per statement; the scan-counter flush happens between batches so it never contends with a lock the batch holds. `busy_timeout=5000` is unchanged. A test runs batched index inserts concurrently with two job-claiming workers and asserts no claim error. |


## Deviations from design

| Date | Task | Deviation | Reason | Follow-up |
| --- | --- | --- | --- | --- |
| 2026-09-06 | B-103 | The screen token is generated at **claim** time, not at approval. Approval stores an unusable placeholder hash. | The design says approval "creates the screens row and a token", but the plaintext must reach the TV at claim time. Holding it in memory between the two calls would lose it across a restart and would keep a live credential in process memory for up to five minutes. Generating it inside the claim transaction gives the same contract with no plaintext at rest and no window where an unclaimed credential is valid. | None. The API contract in §6.8 is unchanged. |
| 2026-09-06 | B-105 | `photo.index.state` is hard-coded to `idle` and progress to zeros. | The indexer is V0.2 (B-203). | Wire to real scan state in B-203/B-212. |
| 2026-09-06 | B-109 | `atrium admin diag` renders the V0.1 subset of the §6.11 document (no `photos`, `jobs`, `cache`, `commands` or `recent_errors` sections). | Those subsystems do not exist yet. | Complete in H-401. |
| 2026-09-06 | B-104 | `online` is derived from `last_seen_at`, which the HTTP layer touches on each authenticated screen request. | The WebSocket hub that owns session presence is V0.3 (B-301/B-302). | Switch to session presence in B-302. |
| 2026-09-06 | B-105 | Superseded: `photo.index` is now wired to the live scheduler. | The indexer exists. | None. |
| 2026-09-06 | B-207 | `build_preview` also writes `photos.width/height` from the decoded image when they differ from the EXIF value. | Design §6.3 takes dimensions from `MetadataReader`, but the EXIF pixel tags are optional and absent from every PNG and most non-camera JPEGs, so the API would report `0x0` for a large part of a real library. The decoded size is measured after the orientation transform, so it describes what a client displays. | None; the DTO contract in §8 is unchanged. |
| 2026-09-06 | B-207 | Preconditions that are not met (source offline, low disk, cache over budget) re-queue the job through a `Defer` outcome that does **not** consume an attempt. | Design §6.3 says "leave the job queued with `next_run_at = +2 min`", which the plain retry path would conflate with a decode failure. Counting an offline share against `preview_attempts` would permanently fail healthy photos after five outages. | None. |
| 2026-09-06 | B-210 | The `random` cursor is `base64("seed:offset")` rather than the literal `seed:offset`. | Design §6.4 gives the cursor contents as `seed:offset` and §8 says cursors are base64. Encoding keeps every collection's cursor opaque and uniform, so a client cannot start depending on one collection's ordering columns. The contents are exactly `seed:offset`. | None. |
| 2026-09-06 | B-210 | `?neighbors=random` returns null neighbours. | A random round is a client-held shuffle position, not a server-side order, so there is no stable previous/next to report. | Documented in `openapi.yaml`. |
| 2026-09-06 | B-203 | The first scan of a source starts `InitialScanDelay` (3 s) after startup rather than one `scan.interval` later. | A 60 s wait before the baseline import begins is a poor first-run experience, and the delay is long enough for the health prober to bind the mount identity first (an unidentified root refuses to scan, which would otherwise cost a whole interval). | None. |
| 2026-09-06 | B-209 | `RunJanitor` is also invoked from `build_preview`'s budget guard, not only after a successful write. | Design §6.3 pauses the job when the cache is over budget; trying to make room first turns a hard stop into a self-healing one, and the janitor collapses concurrent invocations. | None. |
| 2026-09-06 | B-104 | Superseded: `online` is now session presence. | The hub exists. | None. |
| 2026-09-06 | B-109 | Superseded: `atrium admin diag` renders the whole §6.11 document. | The subsystems exist. | None. |
| 2026-09-06 | B-301 | A WebSocket handshake with **no** `Origin` is accepted when it carries `Authorization: Bearer`. Design §9 says every handshake must carry an allowed `Origin`. | A browser always sends an `Origin` and cannot set an `Authorization` header on a WebSocket, so the exception admits only non-browser clients (the CLI, a probe, the V0.4 MCP bridge) and no cross-site page. §6.8's general rule already says requests without `Origin` are honoured with Bearer auth; this makes the WS handshake consistent with it. An `Origin` that *is* present must still be allowed. | Recorded in `docs/api/ws-protocol.md` under "Server behaviour notes". |
| 2026-09-06 | B-301 | A refused handshake is an HTTP `401`, not close code `4001`. | Authentication happens before the upgrade, so no socket exists to close. `4001` stays reserved for a session that loses authorization after the upgrade. | Documented in `ws-protocol.md`. |
| 2026-09-06 | B-301 | `session.ready.pending_command` also redelivers a command that was delivered but never acknowledged, not only an undelivered one. | That is the case a reconnect actually produces: the socket died between delivery and the ack. Redelivery is safe under the rules already in §9 — the client ignores a duplicate `command_id` and applies only a greater `sequence`. | Documented in `ws-protocol.md`. |
| 2026-09-06 | B-302 | A session closed for heartbeat timeout uses close code `1001`. | §9 enumerates no code for it. `1001` is the "reconnect with backoff" signal the client already handles, which is exactly the desired behaviour. A cleanly disconnected client is reported offline immediately; the 45 s rule covers a session that stays open but stops speaking. | Documented in `ws-protocol.md`. |
| 2026-09-06 | H-403 | `POST /admin/token/rotate` issues the replacement, writes and flushes the response, and only then revokes the caller's token, rather than using the existing `AdminService.Rotate`, which revokes first. | Design §6.8 requires the old token to die "after the response is written". Revoking inside the same call would lose the operator's access if the response were dropped. | None; `AdminService.Rotate` is retained for offline callers. |
| 2026-09-06 | B-202 | `require_mount` now means "the root's filesystem is a network filesystem", not "the root is itself a mount point". | Found on the real Mac mini: the authorized photo directory is normally a subdirectory of the share (`<mount>/Photos`), which shares its parent's device number, so the old rule rejected every realistic deployment with `not_a_mount`. The protection that matters survives: a failed mount leaves ENOENT (`offline/root_missing`) or a local `apfs` directory (`unknown/not_a_mount`). | None; `VolumeStats.IsMountPoint` is kept as informational. |
| 2026-09-06 | S-004 | The SQLite DSN carries `_txlock=immediate`, writers retry `SQLITE_BUSY` with jittered backoff, and the indexer commits in batches of 200 files through tx-bound repositories. | Found on the real Mac mini during a 17,000-file first scan: the job pool logged `database is locked (5)` and `(517) SQLITE_BUSY_SNAPSHOT` every second. `(517)` comes from a deferred transaction that reads then writes — `Jobs.Claim` — and no busy timeout can recover it; taking the write lock at `BEGIN` removes the failure mode entirely. Batching cuts the number of write-lock acquisitions during a first scan by two orders of magnitude. | None; `busy_timeout=5000` is unchanged and no schema changed. |

## Verification performed after V0.1

- `go build ./... && go vet ./... && go test ./...` — all packages pass.
- `go test -race ./...` — clean. It found one real bug: `Runtime.Addr` read
  `listener` while `Start` was writing it. The field is now guarded by a mutex.
- `golangci-lint run` — 0 issues.
- `shellcheck deploy/launchd/*.sh` — clean.
- `atrium init` in a temporary directory, then `serve`, `curl /health/live`,
  `admin diag`, a full pairing round trip (start → `admin pair approve <code>`
  → claim), `GET /api/v1/home` with the issued token, and a SIGTERM shutdown.
- The log file was checked for `atr_scr_`, `atr_adm_`, `Authorization` and
  `atrium_screen`: no match.

## Verification performed after V0.2

- `go build ./... && go vet ./... && go test ./...` — all packages pass.
- `go test -race ./internal/...` — clean.
- `golangci-lint run` — 0 issues. `gofmt -l` — empty.
- End-to-end run against a generated library of 22 files (JPEG with an exact
  EXIF offset, JPEG with a bare `DateTimeOriginal`, PNG without metadata, a
  JPEG with `Orientation = 6`, a corrupt JPEG, a header-only 60000×60000 PNG,
  a `private/` subtree and a `.txt` decoy) in a temporary directory with
  `identity.allow_local: true` and a 10 s scan interval:
  - `admin sources list` → `active / online / bound/apfs`, 19 ready, 1 pending
    (the oversized PNG, `preview_error = too_large`), 1 unsupported (the
    corrupt JPEG); 21 files seen, the `.txt` ignored.
  - `GET /home` → `captured_today: 10`, `unknown_captured: 5`,
    `baseline: done`, `index.state: idle`; `nas` shows `online` with no share
    capacity, because APFS is not a network filesystem (FR-12).
  - `GET /photos?collection=captured_today` → items ordered by capture time
    with `meta.unknown_captured_count: 5` and `meta.day: 2026-09-06`.
  - `GET /photos?collection=recent` → empty with `meta.baseline_only: true`.
  - `?collection=random` over three pages → 19 distinct IDs, no repeat,
    `next_cursor: null` on the last page; replaying a cursor is stable.
  - `GET /media/photos/{id}?variant=preview` → `200 image/jpeg`, 741 728 bytes
    (under the 1 MiB target), `ETag` equal to the fingerprint, `304` on
    `If-None-Match`, and the bytes contain no `Exif\x00\x00` segment.
  - Adding 3 files → all three `ready` in `recent` within 25 s (two scan
    intervals plus preview time), `new_today: 3`.
  - Deleting 1 file → still `ready` with `missing gens 1` and media `200`
    after one completed scan; `removed` with media `404` after the second.
  - `admin photos exclude` → media `404` and a persisted `path` rule;
    `include` → media `200` again.
  - `admin exclusions add --prefix private/` then `sources scan --full` → the
    rule survives, 22 of 23 files seen.
  - `admin sources revoke` → media `404` and an empty `nas.sources`;
    `restore` → `200` and the counts return.
  - Moving the root away → health `offline` / `root_missing` within one probe,
    the scan recorded `failed` with no removals, `GET /home` still answered in
    39 ms, and cached previews kept serving. Moving it back → `online`.
  - The log file was checked for `atr_scr_`, `atr_adm_`, `Authorization`,
    `atrium_screen`, `rel_path`, `root_path` and the source root at `INFO` and
    above: no match.
  - The server was stopped with SIGTERM and logged `stopped`.
- Not exercised live: HEIC through `sips` (no sample; covered by a fake
  converter in tests and gated behind `ATRIUM_TEST_SIPS=1`), a real network
  mount, and an identity mismatch against a real second share (covered by
  `FakeFS` tests and by the admin `rebind-identity` route).

## Verification performed after V0.3 and H1

- `go build ./... && go vet ./... && go test -race ./...` — every package
  passes. `golangci-lint run` — 0 issues. `gofmt -l cmd internal` — empty.
- `shellcheck scripts/*.sh deploy/launchd/*.sh` — clean. (`scripts/_common.sh`
  is sourced with a `# shellcheck source=` directive so the callers resolve it.)
- `make release` — `bin/atrium-darwin-arm64` (22.9 MB) and
  `bin/atrium-darwin-amd64` (24.1 MB) with the embedded UI and the version,
  commit and build-date ldflags.
- End-to-end run in a temporary directory with `tls.mode: off`, a local source
  of 10 generated JPEGs (`allow_local: true`, 10 s scan interval) and a small
  Go WebSocket client standing in for the TV (cookie plus `Origin`, heartbeats
  every 2 s, acks `applied` after a 50 ms render delay):
  - `admin sources list` → `active / online / bound/apfs`, 10 ready after two
    scan intervals.
  - Pairing entirely through `curl`: `pair/start` → `pairings/{id}/approve` →
    `pair/{id}/claim`; the token comes back only in the claim response.
  - The fake TV connected and received
    `session.ready {"screen":{"id":"living_room_tv",…},"home_version":25}`;
    `admin screens list` → `online yes`.
  - `screen navigate --route photos --collection recent --wait` → `applied`,
    final route `photos/recent`; `screen show --photo … --wait` → `applied`,
    final route `photo#01M1TQF8…`; `screen refresh --wait` → `applied` with the
    route preserved. Each exited 0.
  - `scripts/bench-commands.sh 20` → `applied=20 not_applied=0 P50=273ms
    P95=277ms max=279ms`, well inside the 1 s target; the figure is dominated by
    the 250 ms `--wait` poll granularity, not by the server.
  - `scripts/bench-api.sh 60` → `/home` P95 0.5 ms, `/photos?random` P95 0.8 ms,
    `/nas/status` P95 0.6 ms, 0 errors (loopback, 10 photos — a floor, not the
    G0 number).
  - `scripts/screens-list.sh` and `scripts/screen-navigate.sh` returned the same
    JSON as the CLI.
  - Stopping the client → `online no` at the next check and
    `screen navigate` → `409`, printing
    `status failed (screen_offline)` and exiting 1 with nothing queued.
  - Reconnecting with acknowledgements suppressed → the command was delivered
    and became `unknown (ack_timeout)` 13 s later, and the next heartbeat
    attached `observed screen reports sequence 0 on route dashboard`.
  - SIGTERM with a command in flight → the client was closed with `1001`
    ("server shutting down"), and after the restart the command read
    `unknown (server_restart)`.
  - A second connection for the same screen → the first received
    `session.superseded` and close `4003`; `screens revoke` → the live session
    received `session.revoked {"reason":"screen_revoked"}` and close `4002`.
  - `admin diag` printed every section (db, photos, jobs, cache, commands,
    pairings, sources with `last_scan`, screens with route and applied
    sequence, recent errors, widgets).
  - `backup now` → `atrium-20260906-144928.db` with a config copy;
    `backup list` and `GET /api/v1/backups` agreed; `restore --from …` while the
    server was up → `a server is running on data; stop it before restoring`.
  - `admin token rotate` → the new token wrote `admin.token`; the old token then
    got `401` and the new one `200`.
  - `GET /media/photos/{id}?variant=preview` with `Range: bytes=0-0` →
    `206 Partial Content`, `Content-Range: bytes 0-0/36113`, `Content-Length: 1`.
  - `GET /diagnostics` searched for `atr_`, `Authorization`, `token_hash`,
    `rel_path`, `root_path`, `atrium_screen` and the source root: 0 matches.
  - The log file searched for the same strings: 0 matches. Shutdown logged
    `stopped`; no process was left running.
- Not exercised live: a real network mount (the run used `allow_local`), HEIC
  through `sips`, the daily backup scheduler firing at `backup.time` (the
  scheduler is a one-minute tick against home-local `HH:MM`; only the manual
  path was run), and the 45 s heartbeat-timeout close, which is covered by a
  fake-clock test — a cleanly disconnected client is reported offline at once.

## Handover for the next run

The backend scope of the V1 base is complete. What is left is not backend code:

- **H-406 (Playwright e2e)** and the README belong to the integrator.
- **G0** is maintainer-executed. `docs/ops/g0-record.md` is the blank template,
  `docs/ops/acceptance.md` gives the command for every PRD §7.1 metric, and
  `docs/ops/failure-matrix.md` gives a real injection step for every §7.2 row.
  Until those numbers exist, no performance acceptance may be declared.
- **Verify with the real web client**: the `data.changed` throttle, the
  `pending_command` path on reconnect, the close-code handling for 4002/4003/
  4004/4005, and the ranged preview probe. The server side of each is tested,
  but only the pair proves the contract.
- **Verify on the Mac mini**: a real SMB mount binding as `smbfs` with the
  authorized root inside the share, `sips` HEIC conversion, and the scan
  duration of the real library against the 60 s interval (design §13 keeps
  partitioned scanning as the pre-designed extension).
- The seams for V0.4 are unchanged: the admin API is complete and audited, and
  `admin_tokens.label` exists so an MCP bridge can be attributed.
