# T09 quality repairs — 2026-10-11

Review of `61c2fe9` found three defects: value-only assertions could confirm an
unresolved owner; a changed transaction identifier left an old equivalence active;
and a trusted Homebrew Node symlink was rejected before task claiming. Original
reviewer store and worker overlays were preserved unchanged and reproduced RED
before repair (store 0.302s, worker 0.174s).

## Changed behavior

- Only a distinct ownership assertion bypasses source mention validation. A
  value-only assertion checks current links on its actual source document, even
  when read through another proof. Actual merge/undo quarantine is covered.
- Transaction equality compares type, issuer and identifier, independently of
  evidence locations. Lookup rows require a current, non-withdrawn published
  proof. Exclusive identity corrections retire old lookups atomically. Shared
  observations, human assertions or a trusted replacement amount missing its
  identity require review and retain the old active publication. Uncertain
  coverage continues carrying old references. Joining an existing corrected
  identity removes the old amount's current references while retaining history.
- Trusted configured executable entry points resolve symlinks before content
  hashing. Resource trees still reject symlinks/non-regular files and enforce
  count/byte bounds and opened-file/size checks. Synthetic symlink target changes,
  broken links, tree escape links, oversized artifacts and real Homebrew Node
  fingerprinting are tested.

Publication version is `archive-publication/4`. No production data/configuration,
deployment, new provider call or external message was used.

## Verification commands

```sh
go test ./...
go test -race ./internal/archive ./internal/archiveworker ./internal/store ./internal/httpapi ./internal/app
# After final stale-current-reference cleanup:
go test ./...
go test -race ./internal/archiveworker ./internal/store ./internal/httpapi ./internal/app
GOOS=linux GOARCH=amd64 go build ./...
go test -overlay=/tmp/t09-quality-overlay.json ./internal/store -run '^TestQuality' -count=1 -v
go test -overlay=/tmp/t09-quality-worker-overlay.json ./internal/archiveworker -run '^TestQuality' -count=1 -v
go test -overlay=/tmp/t09-spec-recheck-new-overlay.json ./internal/store -run '^TestIndependentReview' -count=1 -v
ATRIUM_T09_NATIVE_IMAGE="$PWD/adapters/macos-archive-parser/evidence/report.png" \
ATRIUM_T09_NATIVE_HELPER=/tmp/atriumos-t09-repair-native/helper \
ATRIUM_T09_CAPTURE=../archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json \
go test ./internal/archiveworker -run '^TestWorkerActualNativeWithRetainedCompletion$' -count=1 -v
```

The committed regression tests retain the reviewer counterexamples and add actual
source owner validation across documents, withdrawn lookup isolation, shared and
private peer/manual/missing-proof review, corrected identity joining another
existing transaction, and executable path/resource boundaries. The old internal
transaction helper test had no publication at all; it now requires unpublished
lookup rows not to authorize sharing. Real publication integration tests continue
to require same-identity deduplication.

- `final-go.log`: exit 0; store 7.725s, worker 2.185s, archive 9.894s,
  HTTP 6.755s, app 12.248s; unchanged packages may use Go's cache.
- `affected-race.log`: initial five-package pass, including unchanged archive
  52.162s. Final current-reference cleanup is covered by the subsequent
  `final-race.log` for store and its consumers.
- `final-race.log`: exit 0; worker 3.684s, store 96.414s, HTTP 54.599s,
  app 16.888s.
- `final-linux-build.log`: exit 0, no output.
- `store-overlay.log`: three original reviewer cases pass, 0.421s.
- `worker-overlay.log`: synthetic symlink and actual Homebrew Node both pass,
  2.236s; neither test skipped.
- `prior-spec-overlay.log`: previous source-freezing counterexample passes,
  0.364s.
- `native-replay.log`: pass, 0.661s; two real native chunks, one entity, one event,
  seven observations, zero new model calls. This precedes only the final
  transaction-specific current-reference cleanup, which this nontransaction
  report does not execute.

Original reviewer test SHA-256:

- store: `44bf7eb49a55dc5d83c2e2f30b52097ac1db4a982ed6b382c3b50657e7cf4c3b`
- worker: `6e1ba517bb44726c751d77666a5536160534f584ec9773dc16178fd2cd376892`

These results are implementation evidence. Independent quality re-review and
specification regression remain separate; T10–T18 and live deployment are not
claimed complete.
