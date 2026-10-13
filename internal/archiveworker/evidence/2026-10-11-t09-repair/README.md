# T09 specification repairs — 2026-10-11

This run verifies the retained repair of the five independent specification
findings against the earlier `b91c49b` implementation. Inputs are synthetic;
production data/configuration, outbound messages, and new provider calls were not
used. This is implementation verification, not independent review approval or
the later full-system acceptance matrix.

The repair changes same-source replacement and current cross-source conflict
recalculation, composes independent value/ownership assertions with their own
audit provenance, requires both sides of physical partitions to match, and makes
an advertised subject answer executable without accepting an unconfirmed event
partition. Publication version is `archive-publication/2` so cached publication
products cannot silently reuse the old publication semantics.

## Commands and retained results

All commands ran from the AtriumOS worktree. Logs in this directory are from this
run, rather than the earlier October 4 implementation.

```sh
go test ./internal/store -run 'TestArchiveSpecRegression|TestArchiveIndependentAssertions|TestArchivePartitionSubjectAnswer|TestArchiveTransactionCurrentProofSet' -count=1 -v
go test ./...
go test -race ./internal/archive ./internal/archiveworker ./internal/store ./internal/httpapi ./internal/app
GOOS=linux GOARCH=amd64 go build ./...
adapters/macos-archive-parser/build.sh /tmp/atriumos-t09-repair-native/helper
ATRIUM_T09_NATIVE_IMAGE="$PWD/adapters/macos-archive-parser/evidence/report.png" \
ATRIUM_T09_NATIVE_HELPER=/tmp/atriumos-t09-repair-native/helper \
ATRIUM_T09_CAPTURE=../archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json \
go test ./internal/archiveworker -run '^TestWorkerActualNativeWithRetainedCompletion$' -count=1 -v
```

- `repair-regression.log`: pass, 1.362s. The five defect cases, sixteen
  value/ownership ordering and visibility combinations, subject answer followed
  by continued partition review, and three-source replacement/private/withdraw
  cases all ran.
- `full-go.log`: all packages pass; store 6.417s, archive 18.337s,
  archiveworker 0.693s, HTTP 7.530s, app 12.082s.
- `affected-race.log`: all five packages pass, exit 0; archive 47.764s,
  archiveworker 4.156s, store 83.336s, HTTP 55.070s, app 17.275s.
- `linux-build.log` and `native-build.log`: successful exit 0, no output.
- `native-replay.log`: pass, 2.112s; real native parse produced two chunks,
  one entity, one event, seven observations. Manifest exactly matched the
  retained synthetic completion. Zero new provider calls; no skipped test.

Native replay used the committed synthetic image because the original temporary
image and helper no longer existed. Rebuilt helper SHA-256:
`9136336d8cd0775af55271bab206496ba7a3c4e37bd561d210fcb69e1c25e234`.
Committed image SHA-256:
`49d7b1b278b6b7d670344da7ad310b98a19a1c0b87ac270ca986dc0dc9441980`.

## Evidence limits

The original `/tmp/t09-independent-spec-overlay.json` and combined T08 overlay
were absent on resumption. They were not reconstructed or claimed as rerun.
The five retained repository regression tests and all committed T08 entity tests
ran instead. Fresh independent specification review remains required; repository
tests are not a substitute for that review. Unchanged OpenClaw adapter tests were
not repeated for this store/publication repair.
