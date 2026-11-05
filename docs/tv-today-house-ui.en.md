# Updated TV Today and House interfaces

[中文](tv-today-house-ui.md) | English

Updated: 2026-11-05. Today now uses KOI almanac data and is deployed, with OnePlus acceptance passed. QWeather is configured for production and has passed live API and OnePlus display acceptance; see [Today data sources and release verification](usage/today-data.en.md). The delivery table below retains the November 4 interface release’s historical acceptance record. This update has not completed TCL physical-device acceptance.

## Today: almanac and weather

Today follows the supplied HTML design with two main cards: a wide almanac and weather. The almanac uses the household timezone, `home.timezone`, for the Gregorian date, lunar date, sexagenary calendar labels, week number, day of year, solar-term progress and seven-day week strip. The date changes at household midnight and is checked again on page entry and foreground return after sleep. Lunar dates, pillars, solar terms and the week strip now consistently use KOI’s offline tables and rules. Dates outside the supported 1901–2099 range show an explanation.

The November 4 initial release retained missing activity states. The November 5 version supplies complete activity lists from the simplified traditional `koi-almanac/1.0` rules and adds the month pillar and source label. See the data-source guide above for year/month boundaries and coverage. This page does not provide external event synchronization.

The November 4, 2026 deployment uses optional Open-Meteo weather. See [Today data sources and QWeather configuration](usage/today-data.en.md) for the QWeather adapter added on November 5, its production configuration and live verification. Maintainers must enable the selected provider in `widgets.weather` and configure the actual location; see the [configuration example](../config.example.yaml). Updating the interface does not select a city or populate credentials automatically.

When supplied, the weather card shows current, maximum and minimum temperatures, humidity, wind, sunset, an hourly temperature curve and up to five forecast days. Known WMO codes and English condition descriptions are presented in Chinese; unknown codes or descriptions remain unconfirmed. Air quality requires explicit AQI data and is not inferred from weather codes. Missing fields, an unconfigured provider, stale data and partial forecasts have corresponding placeholders or labels, without invented zeros or example curves. Older cached weather data remains readable when newer fields are absent.

Direction keys move between the almanac, weather and top navigation. Pressing OK on either card refreshes the briefing and weather, reports the outcome and retains the current focus.

## House: fixed navigation and read-only observations

Top navigation stays fixed while the dashboard scrolls internally. The layout retains the floor-plan area, selected-zone details, temperature/humidity history and aquarium positions, plus lighting, climate/air, and doors/windows/cleaning groups. Moving focus to lower sections with direction keys scrolls the focused item into view.

The six current floor-plan zones are illustrative and explicitly labeled “户型示意 · 房间尚未配置” (“Illustrative floor plan · rooms not configured”). They do not represent actual rooms or device locations. Real room geometry, dashboard device categories, environmental history and aquarium display mappings are not connected yet. These positions retain unconfigured or missing states. Device names, generic temperature capabilities and single readings do not establish room locations, water temperature, indoor averages or historical curves. Populated floor plans and curves in tests use synthetic material only.

Authorized Home Assistant devices remain visible in a separate read-only Connected Devices observation area, grouped using explicit room associations; unassociated devices remain unassigned. Each capability retains its actual value, availability and source-change, measurement, receipt and synchronization-confirmation timestamps. Missing timestamps are identified. Missing dashboard mappings do not hide genuine connected-device readings.

Device cards do not send hardware commands or depict a click as a successful operation. Missing, unavailable, failed, stale and disconnected states remain distinct. Disconnection does not label retained values as new observations or a live connection. Core, current-screen and individual photo-source diagnostics, together with Recheck, remain below the dashboard and update according to each source's validity. See the [HA IoT deployment guide](ops/ha-iot-deployment.en.md) for integration and authorization boundaries.

## Compatibility and delivery boundary

This interface update builds on the merged HA support. It preserves schema 18, existing configuration, the system service running as the original ordinary user, source and screen identities, and the [on-demand preview policy with a 2 GiB cache budget](usage/photo-cache.en.md). It does not grant additional home-control permissions or complete room mappings, real historical data or all dynamic IoT recovery acceptance.

The interface implementation commit is `9ca65ae`, with HA merged in `c6f2383` and the final CSS correction in `acd18c6`. The signed Core deployed on November 4 was `today-house-ui-acd18c6+c06c6a591e9a33c6a93bc0def15d3e29061f9d07`. Activation preserved schema 18, configuration, the system service definition and ordinary-user identity, business data, source and photo identities, and screen pairings. The existing KeepAlive mechanism restarted the service normally without changes to its definition.

The historical validation scope for the November 4 interface release is:

| Check | Historical result |
| --- | --- |
| Frontend regression, lint, TypeScript and compatibility | Final `acd18c6` passed all 707 tests across 95 files and 11 targeted checks; lint, TypeScript builds and the Chrome 66 style check passed |
| Go regression | Full repository tests passed; full final candidate-source tests, including the embedded frontend, passed with `-p 1`; weather-component race checks passed |
| Synthetic browser checks | Layout, remote focus and scrolling checked at widths 804, 1366, 1920 and 3840; five-character leap-month labels and the configured-house layout were checked again |
| Signed isolated candidate | Eight backend and seven browser check groups were rerun successfully after the final CSS correction |
| Production deployment and physical devices | The signed version is deployed; OnePlus loaded `0.1.0+acd18c6`, passed the checks below and was returned to Archives; the TCL was offline and has not received this release's physical-device acceptance |

OnePlus checks covered Today's two cards, seven-day strip, household date, Chinese weather or honest empty states, four-direction focus navigation, and OK-triggered Home and Overview refreshes without focus loss. House checks covered six unconfigured zones, three empty temperature/humidity/aquarium cards, every authorized HA capability value and all four timestamp types matching the real API, read-only boundaries, and remote navigation to history, the long device-observation area, all three device groups and Core/NAS diagnostics. Top navigation stayed fixed, and neither page had unintended overflow. The device was returned to Archives and temporary debugging forwards were removed afterward.

Post-deployment runtime checks confirmed online NAS sources with their identity bindings retained, unchanged configuration and service definition, matching signatures and exact versions, schema 18 and the 2 GiB on-demand cache policy. These checks do not replace TCL physical-device or sustained-operation acceptance. Public documentation retains only technical boundaries and validation scope, excluding real household material, device-entity inventories, private locations, credentials and unredacted screenshots.
