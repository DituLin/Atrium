# AI Brain / home-mcp 实施结果

记录日期：2026-09-07（实现与首期验收完成）。工作区：`codex/ai-brain-mcp`（`.worktrees/ai-brain-mcp`）。本记录区分代码验证与真实家庭环境验收，不把前者替代后者。

## 已完成的实现

| 范围 | 交付 |
| --- | --- |
| M1 身份 | 独立 integration principal、哈希凭据、六项权限、来源/屏幕 allowlist、有效期、签发/轮换/撤销/完整策略替换；管理员 CLI 只写新建 0600 token 文件 |
| M1 读取 | 专用 integration API 与公开 DTO；SQL 限域再分页/计数，主体/策略版本绑定游标；照片/屏幕/NAS 元数据不含原路径或媒体 URL |
| M2 命令 | operation + command + sequence 同事务；重复操作不刷新 TTL；轮换保留归属；七天映射保护；离线失败持久化，重启不重投 unknown |
| M3 协议 | 独立 Go MCP stdio 程序、官方 SDK v1.7.0、11 工具、固定 HTTPS Core、有限等待、响应限制、并发控制、取消/EOF；错误结果保留恢复 ID |
| M4 Brain | 私有 SQLite 动作与 run 账本、30 秒/8 调用/2 动作预算、宿主生成 ID、状态恢复、确定性中文回执、OpenClaw 多注册表适配及 JSONL 宿主 |
| 文档/构建 | OpenAPI 13 个新 path（14 个方法）、完整 DTO schema、运维接入文档、Core/MCP 构建及 macOS 双架构目标 |

## 已有验证证据

- `make web`：现有 TV UI 构建成功，嵌入 Core 产物；没有修改本轮 TV 页面功能。
- `make check`：Go vet、golangci-lint（0 issues）、全仓库测试、Core 和 home-mcp 构建通过。
- `go test -race ./...`：全仓库通过。
- 身份与权限：凭据过期/撤销/轮换、非 admin/screen/WS、来源与屏幕限制、空策略、游标跨主体/策略失效测试通过。
- 命令：20 个相同操作并发仅一条 operation/command/sequence；同 ID 冲突；原始 IssuedAt/ExpiresAt 不变；离线结果重用、重启 unknown、七天边界、ULID 时效边界、事务外投递通过。
- 投影：TV 上报的私密 route/collection/error_code 不透传；来源外 photo/resource ID、client_version、Observed 不返回；未知枚举转换 unknown/省略。
- MCP：官方 SDK client 发现 11 工具；严格输入；完整结构化/文本响应；TLS/重定向/凭据文件；输出上限、并发、超时/畸形响应、HTTP 取消传递及真实子进程 EOF/SIGTERM 退出通过。
- **跨层契约**：真实 Core + 临时 SQLite + HTTPS + 官方 MCP client 测试通过。读取、accepted、重复提交一条命令、applied 查询、撤销身份后旧管理员接口可用均覆盖。此测试的 TV 在线状态是 stub，applied 为测试注入，不是真实电视确认。
- **升级**：临时版本 1 数据库迁移至版本 3，原屏幕凭据、LastSequence、命令归属与设置保持；重复迁移无操作。
- OpenAPI 文档路径注册测试解析主 YAML 与外部 JSON；空权限策略响应由公开 JSON Schema 实际验证。

最后的 MCP 命令 ID 关联修复已通过五项回归与 M3 race；M1/M2/M3 独立规格与质量审查均已通过。轮询只能确认原始 command ID，任何不一致返回保留原始恢复标识的 unknown。

## 当前验收与发布状态

