# Web track progress (M0 + V0.1)

| Item | Value |
| --- | --- |
| Track | Web (TypeScript), `web/` except `web/embed.go` and `web/dist/.gitkeep` |
| Scope of this run | M0 (W-001..W-004) and V0.1 (W-101..W-106) |
| Updated | 2026-09-06 |
| Verification | `npm run lint` clean, `npm test` 98 passing (16 files), `npm run build` 57.0 kB gzip JS |

## Task status

| ID | Task | Status | Notes |
| --- | --- | --- | --- |
| W-001 | Web project scaffold | done | `index.html` with inlined critical CSS, `src/main.tsx`, Vite `es2018`, dev proxy, `postbuild` restores `dist/.gitkeep` |
| W-002 | Types for contracts | done | `src/types/{api,ws}.ts` mirror design §8/§9; `assertNever` for exhaustive switches |
| W-003 | Core client libs | done | `api`, `auth`, `ws`, `clock`, `backoff`, `authExpiry`, `storage`, `throttle`, `ids` |
| W-004 | Mock core | done | `mock/{server,core,data}.ts`, `npm run dev:mock`, `/__control/*` hook |
| W-101 | App shell, router state machine, focus/keys | done | `app/router.ts`, `ui/keys.ts`, `ui/focusNav.ts`, `ui/focus.tsx` |
| W-102 | Pair screen | done | `screens/PairScreen.tsx` + `screens/usePairing.ts`, `app/pairing.ts` |
| W-103 | Connect screen and connection state | done | `app/connection.ts`, `screens/ConnectScreen.tsx`, banner on dashboard |
| W-104 | Dashboard v1 | done | clock, status bar, photo-pane placeholder, widget registry, weather/notice |
| W-105 | Auth cache expiry rule | done | `core/authExpiry.ts` + purge in `app/state.ts` |
| W-106 | Error boundaries and degraded rendering | done | `ui/ErrorBoundary.tsx` (per widget + global) |

## Layout

- `src/types/` — HTTP (§8) and WS (§9) contract types, hand-mirrored from the design.
- `src/core/` — framework-free logic: `api`, `auth` (transport seam), `ws`, `clock`, `backoff`, `authExpiry`, `storage`, `throttle`, `ids`. No `any`.
- `src/app/` — pure state: `router`, `connection`, `pairing`, `commands`, `state` (combined reducer); React wiring in `AppProvider`, `context`, `useConnection`, `useTick`, `apiClient`.
- `src/ui/` — `keys` (remote mapping), `focusNav`/`focus` (roving focus), `ErrorBoundary`, `StatusBar`.
- `src/screens/`, `src/widgets/` — presentation only.
- `mock/` — dev-only Node core, excluded from the bundle (own `tsconfig.mock.json`).

## Verified behaviour

- Pairing start → approve (control hook) → claim → `/home` with the `atrium_screen` cookie, second claim `410`.
- WS session: `session.ready`, `heartbeat` → `heartbeat.ack`, injected `screen.command`, `data.changed`.
- Backoff bounds and full jitter; close codes 4001/4002/4003/4004/4005; heartbeat cadence at 15 s with a fake socket.
- Clock with and without `Intl` named-zone support, including home-midnight rollover; 6 h "time unverified".
- Auth-cache boundary at exactly 24 h; purge clears photos and the home snapshot.
- Router whitelist rejects `photos/secret`, `photo/../etc`, `admin`, `__proto__`, extra segments.
- A throwing widget renders a placeholder while its siblings keep rendering.

## Deviations from design

1. **Mock server language/runtime.** The plan names `web/mock/server.ts` with "Node http + ws". It is TypeScript and is run directly by Node 25's native type stripping (`node mock/server.ts`) rather than through a build step. It is type-checked by a separate `tsconfig.mock.json` (not part of the `tsc -b` solution) so its files are never mixed into the app program.
2. **Dev proxy default target.** Design/plan wording points the dev proxy at `https://127.0.0.1:8443`. The default is the mock core at `http://127.0.0.1:8788` because the Go server is not runnable yet; `ATRIUM_PROXY_TARGET=https://127.0.0.1:8443` switches to the real Core, with `secure: false` for the self-signed certificate.
3. **`command.ack` timing.** §7.2 wants the ack sent after the target screen reports `rendered`. V0.1 acks on the state transition, since the photo render path (image `onload`) does not exist until the slideshow lands. `app/commands.ts` is already the pure decision point, so V0.3 only moves the ack call behind the render signal.
4. **`offline` threshold.** The design lists `reconnecting → offline` without a rule for the transition. The client calls itself `offline` after 3 consecutive failed attempts (roughly a minute at 1 s → 30 s backoff). Cached content keeps rendering behind the reconnecting banner regardless.
5. **`nas` widget rendering.** The `nas` widget payload is rendered by the status bar rather than as a card, so it is deliberately absent from the widget registry's rendered-type list. Unknown types are still hidden.
6. **Test environment patch.** Node 25 ships an experimental global `localStorage` that shadows jsdom's and throws without `--localstorage-file`. `src/test/setup.ts` installs an in-memory `Storage` when the ambient one is unusable. No production code is affected.

