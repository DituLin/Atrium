# T12 original XLSX formula membership — 2026-10-11

Implementation baseline: `eff4c2ba6d94d41a47fa15cb43e499c5375869d0`; root's intervening documentation changes are retained. The independent source XLSX and all `/tmp` overlays remain unchanged.

The original real XLSX counterexample first failed RED (400.00 instead of 200.00). After repair the unchanged formula overlay passes (0.436s). The prior invoice-boundary, pure-analysis and current-assertion overlays also pass unchanged (0.380s, 0.332s, 0.407s).

`internal/store/testdata/analysis-f01.xlsx` is a byte-for-byte copy of the synthetic independent-review fixture. Repository tests assert four real extraction chunks and perform Parse→Plan→Validate each chunk→Merge→Publish→Analyze. The result is 200.00; its evidence retains original formula text `SUM(B2:B3)`, the historical 200.00 cache, both distinct invoice identifiers and the current member citations. Previous CSV fixtures were simplified coverage, not full XLSX F01; component and historical verification README wording has been corrected.

The host recognizes only a bounded literal SUM of up to 256 unique same-sheet cell references/ranges from the original validated parse. It does not execute arbitrary formulas. External/cross-sheet/unknown/shared/array/self/duplicate references, missing members, other-cache members and incompatible metric/currency/unit/sample cannot establish membership. Explicit formula references may span different bill IDs/payees/dates, unlike the weaker adjacent-row rule. Current authorized members supply amounts. A formula cache is never an additional charge, even when membership cannot be verified.

Regressions verify manual member correction to 178.10 yields 250.00 while historical cache 200.00 remains only evidence; a stale original cache 199.99 also does not change the 200.00 sum. A private member yields public 71.90 with neither hidden 128.10 nor historical aggregate 200.00 in the response; withdrawing the source empties results, and both changes invalidate prior snapshots. Unknown formula, cross-sheet, external, missing member, self-reference and currency-incompatible scenarios disclose incomplete formula coverage while excluding the cache. Reference parsing has independent bounds tests.

Algorithm version is `archive-analysis/4`, schema remains `/1`; store/Brain use the shared version and OpenClaw validates `/4`. No production data, provider completion, external messaging or deployment was used.

Verification:

- All four original review overlays: exit 0.
- First full `go test ./...`: exit 0 (`full-go.log`).
- Node adapters: exit 0, 63/63 (`openclaw.log`).
- `GOOS=linux GOARCH=amd64 go build ./...`: exit 0, no output.
- Final full `go test ./...` after evidence-metadata cleanup: exit 0, store 9.803s (`final-go.log`).
- `go test -race ./internal/archive ./internal/store ./internal/httpapi ./internal/brain -run 'TestAnaly|TestArchiveAnaly' -count=1`: exit 0, respectively 1.400s / 43.356s / 2.524s / 2.095s (`analysis-race.log`), including actual Core→MCP→Brain integration.
- Fixture byte comparison against the untouched review XLSX: identical.
- `git diff --check`: exit 0.
