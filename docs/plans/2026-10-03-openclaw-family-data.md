# OpenClaw family data implementation plan

**Goal:** IM → OpenClaw → Atrium durable family records → NAS and TV charts, with OpenClaw also able to query the records.

**Architecture:** Extend the existing authenticated Core/Brain/MCP plugin path. Keep SQLite local; use a durable export queue to publish structured per-member records to an explicitly configured NAS directory. Preserve existing television styling and remote navigation, using rounded iOS-like line icons and SVG charts compatible with Chrome 66.

**Tech Stack:** Go, SQLite, OpenClaw plugin/JSONL/MCP, React/TypeScript, SMB-mounted NAS.

## Evidence and current state

- Branch `codex/openclaw-family-data`, starting from `7ad4524`.
- Current integration has ten model-facing tools, mainly device and photo queries/control; no health record storage.
- Current integration entrypoint is a separate local CLI configuration, not the main IM agent.
- OpenClaw before upgrade: 2026.5.20; official updater selected stable 2026.9.8 on 2026-10-03.
- Photo source roots are read-only by design. Family exports need a separate writable directory, not a change to photo-source access.

## Data design

Members have stable IDs and names. Records have a stable caller request ID, member, category (growth, health, checkup, meal, note), effective timestamp, title, notes, and typed measurements (metric, numeric value, unit). Source identifies the trusted integration principal and IM event, not model-provided filesystem paths. Explicit display visibility controls what can appear on the living-room TV. No invented measurements or medical diagnosis.

Writes and a NAS export queue entry commit atomically. Replaying the same request and payload returns the same record; conflicting reuse fails. Revisions/corrections remain traceable. NAS unavailable means local durable record plus pending export, never a false NAS success. The archive is structured JSON grouped by member/date with a documented schema and a recovery/import path.

## Implementation and verification sequence

1. Back up private OpenClaw configuration; update stable version; verify gateway, DeepSeek and plugin compatibility. Keep credentials out of repo and logs.
2. Add domain types and validation, SQLite migration, member/record repositories with stable idempotency and an atomic export queue. Test replay conflicts, member isolation, time ordering, visibility and persistence across reopen.
3. Add authenticated integration and TV endpoints. Extend integration permissions and member allowlists, validate payloads and scope on both reads and writes, and test forbidden access/oversized input.
4. Implement NAS exporter with mount/identity validation, private file modes, atomic publication, retry and recovery. Test disconnect, duplicate publish, corruption and restart.
5. Extend MCP, Brain allowlists/runtime/reports and plugin tool manifest together. Test real read/write flow, trusted identity and deterministic acknowledgements. Remove obsolete hard-coded ten-tool assumptions coherently.
6. Bind the existing IM channel to the appropriate Atrium agent without disrupting other agents. Write agent instructions for member selection, units/time, ambiguity, recording versus plans, and query-before-summary.
7. Add TV family records view: member and category selection, growth/health trend charts, checkup history and meal plans, empty/loading/stale/error and NAS-pending states. Preserve existing design and D-pad navigation. Verify rendered screens and interaction at TV resolution.
8. Build and deploy with backup/rollback; verify synthetic records through OpenClaw → Core → NAS → TV and query them back. Remove validation fixtures. Document exact commands and limitations. Never claim IM or device verification based only on unit tests.

## Completion gates

Latest stable OpenClaw running; DeepSeek working; IM input routed and usable; growing family records persist and can be queried; archive actually exists on NAS; TV shows the same records and charts; tests and rendered/device checks cover the full path. This plan is not completion evidence.

## Progress — 2026-10-03, first implementation turn

