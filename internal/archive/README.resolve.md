# Entity resolution (T08)

`ResolveEntities` uses rule `entity-resolution/2` and is a deterministic, database-free Core function. It accepts
validated document-local mentions, each with a literal name, role, explicit
archive-subject classification, identity evidence references, date precision and
OCR/review flags. It does not accept global IDs from model output. A subject is
not an account or a declaration of family membership; no relationship, gender,
permission or account is inferred.

The input boundary follows T07: Core scopes every `ChunkUnderstanding` local
subject and field reference by chunk/extraction, translates each field's
`subject_refs` independently, and retains the original role text. Core must
verify evidence references and identity field semantics against the immutable
validated products before creating `ResolutionInput`. `Subject=false` preserves
related doctors, engineers, teachers and signers in `LocalRoles` without creating
entities or questions. Explicit independent subject evidence can make the same
role an archive subject. Open role text is not a whitelist or an identity proof.
An empty subject set produces `household_document`. Field ownership is preserved
in `ResolutionPlan.Fields`, including multi-owner and unassigned fields; the
resolver never attaches every field to the first person.

`CandidateSnapshot` is a trusted Core result from one household database
transaction, not a model DTO. It includes a household revision, candidate versions
and completeness. Display aliases generate candidates only. Automatic scoped
bindings require `legacy_member` or the exact `document_mention` key
`documentID#mentionRef`. A correction does not authorize a future family-wide
alias rule. New objects use plan-local refs; matching later mentions against them
prevents duplicate same-batch identities. Names use the versioned NFC/Unicode
space/ASCII-fold rule with pinned x/text v0.31.0 (Go 1.24 compatible with the
repository's Go 1.25 directive); punctuation and word boundaries remain.

Birth years/months never establish a match or get completed from another date.
Known components can contradict an otherwise strong identity. Invalid dates and
unsupported precision remain unresolved. Strong match conclusions contain only
entity IDs, local peer references, evidence refs, conflicts and capability flags.
They contain no identity-number plaintext or digest. The store compares supported
national ID/passport/birth certificate values exactly by household, kind, issuer
and rule version, preserving zeros, with at most 1,024 protected facts per
comparison. Unknown kinds, missing issuers, unsupported versions and overflow
return incomplete/unsupported. Those results cannot authorize empty-family
creation. Peer references express equality of incoming protected values without
passing the values to the resolver. The comparison also returns candidate-scoped
negative entity/peer references for unequal national IDs and birth-certificate
IDs within the same issuer and exact rule. They block a contradictory identity
link without making unrelated household candidates a global conflict. Passport
numbers can change at renewal, so unequal passport values are not an exclusive
negative proof under this rule. Both kinds of references are free of raw values
and hashes. Same-batch negative peer references also fence temporary plan IDs.

Before emitting any create decision, the resolver checks the whole connected
strong-identity peer group for date-component, type, OCR, capability and identity
conflicts. Every affected mention stays unresolved; a member without a birthday
cannot hide contradictions between two other members. This aggregation does not
fill any mention's missing birthday. Every successful link updates the temporary
candidate's evidenced dates and adds previously unseen names only as display
aliases, so later same-batch mentions see them without promoting alias authority.

The store's package-private `archiveEntitySnapshotIn`,
`archiveApplyResolutionIn`, `archiveProtectedIdentityPutIn` and related helpers
are intended for T09's single publication transaction. Apply reloads the snapshot
and recomputes the plan, requiring the same revision, versions, decisions and
field ownership before allocating IDs. A stale plan rolls back, including all
new entities. T09 must additionally check lease, current delegation/policy,
StateVersion, extraction and document revision before calling these helpers, and
commit facts, protected identity facts, associations, checkpoint and outbox in the
same transaction. There is deliberately no public standalone auto-create API or
T08 worker/HTTP publication route. External provider calls never occur here.

Public repository candidate reads require current credential/principal and
`archive.read`; human merges, undo and scoped confirmations also require
`archive.organize`, rechecked under the transaction lock. Internal ingest-only
work can use the package-private helpers after its delegation fence and must not
return this internal snapshot in an ingest receipt. Document confirmation records
a binding; T09 owns versioned clarification validation, link revision and task
resumption.

Schema 13 stores original matched IDs and a versioned redirect/history graph.
Merging never rewrites old link/binding targets or merges observation events.
Undo removes that redirect; original links resolve to their original subjects.
New links created while a merge was active retain merge context and become
`needs_resolution` on undo rather than guessing a split. Their exact mention-linked
attributes and protected identity facts stop participating in matching and public
projection; scoped bindings that would silently reattach them are marked conflicting.
Other confirmed subjects in the same document remain usable. Subsequent consumer
revisions must retain these historical IDs and contexts. Canonical lookup is
household scoped, type checked on merge, and rejects cycles. The legacy mapping
is unique on household/member, restricted to the configured `local_household`,
and stable across replay and renaming; it never changes legacy member permissions.
Legacy display attributes are conservatively omitted from the public projection
until a visible document supplies them; legacy fact visibility is T09's importer
responsibility.

Attributes retain document/legacy source and evidence. Public projection uses
only currently visible document evidence, with explicit private attributes still
excluded; merged private names, aliases and birthdays do not become public
labels. Identity-number facts have no ordinary value/hash index or public
projection; identifying a number never changes document TV visibility. This is
not an encryption/key-management subsystem. Exact protected facts remain under
the same scoped database and original-document authorization boundary as T09's
other protected facts.

Tests cover decisions, same-batch identity/alias conflicts, partial dates,
transaction rollback, concurrent stale snapshots, privacy, scoped authorization,
merge/undo, legacy stability and schema upgrades. T08 does not claim the actual
model-to-published-fact loop, event reconciliation, or production deployment;
those remain T09 and later acceptance work.
