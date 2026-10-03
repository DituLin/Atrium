# Bounded archive understanding (T07)

This package turns an already authorized immutable `ParseResult` into evidence-bound extraction proposals. It does not open originals, authorize users, resolve family identities, assign durable business IDs, execute document instructions, update a database, send messages, or run the T09 durable consumer. No family profiles or predefined person registry are required.

## API and immutable products

- `PlanChunks(parsed)` snapshots the parser product, creates typed evidence views and an immutable manifest, and preserves parser status, coverage, warnings, sheets and native metadata. Native parser versions include the actual engine fingerprint suffix.
- `UnderstandChunk(ctx, plan, chunk, trustedAdapter)` starts one isolated public OpenClaw CLI process. Every attempt gets a fresh invocation nonce and wire digest. The immutable chunk identity remains unchanged.
- `ValidateChunk(plan, chunk, extractionJSON)` performs strict structural, evidence, normalization, ownership and coverage validation. Invalid output fails the whole chunk. It is useful for replay and testing; it does not attest that inference occurred.
- `MergeChunks(plan, validatedChunks)` verifies the complete manifest, rejects missing/replayed/mutated chunks, keeps conflicting interpretations, and deterministically links evidence-anchored event proposals. There is no second model pass.

`UnderstandingPlan`, `ChunkUnderstanding`, and `Understanding` are versioned immutable products with digests. `CompletionReceipt` records the actual per-attempt identity, requested and response models, provider execution attribution, optional usage and configuration fingerprint. The worker computes the fingerprint from the resources it actually loaded; Go verifies it before accepting the response. Replay never replaces it with a current fingerprint. The receipt's wire digest is also bound to the original chunk plus invocation nonce.

T09 should store these products separately and put only verified product references/digests in `Task.StateJSON` (64 KiB). Do not embed a complete parse, plan, prompt or response in task state. This package introduces no database migration or durable retry/checkpoint policy.

## Evidence and ownership

A view identifies its original parent segment/component, optional visual alternative, origin, Unicode-scalar half-open range, exact text, parse digest and parser version. Typed locators, cell state, hidden/merge flags and presence flags are structural metadata, not quotable measured values. Host-generated quote hints point to exact existing ranges; they do not constrain extraction to a form template, invent text, or repair model quotes.

Spreadsheet components distinguish `cell_text`, `numeric_raw`, `historical_cache`, `formula_text`, `format_code` and `derived_display`. Rounded `Segment.Text` is never represented as raw numeric evidence. A cached formula value is historical, not an executed formula. The raw/cache of the same formula cell is exposed once. Missing, empty and zero remain different. Empty cells receive an owned evidence unit. Formula constants, shared-string indices, boolean encodings and cell addresses are not measurements.

A derived display uses a host-generated `derived_value_ref` with original component, format component, parse digest and parser version dependencies. It never supplies a fake raw quote. Numeric decimal candidates require a complete token and parent boundaries, including across chunk boundaries. Precision is retained in the original raw string. Only the explicit formats `0%` and `0.00%` allow the supported percentage conversion. Full dates must be valid literal or parser-derived `YYYY-MM-DD`; no epoch, year, timezone or currency is guessed. The fictional 1900-02-29 is rejected as a normalized date; valid parser-derived 1904 dates are accepted without reinterpreting serials.

Every owned fragment/empty cell has exactly one coverage disposition. Context views have no ownership or coverage entry. Raw field values must be owned; context can supply a subject, label, unit/currency or report heading. This prevents context copies from becoming duplicate measurements. Native review/unconfirmed states and parser coverage gaps remain host-owned and cannot be cleared by a model.

## Chunk and process bounds

- At most 128 total owned/context views per chunk, 256 chunks per plan.
- Actual serialized evidence JSON at most 24 KiB.
- Complete serialized model input, including trusted prompt/schema and the quoted user message, at most 32 KiB; request JSON also has a 32 KiB cap.
- Worker stdout at most 64 KiB; model text is capped below that to leave envelope space. Token hints are advisory, not the resource boundary.
- Strict UTF-8/Unicode scalars, duplicate-key/unknown-field/null rejection, finite values, bounded arrays/references and depth at most 32.
- Core admits at most two processes concurrently, with a deadline capped at 120 seconds, process-group kill, bounded pipe handling and exactly one `Run`/`Wait` ownership path. The worker also has its own abort deadline and SIGTERM handler. It awaits completion/output and does not call `process.exit`.

Long body/cell text is split without trimming any Unicode characters, whitespace or CRLF. Owned ranges form an exact partition. CSV/XLSX/DOCX rows and geometrically overlapping native lines are packed together when they fit. Geometry is a grouping aid, not a claim of semantic table structure. Oversized structural rows are split with `structural_context_limited` retained; merged results then require review and cannot claim complete understanding. Visual alternative groups remain atomic and fail with a typed limit error if a single group cannot fit.

Capacity is reserved for bounded header and boundary context. Long-parent context uses short exact prefix/suffix ranges instead of copying every fragment of the parent. Only visual alternative context is atomic. This preserves ordinary report subject/label/unit context without raising the wire limit.

## Local subjects, fields, dimensions and events

Subjects are extraction-local mentions with literal names, inferred roles and ambiguity. Patient/doctor, student/teacher and owner/engineer remain distinct. Relative names such as 媽媽/妈妈 stay ambiguous. `field.subject_refs` binds each field to zero or more local subjects; household/unassigned fields remain unassigned. `field.dimensions` binds explicit unit/currency literals and evidence to that individual field. For example, preserve `元` rather than inventing `CNY`.

