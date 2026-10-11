# T14 TV family archives evidence

These are synthetic fixtures, not family data. `device-checks.json` was recorded
independently by the parent agent. The PNGs show the actual browser and separately
installed OnePlus test app rendering production single-page preview responses.
The original `io.atrium.tv`, its configuration, and the production household DB
were not modified. OnePlus WebView 119 and Chrome 154 are not Chrome 66 runtime
coverage; the existing legacy API/CSS guards and the Chrome 66 build target cover
that compatibility boundary.

The family route now defaults to all people and all four domains; it does not
pick a legacy member. Public T08 entity filters include people and property;
shared, unassigned and unresolved filters remain available without registration.
Legacy feeding records use the stable legacy archive projection. Screen content
is a typed public list/detail/evidence projection with version, processing,
coverage, original-saved and honest pending NAS status. No organize mutation is
exposed to a screen cookie.

Archive verification runs every 15 seconds, expires by 30 seconds from the start
of the last successful verification, rebuilds the loaded history window, and
ignores aborted/superseded requests. Archive notifications and visibility changes
remove content before revalidation. The preview component aborts old fetches and
revokes object URLs on removal. The guarantee is snapshot authorization plus
bounded invalidation, not instantaneous cross-process revocation. A new archive
command must obtain its own result before a matching command/view render can ack
applied. Navigation by itself cannot ack applied.

`home_show_archive_query` accepts a previously issued opaque search or analysis
query reference. Core binds it to the current credential/principal, household,
screen and expiry, checking `archive.read` plus `screens.control`; it does not
inherit NAS/photo permissions. The command contains only the opaque archive view
ID. The TV re-queries public data. An analysis query retains its operation, metric
and criteria server-side and recomputes public decimal/unit/currency groups.
Private query wording is not returned as a heading or filter description. Query
references expire after ten minutes; display views after two minutes. Commands
must also pass their normal shorter expiry and current authorization on ack.

The real synthetic host path was exercised through Core query receipt → MCP →
Brain executor/ledger → command accepted → browser WebSocket → actual DOM →
command applied. This is not a real model turn or external WeChat/Discord send;
those are T15 acceptance. The four-domain fixture deliberately varies the domain
of already validated synthetic content, so it verifies navigation/filter plumbing,
not model classification quality. NAS synchronization and production deployment
remain T16/T17 tasks and are not claimed by these screenshots.

Current bounded presentation: list pages contain ten documents and the loaded
window is capped at twenty pages. A detail includes at most twenty-five evidence
items with explicit partial coverage. Entity filters offer additional bounded pages (up to 250 entries) through
“更多人物与房屋”. Text/CSV/Office show structured source locators, explicitly not original
layout. Unknown/encrypted files remain saved-only; raster previews require the
trusted macOS helper. Health volume/height/weight series and finance/education
summaries use T12 public analysis; all displayed decimals come from Core unchanged.
Only SVG coordinates use floating point. Algorithm, sample, denominator, rounding
and percentage-point change are presented when supplied. Partial dates are never
expanded into fabricated days, and full instants use the household timezone.

See the verification summary next to this file for the final automated commands.

## Independent review repairs

New IM queries now clear the previous detail and local selection, and acknowledgment requires the current query result DOM. Command tokens also fence stale failures. Regressions cover detail-to-query, query replacement, manual-filter cancellation and a previous failed query.

Saved originals now distinguish processing, retry waiting, needs review and incomplete terminal processing without exposing worker state or provider errors. List and detail read current task state; a failed reprocessing attempt does not replace an existing valid publication. The unchanged reviewer counterexamples and complete Go/Web regressions passed; see `review-repair.json`.

The repaired build also passed the independent real-browser detail-to-query flow: browser entered the PNG detail first, Core/MCP/Brain host sent a health query, and the browser showed exactly one health card before the applied outcome. See `browser-detail-to-show.png`; this is synthetic host execution, not model inference.
