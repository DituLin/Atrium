# Durable archive publication (T09)

`Worker.RunOne` claims only `document.extract`. The default-off `archives.enabled`
service polls once per second when idle; the same background loop drains the
incremental legacy outbox before processing document work. Configure `native_helper_path`, `node_path`, `openclaw_path`,
`worker_plugin_path` and `provider_path` under `archives`; the trusted process
provides `DEEPSEEK_API_KEY`. The understanding adapter is the T07 public OpenClaw
SDK worker, with its existing no-tool/direct-provider boundary, 120-second total
queue-inclusive deadline, concurrency limit and process-group cancellation.
The package-private callback exists only for offline synthetic tests.

## Durable work and fences

The pipeline is verified blob FD → T05 Parse / T06 NativeParser → T07 PlanChunks,
UnderstandChunk, MergeChunks → T08 resolver → store publication. No store import
is introduced into `archive`. Renewal runs during external work; revocation,
lease loss or cancellation cancels the external context. Normal renewal changes
lease version, not StateVersion, and therefore does not invalidate a valid result.

Immutable parse/plan/chunk/publication products carry a digest and fingerprint;
`archive_extraction_products` binds their exact task/key/digest to an immutable
extraction with document/resolution baselines. The fingerprint includes parser,
understanding/schema/prompt resources, resolver, publication/anchor versions,
parse limits and configured executable/plugin/provider content. Same-fingerprint
products may be copied from a published extraction of the same canonical file
into a new source task. Delegations, receipts, budgets and task IDs are not copied.
Each product is at most 32 MiB, each task/fingerprint at most 260 products and
128 MiB total. The task's 64 KiB state contains references and progress only.

Each external call reserves an invocation before leaving the transaction. The
512-unit task budget and two-invocation maximum per chunk operation persist across
restarts; a failed/unknown call is charged. Retry availability is persisted with
one-minute backoff. Only a committed result completes its reservation. A crash
after the external call cannot promise exactly-once provider billing. Schema or
provider failure can consume the one remaining retry, then becomes terminal.
The worker never claims index/NAS tasks.

SaveProductAndCheckpoint and PublishExtraction sample current time after the
write lock and validate current lease token, StateVersion, delegation/principal,
policy and extraction baselines. Publication recomputes the validated T07 closure
and T08 resolution inside the same transaction as entity writes, immutable
observation revisions, active publication, task transition and outbox. A bounded
rebase creates a new extraction baseline without rewriting old extraction data.

## Current facts, identity and privacy

Events use physical parent anchors; observations use physical field slots. CSV
record/column, OOXML part/cell and native geometry remain typed. Native matching
requires the same page/crop/rotation coordinate system, intersection-over-union
at least .90 and a unique bijection. Split/merge or unmatched event partitions
retain the old active publication and ask for review. A new field in an existing
unambiguous event does not create a second event. Two independent text messages
remain two events even if both say 70 ml.

Field subject ownership is independent from event membership. Related doctors,
engineers, teachers and signers are not automatically household subjects. Clear
patient/student substantive text can create a subject without a numeric field;
DOB precision, complete property addresses and supported protected identity
issuer/type/value evidence feed the resolver. Up to eight distinct protected identity
probes per mention are evaluated together; incompatible matches or exclusive-ID
contradictions remain unresolved instead of arbitrarily choosing one identity. Same-source physical confirmed
mentions can reuse current links; field reassignment and unresolved/undone links
cannot silently reconfirm the old owner.

Only complete supported transaction type/issuer/identifier proof can unify events
across documents. Equal amounts alone never deduplicate. Equal proven amounts
share an observation ID but retain each proof's source revision. Conflicting
amounts are immutable alternatives, referenced explicitly by current publications.
Partial re-extraction carries those references; a complete corrected amount
replaces the current conflict reference. Historic alternatives remain auditable.

Manual value/ownership/privacy/withdraw actions require current read+organize,
expected document revision, and a reason. They create independent audit revisions.
`AssertedValue` is user supplied; `Field.Raw` remains the actual original quote.
Manual observation revisions are allocated globally under the transaction lock.
For shared observations, current visible manual assertions take precedence in
revision order, with their actual SourceDocumentID, extraction and publication
provenance. Private assertions do not suppress public source values, and withdrawn
assertion sources no longer supply the current assertion. Re-extraction preserves
local human overrides and cannot copy another proof's private override as its own
source evidence.