The local subject representation is not person-only. A property's full address can be a local subject named by its literal address with role `property`; people remain separate subjects with role `owner` or `engineer`. A `full_address` field can reference that property and multiple owners. T08 may resolve role `property` as a property entity and roles such as `owner` as person mentions; these local IDs are never durable entity IDs. See `TestUnderstandingPropertyAndSharedOwners`.

Event proposals have local IDs, types, verified field/subject references and section evidence. The merged event product contains scoped references back to its original proposals and fields. Identical numeric values or names never establish event identity. Transaction/measurement row anchors retain their exact original ranges, so two real 70 ml rows remain two events. A shared exact report heading/section anchor can link owned fields across chunks.

Anchor policy `native-report-heading-anchor/1` is intentionally narrow: a short single-line first native component near the top of a page, one alternative, no inherited review state, and an explicit report-title marker. A valid prefix and a valid whole bilingual heading then identify that same native section. Original quotes remain in the original chunk proposals. General body prefixes, transaction rows, uncertain/conflicting alternatives and different heading/date evidence are not normalized together. Unsupported overlapping report anchors are retained with `overlapping_report_anchors` and review required; this is not a fuzzy quote repair or global identity inference.

## Public OpenClaw boundary

The private plugin is `adapters/openclaw/archive-worker-plugin`, with a public `definePluginEntry`/`api.registerCli` registration, machine-output descriptor, package extension entry and `cliCommands` manifest. It is discoverable in the actual CLI registration mode; there is no full-only gate. Import/registration reads no stdin or credentials and performs no inference.

The fixed invocation is:

```
node /trusted/openclaw/openclaw.mjs --no-color --log-level silent atrium-archive-worker --json
```

Only trusted host configuration supplies those paths, the worker/provider plugin paths and optionally a single `DEEPSEEK_API_KEY`. Each process has an empty isolated home, state, cwd and workspace plus a private generated config. Environment is explicitly whitelisted; inherited OpenClaw overrides, NODE_OPTIONS, proxies, ordinary IM agent directories and full environment files are excluded. Shell environment loading is disabled. macOS may add its own `__CF_USER_TEXT_ENCODING` inside Node; this is not inherited host configuration.

The config allows only this worker and the necessary trusted DeepSeek provider, denies all tools, disables prompt-injection hooks, and permits only `deepseek/deepseek-v4-flash` as model override/completion model. No channels, MCP tools, skills, CodeMode, ToolSearch or agent fallback are configured.

The worker calls the public `api.runtime.llm.complete` directly, omitting `execution`, with one bounded evidence-only user message (`owned`/`context`), a fixed system prompt, purpose, `reasoning: low`, `temperature: 0`, advisory maxTokens and abort signal. Provider route/auth belongs to OpenClaw. There are no internal hashed imports, custom provider clients, legacy ask/agent-exec calls, cross-provider fallbacks, or agent/auth-profile overrides. Responses must attest `direct-provider`, owner `provider/deepseek`, the fixed requested/model pair, one explicitly allowed response alias, and terminal `stop`.

The zero-tool boundary is supported by actual response execution attribution plus inspection of OpenClaw 2026.9.8's direct completion path, which builds only system prompt/messages and passes no tools/context injection. A constant test log or deny-all config alone is not this proof. See [the recorded evidence summary](understanding/evidence/2026-10-04/README.md).

Production failures expose fixed codes only. Stderr is discarded and causes rejection; polluted stdout is never searched for a convenient JSON substring. Evidence, prompts, raw model output, credential values and provider errors are not production logs. Synthetic opt-in tests may explicitly save their bounded synthetic artifacts.

## Verification and evaluation limits

Ordinary tests cover Unicode partitions and actual input sizes, typed cells/cache/display/date cases, partial/fabricated tokens, strict schema and Unicode, unknown references, per-person/per-dimension binding, native uncertainty, exact duplicate handling, repeated real events, row/boundary context, anchors, protocol/route/stop faults, isolation, floods and cancellation.

`TestUnderstandingLivePublicWorker` uses only synthetic health, education, finance and property material, retaining the original bank statement and a separate explicit-binding control. `TestUnderstandingLiveNativeReport` consumes the retained actual T06 synthetic image parser result; it does not replace that evidence with a handwritten transcript. Set the documented `ATRIUM_UNDERSTANDING_*` test environment variables and supply only the provider key via the trusted test launcher. Native replay requires the actual response fingerprint and chunk/request identities to match.

These are small evaluation fixtures, not acceptance of a user's family archive or the 42-query end-to-end acceptance set. Earlier live attempts exposed invalid offsets/schema additions, an unassigned finance field, and context-only extraction; they were rejected, not repaired or silently discarded. The final configured reasoning hint and host quote/context/anchor policies are versioned and tested. Model quality remains fallible; the T09 consumer must handle fixed failures and review states under its own finite budgets. No production deployment or durable consumer is included here.

Final live acceptance is **incomplete**: final education was strictly rejected for unknown schema fields, and the explicit finance control misclassified 元 as unit instead of currency. Health, original finance, property and the actual three-chunk synthetic native report passed. See the evidence summary for configuration hashes, retained outputs, failed attempts and the health harness-only retry.
