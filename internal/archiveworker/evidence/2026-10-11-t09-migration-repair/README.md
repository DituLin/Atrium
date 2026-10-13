# T09 incomplete identity migration repair — 2026-10-11

Review of `628907d` found a data-loss path: source A had transaction 0012/70,
source B had 0013/80, and A's corrected identity omitted its amount. Canonical
migration removed A's old amount even though no replacement was published. The
unchanged reviewer counterexample reproduced RED (expected one amount, got zero;
store 0.524s) before this repair.

Identity changes now require exactly one safe replacement amount, a usable
decimal/currency signature and resolved subjects. Otherwise publication waits
for clarification before changing equivalences or current references. Native
slot reconciliation runs before that decision so newly detected ambiguity cannot
invalidate a replacement after destructive migration has begun. Publication
version is `archive-publication/5`.

The committed end-to-end matrix tests missing amount, missing normalization,
missing currency, unresolved subject, and complete replacement. The first four
retain the exact old active publication and amount 70; complete replacement
succeeds with exactly amount 80. A direct gate regression tests a pending amount
field. Existing native slot ambiguity and full replacement tests remain enabled.

```sh
go test ./internal/store -run 'TestArchiveIdentityMigrationRequiresSafeReplacementAmount|TestArchiveTransactionReplacementRejectsPendingField' -count=1 -v
go test -overlay=/tmp/t09-quality-recheck-overlay.json ./internal/store -run '^TestQuality' -count=1 -v
go test ./...
go test -race ./internal/store ./internal/archiveworker ./internal/httpapi ./internal/app
GOOS=linux GOARCH=amd64 go build ./...
ATRIUM_T09_NATIVE_IMAGE="$PWD/adapters/macos-archive-parser/evidence/report.png" \
ATRIUM_T09_NATIVE_HELPER=/tmp/atriumos-t09-repair-native/helper \
ATRIUM_T09_CAPTURE=../archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json \
go test ./internal/archiveworker -run '^TestWorkerActualNativeWithRetainedCompletion$' -count=1 -v
```

- Focused matrix and pending-field gate passed in 0.477s.
- `reviewer-overlay.log`: all four original reviewer cases pass, 1.706s;
  the missing-amount case reports `waiting_input`.
- `full-go.log`: exit 0; store 10.532s, worker 1.425s, archive 13.764s,
  HTTP 8.807s, app 11.986s; unchanged packages may reuse Go's cache.
- `linux-build.log`: exit 0, no output.
- `affected-race.log`: exit 0; store 101.471s, worker 5.083s, HTTP 55.898s,
  app 18.530s.
- `native-replay.log`: pass without skipping, 0.567s; two real native chunks,
  one entity, one event, seven observations and zero new provider calls.

The original reviewer test remains unchanged, SHA-256:
`0033294f66aaf9cbb2569284e6d2c355d43965f611b18a57049d5164b9009ac0`.

All inputs remain synthetic. No production configuration/data, external message,
deployment or new model call was used. Independent quality re-review remains
required; this evidence does not claim overall T09 approval or later stages.