## Not in scope for M0/V0.1 (all delivered in V0.2 below)

- Slideshow, preloading, and the `202/404/503` media retry rules (design §7.3).
- Photo collection browsing (`photos/:collection` grid) and the single-photo viewer; both screens were placeholders.
- Cursor/seed handling for `random` rounds; `src/core/ids.ts` already exposed `newRandomSeed`.
- `data.changed` re-fetched `/home` only; the active collection refetch arrived with the collections.
- Full command executor with render-confirmed acks (V0.3, see deviation 3).

---

# Web track progress (V0.2)

| Item | Value |
| --- | --- |
| Scope of this run | V0.2 (W-201..W-205) |
| Updated | 2026-09-06 |
| Verification | `npm run lint` clean, `npm test` 157 passing (22 files), `npm run build` 63.6 kB gzip JS + 2.1 kB gzip CSS |

## Task status

| ID | Task | Status | Notes |
| --- | --- | --- | --- |
| W-201 | Photo pane slideshow | done | `app/slideshow.ts` reducer + `widgets/useSlideshow.ts` driver + `widgets/PhotoPane.tsx`; seeded rounds, preload one ahead, opacity-only crossfade |
| W-202 | Collections browser | done | `app/photoList.ts`, `screens/{PhotosScreen,PhotoGrid,usePhotoList}`; roving focus, pagination on approach, per-collection empty states |
| W-203 | Photo viewer | done | `app/photoViewer.ts`, `screens/{PhotoScreen,usePhotoViewer}`; `neighbors`, slideshow pause, `onload` render signal wired to the `show` ack |
| W-204 | NAS and index status UI | done | `ui/statusText.ts` + `ui/StatusBar.tsx`; health word + last check, baseline progress, share free space when present |
| W-205 | `data.changed` handling | done | `app/dataChanged.ts` (per-target 1 s throttle) wired in `app/useConnection.ts`; slideshow list refresh keeps the visible image |

## What was added

- `src/core/media.ts` — the single place that turns an HTTP status into a rule (`ready / processing / gone / unavailable / error`), with the 5 s / 3-retry budget.
- `src/app/slideshow.ts` — round + seed + skip/drop/retry state. `roundComplete` is set only when the cursor is exhausted, so a new seed starts the next round; fewer than two playable photos is a fixed display and never advances.
- `src/app/photoList.ts` — collection paging and focus arithmetic (shared `nextFocusIndex`), `shouldLoadMore` two rows from the end, `emptyKind` naming the per-collection empty state.
- `src/app/photoViewer.ts` — one photo plus `neighbors`, spinner/skip/leave decisions, `renderedId` from `<img onload>`.
- `src/app/dataChanged.ts` — topic → target mapping and one throttle per target.
- `src/app/homeSelect.ts` — typed reads of the `/home` widget list (clock zone, photo, nas).
- `src/ui/{decodeImage,photoCaption,statusText}.ts`, `src/screens/PhotoGrid.tsx`.
- Mock: `mock/collections.ts` (keyset cursors, seeded `random` rounds via a mulberry32 shuffle, `neighbors`), `/__control/media` (inject `202/404/410/503`, with a countdown) and `/__control/state` (index progress, NAS health, share free space).

## Verified behaviour

- Seeded round over the mock: two pages, no repeats, `next_cursor` null at the end, non-ready previews excluded from the round.
- Media: `200` for a ready preview; injected `202` (`Retry-After: 5`) counted as a retry and served on the next attempt; injected `404` dropped from the round; `503` skipped for the round only.
- `GET /photos/{id}?neighbors=recent` matches the browser's own ordering.
- Browser screen: 8 thumbs render with captions ("estimated" for inferred, "Date unknown" for unknown), Right moves one cell, Down moves a row, Enter opens the viewer, `<img onload>` clears the spinner; empty `recent` explains the baseline rule and offers "Go to slideshow".
- Throttling: a 20-message burst produces one refetch per target plus one trailing refetch; targets throttle independently.

## Deviations from design (V0.2)

