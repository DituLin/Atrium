# AtriumOS 实施与验收记录

更新：2026-10-03。开发分支 `codex/atriumos`，隔离工作树 `.worktrees/atriumos`。所有“规划/开发中/通过”区分记录，生产版本未自动跟随开发分支。

## 目标与入口

- [整体系统设计](../architecture/atriumos-system-design.md)
- [智能综合档案设计](../plans/2026-10-03-family-agentos-archives-design.md)
- [详细开发计划与验收矩阵](../plans/2026-10-03-atriumos-development-plan.md)
- [合成资料与 42 项查询/故障验收契约](../plans/2026-10-03-atriumos-archive-acceptance.md)（待后续任务执行）

## 状态

| 任务 | 状态 | 证据 / 未完成项 |
| --- | --- | --- |
| T00 设计 | 已审阅 | 3 份设计/计划文档及 R01–R14；独立审阅 5 项问题均已修正，见下方记录 |
| T01 持久基础 | 已完成任务级验收 | `02b928a`；需求与质量复审批准，race 与全量 Go 回归通过；未接入 API/worker/生产 |
| T02 家庭授权 | 开发中 | 稳定家庭 ID、显式权限、任务委托与事务内重新授权；尚未验收 |
| T03–T18 | 未开始 | 不将文档或旧 family 记录作为新能力通过证据 |

## 基线核对

`1142c9e` 的 `go test ./...` 和 `node --test adapters/openclaw/*.test.mjs` 在隔离工作树成功；JS 49 项通过。完整日志留本机 `/tmp/atriumos-worktree-baseline.log` 与 `/tmp/atriumos-worktree-baseline-js.log`（临时证据，后续关键结果会写入本记录）。原工作树的既有改动已归档为两个提交，未改生产数据库。

此前已验证微信查询恢复（用户确认）、Discord 写入、家庭记录 NAS 同步、OnePlus “档案”Tab。以上不代表新原件解析、自动归属、综合检索或通用任务已经实现。


## T00 设计审阅

独立审阅提出五项：业务事件与字节去重混淆、私密子内容泄露至原件预览、Brain/Core 两库接收恢复协议不具体、旧记录缺少持续投影、任务历史仅计数。均在档案设计“实施契约补充 / 审阅收敛”及整体设计/计划中修正；复审确认原五项已覆盖。另按复审补充：文件内业务事件在理解提交阶段创建，恢复清单包含业务事件 ID/证据/关联。具体格式质量和真实用户体验仍是后续验收，不因设计审阅通过而标为实现完成。

本机解析能力预检：Swift 6.0.3 可执行，但直接导入 Vision/PDFKit 遇到本机 CLT 的 SwiftBridging 模块重复定义，已终止该探针，未修改系统 SDK。改用 Objective-C、`clang -fobjc-arc -fno-modules` 链接 Foundation/Vision/PDFKit 后编译和运行成功，Vision 返回 zh-Hans/zh-Hant 等支持语言、PDFDocument 类存在。T06 因此优先使用 Objective-C 辅助进程，保持 JSON 协议与 Go 服务解耦。此证据仅证明运行时能力，不替代真实图像/扫描件/表格质量验证。探针位于本机 `/tmp/atriumos-vision-probe.m`，没有读取家庭文件。

本机 SQLite trigram 行为探针：对“张明 体检报告 血常规”查询“张明”“体检”返回 0，“体检报告”“血常规”返回 1，符合官方短词限制。此探针使用 Python 附带 SQLite，并非项目 modernc 构建的验收；T10 仍需在实际项目运行时覆盖短姓名/短词路径及索引重建。


## T01 实现与需求审查

实现提交 `fab2efe`，日期校验修复 `1b44089`。新增家庭作用域任务仓库、迁移 0009、不可变输入、检查点/结果/澄清、原子事件和独立 attempt ID。公开 JSON 不含租约令牌；领取、续租、提交、重试、等待、取消、澄清都在持久状态下工作。暂不包含解析器、HTTP 入口或生产任务调度。

红灯：实现前领域/仓库 API 缺失，测试失败。实现后 `go test -race ./internal/domain ./internal/store` 与 `go test ./...` 通过；旧迁移断言仅从 schema 8 更新到 9 并加入新表名，保留数据兼容断言。

需求复审在 `fab2efe` 发现 Go 时间解析器接受不规范时区/超长小数秒；新增七个失败用例后修复，规定最大 35 字节、两位时分秒、有效时区、可选 1–9 位小数。`1b44089` 经独立需求复审确认通过，复审实际运行 `go test -race -count=1 ./internal/domain ./internal/store -run Archive` 成功。质量审查尚在进行，因此 T01 尚未标为最终验收。

质量审查独立复现两项：调用开始时尚有效的租约，在等待写锁期间过期后仍可完成；同一租约下延迟返回的旧状态可覆盖更新检查点。修复方案为仓库使用可信可注入时钟，在每次事务取得写锁后取时；工作结果带独立 `state_version` 条件，续租只增加事件版本，不使进行中的计算失效。修复与重新审查通过前不进入任务调度或生产接入。

`02b928a` 已实现上述修复：状态版本从 1 开始，Checkpoint/Finish/Retry/Wait 和澄清推进；Claim/Renew/Cancel 不推进；澄清仍用完整事件版本检测过期问题。新增测试先复现旧行为失败，再覆盖同版本竞争、续租兼容、持锁期间过期和事务重试重新取时。实现端 `go test -race ./internal/domain ./internal/store`、`go test ./...` 成功；独立需求复审实际运行 `go test -race -count=1 ./internal/domain ./internal/store` 成功并批准。独立质量复审也批准，实际运行 `go test -race ./internal/domain ./internal/store -run Archive -count=1` 成功，包含此前两个复现场景。T01 因此完成任务级验收，不代表整体档案能力上线。
