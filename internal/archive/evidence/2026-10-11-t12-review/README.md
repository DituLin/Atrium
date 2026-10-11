# T12 review repairs — 2026-10-11

Review baseline: `c95d31c231b5a6ceaf57d95bc0886b61b24dd303`. Original independent review overlays in `/tmp` were not modified. All data below are isolated synthetic fixtures.

The three original review counterexamples were reproduced failing before repairs. Unchanged overlay reruns after repair passed:

- `go test -overlay /tmp/t12-spec-overlay.json ./internal/archive -run '^TestT12Spec' -count=1`: exit 0, 0.506s.
- `go test -overlay /tmp/t12-spec-store-overlay.json ./internal/store -run '^TestT12Spec' -count=1`: exit 0, 0.412s.

Repository regressions cover original qualifiers outside normalized numeric evidence, `<`, `>`, inclusive ASCII/Unicode inequalities, approximate labels, and unrelated earlier metrics. They verify that an explicit manual exact correction supersedes original bounds and a later qualified correction stays non-exact. Current sample, unit, currency, report date and denominator assertions flow through the real parser→validator→publisher→analysis path; original and assertion citations remain present. A private sample closes public numeric dependencies without exposing its value.

Finance regressions retain independent CNY/USD totals and same-currency unproven totals with incomplete coverage. A table total is deduplicated only with a contiguous labelled table membership proof, compatible dimensions and exact reconciliation; canonical ID, document/event identity, confidence or matching amounts alone cannot establish that relationship. Physical table/column, sample, currency, unit, row continuity and reconciliation boundaries have separate regressions. Existing actual CSV 200.00, authorized 250.00/public 200.00, and USD 20.00 regressions remain enabled.

Schema remains `archive-analysis/1`. Algorithm/snapshot validity changes to `archive-analysis/2`; Brain and OpenClaw validate that version. No production data, external messages, provider completions or deployment actions were performed.

Final verification commands and logs:

- `go test ./...`: exit 0 (`full-go.log`).
- `node --test adapters/openclaw/*.test.mjs`: exit 0, 63/63 (`openclaw.log`).
- `GOOS=linux GOARCH=amd64 go build ./...`: exit 0, no output.
- `git diff --check`: exit 0.
- `go test -race ./internal/archive ./internal/store ./internal/httpapi ./internal/homemcp ./internal/brain`: exit 0, respectively 49.114s / 152.125s / 65.181s / 7.190s / 3.766s (`affected-race.log`).
- `go test -race ./internal/archive ./internal/store -run 'TestAnalysisReview|TestAnalysisAggregate' -count=1`: exit 0, 1.531s / 13.564s (`review-race.log`), including the final added qualified-denominator regression. That test-only addition followed the full suite run; implementation was unchanged.
