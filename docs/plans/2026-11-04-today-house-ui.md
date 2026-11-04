# Today and House design implementation plan

> **For agentic workers:** Use subagent-driven development with independent page ownership; integrate and review before release.

**Goal:** Implement the visible Today and House layouts from the November 4 HTML designs, including remote navigation and honest data states.

**Architecture:** Retain the existing authenticated Home, Overview and House loaders and the deployed read-only HA integration. Separate the two dashboards into scoped components/styles and pure presentation models. Extend the existing optional weather widget for real forecasts; keep unconfigured room geometry, device display mappings, aquarium history and almanac fields visibly unavailable rather than copying demonstration values.

**Tech stack:** React 18, TypeScript, existing local lunar calculations, SVG, Go weather service, Vitest and Chromium/Android WebView acceptance. Preserve the Chrome 66 target. Work in this checkout, as requested, without a worktree.

## Design requirements

- Today: existing navigation; title/subtitle/status row; wide horizontal almanac and 500-unit weather card, separated by 28 units. Almanac contains the large Gregorian date, vertical lunar date, year/week/day metadata, auspicious/inauspicious slots, real solar-term progress and seven-day strip. Weather contains current conditions, four statistics, hourly curve and five forecast rows. Old sections hidden in the supplied HTML stay hidden.
- House: fixed navigation above scrolling content, status chips, floor plan beside selected-room details, temperature/humidity/aquarium cards and three device groups. Room focus selects its details; remote traversal scrolls the focused item into view.
- Keep core/NAS diagnostics accessible, with per-source expiry and independent House recovery. Device controls require a real authorized adapter; this task does not simulate successful hardware commands.
- The user confirmed the supplied room layout is illustrative and should remain unconfigured. Synthetic populated floor plans are used only for tests/visual acceptance. Preserve the deployed HA capability observations in a separate read-only device section; do not infer room geometry, device class, aquarium water temperature or ambient averages from device names or generic temperature capabilities.
- Weather uses the existing configured provider only. New fields are optional and old caches remain readable. Missing current conditions cannot become a fabricated zero-degree clear day. No AQI is inferred from weather codes.
- Preserve authorization-generation guards, expiration on entry/foreground, refresh without focus theft, family-zone date boundaries and the daily photo feature.

## Task 1: Today presentation and navigation

Owners: `screens/BriefingScreen.tsx`, `screens/info/AlmanacCard.tsx`, new almanac/weather presentation modules and tests, `styles/briefing.css`.

- [ ] Add meaningful failing date-model tests for cross-year weeks, leap-year day ordinals, family midnight and solar-term boundaries.
- [ ] Build the two cards at the supplied proportions, using actual local-calendar calculations and nullable weather data.
- [ ] Test missing/stale weather, partial forecasts, negative/constant temperatures and left/right/up/OK remote behavior.
- [ ] Move the old notice-deadline checks to the Home consumer that still displays notices; do not retain vacuous assertions on the redesigned page.

Run: `npm test -- src/screens/BriefingScreen.test.tsx src/screens/BriefingAlmanac.test.tsx src/screens/info/` from `web`.

## Task 2: House dashboard

Owners: `screens/HouseScreen.tsx`, new `screens/house/` components/models/tests, `styles/house-dashboard.css`, House screen tests.

- [ ] Define read-only view models with nullable observations and timestamps. Unit tests must distinguish missing, stale, unavailable and observed values.
- [ ] Implement floor-plan selection, room details, genuine sampled chart paths with gaps, aquarium summary and device groups.
- [ ] Render full unconfigured dashboard geometry using no invented room facts or healthy sensor readings. Integrate existing HA devices with every capability, timestamp and unavailable/stale state, retaining authorization-clear behavior.
- [ ] Retain Core/NAS diagnostics, source-specific stale states, explicit refresh, focus preservation and scroll traversal.

Run: `npm test -- src/screens/HouseScreen.test.tsx src/screens/house/ src/app/house.test.ts src/app/houseLifecycle.test.tsx`.

## Task 3: Weather and shared integration

Owners: `internal/widget/weather.go`, `internal/widget/types.go`, weather tests; root owns `web/src/types/api.ts`, shared refresh lifecycle, CSS imports and OpenAPI.

- [ ] First fail provider tests for nullable current fields, real forecast data, timezone conversion, partial arrays, old caches and retained data after provider failure.
- [ ] Fetch optional current humidity/wind, hourly temperature, and daily temperatures/weather/sunset through the existing provider and cache. Use bounded arrays and unambiguous timestamps.
- [ ] Keep these TypeScript additions optional: `humidity_pct`, `wind_speed_kmh`, `wind_direction_deg`, `temperature_min_c`, `temperature_max_c`, `sunset`, `forecast_timezone`, `hourly`, `daily`. Nullable numeric samples preserve missingness.
- [ ] Refresh Home weather with Today while preserving the independent Overview/House recovery paths and authorization guards.
- [ ] Synchronize OpenAPI and bilingual public release documentation.

Run: `go test ./internal/widget ./internal/httpapi`; `npm test -- src/app/refetchers.test.ts src/app/overviewLifecycle.test.tsx src/screens/DashboardScreen.test.tsx`.

## Task 4: Verification and delivery

- [ ] Review implementation against every visible HTML section, including populated synthetic and unconfigured runtime views.
- [ ] Run full frontend tests, lint, typecheck/build and Chrome 66 output guard. Run appropriate Go regression/race checks if the weather service changes.
- [ ] Use synthetic Chromium screenshots at 1920×1080 and 1280×720; verify both pages, remote focus/scroll, stale/error states, and no page errors or unintended overflow.
- [ ] Obtain independent code/design review and resolve findings.
- [ ] Merge the already deployed HA branch into the new UI branch, preserving both implementations and unrelated user files. Build a signed candidate over the actual HA backend closure, verify the package manifest and run isolated acceptance before activation. Preserve schema 18, the system service running as the existing ordinary user, source/SQLite/screen identities and the 2 GiB on-demand policy.
- [ ] Verify online OnePlus navigation/rendering and restore the original tab. Do not claim TCL acceptance while it is offline.
- [ ] Commit only task files, push main as previously requested, and preserve the unrelated HA plan/navigation edits byte-for-byte. Keep private operational evidence outside the repository.
