# AtriumOS Development Plan

> 执行技能：使用 `superpowers:executing-plans` 按任务实施；每批核对实现与设计、测试与实际验收证据。

**Goal:** 在当前 Mac mini + NAS + TV + IM 架构上实现全家的智能综合档案：自动接收文本/图片/文件、保存原件、分类和归属、统一检索、基础分析及可恢复运行，同时明确家庭 AgentOS 后续演进路径。

**Architecture:** 模块化 Go Core 负责持久任务、原件、实体、事实、权限和索引；隔离解析器与受控模型工作进程负责理解；OpenClaw 提供 IM 接入，React TV 提供已有风格的全家档案展示。保留现有 family 记录、屏幕与影像服务。

**Tech Stack:** Go / modernc SQLite / React + TypeScript / OpenClaw + DeepSeek；macOS Vision/PDFKit（Objective-C）解析辅助进程；Office 开放格式解析器。新增依赖必须锁定版本并验证安装与故障路径。

## 0. 执行规则与基线

权威设计：[整体系统设计](../architecture/atriumos-system-design.md)、[档案设计](2026-10-03-family-agentos-archives-design.md)。用户已确定综合档案优先，自动识别人物和类别，无需先登记名单；默认 TV 可见，显式私密覆盖。

工作树 `.worktrees/atriumos`，分支 `codex/atriumos`，起点 `1142c9e`。当前已部署版本不随工作树代码自动更新。旧修改已分别提交为 `41e7f0d`（微信兼容与 Tab 名称）、`1142c9e`（产品方向）。基线全量 Go 测试与 49 项 OpenClaw JS 测试通过。

每个任务执行：写行为回归 → 看见目标失败 → 最小实现 → 针对测试通过 → 审查 → 写实施证据 → 提交。测试覆盖外部可观察行为与失败恢复，避免按字段数机械凑用例。依赖任务未完成前不能把入口公开成可用功能。所有任务状态放在 `docs/ops/atriumos-implementation-status.md`，本计划不伪造已完成标记。

开发与自动化测试只用临时库、隔离根和合成样本。迁移与部署前备份真实库和二进制；恢复演练写新目录。发布影响既有 TV/IM 时先验证隔离链路，再原子替换签名二进制，确认健康与实屏。

## 1. 里程碑与依赖

| 里程碑 | 任务 | 依赖 | 用户可验收结果 |
| --- | --- | --- | --- |
| M0 设计基线 | T00 | 当前代码 | 整体设计、细化档案设计、开发计划和审计矩阵一致 |
| M1 持久接收基础 | T01–T04 | M0 | 原件与任务可靠接收，重启不重复/不丢失 |
| M2 解析与理解 | T05–T09 | M1 | 文本/图片/PDF/Office 自动分类归属，歧义可恢复 |
| M3 查询与分析 | T10–T12 | M2 | 全家四领域统一检索，结果有出处、统计可复算 |
| M4 IM 与 TV 闭环 | T13–T15 | M1–M3 | 微信/Discord 输入到全家档案实屏和查询回复 |
| M5 归档恢复与发布 | T16–T18 | M1–M4 | NAS 原件与元数据恢复、迁移、负载/故障/真实验收 |

T04 开发期先对接受控 CLI/测试接收；T13 接入实际 IM。T01–T04 不能单独称为智能档案完成。长期 A2–A4 见最后一节，不以它们替代本期任务。

## 2. 详细任务

### T00 — 设计、审阅与契约

文件：`docs/architecture/atriumos-system-design.md`；更新 `docs/plans/2026-10-03-family-agentos-archives-design.md`；本计划；`docs/ops/atriumos-implementation-status.md`。

步骤：核对真实模块与生产事实 → 明确四端职责和领域边界 → 写状态机/数据契约/验收 → 独立审阅发现矛盾 → 修正文档 → 提交。通过条件：每个显式需求有实现任务与验收项，不声称设计已经上线。

### T01 — 档案领域契约与持久任务租约

新增：`internal/domain/archive.go`、`internal/domain/archive_test.go`、`internal/store/archive_tasks.go`、`internal/store/archive_tasks_test.go`、`internal/store/migrations/0009_archive_foundation.sql`。

