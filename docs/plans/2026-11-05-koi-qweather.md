# KOI almanac data and QWeather implementation plan

**Goal:** Fill Today's missing almanac fields from the actual KOI project and connect real QWeather observations and forecasts.

**Architecture:** Keep the family-local date and existing Today/House layout. Adapt the verified KOI calendar source through a pure presentation boundary. Add a server-side QWeather provider behind the existing cached weather service; credentials remain in protected local files. Do not create a worktree or alter unrelated user edits.

**Tech stack:** React/TypeScript, Go standard-library HTTP/Ed25519, existing SQLite widget cache, Vitest and Go tests, browser and OnePlus verification.

## Source and contract checks

- [x] Identify current missing fields: auspicious/inauspicious activities and month sexagenary label. Gregorian/lunar date, year/day labels, solar terms and week strip already have local calculations.
- [x] Verify the Mac mini's `koi-calendar` directory contains only a specification, not a reusable implementation. A saved KOI project is on another host; request access to its actual source rather than claiming an invented data source.
- [x] Verify current QWeather v1 coordinate endpoints and authentication against official documentation. Preserve optional forecast fields and WMO-normalized weather codes.
- [x] Obtain the user-provided KOI repository at `f973da1b07a8bc3a387c202fd1a134f24924a3fc`. It supplies generic offline tables/rules and reference fixtures. No LICENSE is present; reuse is authorized by the repository owner in this task, without claiming independent license verification. Pin the commit and hashes.
- [x] Audit the 1901–2099 range, leap months, whole-civil-day Lichun year/Jie month transitions, UTC+8 minute-precision terms and simplified `koi-almanac/1.0` rules. Existing fixtures are conformance checks rather than an independent authoritative almanac.
- [x] Port the engine and preserve January 1–5, 1901 month semantics using the previous Jie interval index 10 without fabricating a December 1900 term instant. Missing range-edge term progress stays missing.

## Server-side QWeather

Owner: weather provider agent; files `internal/widget/qweather*.go`, weather service/types/tests and weather configuration/validation/redaction.

- [x] First run failing synthetic tests for protected API-key and Ed25519 JWT authentication, current/hourly/daily JSON, nullable readings, correct units and location-local date boundaries.
- [x] Add `widgets.weather.qweather`: API host, timezone, API-key file or JWT developer/project/credential IDs and private-key file. Exactly one authentication mode; no secrets in config output or screen payloads.
- [x] Fetch current conditions, 24 hours and five days with bounded HTTPS requests. Convert humidity fractions to percent and m/s to km/h only for verified units. Retain valid current conditions when an optional forecast fails; unknown conditions remain unknown.
- [x] Bind cached data to provider and location, keep observation timestamps distinct from fetch time, preserve cross-midnight daily projection, and apply bounded failure backoff.
- [x] Retain provider attributions and sanitize transport errors. Run widget/config tests, race checks and independent synthetic contract probes.

Root integration: `internal/app/runtime.go` supplies the household timezone only when no weather-location zone is configured. `docs/api/openapi.yaml` documents optional observation and attribution fields. Real activation requires the operator's actual host, credentials and weather location; none are inferred from examples.

## Frontend and calendar adaptation

- [x] Show QWeather attribution beside weather wherever consumed, without nested interactive elements or remote-focus theft. Keep observed time distinct from refresh time and cover stale readings.
- [x] Port KOI as a pure TypeScript module with original tables, rules and fixtures. Use it consistently for all Today calendar fields; keep legacy consumers unchanged except sharing festival labels. Map complete month/activity fields into `AlmanacCard`.
- [x] Verify weather refresh focus, empty/stale forecasts, attribution navigation and Chrome 66 output at 1920, 1366 and 804 widths.
- [x] Verify 12 representative dates at 1920 and 804 widths, including the longest activity lists, two-digit dates, leap-month names and range boundaries. Independent full-range comparison matches the original Python rule implementation; independent sxtwl lunar comparison also passes (72,684 days).

## Delivery

- [x] Run complete frontend tests, lint, typecheck/build and appropriate Go tests. Obtain independent review.
- [x] Update bilingual public usage/deployment documentation with implemented capabilities and actual verification status.
- [x] Build a signed candidate against the current deployed source closure. Verify in isolation, back up before activation, preserve schema, source/screen/archive identities and the 2 GiB on-demand media policy.
- [x] Verify the actual OnePlus UI and restore its original route. Commit/push only task changes, retain unrelated documents byte-for-byte, and clean only this task's temporary build caches.