7. **Media status is probed before the `<img>` is committed.** §7.3 describes loading through `<img src>` while distinguishing `202 / 404 / 503`. An `<img>` reports only "error", so `ApiClient.probeMedia` (a `Range: bytes=0-0` GET) classifies the response first and the decoded image follows. That is one extra, tiny request per photo; if G0 shows it matters, the `preview.status` in the list plus `onerror` can replace it for the happy path.
8. **`command.ack` for `show` now waits for the render.** Deviation 3 is partly closed: `pendingAckFor` / `resolvePendingAck` in `app/commands.ts` hold the ack until the viewer reports `<img onload>`, and `failedAck` answers `photo_unavailable` when the photo turns out to be missing. `navigate` still acks on the route change. The remaining V0.3 work is the timeout/expiry side.
9. **`photo` left the widget registry.** The dashboard composes the photo pane itself (it owns the slideshow), so `RENDERED_WIDGET_TYPES` is now `clock | weather | notice`; `photo` and `nas` are drawn by the dashboard and the status bar. Unknown types are still hidden.
10. **Back from a photo opened on the dashboard.** `backRoute` used to always return `photos/recent`. A photo route without a collection (Enter on the slideshow, or a `show` command from the dashboard) now returns to `dashboard`, which is the route it came from (W-203: "Back returns to the previous route").
11. **Dashboard remote entry points.** Design §7.1 says the browser is entered with Left/Right from the dashboard; PRD 4.2 adds "Enter opens the current photo". Both are implemented in `DashboardScreen` (Left/Right → `photos/recent`, Enter → `photo/<current slide>`), which the plan does not spell out under W-202.
12. **Mock fixtures are relative to today.** `mock/data.ts` dates its 30 photos relative to the current home day so `captured_today` is never permanently empty, and marks a few previews `pending`/`failed` so the skip rules have something to act on.

## Gaps for V0.3 (W-301..W-304)

- `useConnection` still holds the whole socket wiring; W-301 should split the session (heartbeat cadence, `session.ready` + `pending_command`) out of it.
- The pending-ack path is exercised by unit tests only; W-302 needs the scripted `show` → `onload` → `ack applied` round trip against the mock, plus `superseded` / `invalid_route` / `photo_unavailable` coverage.
- `session.revoked` clears state; `session.superseded` is handled by the close code only (W-303 should surface the message on the connect screen).
- Reconnect keeps the current route but does not yet re-validate it against a fresh snapshot (W-304).

---

# Web track progress (V0.3)

| Item | Value |
| --- | --- |
| Scope of this run | V0.3 (W-301..W-304) plus the three consistency items |
| Updated | 2026-09-06 |
| Verification | `npm run lint` clean, `npm test` 214 passing (28 files), `npm run build` 64.4 kB gzip JS + 2.1 kB gzip CSS |

## Task status

| ID | Task | Status | Notes |
| --- | --- | --- | --- |
| W-301 | WS session integration | done | `app/session.ts` (message semantics) split out of `useConnection`; heartbeat cadence stays in `core/ws.ts`; `session.ready` applies `pending_command` and reports the route with `state` |
| W-302 | Command executor | done | `app/commandExecutor.ts`; sequence guard, duplicate ignore, `navigate`/`show`/`refresh`, render-confirmed `show` ack, `superseded` / `invalid_route` / `invalid_payload` / `photo_unavailable` / `render_failed` |
| W-303 | Change push handling | done | `data.changed` → `dataChanged.ts` mapping through the session; `session.revoked` clears everything and lands on `pair`; `session.superseded` sets the connect-screen message with a focused manual retry |
| W-304 | Reconnect and snapshot sync | done | `app/snapshotSync.ts` + `app/routeSync.ts`: `/home` before the socket, route kept when still valid, `photo/:id` whose photo is gone → dashboard, cold start → dashboard, `appliedSequence` survives so old commands ack `superseded` |
| (a) | `probeMedia` 200 **and** 206 | done | Documented in `core/api.ts`, `isMediaSuccess` exported, tests for `200/206/202/404/410/503` |
| (b) | `RouteState` shape | done | Everything on the wire goes through `toRouteState`; `router.test.ts` asserts only `{name, collection?, photo_id?}` is ever emitted, and the mock records the heartbeat/`state` route for the E2E assertion |
| (c) | Close codes 4001–4005 | done | `/__control/close` in the mock plus a real 4005 on a malformed/oversized frame; `realtime.integration.test.ts` drives 4001/4002/4003/4004/4005/1001 end to end |

## What was added

- `src/app/session.ts` — what each server message *means*; no timers, no React.
- `src/app/commandExecutor.ts` — §6.5 turned into dispatches and acks, with the
  held `show` ack.
- `src/app/refetchers.ts` — one place for the four re-reads, in a promise form
  (`refresh` awaits them) and a fire-and-forget form (`data.changed` throttle).
- `src/app/snapshotSync.ts` — the preflight: `/home`, then route re-validation.
- `src/app/routeSync.ts` — the pure "which route survives a reconnect" rules.
- `src/test/fakeWs.ts` — scriptable socket + deterministic timer queue.
- `src/test/screenHarness.ts` — a headless screen (real reducer, session,
  executor, transport) used by the end-to-end test.