- Created requested feature branch and backed up private OpenClaw configurations under `~/Atrium/rollback-openclaw-20261003-165138` (never commit that directory).
- Added family domain types, validation, migration 0007, member/record repository and atomic pending-export rows. IM identity remains a caller-owned parameter; HTTP authorization and IM binding are still TODO, so this repository is not yet exposed.
- Store/domain tests including race detector pass; existing HTTP/Brain/MCP/Brainhost tests pass; OpenClaw adapter unit tests: 44 passed.
- Existing migration tests updated to expect schema 7 while retaining data preservation assertions.
- Installed Node 24.21.0 and OpenClaw 2026.9.8. The old updater failed first on Node 23, then on an esbuild hardlink during staged swap; direct npm installation succeeded. Configuration/state migration and plugin convergence require follow-up verification.
- Actual main config currently has Discord and openclaw-weixin channels, main agent on DeepSeek, no agent bindings. Do not assume the old Feishu channel still exists.
- NAS SMB share currently mounted at `/Volumes/home`; no family archive directory has been created and no personal records imported yet.
- Remaining: authorized API/member policy, NAS exporter/recovery, Brain/MCP/plugin tools, real IM routing, TV screen/charts, deployment and full-path acceptance. Existing synthetic test data is isolated in temporary databases.
- Post-update repair completed. Gateway reinstalled explicitly on `/opt/homebrew/opt/node@24/bin/node`; gateway version 2026.9.8 and connectivity probe verified. State/config environment points to the real `.openclaw.pre-migration` directory because newer atomic writes reject the `.openclaw` symlink parent. Default CLI paths refer to the same file via symlink; a textual mismatch warning is expected unless using the same environment.
- New OpenClaw CLI refuses `models status --probe` while its gateway is running. Final model probe is performed with a deliberate brief stop and guaranteed restart; do not interpret `{ok:false}` JSON with exit 0 as a passing probe.
- Final DeepSeek probe resolved the correct default model but returned `status: unknown` (latency 16701 ms), so model connectivity remains UNVERIFIED and must be diagnosed next. Gateway was restarted after the probe. Upgrade installation is done; operational acceptance is not yet complete.

## Progress — second implementation turn

- Added family.read/family.write permissions and explicit member allowlists, including reference validation and CLI --members flags.
- Added admin member provisioning, integration record reads/writes and TV-visible projection endpoints. Tests verify private records stay off TV, missing scope/member is denied, forged principal fields fail, repeated writes return the same result and conflicting replays return 409.
- Added family_archive configuration and runtime worker. The worker publishes per-member/month JSON with archive identity and network filesystem checks; pending state survives outage or conflict. Root directories/identity are provisioned explicitly, separate from photo roots.
- Archive tests verify exact retry after a publication-before-ack crash, invalid identity, local filesystem denial, corrupted existing file and symlink escape denial. Config validation rejects overlapping writable/read-only roots.
- Direct DeepSeek API check with existing credential returned HTTP 200 in 0.7s. This proves service/credential reachability, not yet the full OpenClaw agent path.
- Added API schemas and docs/ops/family-records.md. No production deployment or synthetic insertion into the household database yet.
- Actual configured OpenClaw Gateway agent turn succeeded: status=ok, final=OK, provider=deepseek, requested model=deepseek-v4-flash, response model=deepseek-flash, duration 1485ms; no tools and no --deliver. This supersedes the earlier inconclusive standalone probe. Isolated exec ignored the installed provider configuration and reported unknown model, so it is not the acceptance path for this installation.
- `go test ./...` passed across all Go packages after API, archive worker, configuration and schema changes. `go test -race ./internal/familyarchive` passed. No claim of production NAS/IM/TV end-to-end completion.

## Progress — third implementation turn

- Added three MCP/model tools: home_list_family_members, home_list_family_records, home_record_family_entry. MCP inventory is 14 including host recovery; model/plugin inventory is 13. Host owns request_id, stripped from model schemas; plugin rejects injected identity fields.
- Record writes do not use the screen action pipeline. Host turn/call identity creates the request ID, so same call replay is idempotent and changed content conflicts. This is not cross-run IM deduplication; ensure channel inbound replay semantics before claiming IM completion.
- Real Core + authenticated HTTP + MCP + Brain test verifies member query, write, repeated write, conflict, record query and rejected request-ID injection. Synthetic data only in test databases.
- Added deterministic family receipts, including record content and pending/synced NAS distinction. Receipt validator matches member/content/time to the requested write and rejects an unproven synced claim.
- Added stable cursor pagination across store, API, MCP and plugin. Cursor binds member/projection; authorization independently rechecks each page.
- Loaded frontend-design skill and inspected existing TopNav/router/House layout. TV family screen implementation and visual checks are still outstanding.

## Progress — TV family surface and navigation