## QWeather implementation checkpoint before KOI integration (2026-11-05)

- QWeather adapter and UI are implemented. Full frontend regression: 96 files / 718 tests passed; lint and TypeScript/build passed. Full Go regression and widget/config race tests passed. An independent review retained its original timeout/null/attribution probes and verified cache isolation, bounded retries and concurrent refresh deduplication.
- Synthetic browser checks passed at 1920×1080, 1366×768 and 804×384, including long attribution remote scrolling, safe links that do not trigger refresh, complete card layout and House attribution. All task-owned synthetic listeners were stopped.
- The user supplied the actual KOI GitHub source on November 5. Calendar implementation is now complete from the pinned repository; the final verification is recorded below. KOI weather is a placeholder and contains no reusable QWeather credentials. At that checkpoint a dedicated QWeather host, credentials and a confirmed weather location were unavailable; production weather configuration was unchanged. The subsequent activation is recorded below.
- Signed calendar production activation and OnePlus physical-device validation passed for `3f9e24c`. Calendar acceptance did not establish real QWeather account acceptance; its subsequent live activation is recorded below.

## KOI implementation verification (2026-11-05)

- Pure offline engine: 411 tests passed, including 373 original fixture cases and whole-range lunar round trips. Original tables/rules/fixtures and their recorded hashes match KOI byte-for-byte.
- Complete frontend regression: 98 files / 1,135 tests passed; ESLint passed. Existing screen assertions were updated for the newly available KOI source while preserving family-date and week behavior.
- The exact current deployed Go source closure plus the allowlisted QWeather changes passed all Go tests and widget/config race checks. No unrelated backend implementation is being replaced.
- Browser checks cover 12 dates at 1920×1080 and 804×384, with a different browser timezone, long activity lists, leap months, range edges and removed flat/flatMap support. No overlap or page errors.
- Independent original-reference comparison covers all 72,684 supported civil days and all 4,776 term entries; sxtwl lunar comparison also matches. Upstream sxtwl month-pillar getter differs from KOI’s bundled whole-day term convention on two historical dates; preserve the explicitly versioned KOI contract.
- Signed deployment and OnePlus verification passed for `3f9e24c`: 8 backend and 8 embedded browser groups, exact original-reference calendar projection on OnePlus, retained archive/IoT behavior, original Archives route restored and temporary forward removed. Schema, config, service definition, ordinary-user identity, existing business/source/photo/screen identities and on-demand cache policy were preserved. At that checkpoint weather activation still required actual account/location configuration.

## QWeather production activation (2026-11-05)

- [x] Receive operator-provided dedicated Host, JWT IDs and requested district; retain all account material exclusively in protected local configuration. The existing Ed25519 PKCS8 PEM private key remains owned by the service user with mode 0600. No account values, private location or key material enter the repository.
- [x] Independently probe authenticated GeoAPI and all three Weather v1 endpoints. Verify an exact administrative-district match, coordinates and timezone, plus HTTP 200 and valid current/24-hour/five-day response structures.
- [x] Validate the candidate with the current Go config loader and actual QWeather adapter: 24 hourly entries, five daily entries, normalized readings, daily extremes/sunset and attribution present.
- [x] Back up configuration, atomically change only its weather section and restart the existing ordinary-user KeepAlive service. Preserve the signed binary, service definition, schema 18, business records, source/photo/screen identities and on-demand cache budget. Both NAS sources remain online. Refresh interval is 30 minutes and stale-after is two hours.
- [x] Verify actual OnePlus wire-to-DOM values and source declarations on Today and House, complete forecasts, optional observation-time handling, remote navigation and OK refresh without focus loss. Restore Archives and remove the temporary forward. No TCL acceptance was added.
- [x] Synchronize bilingual public usage and deployment status, keeping prior code-only acceptance separate from real-account activation.

The initial private device probe used unsupported administrative navigation routes and incorrectly required an observation timestamp. Its original script/result/hash were retained; the validation script was corrected to use real top navigation and the established optional-timestamp contract. The successful final device probe required no product changes.