任务字段：id、household_id、kind、request_key、input_json、status、stage、attempts、available_at、lease_token、lease_expires_at、last_error_code、created_at、updated_at。JSON 输入验证为有界对象；T01 不执行其中的路径或命令，后续服务 schema 只接受宿主签发的附件/对象引用。task_id 和 lease_token 由宿主生成。`request_key` 在家庭内唯一，重放内容不同返回 idempotency_conflict。

领域同时明确文档时间精度、来源引用、实体/事实证据形状；先做验证与队列，不一次建立未使用的全套表。

红灯场景：两个 worker 争同一任务只成功一个；租约过期重新领取，旧 token 完成失败；崩溃后重开 DB 仍有任务；同请求重放复用 ID，换正文冲突；取消后旧 worker 不可提交；waiting_input 不被普通领取。

实现：事务领取到期 queued/retry_wait 和过期 running；Claim 递增 attempts 并发放新 token；Renew/Finish/Retry/Wait 均要求当前 token 且租约未过期；Cancel 使 token 失效；ResolveWaiting 以预期状态版本恢复，澄清参数有幂等键。完成结果与阶段事件原子保存。每次领取有独立 attempt_id（不同于保密的 lease token）；事件记录输入/检查点/结果版本，已提交检查点可被后续尝试复用。

质量审查补充：时间由仓库可信时钟提供，在每次取得事务写锁后采样，重试重新采样；调用者不能传入旧时间延长租约。`state_version` 独立于事件 `version`，Checkpoint/Finish/Retry/Wait 携带预期状态版本，旧结果拒绝覆盖；续租不推进状态版本。受控时钟与锁协调测试必须覆盖等待锁期间过期和同一尝试乱序回传。

验证：`go test -race ./internal/domain ./internal/store`；队列测试只使用受控时钟而不 sleep。提交：`feat: add durable archive task contracts and leases`。

### T02 — 家庭作用域及档案授权

修改：`internal/domain/integration.go`、`internal/auth/integration.go`、`internal/httpapi/integration_admin.go`、`internal/cli/admin_integrations.go`；对应测试。新增迁移将稳定 household_id 持久保存，不依据房屋显示名生成。

红灯：有 archive.read 无 household_ids 不可读；另一家庭 task/document 返回 not_found；权限撤销/过期使提交和查询失败；旧 family/member 权限行为不变。

实现 archive.read/ingest/organize/analyze 与 household_ids；任务记录接入 principal 和 policy_version；每阶段提交重新授权。自动建实体不修改集成策略。CLI 更新完整策略并保留旧已授权权限。验证：`go test -race ./internal/auth ./internal/httpapi ./internal/domain ./internal/cli`。

实现边界（独立架构审阅补充）：

- 持久随机家庭 ID 与本机默认家庭引用，重开库、改显示名不改变 ID；只读管理员接口和 `admin household show` 用于发现。旧集成不自动获新权限。
- `archive.ingest` 委托自动解析、分类、建立实体和提交接收任务；`archive.organize` 用于人工改归属/合并/私密修订。读取另需 `archive.read`，分析需 read + analyze。权限均须命中家庭白名单。
- HTTP 身份保留 principal 和 credential ID。写事务内重新检查凭据与 principal；后台任务只绑定 stable principal + policy_version，正常凭据轮换不取消已接受任务，principal 撤销/过期或策略代次变化拒绝后续提交。
- 授权、租约和状态条件在同一个仓库事务检查，使用锁后时间；不得在 service 先检查再另开写事务。供后续 T04/T09 使用的事务内辅助方法留在 store 包内，使来源/事实/任务状态可原子提交；不新增通用 callback 框架。
- Get/Events 在同一数据库快照检查当前身份并读取限定家庭的数据；越家庭与不存在资源统一 not_found，失效凭据 unauthorized。幂等重放先重新授权。接收回执只暴露本次接收必要状态，不授予其他档案读取权。
- 整理权限以家庭为范围，不把原始接入 principal 当作唯一可操作人。同家庭 read + organize 可跨微信/Discord 回答有效委托的澄清或取消任务，并在事件记录实际操作者；原始委托和来源不改。取消不发布结果，当前有权用户可以取消已失效委托的任务；继续处理的澄清仍须原委托有效。
- 保留 CLI `policy` 完整替换语义，增加 `grant-archives` 与 expected_policy_version 条件，在服务端原子合并明确的 archive 权限/household_ids 并保留全部旧 allowlist；版本冲突不自动覆盖。加入稳定 ID、撤销竞争、策略变化、凭据轮换、跨 principal 重放及完整策略保留测试。

