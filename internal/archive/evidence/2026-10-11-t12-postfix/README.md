# T12 postfix qualifier quality repair — 2026-10-11

Implementation baseline: `50c4305cfd3662b8da8197eaa44aba58bf034cbd`; root's intervening documentation commits are retained. All original `/tmp` review overlays remain unchanged.

The original quality counterexample was reproduced RED: source `70左右 ml ...` with valid numeric evidence `70` produced an exact complete sum. The repository's new postfix matrix also failed before implementation. After repair the unchanged quality overlay passes (1.181s). All four prior semantic overlays also pass unchanged: XLSX formula 0.413s, invoice boundary 0.996s, pure analysis 1.190s and current assertion 0.693s.

The source check recognizes the closed set 左右/以上/以下/上下 within 24 runes after the numeric evidence. It checks either immediate suffixes or suffixes after that field's exact source-bound unit/currency evidence in the same parent component. Only horizontal whitespace may intervene; it never skips punctuation, a newline, a label or an unrelated field. The numerator and ratio denominator use the same helper. Original source/evidence are not rewritten; a current explicit exact value assertion supersedes the original qualifier.

Actual Parse→Validate→Publish→Analyze regressions cover 70左右, 70以上, 70以下, 70ml左右, 70 ml以上, 70 ml以下, 100左右, 100以下, 100分左右 and 100 分以下. Qualified values are excluded from exact calculation and mark coverage incomplete. Manual exact corrections restore computation. Semicolon, Chinese comma, newline, explicit sample label and a suffix belonging to a separate sample do not change the current numeric value.

Algorithm advances to `archive-analysis/5`, schema stays `/1`; obsolete algorithm snapshots are rejected and OpenClaw validates `/5`. No production data, provider completion, external messages or deployment were used.

Verification:

- Five original review overlays: exit 0, unchanged.
- Full `go test ./...`: exit 0, archive 16.284s / store 12.997s / HTTP 8.984s (`full-go.log`).
- `go test -race ./internal/archive ./internal/store ./internal/httpapi ./internal/brain -run 'TestAnaly|TestArchiveAnaly' -count=1`: exit 0, 1.402s / 51.684s / 2.144s / 1.699s (`analysis-race.log`), including actual Core→MCP→Brain integration.
- Node adapter suite: exit 0, 63/63 (`openclaw.log`).
- `GOOS=linux GOARCH=amd64 go build ./...`: exit 0, no output.
- `git diff --check`: exit 0.
