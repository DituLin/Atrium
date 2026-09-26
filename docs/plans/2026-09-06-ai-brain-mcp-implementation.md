# AI Brain / home-mcp Implementation Plan

> For implementation: use the test-driven-development and verification-before-completion workflows. Source of requirements: `docs/tech/2026-09/atrium-home-hub/ai-brain-home-mcp-design.md`.

**Goal:** Implement the complete scoped Core → home-mcp → Brain integration and verify real screen execution without weakening existing clients.

**Architecture:** Core owns service principals, filtered integration projections, durable operations and command truth. A separate Go MCP process calls the integration API. A constrained Brain adapter preserves operations and integrates with the selected model/runtime.

**Tech stack:** Existing Go/SQLite/Core, official Go MCP SDK, existing Android TV. Runtime/model selection remains an explicit integration decision; installed OpenClaw is available for compatibility inspection.

## Working baseline

Isolated worktree `.worktrees/ai-brain-mcp`, branch `codex/ai-brain-mcp`. Current uncommitted Android and Core fixes copied intact. Existing running Core is not changed during development. No NAS mount/power management changes.

## Tasks and gates

1. **M1 service identity:** New domain/store migration, auth service and scope. Write failing tests for expiry, revoke, rotation, empty/malformed policy and isolation from admin/screen routes. Implement principal-linked credentials and current policy checks. Add maintainer CLI for issue/list/update/rotate/revoke with safe token-file handling.
2. **M1 projections:** Define public integration DTOs; tests for screen/source restriction, photo exclusions, safe counts, principal-bound cursors and no media/path exposure. Add dedicated integration routes using existing business/query services. Recheck policy on each call and avoid shared unrestricted snapshots.
3. **M2 durable operations:** Test 20 concurrent identical requests, conflicting payload, offline result reuse, TTL, restart and permission shrink. Add atomic operation + command/sequence transaction, result lookup and safe retention. Reuse normal TV delivery and acknowledgement lifecycle.
4. **M3 MCP server:** Lock SDK version. Add separate executable/config/HTTPS client, 11 strict-schema tools, bounded output/concurrency/wait, lifecycle and cancellation. Test using actual MCP transport with a fixture Core; prove no token or URL leakage.
5. **M4 Brain:** Confirm runtime interface/model configuration without exposing secrets. Add constrained runtime adapter, durable action IDs, budgets, result-based Chinese responses and scenario tests. Preserve full goal if real model setup remains pending; do not substitute fake-model results for live AI verification.
6. **M5 verification:** Unit/race/lint/build, complete OpenAPI and operation docs, MCP protocol tests, real OnePlus controlled smoke and Brain/MCP failure isolation. Record exact coverage, missing evidence and release artifacts in `docs/ops/ai-brain-mcp-results.md`.

Each change begins with a relevant failing test, followed by implementation and the affected regression suite. Spec review precedes final quality review. No goal completion until every source requirement is accounted for by current evidence.

## Progress

- [x] Inspect current code, goal and runtime availability; create worktree and carry baseline.
- [x] M1 service identity and CLI.
- [x] M1 scoped projections.
- [x] M2 command operations.
- [x] M3 MCP tools and executable.
- [ ] M4 Brain integration.
- [ ] M5 independent review, complete checks, real-device results.

Current evidence and remaining scope: [implementation results](../ops/ai-brain-mcp-results.md). M1–M3 spec and quality review passed; M4 runtime/model selection and M5 physical-device verification are pending.

## M4 共用宿主模块（2026-09-06 后续）

运行时/模型提供方未定，但所有候选路线都需要先持久记录动作，因此先新增 `internal/brain` 共用组件，不引入模型 SDK 或自定义聊天服务：

- 独立私有 SQLite 账本，不 import Core store。scope 由后续宿主固定绑定 Core origin + principal，凭据轮换不改变 scope。
- 每个 host turn/call 对应持久 ULID；事务提交后仅首次调用获得发送许可。重复调用及重启后只查 operation，不重新发送。
- 本地未得到结果为 `unconfirmed`，与 Core 终态 `unknown` 区分。命令 ID 不能变化，终态不能回退；恢复队列通过分页枚举。
- 每轮最多两条持久屏幕动作；受限执行器还需落实最多八次工具调用、30 秒总时限、白名单、同屏串行与取消。
- 此模块完成后仍需实际运行时接线、模型行为/中文用例、追踪关联和真实设备验收，不能以模块测试代替 M4/M5。

M4 共用账本、ScopeFor、Executor、ModelTools 已实现并通过目标回归；共用模块的 turn_ref 追踪完整链已补齐并通过跨层测试；剩余为运行时接线、模型/数据去向选择、中文用例及真实模型+OnePlus验收。M4/M5仍未勾选。


2026-09-06 23:54 增量：确定性中文动作回执及共享证据解析已通过双审查和检查；家庭 Core 受控升级后，真实 Executor→stdio MCP→HTTPS Core→OnePlus 展示照片/恢复首页闭环与停止 MCP 后的 CLI/遥控回归通过。具体 evidence 见实机记录。剩余为模型路线/提供方、真实模型接线及行为和完整故障验证，M4/M5 保持未勾选。
