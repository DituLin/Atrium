# Synthetic public-worker evaluation, 2026-10-04

Overall live acceptance is **incomplete**. This is evidence for independent T07 review, not acceptance of the family archive or the 42-query system tests.

Final configuration fingerprint: `0fd2243da057e295d4b430caeadf7b4aca4f6d8e07dfc194ca1083e7cbcac72b`. Every saved response contains the worker's actual loaded-resource fingerprint, per-attempt invocation/wire digest, provider/model/response alias, stop reason, execution attribution and optional usage. The manifest hashes the unchanged prompt/schema and capture files. No earlier response was relabelled with this fingerprint.

The final public worker used OpenClaw 2026.9.8, public `runtime.llm.complete`, `deepseek/deepseek-v4-flash`, reasoning low, temperature 0, maxTokens 12000, and one evidence-only owned/context message. The model controls are advisory; hard byte/process limits remain enforced. No provider-native JSON constraint was requested; actual DeepSeek support for the public responseFormat option remains an investigation for a subsequent fix.

| Synthetic case | Final outcome |
| --- | --- |
| Health | PASS after a harness-only retry: 145.6 g/L, 2026-10-04, patient/doctor roles, patient field binding, one report and complete coverage. Initial batch completed the worker but failed writing evidence because the destination directory had not yet been created. No semantic result was accepted from that first attempt. Test setup now creates the directory before inference. |
| Education | Strict rejection `understand_worker_schema_unknown`; no product. No retry after model failure. |
| Original ordinary bank statement | PASS: 130.50, literal currency 元, 2026-10-01, account_holder/payee roles, amount bound to holder, one transaction and complete coverage. |
| Explicit-binding bank statement control | Schema/evidence validation succeeded, evaluation FAILED: model classified 元 as `unit` rather than `currency`. Exact amount/date, holder binding and one transaction were present. The generic test message says missing field/event; this inspection records the precise dimension-classification error. |
| Property | PASS: 88.75 平方米, 2026-09-30, owner/engineer roles, one report and complete coverage. Area is permitted to remain unassigned; this fixture does not prove automatic property identity resolution. |
| Actual T06 synthetic image parse | PASS: retained 19 native segments, 3 chunks, all strict validations, one merged report. Delta -2.75 cites exact 变化 Delta label, unit mmol/L and patient subject reference. Original heading-prefix/full-heading quotes are retained under the narrow host anchor policy. |

The final batch log and separate health harness-retry log are retained. Raw successful worker envelopes are bounded synthetic outputs, not production logs. Rejected schema output is intentionally not captured: the production boundary returns only a fixed code. Validated products exist only for passing evaluators. The control envelope is deliberately retained as failed evaluation evidence.

Earlier configurations exposed invalid offsets/quotes, unknown fields, missing finance subject assignment and context-only raw extraction. They were strictly rejected. Host quote hints, field bindings, dimensions, structural context, and evidence anchors addressed concrete failures. An earlier reasoning-low image run validated all three chunks but yielded two heading anchors; deterministic narrow anchor replay linked them. None of those earlier successes is claimed as final-configuration acceptance. The final batch above is the current evidence.

## Zero-tool boundary

Actual envelopes attest execution mode `direct-provider`, owner kind `provider`, ID `deepseek`, requested model `deepseek/deepseek-v4-flash`, model `deepseek-v4-flash`, response alias `deepseek-flash`, and terminal `stop`. Route checks reject mismatches. This attestation is combined with read-only inspection of the installed OpenClaw direct completion source:

- `runtime-llm.runtime-N0uhYzFi.mjs`, SHA256 `ae41d1e542c2f159f27c56de3e66e63672924ec0689b94fef78fb277aaff5cf1`, lines 591–615 constructs only systemPrompt/messages and passes that context directly to simple completion; lines 625–639 retain actual provider stop/model/execution attribution.
- `simple-completion-execution-DrTniyOr.mjs`, SHA256 `a742e0e23d0f3d0ffe7e6c0d98abfae37f6d70332915d59bcc07ed3404c62924`, line 52 forwards that context to completeSimple.

These files were inspected, never imported by the worker. No tools/context injection are attached to this path. The private generated config additionally denies all tools and allowlists only the worker and provider. This is path/route proof, not an invented per-call tool trace.

## Reproduction and verification

Use a trusted host to provide the single DEEPSEEK_API_KEY in the environment; never read ordinary agent configuration into the worker. Set ATRIUM_UNDERSTANDING_LIVE=1, ATRIUM_DEEPSEEK_PLUGIN to the trusted installed provider path, ATRIUM_SYNTHETIC_EVIDENCE to an empty output directory, and ATRIUM_UNDERSTANDING_NATIVE_REPORT to this directory's synthetic-native-report.parse.json. Run `go test ./internal/archive -run TestUnderstandingLive -count=1 -v`. A new run is another model sample and can fail; this evidence is not a deterministic quality guarantee.

For offline native replay set ATRIUM_SYNTHETIC_EVIDENCE to this directory and ATRIUM_UNDERSTANDING_NATIVE_REPORT to synthetic-native-report.parse.json, then run `go test ./internal/archive -run TestUnderstandingNativeReportReplay -count=1`. Replay verifies actual fingerprint and request identities before validating/merging; it does not call the provider.

Fresh final code checks: `go test ./...`, `go test -race ./internal/archive`, Linux amd64 archive test-binary compilation, four Node worker tests, and retained T06 native context/conflict handoff regressions passed. No production deployment, database update or durable consumer was performed.