### T03 — 不可变原件存储

新增：`internal/archive/blob.go`、`blob_test.go`、`internal/store/archive_documents.go` 及测试/迁移；修改 `internal/app/datadir.go`。

红灯：大文件/空输入策略、相同内容复用、hash/size 不符拒绝、路径穿越/符号链接拒绝、写入中断不产生可读成功、未同步原件不能被缓存回收。

实现流式有界接收、MIME 检测、SHA-256、临时文件/fsync/原子发布；文件系统与 SQLite 边界用可重入 reconcile：原件已发布但 DB 失败允许下一次复用，DB 不能引用未发布文件。内容键在家庭内隔离，文档与来源分离。异步孤儿清理只针对无 DB 引用且超过保留窗的临时对象。

文件系统预审收敛：BlobStore 初始化固定本机根和字节上限（初始 32 MiB），Put 接收 household + Reader + 可选预期大小/哈希；返回哈希、实际大小、sniffed MIME 和存储版本，不返回路径。拒绝空输入，读取 max+1 检测超限；T04 负责 HTTP deadline/body 关闭，不为任意阻塞 Reader 启动失控 goroutine。MIME 只按字节检测，Office ZIP 细分在 T05 完成。

根下各层使用目录 fd + `Openat(O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC)`，文件 no-follow 并验证 regular file；严格组件验证。临时文件以随机名/0600 独占创建，流写和校验后 Sync，再用 `Linkat` 原子无覆盖发布。目标存在必须重验 size/hash，一致复用、损坏报错；同步最终目录和新目录的父级，再报告发布。不能用会覆盖目标的 Rename，也不能单凭允许根内 symlink 的 os.Root 声称拒绝符号链接。Layout 只提供 Archives 路径，安全初始化不沿用 MkdirAll+Chmod 的跟随链接流程。维持 macOS 构建与现有 Linux CI 可编译。

文件发布不持有 SQLite 事务。T03 最小表为 household+sha256 唯一的 archive_blobs 与有复合家庭外键的 archive_documents；原始文件名/消息键留 T04 sources。文档不强加 blob_hash 唯一约束，以支持同文不同文本事件。store 包内 metadata/doc 方法供 T04 在同一事务重新授权并建立 source/document/task；DB 失败不能删除可能已有别的引用的 blob。OpenVerified 只接收经上层文档授权得到的内部引用，裸哈希不构成下载权限。孤儿 GC/NAS/解析器暂不在 T03 实现。

故障验收补充：同家庭并发同内容、跨家庭隔离、每层 symlink、FIFO/目录冒充文件、已有 blob 损坏、短读与 Reader 错误、写入/同步/链接失败、发布后 DB 回滚重试及已有引用不被失败清理删除。当前承诺进程崩溃恢复；现有 SQLite synchronous=NORMAL 与普通 fsync 不足以宣称整条接收具备断电硬件缓存耐久。

验证：`go test -race ./internal/archive ./internal/store ./internal/app`。不得向既有只读照片目录写入。

### T04 — 接收 API 与任务查询

新增：`internal/httpapi/archive_intake.go`、`archive_intake_test.go`、`internal/archive/intake.go` 及测试；API 契约 `docs/api/archives.openapi.json`。

输入采用可信接入 metadata 和有界字节流，主体暂未知不要求 member_id；纯文本以原文作为原件。返回 `document_id`、`task_id`、saved/queued、NAS 状态。任务查询返回阶段、逐文件结果和澄清问题，不返回服务器路径。