| 项目 | 当前事实 / 出口 |
| --- | --- |
| M4 Brain 运行时 | OpenClaw 2026.5.20 + 用户已配置的 DeepSeek v4 flash 已接通；专用私有配置、10 工具白名单、本地 CLI 入口 |
| Brain 行为控制 | 共用账本、执行器、模型工具投影、追踪及确定性中文回执已实现；运行时接线、独立私有回执与 ask 最终输出已完成；不采用模型自由摘要作为结果 |
| 工具追踪 | 共用执行器 → MCP → Core 的 turn/call/request/operation/command 日志链已实现并通过跨层测试；真实模型工具调用已接通 |
| 模型数据去向 | 用户授权使用现有 DeepSeek；只发送范围内工具元数据，无原 NAS 路径/媒体 URL/凭据；专用 agent 禁用长期记忆，保留策略见运维文档 |
| 自然语言 → OnePlus | 实际照片展示、回首页、最近集合已取得 applied；重复同图漏确认已修复；最终三类动作各 3 次 applied |
| 家庭环境升级 | 23:49 已备份并升级家庭 Core，迁移至版本 3，原 OnePlus 配对保留；真实 MCP 展示照片和恢复首页均 applied，见实机记录 |
| M5 故障解耦 | 真实 MCP 停止/身份撤销后，原 CLI、遥控器、照片、首页继续可用；模型端点连接拒绝与最终 ask 失败回执通过；确定性故障矩阵及恢复测试通过 |
| 发布 | 首期实现与验收完成；未合并、未提交 Git 发布；Mac 断电恢复、NAS 自动挂载仍暂停 |

真实设备验证继续沿用 `docs/ops/android-tv-results-2026-09-06.md` 的基础环境，但其中的历史记录不能当作本轮 AI 验收。M4/M5 首期验收已完成，详细证据与局限见下方记录。

使用方法见 [运维接入](ai-brain-mcp-runbook.md)，设计依据见 [技术方案](../tech/2026-09/atrium-home-hub/ai-brain-home-mcp-design.md)。

## M4 共用模块增量（升级前历史记录）

`internal/brain` 已增加独立私有 SQLite 账本、origin/principal scope helper 与 MCP 执行器。ledger 与 scope 已通过独立规格/质量审查；executor 规格与质量审查均已通过，锁生命周期回归及 race 通过。此模块不调用模型、不读取模型凭据，尚不是运行中的 Brain。

已观察的模块证据：跨两个账本连接的并发仅一次发送许可、重开后不重发、每轮两动作、终态 unknown 不回退、101 条待恢复记录分页、拒绝不安全文件/符号链接/外部 DB，以及实际 Core + TLS + 官方 MCP client + Executor 的 accepted→查询→applied、重复不新增命令、取消阻止后续调用。TV 确认仍由测试注入，不算实机验证。

OpenClaw 只做了安装源码/文档兼容性核查，见 [记录](openclaw-compatibility.md)。用户的运行时/模型选择仍待回复。

本轮新增验证：`make check` 全部通过；Brain 与 HTTP 跨层 race 通过。`ActionOutcomeError` 在落账后的取消/传输/校验失败中保留可信 operation/command ID；模型端从实际 home-mcp 工具定义生成 10 个受限工具，host-only 恢复工具及 operation_id/wait_ms 不暴露给模型。后端 home-mcp 仍提供完整 11 工具。

家庭环境只读预检：OnePlus USB 在线；现有 Core 报 `oneplus6t_tv` online=true，运行版本仍为原 `c459876-dirty+c459876`（13:11 构建）。未升级家庭运行服务，本轮无模型或 TV 动作实测。

## M4 追踪增量（升级前历史记录）

已实现协议元数据追踪、Core 审计具体 integration 主体关联、稳定错误码日志与敏感原文排除。实际 SDK + HTTPS + Core 的跨层测试核对三层共享 turn/call、MCP/Core request、operation/command 关联；额外测试覆盖非法元数据、权限/参数/冲突错误和原始错误原因不入日志。本轮 `make check`（全仓测试、vet、lint、两程序构建）及 Brain/MCP/HTTP/integration 四包 race 均通过。尚未接入真实模型、升级家庭 Core 或执行 OnePlus AI 验收。

追踪增量的独立规格复审与质量审查均通过，无剩余审查阻塞项；质量审查另行运行四包 race 通过。

## 最新增量：中文回执与真实设备

新增 `DescribeAction`，与账本观察共享严格证据解析，区分 accepted/applied/failed/expired/unknown，保留观测时间；响应异常或宿主错误为本地 unconfirmed，保留可信恢复标识，不输出错误原文、不虚构照片张数。状态与 MCP IsError 矛盾时拒绝成功证据。测试先红后绿，Brain/HTTP 回归、`make check` 与独立 race 通过；规格和质量审查均通过。