- Router action `router.commandStarted`: moves the route and remembers the
  command id **without** advancing `appliedSequence`.
- Connection: `connection.superseded` (the message, ahead of the close frame)
  and `connection.retry` with a `retryNonce` the socket effect keys off.
- Mock: `/__control/command` takes `sequence`, `ttl_ms` and `command_id` and
  holds a command as `pending_command` when nothing is connected;
  `/__control/acks` captures every `command.ack`; `/__control/revoke`,
  `/__control/supersede`, `/__control/close {code}`; `/__control/changed`
  takes `count` for bursts; sessions record `route` and `applied_sequence`;
  malformed or oversized frames are closed with `4005`.

## Verified behaviour

- Scripted end to end against the spawned mock (`src/test/realtime.integration.test.ts`,
  13 cases): pair → `/home` → socket → `session.ready` → heartbeat carrying
  `{name: "dashboard"}` → `navigate photos/captured_today` → ack `applied` with
  `route.name = "photos"` and the route reported back with `state` → `show` →
  ack `applied` only after the probe + `<img onload>`, carrying `resource_id` →
  `show` of an unknown photo → ack `failed / photo_unavailable` → stale
  sequence → ack `failed / superseded` → `refresh` → ack `applied` with the
  route unchanged and the pause kept → 20-message `data.changed` burst →
  `session.superseded` → the client stays `stopped` → a fresh session receives
  the held command inside `session.ready` and applies it → `session.revoked` →
  state cleared, route `pair`, `appliedSequence` back to 0.
- Close codes: 4001 routes to pairing (the `/home` preflight fails first),
  4002 clears and pairs, 4003 stops, 4004/4005/1001 reconnect and the client
  really comes back online.
- Heartbeat cadence with a fake socket: one on open, then exactly one per 15 s.

## Deviations from design (V0.3)

13. **`appliedSequence` moves only with an `applied` ack.** §6.5 says the client
    applies a sequence when it acks `applied`, so a `show` first dispatches
    `router.commandStarted` (route + duplicate-guard) and only bumps the
    sequence when the image decoded. A `show` that ends in `photo_unavailable`
    therefore leaves `applied_sequence` where it was, and the next command with
    the same sequence is not treated as stale.
14. **`refresh` does not re-open the photo viewer.** PRD 5.3 asks `refresh` to
    keep the page and the pause state. For `dashboard` it re-reads `/home` plus
    the slideshow round, for `photos/:c` `/home` plus the collection, and for
    `photo/:id` `/home` plus the collection behind it — the decoded image on
    screen is deliberately left alone so a refresh does not flash a spinner.
    A photo that has actually disappeared is still caught by the viewer's own
    media rules (V0.2) and by the reconnect check (W-304).
    A failed re-read acks `failed / render_failed` rather than `applied`.
15. **Cold start is a guarantee, not a correction.** "Cold start returns to
    dashboard" is satisfied by `initialRouterState.route`; `syncSnapshot` does
    not force a navigation on a cold start, because that would stomp on a route
    the app itself set during the first paint. It only corrects a restored
    route (`pair`/`connect` → dashboard, a gone photo → dashboard) and skips the
    correction entirely if the screen navigated while the preflight was in
    flight.
16. **`session.superseded` is acted on twice.** The message sets
    `stopReason = 'superseded'` immediately and the `4003` close sets it again,
    so the connect screen is right even if the close frame never arrives.
    Getting back requires the on-screen retry (focused button, Enter on the
    remote), which bumps `connection.retryNonce` and rebuilds the client —
    the design only says "stop reconnecting", it does not name a way back.
17. **`Range: bytes=0-0` is dropped by jsdom.** The probe is spec-legal in
    browsers (a "simple range header value" is exempt from the forbidden-header
    rule), but jsdom's `Headers` still strips it, so `api.test.ts` asserts on
    the raw `RequestInit` instead. Consequence for the real Core: the probe may
    legitimately be answered with either `200` or `206`, and both mean ready.

## Notes for the backend integrator

- The client sends `command.ack` for **every** decision including rejections,
  and never sends a second ack for a re-delivered `command_id`.
- `expires_at` is parsed but never compared against the local clock; a command
  that reaches the client is executed no matter how old it looks.
- The heartbeat's `route` and the `state` payload are exactly
  `{name, collection?, photo_id?}`; absent fields are omitted, never `null`.
- The client opens the socket only after a successful `GET /api/v1/home`, so a
  Core that accepts the WS handshake before `/home` is ready will simply not be
  reached until `/home` answers.
- The media probe is a ranged GET; `http.ServeContent` will answer `206`.
