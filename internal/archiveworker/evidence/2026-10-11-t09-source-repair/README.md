# T09 source evidence after human edits — 2026-10-11

Fixed-base review of `4ae4db6` found that reassigning a transaction to Bob froze
its source amount: complete same-anchor re-extraction from 70 to 80 still read
70. The unchanged independent overlay reproduced that failure before this repair
(expected 80, actual 70; store 0.303s).

The repair removes whole-user-revision freezing. Each complete extraction creates
its own current source revision. The separate value and ownership audits remain
sticky, with their original authored provenance, while composed reads use the
assertion source's current carrying evidence. Public assertions require both
authored and current evidence dependencies to be visible. Current transaction
conflicts compare current source values even when a human axis exists.
Publication version advances to `archive-publication/3`.

The old `TestArchivePrivateAndHumanValueSurviveReextract` asserted the frozen
storage arrangement directly (`active.assertion_kind=user`). It now verifies the
actual contract: source Raw 80, current AssertedValue 75, independent immutable
audit retained, original restricted, and no public observation. The first full
Go run failed only that old storage assertion; the corrected behavior test and
subsequent full run passed.

## Verification

```sh
go test -overlay=/tmp/t09-spec-recheck-new-overlay.json ./internal/store -run '^TestIndependentReview' -count=1 -v
go test ./internal/store -run 'TestArchiveManualAxesDoNotFreezeReextractedSource|TestArchivePrivateAndHumanValueSurviveReextract' -count=1 -v
go test ./...
go test -race ./internal/archive ./internal/archiveworker ./internal/store ./internal/httpapi ./internal/app
GOOS=linux GOARCH=amd64 go build ./...
ATRIUM_T09_NATIVE_IMAGE="$PWD/adapters/macos-archive-parser/evidence/report.png" \
ATRIUM_T09_NATIVE_HELPER=/tmp/atriumos-t09-repair-native/helper \
ATRIUM_T09_CAPTURE=../archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json \
go test ./internal/archiveworker -run '^TestWorkerActualNativeWithRetainedCompletion$' -count=1 -v
```

- `independent-overlay.log`: unchanged reviewer counterexample passes, 0.398s.
- `source-matrix.log`: passes, 0.578s; ten cases combine ownership-only or value
  plus ownership with single source, either source private, or either source
  withdrawn. Tests check latest source storage/extraction, composed values,
  authored audit provenance, current source conflict, and public/authenticated
  reads. The separate existing privacy/value test also passes.
- `full-go.log`: exit 0; store 5.033s, worker 0.667s, app 12.077s. Go reused its
  cache for unchanged packages as shown explicitly in the log.
- `affected-race.log`: all five packages pass, exit 0; archive 47.609s,
  worker 4.100s, store 93.290s, HTTP 54.833s, app 17.509s.
- `linux-build.log`: exit 0, no output.
- `native-replay.log`: pass without skipping, 0.615s; two actual native chunks,
  one entity, one event, seven observations, zero new model calls.

The original reviewer test was not edited; its SHA-256 is
`2559a0d3ec17e7b61c43619b08cf865c73167f014779a30fa8a36e8a0e6bc5e5`.
The helper and committed synthetic PNG are the same verified artifacts described
in the adjacent `2026-10-11-t09-repair` evidence. No production data/configuration,
external message, deployment, or new provider invocation was used. This record
does not claim independent review approval or completion of T10–T18.
