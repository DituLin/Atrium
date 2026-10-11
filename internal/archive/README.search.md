# Current archive search (T10)

This component exposes structured, household-wide archive search. It does not
wire a language model into these APIs (T11), compute aggregates (T12), replace the
TV UI (T14), or constitute the 42-query live-model acceptance run (T15).

## APIs

With archives enabled, authenticated integration GET endpoints are:

- `/api/v1/integrations/archives/search`
- `/api/v1/integrations/archives/entities`
- `/api/v1/integrations/archives/documents/{id}/evidence`

Paired screens use the corresponding `/api/v1/archives/...` routes. Screens
always receive the public projection. No query parameter chooses a household,
credential, principal, or privacy projection. The host supplies them. Responses
use the existing integration envelope and `Cache-Control: no-store`.

Search accepts `query`, `entity_id`, `domain`, `document_type`, `date_from`,
`date_to`, `ownership`, `limit`, and `cursor`. Entity search accepts only query,
entity ID and pagination. Evidence accepts only pagination. Unknown or repeated
parameters are rejected. Domains are health/education/finance/property/other/
unknown; document types remain open labels. Ownership is confirmed, household,
waiting, or unassigned. Dates are literal full dates; An explicit household label or household/school/family notice classification
can establish household ownership; missing person attribution alone stays
unassigned. Month/year precision never
silently becomes a day that passes a date range.

Default page size is 10, maximum 25. Responses are checked by serialized JSON
bytes against 30 KiB, leaving room for the existing envelope below 32 KiB.
Evidence pages paginate evidence within one current document. `legacy:{root}`
identifies an existing family-record chain and is also a valid evidence target.
Results explicitly distinguish immutable source excerpts, currently confirmed
observations, unresolved source evidence, and human asserted values. An assertion
carries its audit ID; its original excerpt remains the original value.

## Read and authorization boundary

`DB.InReadTx` uses `BeginTx(ReadOnly:true)`. With the pinned modernc driver this
uses a deferred read transaction rather than the connection's `immediate`
writer lock. Tests hold this snapshot while another connection commits a write.
Current credentials, principals, policy, household membership or screen token
hash/status/local household are checked inside the same read snapshot as the
result. Authorization is valid at that snapshot, not a promise about a later
network delivery instant.

The index is disposable. Its rows carry household data epoch and rebuild
generation. Documents, identity resolution, private regions, human assertions,
source/task state, and legacy revisions invalidate dependent cached results.
Only a row from the current epoch/generation can provide text, classification,
entities, dates or evidence. A withdrawn or private whole document is filtered
before candidate selection. Stale rows can supply none of their old content;
only bounded safe metadata is returned until rebuilt. Hidden documents do not
contribute ranks or visible pending counts. Sorting uses stable document IDs,
with legacy root IDs after normal document IDs, not global BM25 statistics.

Search uses the T09 current observation closure, including independent manual
value/ownership assertions and cross-document provenance. Current publications
may retain observation revisions from older extractions; evidence resolves the
product bound to that observation's extraction, not only the latest extraction.
Evidence retains component kind/ID and OCR alternative ID alongside its locator.
Entity filters use confirmed current associations. A waiting document can be
found through source evidence but its unresolved fields are not promoted to
confirmed facts or entity filters. Legacy projections are checked against the
actual current immutable family-record chain on every read; a newer private,
corrected or retracted record immediately suppresses an older projection.

## Safe text, bounds and coverage

FTS5 receives a quoted, escaped literal phrase through SQL parameters. Operators
such as `NOT`, quotes and `*` have no caller-controlled grammar. Bounded `instr`
and exact substring checks support two-character Chinese names, aliases, and
Chinese substrings that the Unicode tokenizer does not separate. Source evidence
containing the literal query is preferred within a result; document order stays
stable. A literal `NOT` can still match the literal substring in `notice`.

Text comes from validated publication-bound understanding products and the
current observation closure, never an unconditional dump of parsed text. The
supported protected identity field types are national_id, passport, and
birth_certificate. Their known values and identified evidence ranges are excluded
from both projections. For original-context excerpts, an intersecting protected
view is conservatively omitted; independent confirmed fields elsewhere in the
same paragraph remain searchable. This restriction is marked as limited coverage.
This is not a claim of universal PII detection. Original file authorization is
separate and unchanged.

Public source excerpts must also be disjoint from private physical regions;
uncertain physical correspondence excludes the excerpt. Whole-document
classification and entity labels are suppressed when their public provenance
cannot exclude a private child region. Literal text can mention a doctor or
engineer without creating a family entity for them.

Limits are explicit:

- 2-second read deadline, at most 1,000 candidates and 1 Mi Unicode characters per
  search scan, with a signed continuation and incomplete coverage when exhausted.
- At most 64 evidence entries per cached document; fact excerpts are at most 256
  runes and original-context excerpts at most 512, with overlapping windows.
- Cached projection JSON and text are each bounded to 64 KiB. Large entity/date
  lists and entity label pages are truncated explicitly, never marked complete.
- Search returns at most 3 evidence entries per item; use the evidence endpoint
  for its bounded detail pages. It is not a full original download.
- Unprepared indexes, partial parsing, review-required documents and truncation
  keep `coverage.complete=false`. An incomplete zero result means the search is
  incomplete, not that the household has no such documents.

Cursors use the same HMAC-SHA256/base64url envelope pattern and host key as the
existing integration cursor mechanism. They bind current identity/policy,
projection, normalized criteria, data epoch, index generation/revision and last
stable key. Tampering, changed criteria or changed state requires a new search.
Index progress itself changes the index revision, so a cursor cannot skip newly
indexed rows from an earlier key range.

## Index maintenance and recovery

The archive runtime invokes `IndexArchives` in bounded batches. Projection
compilation occurs under a read snapshot, outside the writer lock. The short
write transaction checks the unchanged epoch/generation before committing rows,
FTS entries, index revision and outbox acknowledgments. Concurrent mutations
reject stale compilation. Batches cap at 32 documents and stop after a completed
document when their compilation time budget is reached. Errors leave pending
work; they do not fabricate completion.

`document_changed` delivery is resolved against the current database, not the
old payload. Replayed old outbox rows are acknowledged only when both current
projections exist and their document revision covers the message. Rebuild bumps
the generation, fills new rows in bounded batches, and exposes `indexing` until
ready. It does not delete an index and report a complete empty collection.
No original, immutable extraction, assertion or observation history is deleted.

## Verification scope

`internal/store/archive_search_test.go` contains a reproducible four-domain
fixture with 32 separately asserted structured questions over eight documents,
plus source-range, waiting-owner, alias, multi-subject, partial date, privacy,
withdrawal, current-old-extraction, legacy correction, paging, byte limits,
credential/screen revocation, rebuild, replay and reader/writer concurrency
checks. HTTP tests check route availability and strict input handling.

These are synthetic structured search tests using actual parsing, validated
extractions and publication transactions. No new provider calls are used. They do
not replace the real IM/native-model/TV acceptance dataset, NAS recovery exercise,
or the 10,000-document P50/P95 and long-running production tests assigned to
later tasks. No performance claim for arbitrary 10,000 x 2 MiB inputs is made.
