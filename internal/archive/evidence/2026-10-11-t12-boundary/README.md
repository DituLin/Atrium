# T12 explicit aggregation boundaries — 2026-10-11

Review baseline: `c7c533ccd72967ff044e41c627da38e55093ea56`. Synthetic fixtures only. Original review overlays were not modified.

The independent CSV counterexample was reproduced RED: adjacent CNY 10.00 line item for INV-A and CNY 10.00 total for INV-B incorrectly produced 10.00. The unchanged boundary overlay passes after repair (0.399s), returning 20.00. Both prior overlays also pass unchanged (archive 0.351s, store 0.456s).

Current explicit identifiers now bound the table aggregation rule. Invoice/bill, transaction, order, statement, receipt, account, application-specific ID/number fields, counterparties, owners and event dates must agree; a missing or ambiguous boundary cannot establish equivalence. Both store membership assembly and the arithmetic engine enforce this. Distinct canonical events alone do not imply separate bills: the earlier simplified CSV rows (not the full XLSX F01) still deduplicate when their explicit boundaries do not conflict.

Actual Parse→Validate→Publish→Analyze regressions cover independent equal-valued invoices; multiple identifier aliases; a current manual identifier changing 10.00 to 20.00 and back; private identifier closure; and a correction of original INV-B to current INV-A with both original and assertion evidence preserved in the deduplicated result. Pure tests prevent supplied membership from overriding explicit invoice, transaction, owner, date, unknown or missing boundaries. The existing simplified CSV (not the full XLSX F01), CNY 250/200 and USD 20 tests remain active.

Algorithm version advances to `archive-analysis/3`; response schema remains `/1`. Snapshot checks, Brain's shared algorithm constant, and OpenClaw's renderer agree on the new version. No production data, provider completion, external messaging or deployment was used.

Verification:

- Original boundary overlay: exit 0; previous two overlays: exit 0.
- Analysis regressions with race: archive 2.363s, store 25.850s, HTTP (including actual Core→MCP→Brain) 2.673s, Brain 2.256s, all exit 0 (`analysis-race.log`). The MCP package had no matching standalone test; its full non-race suite runs in the full Go check.
- Node adapter regression: exit 0, 63/63 (`openclaw.log`).
- Linux amd64 `go build ./...`: exit 0, no output.
- First full Go run failed only `TestNativeReapsDescendantsOnCancelAndPipeDelay/cancel`: its one-second timeout elapsed before the fixture created its PID file (`full-go.log`). The unchanged native test passed three consecutive standalone reruns (`native-retry.log`); no native code or timeout was changed.
- Full Go rerun: exit 0, archive 9.622s / store 8.051s (`full-go-retry.log`), including the last added original-identifier correction regression.
- Final invoice/identity regressions with race: exit 0, store 7.357s (`final-boundary-race.log`).
- `git diff --check`: exit 0.