- Implemented family members/categories, actual-record trend charts grouped by metric and unit, history pagination, explicit NAS sync state, and empty/failure views. Added home menu and top navigation entry with rounded line icons and the existing paper/celadon design.
- Member switches and authorization revocation remove previously displayed records. Polling pauses during history browsing, and late poll responses cannot overwrite loaded history; regression test covers the race.
- Updated remote navigation tests for the new entry. Frontend suite passed 62 files / 444 tests before adding the family remote-command test; that additional command test then passed. Typecheck and production web build passed.
- Browser verification with synthetic API fixtures at 1920x1080 and 804x384 completed without page errors. Fixed white text inherited by sidebar controls. Screenshots saved at /tmp/atrium-family-1920.png and /tmp/atrium-family-804.png. Actual TV-device verification remains outstanding.
- Enabled `home_navigate_screen` route `family` across Brain validation, MCP schema, Core route validation, frontend command handling, and API schema. Collection arguments remain invalid for this route. Brain/domain/screen/MCP/HTTP test suites passed; additional Core family navigation cases passed.
- U-disk side request: macOS diskutil and USB inventory did not detect an external disk, so no format/copy occurred. Existing release APK is Atrium TV 0.2.1. Await device detection; never erase an internal disk.
- Remaining acceptance: production family archive provisioning/config, deployed OpenClaw tools and real model tool path, current IM channel routing/dedup, actual NAS export, TV deployment and complete record/read/display verification. No real household records have been fabricated or inserted.

## Progress — real runtime compatibility and first deployment

- Found and fixed overlooked 10-tool guard in cmd/atrium-brain-host and stale plugin manifest. Actual --tools-json now succeeds with 13 tools; added manifest inventory regression test.
- OpenClaw 2026.9.8 removed before_agent_start: migrated to before_agent_run with the same fail-closed identity binding. Real DeepSeek tool lifecycle succeeded using trusted run ID in context.
- Created private independent local-test state/config under ~/Atrium/family-staging; migrated agent/workspace with OpenClaw agents add and doctor. Existing IM Gateway remained running. Global and agent tool allowlists updated to 13. Disabled codeMode/toolSearch wrappers in the dedicated config to preserve exact toolSummary-to-receipt count validation. Real ask status and family member/record queries both completed failed=false; family query returned 阿布 and no records.
- Full Go tests passed; plugin tests passed before deployment. New signed Core/Web deployed to ~/Atrium/atrium after original binary/config and SQLite-consistent backup (integrity_check=ok) at ~/Atrium/rollback-family-20261003. Service running and HTTPS root/diagnostics returned successfully after startup.
- Provisioned real member abu (阿布), added family permissions/member allowlist to existing principal. No synthetic records inserted (production family_records=0).
- Provisioned NAS /Volumes/home/AtriumFamily with unique marker and enabled production family_archive with allow_local=false. Actual export remains unverified until isolated synthetic acceptance or a real user record is available.
- Asked user whether IM integration should target both existing Weixin and Discord or one only. Still pending. No outbound IM message sent.
- Remaining: isolated real-model write→NAS verification, production runtime/ask promotion, principal expiry renewal (2026-10-07), IM binding and inbound replay handling, actual TV/device verification, correction/recovery planned work, full acceptance audit. Goal remains active.

- Promoted the updated binaries/plugin/tool inventory/config to the installed ~/Atrium/brain/ask entry, preserving original files under rollback-family-20261003/brain. It explicitly uses the independent family-staging/state runtime and the tested workspace/agent paths. The directory remains named family-staging and must not be removed as disposable files. Installed entry acceptance is recorded below.
- Installed `~/Atrium/brain/ask` returned exit 0 for a real DeepSeek authorized-member query, with deterministic receipt listing 阿布（abu）. Plugin regression suite: 46 passed. Production core remains running; no family records inserted.

## Progress — real write/archive/display and IM gateway setup

- Isolated same-binary Core acceptance at localhost:9443, private data under ~/Atrium/family-acceptance and separate SMB root /Volumes/home/AtriumFamilyAcceptance-20261003. Real DeepSeek wrote one explicitly synthetic fixture_baby growth record. ask failed=false, pending receipt; actual NAS JSON verified matching title/time/70 cm/8 kg. No production family record inserted.
- Real paired browser (no API mocks) read the record, rendered two single-point charts and NAS-synced state; remote navigation returned applied. 1920/804 screenshots and logs retained in acceptance folder. First browser attempt used an incorrect empty-home selector; corrected to wait for pairing completion, then passed.
- Upgraded pinned Weixin provider 2.4.3→2.4.9 through official OpenClaw update, removing plugin SDK import error. Gateway hot reload completed. Current Weixin running=true but lifecycle starting; Discord connected=true, direct probe failed ECONNRESET. Actual channel transport acceptance still unproven.
- Added Atrium plugin to existing main agent through alsoAllow without altering channel authorization/routing. Main workspace instructions route structured family requests to Atrium and distinguish NAS pending/synced and TV visibility. Gateway DeepSeek query returned real member abu; gateway screen command reached applied in Core (model initially stopped at accepted, instructions amended to query original command).
- Created new scoped principal openclaw-family-gateway expiring 2027-10-03 with same screen/source permissions plus abu family access. Installed maintainer ask and gateway use it; old principal/credential preserved for old ledger recovery and expires 2026-10-07. Credentials remain local private files, no values logged.
- Android OnePlus test device had old page in memory: initial family command failed invalid_route. Restarted app (no reinstall/reset), next command applied and screenshot shows 阿布 empty family page. Real TV hardware is not connected, so do not claim TV-manufacturer-specific acceptance.
- Requested user to send a read-only IM query for actual ingress acceptance; no outbound IM sends performed. Need durable inbound replay verification, corrections/retraction and archive restore/import per plan, plus completion audit. Goal active.