红灯：未授权接收、重投、同键不同字节、超限、接收途中取消、文件已写但任务提交失败、多个附件部分失败。验证：真实临时 HTTP server 上传并查回哈希及任务，不用 mock 代替落盘。

### T05 — 解析器接口与文本 / Office

新增：`internal/archive/parse.go`、`text.go`、`office.go` 及测试；`testdata/archive/` 合成文本、DOCX、XLSX、CSV 和异常包。

输出 `segments[{id,text,locator}]` 和 warnings；locator 包含段落/表格行列/工作表及单元格。UTF-8 校验和有界编码识别；DOCX/XLSX 只读取包内内容，禁网络引用和宏/公式执行；限制解压后总量、条目数和层级。公式保留原式及已有缓存值，缺缓存不计算假数值。

红灯：表格边界、合并单元格、缺失值、CSV 引号/换行、ZIP 炸弹大小、恶意路径、空/损坏/加密文件状态。验证格式矩阵逐项记录；只解析正文字符串不足以通过。

### T06 — macOS 图片 OCR 与 PDF

新增：`adapters/macos-archive-parser/` Objective-C 命令行、构建说明、合成样本；`internal/archive/native_parser.go` 及测试。使用 Vision/PDFKit，进程 stdout 只输出协议，stderr 不含原文。

红灯：中英文混排、旋转、双栏、扫描 PDF、混合文本/扫描页、页数上限、坏图/超大像素、进程超时；逐页 JSON 坐标能回到原件。无平台能力时返回 parser_unavailable，不能丢原件或宣称空成功。

验证：本机真实运行原生辅助程序，逐字段核对合成样本；Go 测超时/协议/资源限制。先展示原图和提取定位检查，再记录准确率；表格仍不可靠时保留待核对并报告，不删需求。

### T07 — 分类与事实提取执行器

新增：`internal/archive/understand.go`、`understand_test.go`、版本化 schema/prompt；受控 Agent worker 适配在 `adapters/openclaw/archive-worker.mjs`。

输入仅任务已授权片段；输出领域、文档类型、主体角色、原始字段、规范候选与证据。模型输出必须经结构/大小/引用验证，不能通过 JSON 填写本机路径或新权限。多页分块与合并在同一任务版本下完成；不截断后称全量。

验证固定错误输出、无来源数字、prompt 注入样本、模型故障、片段超预算；再用实际 DeepSeek 对合成四领域样本评估，不以 mock 通过代替真实提取质量。金额计算不用模型。

### T08 — 实体匹配与可撤销关联

新增：`internal/archive/resolve.go`、`resolve_test.go`；`internal/store/archive_entities.go` 及迁移/测试。

红灯：两人/同名/别名/“妈妈”无参照/医生与受检人/共同房产/多被保人。独立明确主体自动创建；已有匹配需明确证据，冲突进待归属；文档可多实体多角色。合并/拆分是带版本的关联修订，不能删除证据历史。

验证人物创建不新增账户或管理员权限；旧阿布映射到已有 stable ID；单份多人文档不复制原件也不串事实。

### T09 — 原子提交、澄清与修订

新增：`internal/store/archive_extractions.go`、`internal/archive/commit.go`、对应测试及迁移；扩展任务查询和澄清 API。

在有效租约/授权下，原子提交版本化片段、分类、实体关联、事实及索引/outbox 任务。丢失租约不得提交一半。澄清沿用 task_id 和预期版本；过期回答返回冲突，缺主体不影响其他附件。

红灯：提交中断、并发澄清、重复模型结果、改归属、撤回、私密修订及重新提取；历史事实不能重复计入统计。增加 archive_subject_events 区分物理去重和真实事件，三种计数规则按档案设计验证。

### T10 — 统一检索与证据

新增：`internal/archive/search.go`、`internal/store/archive_search.go` 及测试；FTS 迁移；查询 API。

实现实体/类别/日期过滤与原文检索；原件/片段/事实/关联先授权后排名。中文两字姓名和短词必须有别名及有界子串路径；分页稳定、字面查询参数化，不允许任意 SQL/FTS 语法注入。索引只含有效可读版本，撤回与重建一致。

