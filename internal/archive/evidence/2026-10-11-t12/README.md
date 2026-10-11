# T12 verification — 2026-10-11

Synthetic fixtures only; no production database, archive originals, IM delivery, NAS mounts or model credentials were changed.

- `go test ./...`: exit 0, final implementation. New analysis source/tests are not cached in the final log (store 8.988s). Existing unrelated packages may reuse Go's verified content cache.
- `go test -race ./internal/archive ./internal/store ./internal/httpapi ./internal/homemcp ./internal/brain`: exit 0 (archive 49.124s, store 141.344s, HTTP 65.535s, MCP 6.293s, Brain 3.413s). Subsequently added owner compatibility, legacy-pending coverage, unknown-unit separation and strict analysis query receipt checks were verified with the final targeted race command below.
- `go test -race ./internal/archive ./internal/store ./internal/brain -run 'TestAnalyze|TestArchiveAnalysis' -count=1`: exit 0 on final code (1.450s / 8.580s / 1.679s).
- `node --test adapters/openclaw/*.test.mjs`: exit 0, 63/63 tests passed; final log retained separately.
- `GOOS=linux GOARCH=amd64 go build ./...`: exit 0 on final code, no stdout/stderr.
- `git diff --check`: exit 0.

Specific source-bearing fixtures: actual CSV Parse→Plan→Validate→Merge→Publish→Analyze returns 200.00 rather than 400.00 for two line items and their total. Additional current sources establish authorized CNY 250.00 / USD 20.00 versus public CNY 200.00 / USD 20.00. Immutable legacy JSON amounts establish 450 ml sum and 70 ml latest separately. Actual TLS Core + official MCP transport + Brain executor discovers and calls the analysis tool, returning 450 ml and both citations for 330+120. No model callback is replaced with an answer mapping; this test does not invoke a new provider completion.

Fault regressions cover no search indexing, current private projection, adding a matching source, withdrawal, rebuild generation, revocation, independent manual value and ownership, pending legacy projection, strict HTTP argument rejection and mismatched-query receipts. Pure tests cover proof ordering, explicit counts, decimal/currency/role grouping, missing dimensions, comparator values, invalid ratio denominators, coarse dates and exact percentage-point change.

This evidence supports handing T12 to independent review. It does not claim T13–T18, the full 42-query acceptance suite, TV/native rendering, production migration, or deployment completion.