## Progress — NAS family-only recovery

- Added `atrium family restore`: validates identity/network archive, strict bounded JSON, exact member/date/id paths, explicit member mappings, and rejects symlinks. Creates a new local database outside the archive via temporary DB + checkpoint + no-replace publication; never modifies archive or existing output.
- Store recovery preserves record IDs, payload, visibility and creation/effective times; verified archive time becomes sync time. Restored provenance is archive identity, not a fabricated original IM principal/request. Full Core state and IM replay keys are not recoverable from current archive schema; documented explicitly.
- Regression tests cover successful roundtrip and unchanged source, existing output rejection, wrong marker/local volume, missing member, corrupt/moved/symlink/unknown-field/trailing-JSON documents, cancellation and output under archive. Relevant archive/store/CLI suites pass.
- Real SMB recovery of prior synthetic model-written record succeeded, output integrity_check=ok, one record and one synced acknowledgement; second restore to same output rejected. Installed signed ~/Atrium/atrium-maintenance without restarting or replacing the running Core.
- Inbound replay audit found OpenClaw's dispatch cache keyed on MessageSid with 20-minute TTL, while Weixin 2.4.9 generates fresh MessageSid and places provider ID in MessageSidFull. Durable sourceTurnId paths exist but do not yet prove permanent replay exclusion for this tool. Need trusted canonical-message binding/claim in family write path, with tests across new model runs and restart, before claiming replay-safe IM completion.
- Still pending: traceable corrections/retraction, durable IM replay binding and actual IM ingress confirmation, final review/verification. Goal active; no repeated blocker audit (this turn changed code and verified real recovery).

## Progress — durable canonical IM admission

- Added private host `ingress` RPC and scoped Brain `brain_inbound_messages` table. Trusted canonical channel/account/conversation/sender/message identity is hashed, committed before model dispatch, and retained without TTL. Concurrent/restarted runtimes reject the same claim; raw message bodies and identifiers are not persisted.
- Plugin before_dispatch handles only configured-agent Discord/Weixin sessions. Requires matching canonical identity, rejects temporary Weixin fallback IDs and non-native Discord IDs, and fails closed on unavailable ledger. Does not inspect message text or expose identity fields as model tools.
- Duplicate returns a neutral previously-received/unconfirmed outcome message and does not launch another model run. Crash after claim intentionally requires a new query to verify outcome; it never auto-replays writes. Scope includes all configured-agent IM messages, not guessed family keywords. Documented limits for new principal/ledger and NAS-only restore.
- Go Brain/brainhost race suites passed; plugin suite 48 passed. Actual JS plugin→JSONL→Go host test with synthetic trusted contexts verified first admission, replay rejection, and rejection after host restart, with no IM sends or Core writes.
- Updated installed host and plugin together; bumped shared-controller symbol v2 to prevent a hot reload retaining old hook code. Gateway plugin reload completed generation 6. This is synthetic hook/runtime validation, not proof of a real Weixin/Discord inbound event; user read-only IM check remains outstanding.

## Progress — installed IM mapper compatibility audit

- Rechecked live gateway: both Discord accounts connected/ready with recent transport activity; Weixin running with recent activity but lifecycle still starting. Neither channel reports an inbound message. This is current transport evidence, not a successful user IM roundtrip.
- Inspected installed 2026.9.8 dispatcher and mapper: before_dispatch receives canonical MessageSidFull ahead of transient MessageSid, sender/account fields, and the inbound-claim conversation projection.
- Added and ran `scripts/check-openclaw-ingress.mjs` against the actual installed mapper. Synthetic Discord and Weixin contexts pass first admission/replay rejection; changing only Weixin transient ID remains a duplicate; missing sender is denied without a ledger claim. The script does not send IM, invoke models or write household data.
- Previous USB-only turn did not advance this goal. This turn added executable compatibility evidence and reconciled outdated adapter documentation. Real inbound acceptance, traceable corrections/retraction, and final completion audit remain outstanding; goal stays active.

