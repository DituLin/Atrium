# TV source-label simplification and milk age title

**Goal:** Keep Today and House visually simple, and replace a selected person's milk-card name suffix with their calendar age.

**Architecture:** Keep all existing calendar/weather data and provenance. Move visible source declarations into an accessible Settings entry; remove supplier/version labels from Today and House. Compute completed calendar years/months and residual days below one year from the selected person's existing public growth profile and the server-synchronized household date. Use the current checkout, without a worktree, and preserve unrelated user documents.

**Tech stack:** React, TypeScript, existing remote-focus components and calendar helpers, Vitest, synthetic browser validation and actual OnePlus acceptance.

## Implementation

- [x] Remove KOI branding/version from the almanac heading while preserving all actual calendar fields.
- [x] Remove expanded weather-source text from Today and House, including obsolete focus paths. Add one clearly reachable Settings data-source entry with supplier links and complete available attribution statements.
- [x] Resolve the selected person by exact entity ID and read only that profile's date of birth. Calculate completed calendar years/months in the household timezone, with residual civil days below one year. Show `奶量 - 6个月9天`, `奶量 - 1岁0个月` or `奶量 - 1岁2个月`; show only “奶量” when a selected person lacks a valid profile/date; retain scope labels for non-person filters.
- [x] Verify birthday/month-end/leap-year/invalid/future date handling, household midnight and foreground return, plus period controls and remote focus.

## Delivery

- [x] Verify public documentation in Chinese and English, and retain source attribution in Settings. No real names, dates of birth, account information, locations or device screenshots enter the repository.
- [x] Run the appropriate frontend regression, lint, typecheck/build and Chrome 66 checks. Independently review age semantics and focus behavior; inspect synthetic browser layouts.
- [ ] Build a signed frontend-only candidate over the exact currently deployed source closure. Preserve active weather configuration, service identity, schema 18, data/source/screen identities and the 2 GiB on-demand cache policy.
- [ ] Verify the actual embedded candidate, deploy with backup and rollback protection, then check OnePlus and return it to Archives. Commit and push task changes to main, preserving the unrelated documents byte-for-byte.

## Source-display design

QWeather's attribution guidance requires a clear provider name and official link in the product. Its page-local example uses advisory wording and does not prescribe a fixed watermark. Centralizing the declaration under a visible Settings data-source entry is the product's implementation choice, rather than an explicit Settings exemption from the provider. Preserve all supplier metadata there. References: https://dev.qweather.com/docs/terms/attribution/ and https://www.qweather.com/terms/developers-eula .
