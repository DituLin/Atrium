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

Deployment and a fresh APK process must still be checked on the physical TV;
a temporary patched page alone is not acceptance evidence.