**家庭 Core 已升级，真实 MCP 控制链路通过。** 展示照片、恢复首页均获得真实 OnePlus applied，截图确认渲染；撤销临时身份后旧 token 失败，停止 MCP 后原 CLI 与遥控操作仍正常。详见 [实机记录](ai-brain-mcp-device-results-2026-09-06.md)。前述“未升级/未实测”增量条目是升级前历史事实，以本节与顶部当前状态表为准。尚未接入真实模型，M4/M5 整体仍未完成。

## 2026-09-07 OpenClaw 实接增量

此前“运行时/模型待定”均为历史阶段。用户已确认现有 OpenClaw 配置 DeepSeek 可用，本轮复用该提供方，通过专用配置运行，无聊天渠道投递。JSONL 宿主与插件已实现并经独立审查，29 项 Node 测试、Go 全量 check 及相关包 race 通过。真实模型场景详情与质量限制将记录于 `ai-brain-mcp-model-results-2026-09-07.md`。

## 最终交付（2026-09-07）

完整实现包含受限 Core API、幂等操作映射、11 工具 MCP、10 工具 Brain 投影、OpenClaw 插件、持久执行/恢复账本及本机 ask 入口。真实模型/TV、故障解耦和固定中文回归已完成，详见 [模型与实机验收](ai-brain-mcp-model-results-2026-09-07.md)、[fixture 验收](ai-brain-mcp-fixture-results.md) 与 [模型故障验收](ai-brain-mcp-model-network-results.md)。前述“未接入模型”段落为历史阶段记录。

最终 Go check / 相关包 race、Web 216 项测试和构建、Node 44 项测试通过。原生 OpenClaw 摘要存在措辞风险，因此日常必须使用 ask 的事实回执；该限制和维护方法已记入运维文档。视频、日历、房屋控制、长期记忆、远程聊天及宋式视觉仍按原方案属于后续迭代，不在这轮工具中发布空实现。

## 技术方案完成核对

| 方案要求 | 可复核证据 |
| --- | --- |
| M0 工具 / 权限 / 运行时契约 | 技术方案决策表、OpenAPI、实际 MCP 11 工具与模型 10 工具发现 |
| M1 独立身份、限域、撤销与轮换 | auth / store / HTTP integration tests；实机旧身份撤销记录 |
| M2 同操作并发、冲突、过期与重启 | screen/integration_test.go 的并发、TTL、rotation、retention、offline/restart 测试 |
| M3 固定 TLS Core、严格协议与错误、取消及退出 | homemcp/server_test.go；真实 SDK stdio/HTTPS 验证；双架构 MCP/Brain 构建通过 |
| §8.1 本地维护者入口，无聊天投递 | 已部署 ask，独立配置、session UUID，真实便利命令回首页 applied |
| §8.2 意图、歧义与唯一目标 | 固定中文三次重复；多候选真实模型 fixture 三次与最终入口复核 |
| §8.3 真实 ID、元数据仅作数据 | 真实照片工具→show 及投影测试；引用注入三次无额外工具；固定工具/权限白名单 |
| §8.4 可逆动作直接执行且不能绕行 | 三类动作各三次 applied；插件仅十个工具且拒绝未知参数 / 工具 |
| §8.5 观测时间、事实与未知结果 | Go DescribeAction / Receipt，ask 确定性渲染；真实状态和历史 unknown 恢复 |
| §8.6 预算、取消与操作恢复 | runtime/executor/receipt tests；ask 30 秒进程组期限、部分回执与统计不符回归 |
| §8.7 串行与停止 | Executor 同屏锁 / 可取消等待；Core sequence；插件 run 墓碑及停止后不可复活测试 |
| §9 日志、资源与故障解耦 | 三层追踪测试；模型 ECONNREFUSED 无动作；Brain/MCP 停止后 CLI 与遥控可用 |
| §11 baseline-only / 不支持功能 / 中文回归 | fixture 记录、正式入口每场景三次、模型与实机结果文档 |
| §11 POST 丢失 / 越权 / token 策略 / 故障矩阵 | MCP 丢响应测试、integration 权限/游标/命令测试、跨层 TLS 契约测试与实机撤销 |

最终部署文件与仓库 ask / renderer / Brain binary 一致；账本汇总为 22 applied、2 unknown、0 pending。未把历史 unknown 改写为成功，也没有恢复重发。基础版长期运行、视频/日历/房屋、视觉设计与远程聊天仍在本方案明确的后续范围。
