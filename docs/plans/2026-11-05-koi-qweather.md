# KOI almanac data and QWeather implementation plan

**Goal:** Fill Today's missing almanac fields from the actual KOI project and connect real QWeather observations and forecasts.

**Architecture:** Keep the family-local date and existing Today/House layout. Adapt the verified KOI calendar source through a pure presentation boundary. Add a server-side QWeather provider behind the existing cached weather service; credentials remain in protected local files. Do not create a worktree or alter unrelated user edits.

**Tech stack:** React/TypeScript, Go standard-library HTTP/Ed25519, existing SQLite widget cache, Vitest and Go tests, browser and OnePlus verification.

## Source and contract checks

- [x] Identify current missing fields: auspicious/inauspicious activities and month sexagenary label. Gregorian/lunar date, year/day labels, solar terms and week strip already have local calculations.
- [x] Verify the Mac mini's `koi-calendar` directory contains only a specification, not a reusable implementation. A saved KOI project is on another host; request access to its actual source rather than claiming an invented data source.
- [x] Verify current QWeather v1 coordinate endpoints and authentication against official documentation. Preserve optional forecast fields and WMO-normalized weather codes.
- [ ] Confirm actual KOI implementation, license and date-boundary conventions before importing any data or library. Compare representative dates, leap months, solar-term month transitions and local midnight with KOI's own outputs.

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
- [ ] Once the actual KOI source is available, define the smallest reusable calendar module and map real month/activity fields into `AlmanacCard`. Preserve the source's semantic boundary and existing family timezone; do not copy design examples into data.
- [x] Verify weather refresh focus, empty/stale forecasts, attribution navigation and Chrome 66 output at 1920, 1366 and 804 widths.
- [ ] Verify long calendar activity lists and month labels once the KOI source is available.

## Delivery

- [x] Run complete frontend tests, lint, typecheck/build and appropriate Go tests. Obtain independent review.
- [x] Update bilingual public usage/deployment documentation with implemented capabilities and actual verification status.
- [ ] Build a signed candidate against the current deployed source closure. Verify in isolation, back up before activation, preserve schema, source/screen/archive identities and the 2 GiB on-demand media policy.
- [ ] Verify the actual OnePlus UI and restore its original route. Commit/push only task changes, retain unrelated documents byte-for-byte, and clean only this task's temporary build caches.

## Verified code status (2026-11-05)

- QWeather adapter and UI are implemented. Full frontend regression: 96 files / 718 tests passed; lint and TypeScript/build passed. Full Go regression and widget/config race tests passed. An independent review retained its original timeout/null/attribution probes and verified cache isolation, bounded retries and concurrent refresh deduplication.
- Synthetic browser checks passed at 1920×1080, 1366×768 and 804×384, including long attribution remote scrolling, safe links that do not trigger refresh, complete card layout and House attribution. All task-owned synthetic listeners were stopped.
- Required inputs remain unavailable: the actual KOI source, dedicated QWeather host and credentials, and a confirmed weather location. Saved-project metadata locates KOI on a different host. The current Mac mini copy is only a specification. No calendar values have been invented, and production configuration/service have not been changed for this feature.
- Signed production activation and physical-device validation remain pending those inputs. Prior release verification is not evidence of QWeather production acceptance.