合成四领域检索集 ≥ 30 个问题，每条预期档案和证据点可检查；测试姓名别名、多主体、未知日期、零结果、私密/撤销、分页和同文件多来源。具体样本与 42 项预期见[综合档案合成验收集](2026-10-03-atriumos-archive-acceptance.md)，它是待执行契约，不是通过报告。记录召回不足，必要时再增语义检索，不以加入向量库作为验收。

### T11 — 自然语言查询工具与来源回答

修改：`internal/homemcp/server.go`、`internal/integration/`、`internal/brain/model_tools.go` / `executor.go` / `receipt.go`、`adapters/openclaw/index.js` / manifest / tests。

工具：接收任务状态、检索、文档证据、实体查询、受控澄清/更正。宿主注入 request/operation 标识，模型只能传业务条件。回复区分原件、提取事实与解释，任何声称“已保存/已同步/已更正”必须有 Core 回执。

验证完整工具库存、参数边界、工具结果上限、模型实际查询两领域以上并正确引用；旧十三项工具回归继续通过。

### T12 — 基础分析与数据覆盖

新增：`internal/archive/analyze.go`、`analyze_test.go`、分析快照存储/API。

代码计算 count/sum/latest/time series；原始字段保留，单位/币种不一致分组，日期精度不足不能放到猜测日期。支出不将同笔账单/付款凭证直接重复相加；无法证明同一事件则提示候选重复并标覆盖范围。结果绑定事实版本，修订后失效。

验收：喂养 330 ml 奶粉 + 120 ml 母乳=450 ml；70 ml 是最近一次不是当日合计；教育不同满分不直接比较；金额用十进制精确表达；医疗参考范围来自报告。只说已归档范围，不推断全家全量事实。

### T13 — 微信与 Discord 可信附件接入

扩展：`adapters/openclaw/` 入站桥接、`scripts/check-openclaw-ingress.mjs`、协议和运行文档。

从实际安装版 OpenClaw 的可信 hook/media facts 获取附件，校验账户/会话/原始消息/附件序号和可读根，不让模型传路径。当前 before_dispatch durable claim 与 intake 必须可恢复关联，避免 claim 成功后进程崩溃造成永远拒绝重投却没有接收任务。重投查询同一任务，不能重新执行完整归档。

红灯：hook 字段缺失、Discord 多附件、微信图片/文件、跨渠道同文件、源文件清理前保存、路径越界、临时 ID、duplicate delivery。真实渠道最终由用户提供新合成/真实文件进行验收；不代用户发送未授权外部消息。

### T14 — TV 档案信息架构与详情

修改：`web/src/screens/FamilyScreen.tsx`、`web/src/screens/family/`、`web/src/types/family.ts`、`web/src/core/` API、`web/src/styles/family.css`；复用 `TopNav` 与 `RemoteButton`。

全家档案默认页、人物/房屋/领域筛选、处理状态、来源和时间线、原件预览；搜索可用 IM 发起+TV结果，以及 TV 遥控可操作的筛选。保留 family 路由和旧喂养记录兼容层。标题和指标为中文；日期明确到已知精度，同日事件显示时间。

验证私密子内容的原件/缩略图/片段/缓存闭包、公开统计独立计算、授权失效后清除、旧请求不覆盖新状态、焦点/返回/滚动保持、Chrome 66 内置与 CSS 限制、OnePlus 实屏。构建命令 `npm --prefix web ci` 后 `npm --prefix web run build`，按 package.json 的测试命令运行，不猜脚本。

### T15 — 端到端综合档案体验

新增：`scripts/e2e/archives/` 与混合样本验收脚本（生产数据不提交）。

在隔离 Core 接收至少两个未预建人物、四领域和全部首批格式，通过真实解析、真实模型、Core 提交、检索与配对浏览器展示。模型不足不能用预填答案替代。歧义样本恢复原任务、重复不重复统计、待归属可检索。停模型/Core/NAS 分别检查降级。

### T16 — NAS 归档与可恢复清单

新增：`internal/archive/export.go` / `restore.go`、测试、CLI 支持；复用已存在挂载身份校验，保持照片目录只读。

