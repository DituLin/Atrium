# T09 synthetic verification — 2026-10-04

All inputs are synthetic. No production DB, real family archive, new provider
request, deployment or external message was used. The later 42-query acceptance
matrix is not part of these results. These results are implementation evidence;
independent specification and quality review remain separate.

Commands run from the AtriumOS worktree:

```sh
go test ./...
go test -race ./internal/archiveworker ./internal/store ./internal/httpapi ./internal/app
GOOS=linux GOARCH=amd64 go build ./...
# In adapters/openclaw:
node --test *.test.mjs
# Four retained T08 overlays combined into one Replace map:
go test -overlay=/tmp/atriumos-t09-t08-overlays.json ./internal/store -run 'TestIndependentSpec|TestQuality' -count=1
ATRIUM_T09_NATIVE_IMAGE=/tmp/atriumos-t06-fixtures/report.png ATRIUM_T09_NATIVE_HELPER=/tmp/atriumos-quality-review/helper ATRIUM_T09_CAPTURE=../archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json go test ./internal/archiveworker -run TestWorkerActualNative -count=1 -v
```

The native replay ran without skipping: two real helper/host chunks, one entity,
one event, seven observations. Understanding responses came from the retained
T07 capture after manifest validation; this run did not launch OpenClaw or make
new model calls. The original T07 capture and its live-provider attribution were
not modified. Native rectangle/slot correction has separate deterministic store
coverage; it does not represent a second live OCR/model run.

The OpenClaw regression had 55 passing tests, zero failures or skips. Linux
cross-build completed with exit zero. The T08 overlays cover strong negative and
same-batch identity evidence, scoped aliases, merge/undo binding invalidation,
permutation, immutable snapshots, and transaction rollback; all passed.

Selected fault and behavior coverage:

| Test | Actual boundary covered |
| --- | --- |
| WorkerUnknownCallSurvivesDatabaseRestart | Real DB close/reopen after external callback succeeds but product INSERT aborts; paid reservation remains; bounded retry succeeds |
| WorkerCompletedCheckpointRestartsWithoutExternalCall | Product/checkpoint committed, publication outbox INSERT aborts; restart publishes without another callback |
| WorkerCallBudgetRemainsExhaustedAfterRestart | Two failures, restart, third attempt terminates without another external callback |
| WorkerRevocationCancelsExternalCall | Principal revoked during blocked callback; lease renewal cancels context; no observations committed |
| ArchivePublishAtomicAndSameFileSecondSource | Publication rollback removes facts; retry atomic; second source retains own task |
| ArchiveConflictReferencesSurvivePartialAndExpireOnReplacement | Real host Parse/Validate/Merge/Publish under new extraction retains old conflict on uncertain coverage; complete correction removes only current conflict reference |
| ArchiveManualRevisionOfSharedObservationUsesFreshRevision | Two documents read the visible asserted value with actual provenance; private assertion does not affect public source; new proof never borrows private assertion; withdrawal restores valid source reading |
| ArchiveConcurrentTypedAnswersHaveOneWinner | Concurrent different typed answers under one resolution key; one winner and one entity |
| ArchiveStaleQuestionCanRefreshUsingCommittedProducts | Document changes invalidate question; internal maintenance queues same task; committed product rebase generates answerable new version |
| ArchiveNewFieldKeepsEventAndSplitKeepsOldActive | New field reuses event; ambiguous split retains prior active publication |
| ArchiveIdenticalTextMessagesRemainTwoEvents | Two independent identical text messages create two events/observations |
| ArchiveMultipleCompatibleIdentityEvidencePublishesTogether | National ID plus passport publish together with two protected facts |
| ArchiveMultipleIdentitiesPointingAtDifferentPeopleRemainUnresolved | Multiple identifiers matching distinct known people do not merge them |
| ArchiveCurrentViewDoesNotTreatUnresolvedLinkAsConfirmed | Simulated current link needs_resolution excludes confirmed reading while keeping immutable history; actual merge/undo is separately covered by T08 |
| ArchiveLegacyOutboxProjectsCurrentChainDespiteOldDelivery | New/corrected/private/withdrawn legacy chain projected in old/new delivery order without rollback |

Other tests cover physical parent and typed derived dependencies, private dimension
and transaction-proof dependencies, whole versus child privacy, protected leading
zeros, same-source subject reuse without DOB, manual per-field reassignment,
stale/unauthorized questions, source privacy document fences, task kind filtering,
product count/byte-limit rollback and current-only observation reads.

The final small recovery/identity changes were followed by the affected Go and
race runs recorded alongside this file. Earlier full-race results include HTTP
and app packages; final affected-race results cover store and archiveworker.
