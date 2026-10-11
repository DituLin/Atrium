# T13 verification — 2026-10-11

All data in this directory is synthetic. No production family database or IM delivery is represented.

| Evidence | Command / boundary | Result |
| --- | --- | --- |
| `go.log` | `go test -p 1 ./...` | PASS (opt-in model test is skipped by default) |
| `node.log` | `node --test adapters/openclaw/*.test.mjs scripts/patch-archive-ingress.test.mjs` | 87 passed, 0 failed |
| `linux.log` | `GOOS=linux GOARCH=amd64 go build ./...` | Exit 0; empty compiler output |
| `race.log` | `go test -race ./internal/brain ./internal/brainhost ./internal/archivespool ./internal/store` | PASS; store 177.833 seconds |
| `core-cli.log` | `go test ./internal/store -run '^TestArchiveIngressRealCoreAndHostInventory$' -count=1 -v` | Real synthetic TLS Core; actual built CLI/MCP inventory startup |
| `weixin-native.log` | `node --experimental-vm-modules scripts/check-archive-native-monitor.mjs WEIXIN_PACKAGE` | Actual transformed installed monitor; transport, admission and ordinary dispatch are doubles |
| `discord-native.log` | `node scripts/check-archive-native-discord.mjs DISCORD_PACKAGE` | Actual installed mapper/dispatcher/SDK lifecycle; preflight, ingress and model are doubles |
| `private-normal-model.json` | Opt-in `TestArchiveIngressModelIntentLive/private` | Actual OpenClaw/DeepSeek normal tool round chose private; Core saved |
| `inherit-normal-model.json` | Opt-in `TestArchiveIngressModelIntentLive/inherit` | Actual OpenClaw/DeepSeek normal tool round chose inherit; Core saved |

Native tests read installed Weixin 2.4.9 / Discord 2026.9.8 and patch only isolated copies. The pins file records exact source/runtime hashes. Node validation used 24.21.0. The model test uses a synthetic native-context wrapper and a synthetic report, with real TLS Core and Brain/MCP binaries. Its two captions are retained in the test source. Historical model responses are retained unchanged, including the older extra generic screen-boundary sentence; the final renderer suppresses that unrelated sentence for archive-only receipts.

The model opt-in command requires `ATRIUM_INGRESS_MODEL_LIVE=1`, `ATRIUM_DEEPSEEK_PLUGIN` pointing to the installed provider package, `ATRIUM_INGRESS_EVIDENCE` pointing to a private output directory, and `DEEPSEEK_API_KEY` supplied only via child environment. No key, environment dump or real configuration is included in evidence. The captured model test completed in 13.85 seconds with both cases passing; it is not reissued as part of ordinary CI.

A real source lookup recovering a lost successful response and a consumed native turn recovering uploads are separate tests. Neither fixture alone is claimed as an actual user IM transport acceptance test. Production enablement and new user-sent Weixin/Discord messages remain rollout work.

## Independent requirement review follow-up

Two queue starvation defects were reproduced before repair: the download window counted old staged/clarification entries, and the delivery window limited all accounts before filtering the requested account. Both original reviewer scripts were run unchanged before and after repair. `/tmp/t13-spec-starvation.mjs` changed from `downloaded:0` / assertion failure to `downloaded:1,last:staged,pendingCount:0`; `/tmp/t13-spec-deliveries.mjs` changed from `accountB:0` / assertion failure to `accountB:1,totalPending:257`.

The repository regressions first failed both cases, then passed after SQL filtering moved before bounded selection. A 136-message mixed download queue covers 32 staged, 32 future retry, 32 exhausted and 40 due inputs across successive windows. A 514-message delivery queue covers separate accounts and channels before the 256-message limit. Full Node suite now passes 89/89 (`review-node.log`). Both actual native harnesses pass (`review-*-native.log`), and the real TLS Core/CLI test passes (`review-core-cli.log`). This repair changes only inbox SQL selection and tests; Go/runtime patch code and model semantics are unchanged, so previous race/Linux/model evidence remains applicable without another model request.
