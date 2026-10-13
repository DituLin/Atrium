# Growth dashboard implementation plan

**Goal:** Add a person-scoped height/weight dashboard and readable X/Y scales to existing archive and milk charts.

**Architecture:** Reuse authorized Core series analysis, separate height and weight by units/sample, and publish only a generation-consistent ArchiveReader snapshot. Render all curves through one time-scaled SVG component. Use existing paper/celadon TV styling and remote navigation.

**Tech stack:** React, TypeScript, SVG, Vitest, existing archive APIs.

## Approved design

- Selecting a person in all/health archives shows their growth dashboard above feeding/history. No hardcoded real names or person IDs.
- Show height and weight separately with exact latest measurement, date, count and chronological measurement details. Support last 3 calendar months, last year and all records (default).
- Query complete authorized series, not the current ten-card page. Include explicit Chinese metric aliases. Keep incompatible units/samples separate and show bounded/unavailable/partial states.
- X axis uses actual elapsed time with visible date ticks; Y axis uses readable numeric ticks and the original unit. Preserve exact source decimal strings in measurement labels. Never manufacture missing points or forecast future measurements.
- Existing milk day/week/month and archive curves use the same axes. Incomplete dates remain textual and are not silently assigned an exact date.
- Person summaries currently expose no authorized birth date; display calendar dates. Age labels require a separately authorized, confirmed birth date; do not infer one or expose protected identity facts.

## Implementation steps

1. Add chart scale/renderer regressions: uneven dates, one point, constant values, decimal labels, invalid dates; implement shared `TrendPlot.tsx` and integrate existing three chart renderers.
2. Add reader tests for person/date scope, aliases, generation mismatch, bound fallback and invalidation; implement `growthTrend.ts`, saved period and snapshot integration in `archiveReader.ts`.
3. Add `GrowthDashboard.tsx`, scoped screen integration and remote focus behavior; verify person/property/query/detail transitions and date-period controls.
4. Add bilingual usage notes. Run focused tests, full Web tests, lint/typecheck/build. Inspect browser rendering with synthetic data at TV dimensions and perform remote-key interaction checks.
5. Review the diff, commit the implementation and report the exact validation and deployment boundary.

## Baseline

Existing FamilyScreen and ArchiveReader tests: 23 passed on main `965a7c5` before implementation. Feature worktree: `codex/growth-dashboard`.

## Verification

- Full Web checks passed initially: 479 tests, lint, typecheck and production build.
- Independent review found two mixed date-precision/timezone issues. Both were reproduced with failing Los Angeles fixtures, then fixed using a shared calendar-day key. The fresh follow-up review found no remaining actionable findings; five targeted regressions passed.
- Synthetic browser checks passed at 1920×1080 and 1280×720. Verified visible numeric/date axes, separate charts, remote range selection and retained focus, with no page errors or horizontal overflow. Temporary visual fixtures were removed; screenshots contain only synthetic measurements and stay outside the repository.
- Final full Web checks passed: 482 tests across 68 files, lint, typecheck and production build. The feature does not change the database schema or deploy a new running Core by itself.