## Progress — traceable correction/retraction implementation

- Added migration 0008 and immutable revision fields (`supersedes_id`, `revision_reason`, `retracted`). Same-member current targets only, one successor per record, explicit reason, terminal retraction; old request replay reports its current successor. Concurrent corrections have one winner. Original payloads remain intact.
- Integration queries retain history with `superseded_by`; TV queries exclude superseded/retracted records even when the successor is private. Existing TV refresh behavior applies. No extra model tools: extended the existing family write tool through MCP, Brain argument validation, plugin field allowlists and deterministic receipts; receipts include exact record IDs and historical/retracted labels.
- Ordinary NAS files retain schema 1; revision files use schema 2. Dynamic successor metadata is excluded, so original archive bytes and retries remain identical. Restore validates the complete revision graph after all files are imported, then rebuilds links; missing/cross-member/forked/cyclic graphs do not publish an output DB.
- Full Go suite passed; race suites for store/archive/HTTP passed. Actual in-memory Brain→MCP→HTTPS Core test now creates, corrects, retracts and queries history, verifying TV projection. Archive export→restore roundtrip and out-of-order recovery pass. Plugin suite 49 passed; frontend typecheck/production build and Go binary build passed.
- These revision changes are built in the worktree but NOT yet deployed to the running household Core/plugin/maintenance binaries. Next: isolated real-model revision acceptance, coordinated backed-up deployment, then installed read-only verification. No production household record was written. Real IM ingress remains unverified; no outbound IM sent. Goal remains active.


## Progress — deployed revisions and final acceptance audit

- Real DeepSeek corrected the isolated synthetic height 70→71 cm, queried both versions, then retracted the current record. Live paired browser showed only 71 cm before retraction and no record/chart after its automatic refresh. All three historical NAS files synced; actual SMB restore preserved both revision links and produced zero active TV observations, integrity_check=ok.
- Real DeepSeek additionally created health (85 bpm), checkup and meal-plan fixtures. Paired browser verified each category, keyboard selection, expected text/chart and NAS synced state; no page errors. Production records stayed zero. Synthetic NAS files were hash-verified into local evidence then removed; isolated servers stopped through confirmed live session handles.
- Backed up current household DB/binaries/config/tools and agent instructions under rollback-family-20261003/revisions. Deployed signed Core + maintenance, MCP, Brain host, plugin and schemas together. Production schema=8/integrity=ok/HTTPS=200. Installed maintainer query and main gateway DeepSeek query succeeded without writes or --deliver. Updated both agent instructions for revision semantics and corrected a stale ten-tool statement.
- Reload generation 7 logged old provider cleanup timeouts. Subsequent live Discord ready/connected state and real gateway tool calls (failures=0) verified active operation; Weixin remains starting with activity, not proven messaging. Actual attached Android family navigation sequence303 applied and screenshot inspected.
- Reconfirmed installed and npm stable OpenClaw both 2026.9.8; installed artifacts match verified build. go vet passed. Final requirement/evidence table is in docs/ops/family-records.md. No extra feature work remains from this plan; actual IM input/reply is the unresolved completion gate.
- The same missing real IM verification has persisted across the mapper audit, revision implementation and deployment turns. Independent implementation and acceptance work is now complete. No inbound event has arrived and no permission to send an IM message on the user's behalf was given; a user private query is needed to finish this gate. Do not claim goal complete.


## Completion evidence — real Discord roundtrip

- User confirmed receipt of the requested private IM query response. Fresh channel status shows Discord inbound/outbound at 19:29 local; the durable ingress ledger now has one claim.
- Read-only inspection of the actual Discord session transcript confirms DeepSeek V4 Flash invoked home_list_family_members and home_list_family_records for abu, both successful; response returned the real empty Atrium record list and was received by the user.
- This resolves the final blocked gate. Original goal requires an IM app; Discord is the verified path. Weixin transport and manufacturer-specific television hardware are explicitly unverified, not claimed as accepted. Core/model write→NAS→TV, corrections/retraction/recovery, branch/deployment/style/tests remain supported by the previously recorded evidence. No pending implementation or required acceptance work remains for the original objective.