原件、提取版本、实体与关联、事实、业务事件稳定 ID/归并证据/文档事实关联及索引重建信息的清单包含哈希及 schema；先文件后清单，全部读回成功才确认。导入到新目录，校验引用闭包、权限范围、冲突、版本与大小。恢复不重放旧操作/通知。

验收：NAS 断开/错误挂载、写一半、损坏哈希、缺文件、重复恢复、旧家人 JSON 与新归档共存、完整恢复后四领域检索及三类事件计数、账单/付款凭证归并结果一致。

### T17 — 迁移、性能与发布

新增：`docs/ops/atriumos-runbook.md`；迁移/降级脚本；修改构建部署脚本。

schema 8 一致备份→隔离升级→旧六条记录与更正历史核对→事务 outbox 回填与增量投影→旧接口新增/修订/隐藏/撤回验证→新功能 feature flag 启用→签名构建→原子部署→健康/IM/TV验收。回滚应用前核对 schema 兼容，必要时用备份恢复到独立目录，不能让旧二进制直接写新库。

跑一万份合成文档检索与队列竞争，报告硬件、样本大小、P50/P95、错误率及占用；OCR/模型吞吐另计。一小时持续接收/查询故障恢复通过，真实 TV 长稳验收如未覆盖明确保留，不写成通过。

### T18 — 完成审计与用户使用说明

逐项检查下方矩阵；文档与实现一致，所有命令退出状态、样本证据、剩余限制明确。独立代码审查后修复重要问题。用户说明涵盖直接发资料、查询、纠正归属、查看原件、私密覆盖和恢复范围。只在 M1–M5 全部有证据后将第一阶段标为交付。

## 3. 验收追踪矩阵

| ID | 必须满足的需求 | 任务/证据 |
| --- | --- | --- |
| R01 | 全家综合而非仅阿布/健康 | T08/T10/T14/T15，混合四领域和多人实测 |
| R02 | 不先登记名单和类别 | T07/T08，未知明确主体自动建档、类型自动识别 |
| R03 | 文本/图片/多种文件 | T05/T06/T15，逐格式解析定位与失败状态 |
| R04 | 原件、证据、修订可追溯 | T03/T09/T16，哈希/版本/恢复一致 |
| R05 | 微信/Discord 接收和查询 | T11/T13/T15，实际入站与工具回执，不靠 CLI 冒充 |
| R06 | Mac mini/NAS/TV 分工 | T04/T06/T14/T16/T17，运行与故障证据 |
| R07 | 延续现有风格和遥控 | T14，配对浏览器/OnePlus/老 TV 兼容检查 |
| R08 | 数据分析可复算有覆盖范围 | T12，确定性案例与失效重建 |
| R09 | 人物归属不串人 | T08/T09，多主体/同名/角色/纠正样本 |
| R10 | 重启、重投、模型/NAS 故障不丢不重 | T01/T03/T09/T13/T16，崩溃/竞争/恢复测试 |
| R11 | 默认 TV 可见与私密覆盖 | T02/T09/T14，授权代次及去重/重新解析后可见性 |
| R12 | 旧数据和功能兼容 | T17，全量回归、六条记录与影像/屏幕检查 |
| R13 | 整体设计/完善档案/详细计划 | T00，链接与需求覆盖审阅 |
| R14 | 实际执行计划，不止文档 | T01–T18，代码、测试、部署与最终验收 |

## 4. 后续阶段的进入条件

A2：第一阶段档案与事实稳定后，逐领域增加健康趋势、教育比较、财务完整性、房屋维护时间线；每种分析先定义可比性与覆盖范围。

A3：在持久任务基础上增加用户可查看/修订的行动计划，支持等待澄清、暂停/取消、预算和明确授权的通知/提醒。必须验证未知外部结果与重试语义后才接动作工具。

A4：基于具体家庭需求增加设备、日历和生活服务连接器，各自有权限契约、测试、撤销与恢复。未来生态不要求当前创建空插件或伪数据页面。

本轮 goal 保持到约定的整体文档、档案细化、详细计划和第一阶段实际实施全部验证完成；不能因为某个里程碑通过就缩小完成定义。