Subregion privacy blocks unredacted originals/pages/context immediately while
independent public facts remain eligible. Value, unit/currency dimension and
transaction-proof anchors form visibility dependencies; parser-derived values
retain original/format component dependency records. Privacy keeps historical
anchors so re-parsing cannot reveal an old private region. Entity public labels
from a partly private document are conservatively withheld. Whole-document
privacy and withdrawal suppress its public projection. Both manual privacy and a
new private intake source advance the document fence and enqueue a change.

`CurrentArchiveObservations` is an authenticated read repository, not an anonymous
screen endpoint. It checks current publication references, current confirmed
links, visible conflicts and visible human assertion provenance. Consumers must
deduplicate shared observation IDs, respect AssertedValue/AssertionKind, and keep
historical revisions out of statistics. It returns bounded results (4096 rows,
4 MiB); exceeding the bound is a conflict, never silent pagination/truncation.
T10 still owns unified search/index consumers and screen-specific authorization.

## Typed questions and endpoints

Enabled routes under `/api/v1/integrations/archives`:

- `GET /tasks/{id}/question`: current read permission; versioned question,
  bounded subject/target labels and allowed actions. No lease token.
- `POST /tasks/{id}/answer`: question/version/waiting_task_version/extraction_id/
  document_revision/resolution_key plus typed action and optional target/name.
  `select_subject`, `create_subject` and `keep_current` are checked against current
  question, allowed targets, entity/resolution/document versions and original
  delegation in the answer transaction. Creating a subject records a user
  assertion, not a fabricated source quote. Stale document/entity questions are requeued by bounded worker maintenance
  under the unchanged delegation; committed products generate a fresh version.
  Same-key/same-content replay is
  idempotent; changed content conflicts. Answering does not refresh delegation.
- `POST /documents/{id}/revisions`: document_revision, kind, reason and the
  kind-specific observation_id/value/entity_id. Strict JSON rejects unknown,
  duplicated and null fields. The repository independently rechecks read+organize.

Ambiguous event partitions and uncertain field evidence offer `keep_current`;
manual revisions can correct existing facts. They do not silently accept a new
model partition. Missing subject evidence does not block independent safe fields
or another attachment. Task results retain actual partial/empty/encrypted/invalid/
limit_exceeded parsing status. Generic T01 ResolveWaiting cannot bypass a typed
business question.

## Legacy and downstream outbox

The family_records INSERT trigger creates a legacy outbox row in the record's
transaction. Projection follows the current supersedes chain, retains legacy
record IDs and one stable root event ID; old deliveries cannot roll it back.
Privacy/withdrawal are read from current chain state. T17 still owns full historic
backfill and recovery acceptance. `document_changed(home,doc,revision)` stays
pending for T10. Publishing confirmed facts does not pretend indexing completed.

## Synthetic verification

Tests cover publication rollback, committed checkpoint recovery, unknown paid
calls across actual DB close/reopen, finite budget across restart, revocation
cancellation, independent task/source identities, immutable products/total caps,
rebase, typed/stale/unauthorized questions, manual revisions/privacy, current
identity filtering, physical slot reconciliation, new fields versus partitions,
transactions/alternatives/privacy and legacy out-of-order delivery.

`TestWorkerActualNativeWithRetainedCompletion` is opt-in (see the exact
name in native_replay_test.go): set ATRIUM_T09_NATIVE_IMAGE,
ATRIUM_T09_NATIVE_HELPER and ATRIUM_T09_CAPTURE to the synthetic PNG, compiled
helper and retained T07 compact native-report.validated.json. The test runs the
real native helper and host parsing/manifest validation, checks the captured
chunk identities, then replays the retained validated model responses. The
verified sample produced two chunks, one person, one event and seven observations,
with zero new provider calls. It is not a new OpenClaw/provider execution. T07's
original captures and provider attribution remain unchanged. No real family
archives or the later 42-query acceptance set are included in these tests.
