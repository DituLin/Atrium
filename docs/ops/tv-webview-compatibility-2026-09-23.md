# Android 9 / WebView 66 connection compatibility

## Observed failure

The real TCL TV reached Core over HTTPS and loaded the JavaScript and CSS
successfully, but displayed small text and a reconnect/authorization screen.
This was not a NAS failure or an unreachable Core address.

Three incompatibilities were reproduced on the device:

- WebView 66 lacks `globalThis`. The default API fetch, WebSocket timers and
  message ID generator referenced it. Removing `globalThis` in a modern
  browser also reproduced the failure before any API request.
- CSS `min()` / `max()` are unsupported. The design-pixel custom property
  therefore invalidated font sizes and spacing when consumed.
- After pairing, cookie-authenticated GET requests returned 403. This engine
  omits Origin and Sec-Fetch-Site on these requests; the server's
  `Referrer-Policy: no-referrer` also prevented the existing Referer fallback.
  Repeated failures then caused the normal authentication cooldown (429).

Temporary, page-local probes confirmed each cause: a window alias, valid scale
values, and a same-origin referrer policy restored the connection. These probes
are diagnostic evidence, not a persistent repair.

## Repair

Use `window` for browser runtime APIs. Express design scaling with viewport
units and aspect-ratio media queries; keep the focus-width floor behind
`@supports`. Apply equivalent fallback sizing to pairing digits and the home
accent rule. Serve `Referrer-Policy: same-origin` so legacy clients can prove
same-origin requests without exposing referrers to other origins. Cookie,
TLS, allowed-origin, and cross-site rejection rules remain enforced.

Regression checks: three frontend tests fail on the previous globalThis
implementation and pass after the repair; a response-header test fails before
the policy change and passes after. The existing authentication matrix covers
allowed and foreign Referer values, missing origin information, and cross-site
fetch metadata. Frontend: 55 files / 388 tests pass, lint and build pass.
Backend: `make web-sync check` passes.

## Production verification

Deployed commit `8b4edd2` with a binary/config/database rollback snapshot.
The response header and served bundle match the candidate. After force-stopping
and reopening the physical TV APK, the screen automatically connected and
rendered the home photo and layout. Its runtime still has no globalThis, no
inline scale override, and no injected referrer meta tag: this is the deployed
repair, not the temporary probe. Core reports the paired TV online.

ADB D-pad center opened the home photo, right selected a different loaded
photo, and Back returned home. A private 1920×1080 screenshot was inspected.
This verifies the reported connection/scaling failure and basic photo remote
interaction, not every layout on WebView 66 or TV video decoding/audio.

Replacing the unsigned Core binary caused macOS to request Network Volumes
permission again (confirmed in TCC logs). Cached photos remain readable, but
the first NAS scan timed out at zero files. NAS original-video acceptance is
pending that OS permission; the source health label alone is not proof.

The current APK was rebuilt/checked with Android unit tests. Native code did
not change for this web/Core repair, so the APK hash is unchanged from the
latest remote-video build. A dated installation copy is prepared locally;
USB transfer remains pending because macOS detects no external physical disk.

