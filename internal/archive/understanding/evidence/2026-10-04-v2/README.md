# T07 v2 review fixes and synthetic evaluation

This directory retains bounded synthetic evidence for the review fixes. Original v1 evidence in `../2026-10-04` is unchanged. No output was supplied with missing labels or relabelled to claim another configuration generated it.

## Verified implementation defects

The unchanged independent overlay reproduced three failures before changes: a date normalized from a different attached quote, the same original anchor range duplicated through owned/context view aliases, and rejected open labels. All three now pass unchanged. Added literal/partial/derived date cross-binding cases, native valid-date cases, canonical overlap and distinct parse/alternative/origin/component/range negatives, label bounds/provenance, and a separate bill section counterexample. Existing repeated 70 ml rows and distinct reports remain separate.

`archive-understand/2` / `archive-extraction/2` require open labels and retain their per-chunk provenance. `exact-parent-evidence/2` uses original evidence provenance for both exact identity and overlap; original proposal references remain available. The native heading expansion policy remains narrow and unchanged.

## Diagnosis and bounded attempts

Three synthetic-only public CLI diagnostics with the former v1 prompt/schema and education evidence produced no unknown-field violations. This finite diagnostic limit was exhausted; the original intermittent unknown-field subtype remains unreproduced. `v1-diagnostics` contains structural diagnostics and direct-provider metadata only, not raw model values. No production diagnostic/logging path was added. It would be incorrect to attribute that earlier failure to a particular unknown property.

V2 adds an exhaustive output-key guide and an explicit distinction between money currency and measurement unit. Public `responseFormat: {type: "json_object"}` is a JSON syntax control, not schema enforcement. Unknown fields and bad evidence still fail the entire chunk. Temperature remains advisory, particularly with thinking enabled, and no deterministic-generation claim is made.

- `thinking-off`: all five text cases passed; native chunk 1 was rejected for coverage. It marked unused headers/labels extracted and attempted context values as fields. No rejected product was accepted. This sample does not prove thinking mode caused the failure.
- `thinking-low`: all five text cases passed and all four native chunks passed strict validation, but the native evaluator rejected a separate fee event in addition to the report. All expected raw values/dimensions and patient/doctor roles were present. It was not a canonical-anchor failure. These captures retain the pre-grouping prompt and their original configuration fingerprints.
- `section-guidance`: all five text cases passed, but native chunk 1 classified context-only values as owned coverage and was rejected. This longer prompt also caused the long Unicode/long-cell fixtures to exceed the chunk-count limit; that regression was reproduced and fixed by compacting equivalent instructions and sharing local schema definitions, not increasing budgets.
- `compact`: all five text cases passed under one final configuration; both actual native chunks passed strict validation with all expected values/dimensions/roles. The first host merge produced two reports because one anchor cited the title and the other the same title plus its report date. The retained live log records that failure. A narrow host section policy then made replay of these UNCHANGED actual envelopes pass the complete native evaluator, with one report. No further model call, prompt change or resource-fingerprint relabelling occurred after this capture.

The original image was visually inspected: one health-report title, a common name/birth/report-date header, and a single item/value/unit table containing hemoglobin, fee and Delta. There is no independent bill number, transaction date or payment section. The final guidance therefore treats currency as a field dimension rather than an event boundary; it explicitly preserves separate bills, transactions, multiple reports and repeated genuine rows. This is document-section semantics, not a general single-event restriction.

## Public route and controls

All calls use the actual private public-CLI worker and `api.runtime.llm.complete`, with the fixed DeepSeek flash model and direct-provider owner attribution. Tools are not attached; no internal runtime import, custom client, agent fallback or transport replacement was introduced. The v1 source-path evidence for zero tools still applies.

Read-only installed transport inspection additionally found:

- `openai-completions-compat-BJf57ZxN.mjs`, SHA256 `86c0d4b1cdda34af10a9331d16687f042c4ad61d2fc8d92d526cf69a29453178`, lines 29–31 passes provider-shaped json_object through. This does not establish json_schema support for DeepSeek.
- `openai-completions-stream-dju_5F3P.mjs`, SHA256 `590082faab7127b061cd23bacf491d77fba1691ac0d8bd73df42c4b2179a62fc`, lines 651–666 maps the DeepSeek thinking flag. The public SDK documents reasoning off and direct responseFormat; these private files are inspected only, never imported.

Worker receipts retain actual loaded-resource fingerprints and invocation identities. Manifests distinguish prompts/settings for every captured batch. The old prompt is retained as `pre-grouping-prompt-v2.txt`; the first three batches share `inline-schema-v2.json`, while compact uses its own new shared-definitions schema snapshot. Replay with a different active prompt/config must reject rather than rewrite history. No family archive, durable consumer or 42-query system acceptance is claimed.

## Final compact result and replay

Final actual configuration fingerprint: `88ed68580fe3ceaf4ee700fc3118e26955fbf465136fb469adc341ab7b3ad961`. The compact manifest contains actual prompt/schema hashes and per-file hashes. Each worker envelope retains its original invocation/wire digest, route, model aliases, stop reason and usage. Native responses cite title-only versus title+date; original proposals remain present in the validated product.

Host policy `native-report-header-section/1` requires whole-page counts proving one short native report title and one explicit supported report-date line, both atomic and without inherited uncertainty, within the same header region. It normalizes their verified subsets to one host section anchor, never arbitrary overlapping evidence. Same-page repeated/different report titles, multiple dates, date alternatives/review, birth-date/arbitrary-ISO lines, different pages, outside-header dates and extra birth evidence remain separate/review in regression tests. Generic exact-parent anchors retain their existing semantics.

The strict replay checks 1988-03-12 and 2026-10-04, 145.6 g/L, 1234.50 CNY, -2.75 with its exact Delta label and mmol/L, patient/doctor roles, patient field bindings, all expected fields linked to the one report, and complete owned coverage. This is the final finite synthetic evidence, not proof that stochastic schema failures are eliminated or acceptance of a real family's system.

For independent replay, COPY `compact` into a temporary directory to preserve captures, set `ATRIUM_SYNTHETIC_EVIDENCE` to that copy and `ATRIUM_UNDERSTANDING_NATIVE_REPORT` to its `synthetic-native-report.parse.json`, then run `go test ./internal/archive -run TestUnderstandingNativeReportReplay -count=1 -v`. No provider credential or network call is required. The current host rejects previous-configuration envelopes rather than supplying missing labels or altering their fingerprints.

The overlap scan also had a confirmed bounded-input performance defect: 5,000 disjoint events exceeded a two-second test limit with pairwise event/anchor comparisons. The replacement groups canonical component ranges and sorts/sweeps them in O(R log R), checking only different merged events; the same 5,000-event test completes below 0.01 seconds on this host. Internal same-event overlap, touching ranges and different alternatives do not trigger review.

Final verification passed: full `go test ./...`; `go test -race ./internal/archive` (44.050s); Linux amd64 archive test-binary compilation; six Node tests; unchanged independent three-probe overlay; retained native context/conflict handoff; strict saved compact native replay. Independent root replay from a temporary copy also passed. Production and root-owned status/design documents were not changed.
