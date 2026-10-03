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

文件系统家庭组件编码精确 ID 字节为小写、无 padding 的 base32，不直接使用可含大小写的 ID，也不改变数据库身份。这样 macOS 大小写不敏感卷上的 `home-a` / `HOME-A` 仍隔离；最长 128 字节合法 ID 编码为至多 205 字节，满足单组件限制。布局为 `archives/<encoded-household>/blobs/<hash-prefix>/<hash>`；验收覆盖大小写变体物理对象不同、最长 ID 及编码后所有层级的符号链接拒绝。

文件发布不持有 SQLite 事务。T03 最小表为 household+sha256 唯一的 archive_blobs 与有复合家庭外键的 archive_documents；原始文件名/消息键留 T04 sources。文档不强加 blob_hash 唯一约束，以支持同文不同文本事件。store 包内 metadata/doc 方法供 T04 在同一事务重新授权并建立 source/document/task；DB 失败不能删除可能已有别的引用的 blob。OpenVerified 只接收经上层文档授权得到的内部引用，裸哈希不构成下载权限。孤儿 GC/NAS/解析器暂不在 T03 实现。

故障验收补充：同家庭并发同内容、跨家庭隔离、每层 symlink、FIFO/目录冒充文件、已有 blob 损坏、短读与 Reader 错误、写入/同步/链接失败、发布后 DB 回滚重试及已有引用不被失败清理删除。当前承诺进程崩溃恢复；现有 SQLite synchronous=NORMAL 与普通 fsync 不足以宣称整条接收具备断电硬件缓存耐久。

验证：`go test -race ./internal/archive ./internal/store ./internal/app`。不得向既有只读照片目录写入。

### T04 — 接收 API 与任务查询

新增：`internal/httpapi/archive_intake.go`、`archive_intake_test.go`、`internal/archive/intake.go` 及测试；API 契约 `docs/api/archives.openapi.json`。

输入采用可信接入 metadata 和有界字节流，主体暂未知不要求 member_id；纯文本以原文作为原件。仅 ingest 返回自有 receipt/task ID、本次保存状态、实际大小/哈希和接收时间；有 read 时才附 document 引用及完整状态，不返回其他来源的任务或去重命中。明确原件 saved、处理 queued、NAS pending 的不同含义；不提前声称已识别或已同步。任务查询使用面向用户的阶段/结果/澄清投影，不直接泄露内部路径或任意 worker 状态对象。

独立协议预审后的最小接口：

- `POST /api/v1/integrations/archives/intakes`：一次接收一个文本/附件槽。流式 multipart，第一个 part 为有界 JSON metadata，第二个为字节内容；拒绝额外 part、重复 metadata 和未知字段，确认合法结束后才能提交。文本原文不 trim 或改换行。
- `POST /api/v1/integrations/archives/intakes/lookup`：以来源 metadata 查询本 principal 的已提交回执，无须重传文件；仅 ingest 仍可查自己的简化状态。未找到只表示暂无已提交回执，不能判定并发上传一定失败。
- `GET /api/v1/integrations/archives/tasks/{id}`：当前 archive.read 查询本家庭任务。保持 T02 Get 的 read 要求，不为回执查询暗中放宽。

metadata 只含 kind(text/file)、渠道/账户/会话/发送者/原始消息 ID、槽编号、原名、可选声明 MIME/预期大小/哈希及 visibility_intent(inherit/private)。household 从 LocalHousehold 获取，actor 从服务端认证身份取得；拒绝输入 household/principal/member/document/本机路径/URL。Core 对版本化规范数组 `[version,household,principal,channel,account,conversation,sender,message_id,kind,slot]` 做 SHA-256 得到 source_key；不能接受模型自造 request key。T04 只验证受认证宿主提交，真实 IM 消息字段绑定在 T13 验收。

同一 source 比较 hash/size/kind/visibility intent 等固定业务参数，相同重放原 receipt/task，不同返回 idempotency_conflict；临时路径、当前时刻、随机 ID 不参与判等。text 的不同来源即使字节相同也建立不同 logical document；file 同内容在家庭内复用 canonical document。每个新 source 建立自有 receipt/task，跨 principal 不转移旧 task 委托；因此跨渠道同文件是两条 source/两个任务/一个文档。T09 按文档提取与业务事件版本保证不重复统计。

判等中的 hash/size 指实际字节结果；可选 expected_size/expected_sha256 只是传输断言，每次上传均校验，重试时从省略改为提供正确断言不改变业务身份。原名、声明 MIME、规范化后的 visibility intent 属于来源固定元数据，变化须报告来源冲突。

逐槽接收独立提交，多附件成功槽不随失败槽回滚，适配器维护预期槽清单并补传失败槽；T04 不提供虚假的整消息全部成功标记。上传先验证当前权限并记录 policy_version，再在无数据库写锁时 Put，确认 multipart 结束，然后单个写事务重新验证 credential/principal/同一策略代次，先查来源冲突，再建立 blob/document/source/task 和可见性审计。Core 提交而回执丢失由 lookup 恢复；DB 失败留下可复用 blob，不删除物理原件。

`inherit` 只需 ingest，新文档采用既定默认 TV 可见，已有文档保持当前设置。显式 `private` 是组合操作：在查询任何来源键/哈希之前统一要求 ingest + read + organize，新旧文件一致，最终事务再次检查；将既有 public 降为 private 时增加可见性版本并记录 actor/source/前后值/时间，同事务提交。已经 private 不做无意义修订。intake 不接受 public 意图，重发/重新解析不能把 private 提升为公开；也不能静默忽略 private 请求。此统一授权替代无条件“仅 ingest 可设置私密”的模糊解释，不增加人工审核流程。

保留集成鉴权、限流与追踪，但上传独立使用有界 deadline/MaxBytesReader/实际 body read deadline；现有 requireIntegration 五秒 context 不足以处理 32 MiB 流式接收，单独延长 context 也不能终止阻塞 Read。新增默认关闭的档案 intake feature flag，关闭时不注册业务入口、不启动消费者；本任务不改 OpenClaw 工具清单。使用真实临时 Core/SQLite/BlobStore/HTTP 验证，并实际 CLI/curl 走上传→lookup→task 查询；原生产服务保持关闭直至发布任务。

具体配置为 `archives.enabled`（默认 false），与旧 `family_archive` NAS 导出开关分开；原件沿用 T03 的 Layout.Archives（dataDir/archives）。服务层通过领域类型与窄仓库接口交接，archive 包不依赖 store。关闭开关时不初始化原件目录；退出时终止未结束的上传读取再关闭 BlobStore，避免被持有读取锁的阻塞 body 拖住。

红灯：未授权接收、重投、同键不同字节/intent、超限、接收途中取消、文件已写但任务提交失败、回执丢失、上传期间撤销/轮换/策略变化、多个附件部分失败、跨 principal 私密去重、默认重发不公开、private 组合权限和可见性审计原子性。验证：真实临时 HTTP server 上传并查回哈希及任务，不用 mock 代替落盘。

### T05 — 解析器接口与文本 / Office

新增：`internal/archive/parse.go`、`text.go`、`office.go` 及测试；`testdata/archive/` 合成文本、DOCX、XLSX、CSV 和异常包。

输出 `segments[{id,text,locator}]` 和 warnings；locator 包含段落/表格行列/工作表及单元格。UTF-8 校验和有界编码识别；DOCX/XLSX 只读取包内内容，禁网络引用和宏/公式执行；限制解压后总量、条目数和层级。公式保留原式及已有缓存值，缺缓存不计算假数值。

红灯：表格边界、合并单元格、缺失值、CSV 引号/换行、ZIP 炸弹大小、恶意路径、空/损坏/加密文件状态。验证格式矩阵逐项记录；只解析正文字符串不足以通过。

独立解析预审收敛（设计契约，尚未实现）：

- 解析器接收已授权原件的 ReaderAt/大小/哈希与格式 hint，不接收服务器路径，不导入 store，不联网或修改原件。输出 detected_format、parser_version、status、segments、warnings、实际资源计数与覆盖范围。区分 parsed/partial/empty/unsupported/encrypted/invalid/limit_exceeded；I/O 或取消返回 error，nil error 不能单独证明完整解析。segment ID 对同原件、同解析版本稳定，不承诺跨版本相同。
- locator 明确文本解码后字符范围（半开区间、单位明确）、CSV 逻辑记录/列及解码输入物理行/字节列、DOCX 包内 part URI/段落或 table path/row/grid-column/paragraph、XLSX sheet ID/名称/part URI/A1 地址。解码偏移不冒充原件字节偏移；T06 增加页与归一化矩形。单元格结构保留原始值、数据类型、公式/缓存存在性、样式与合并关系。
- 首期编码只支持严格 UTF-8（可带 BOM）及带 BOM 的 UTF-16LE/BE，校验 surrogate、奇数字节和解码后上限；不猜 GBK 或替换坏字节称成功。CSV 只支持逗号，逐条读取，严格引号，保留空格/尾空字段，宽度变化警告、不补零。Go CSV 的 CRLF→LF 和 FieldPos 字节列语义必须在证据中明确；以 `=` 开头仍是原始文本。
- ZIP 类型由 content types + 根 relationships + 主 part 类型证明，不靠扩展名、声明 MIME 或固定文件名。先支持 Transitional OOXML；Strict、宏、二进制工作簿等不支持的变体明确标记。namespace 按 URI。包内关系的相对 `../` 在归一化后仍位于包内可以合法，不能和 ZIP entry 路径穿越规则混淆；外部关系只警告，绝不读取。
- 初始限额：源 32 MiB、ZIP 4096 entries、单 part 16 MiB、累计解压 64 MiB、XML 深度 64、最终文本 2 MiB；另对字符串、cell、segment、关系数及压缩比设显式限额和测试。先有界预检 central directory 字节数/entry 数，再调用 zip.NewReader，之后校验实际目录；读取阶段也计数，不能只信 header。拒绝重复/规范化冲突 part、symlink/异常 entry/越界关系；只支持 Store/Deflate，不递归展开。用到的 part 读至结尾以验证 CRC；XML 不允许 DTD/自定义实体，超限或损坏不得称完整成功。
- DOCX 保留正文顺序、run/tab/换行和嵌套表格路径，横向 gridSpan、纵向 vMerge 保存 anchor/span/continuation；不重复输出 cell 与其内段落导致重复事实。未支持的页眉/脚注/文本框/修订/嵌入图像等若存在，记录 skipped parts/features 并标 partial；不把已删除修订混入当前正文。
- XLSX 沿 workbook 的 r:id 取 sheet 关系；支持 shared/inline/rich text、数值/布尔/错误/日期，shared-string 越界为 invalid。按稀疏坐标读取，区分缺格、显式空格、空字符串、零、无缓存公式；维度声明不驱动巨型矩阵。保存公式原文/属性/shared group 及可选历史缓存，不执行或伪展开；合并区域不复制金额。保留 style/numFmt/code，有限显示格式须逐项测试，未知格式保留 raw 并警告；date1904 与 1900 日期异常单独覆盖，无时区值不补时区。保留 sheet/row/column 隐藏标志。
- CFB magic 本身不证明加密，首版可以返回 unsupported_compound_container；只有有界验证 EncryptionInfo/EncryptedPackage 后才能称 encrypted。工作表保护不等于文件加密。未知/加密资料仍保留原件，并可如实解释未解析原因。

合成夹具对每类固定预期 segment、locator、原始值、合并/缓存/缺值与警告，包括正常四领域样本、中文工作表、两套日期、坏关系/重复 entry/CRC/真实解压超限。标准依据：[ECMA-376](https://ecma-international.org/publications-and-standards/standards/ecma-376/)、[Spreadsheet Cell](https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.spreadsheet.cell)、[Excel 日期系统](https://support.microsoft.com/en-us/excel/date-systems-in-excel)、[MS-OFFCRYPTO](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/b60c8b35-2db2-4409-8710-59d88a793f83)。这些限额与支持范围需实际实现验收，不以标准链接代替解析证据。

### T06 — macOS 图片 OCR 与 PDF

新增：`adapters/macos-archive-parser/` Objective-C 命令行、构建说明、合成样本；`internal/archive/native_parser.go` 及测试。使用 Vision/PDFKit，进程 stdout 只输出协议，stderr 不含原文。

红灯：中英文混排、旋转、双栏、扫描 PDF、混合文本/扫描页、页数上限、坏图/超大像素、进程超时；逐页 JSON 坐标能回到原件。无平台能力时返回 parser_unavailable，不能丢原件或宣称空成功。

验证：本机真实运行原生辅助程序，逐字段核对合成样本；Go 测超时/协议/资源限制。先展示原图和提取定位检查，再记录准确率；表格仍不可靠时保留待核对并报告，不删需求。

独立原生解析预审收敛（设计契约，尚未实现）：

- Go 接收 fresh OpenVerified 文件句柄，ExtraFiles 将原件交给 helper 的 FD 3，不传原件路径、不从 File.Name 重开。helper fstat 检查普通文件和大小，有界 pread 后使用 PDFKit/ImageIO data API。可信本机配置固定绝对 helper 路径；固定参数、小型 stdin 配置、必要环境白名单、不经 shell。解析器不依赖 store，不新增裸哈希读取入口。
- stdout 仅一个版本化 JSON，复用 T05 status/segments/coverage，加 engine 指纹（helper/OS build/Vision revision/languages/settings）、页几何、origin/alternatives/requires_review。页码从 1 开始，图片视为一页。矩形统一为显示方向、左上原点、归一化 `[x,y,width,height]`；Vision 下左原点转为 `(x,1-y-h,w,h)`。PDF native 与 raster OCR 共用 crop box、旋转、平移变换，避免二次旋转。若保留 NSRange 标明 utf16_code_unit，最低只承诺页和矩形，不冒充字符或原件字节偏移。
- 每个允许处理的 PDF 页面都渲染 OCR，不能因存在文本层或字符阈值而跳过。原生文本按行定位；只有位置匹配明确且除空白外文本一致，才组成同一证据组，保留 native 原文与 OCR alternative。不能先规范数字、负号、小数点或日期再当一致。重叠冲突/一对多不明确是一个 requires_review 组，不是两条独立事实候选；native 无视觉对应标 native_unconfirmed，OCR 独有区域保留。几何排序只用于稳定输出，不把数组顺序当行列、主体、单位关系。segment 标识不使用 Vision UUID，版本包含引擎/设置指纹，不承诺 OS 升级后 OCR 不变。
- ImageIO 关闭预解码缓存，先查实际类型/帧数/尺寸/EXIF 1–8，校验有限正尺寸和乘法溢出，再受限 thumbnail 解码。若 thumbnail 已应用方向变换，Vision 按 Up 处理。初始限额为图片 60 MP/最长边 16,384、单页 raster 16 MP/最长边 4,096、PDF 100 页、文本 2 MiB、20,000 segments、stdout 8 MiB；alternatives 也计入。多帧首期可明确 unsupported，不能只取首帧称完整。101 页预检 limit_exceeded，逐页释放对象；先一个并发 helper，性能验收后再调整。输入/raster 限额不是 PDFKit 内部解码的硬内存上限。
- Accurate、明确 revision 与语言配置，预检实际支持 zh-Hans/en-US，不静默降至英文/Fast；繁体另做实际夹具后宣称支持。parsed 只表示目标页文本提取流程完成，不等于 OCR 零错误；部分页失败为 partial，全部已处理但未识字为 empty/no_text_detected。PDF isLocked 才表示当前密码阻止读取，isEncrypted 不足以判断；首版不接收密码。复制限制记录为限制元数据，按实际可读取/渲染能力报告，不能仅因 allowsCopying=false 就误称损坏/加密。平台/helper/语言缺失返回类型化 parser_unavailable，保留原件。
- 解析专属总 deadline 初以 120 秒做基准，不沿用视频 15 秒；不能把总超时说成硬每页超时。CommandContext、有界 stdout/stderr（stderr 64 KiB）、超限取消、WaitDelay；Start 成功后一次 Wait 并在回收前持有并发槽。进程组取消若采用须实测，Process.Kill 不代表杀整个组。只接受成功退出和唯一完整 JSON，严格校验版本/页码/有限矩形/字符串数组/覆盖计数。生产错误仅固定码，不传播 NSError、原始 stderr、参数或识别文字；崩溃/超时/坏协议不能变成 empty/parsed。
- 实际夹具覆盖中英姓名/日期/金额/小数/负号/单位/区间、物理旋转与 EXIF 镜像、双栏表格、纯文本 PDF/扫描 PDF/扫描正文+文字页脚/多页混合、native/OCR 数字冲突、非零 crop box 与 90/180/270 度页旋转、非 BMP 字符、空白/损坏/锁定/101 页/超像素/多帧。Go helper 夹具覆盖挂起/取消/输出洪泛/半截或多份 JSON/坏版本/NaN 或越界框/退出异常与无残留进程。保存引擎指纹、原图与框叠加图，比较字段和框覆盖范围，不以整段 OCR 快照或仅字符串出现代替证据关系验证。

### T07 — 分类与事实提取执行器

新增：`internal/archive/understand.go`、`understand_test.go`、版本化 schema/prompt；受控 Agent worker 适配在 `adapters/openclaw/archive-worker.mjs`。

输入仅任务已授权片段；输出领域、文档类型、主体角色、原始字段、规范候选与证据。模型输出必须经结构/大小/引用验证，不能通过 JSON 填写本机路径或新权限。多页分块与合并在同一任务版本下完成；不截断后称全量。

验证固定错误输出、无来源数字、prompt 注入样本、模型故障、片段超预算；再用实际 DeepSeek 对合成四领域样本评估，不以 mock 通过代替真实提取质量。金额计算不用模型。

独立模型入口预审收敛（基于本机 OpenClaw 2026.9.8，尚未调用模型验收）：

- 新增仅注册私有 stdin/stdout CLI 的小插件及 manifest，通过公开 `api.runtime.llm.complete` 的 direct-provider 模式复用 OpenClaw 的 DeepSeek 路由/凭据。它需要真实插件宿主，不能在独立脚本伪造 api.runtime，也不导入带哈希的内部 dist 模块。旧 ask.mjs 只渲染工具回执且限 30 秒，不能当理解接口；agent exec 默认 coding 工具，isolated 不等于无工具。当前 isolated completion 接受 length 后丢弃 stopReason，不适合作本期默认。公开契约见本机 `docs/plugins/sdk-runtime/models.md`；实现时再次核验已安装版本。
- 专用宿主配置只包含 archive worker 与必要 provider，独立空工作目录/状态、无渠道/MCP/家庭工具/记忆；deny-all 工具、空 skills、禁止 contextInjection 和 Code Mode/Tool Search。completion 仅固定版本化 systemPrompt 与一个有界 user 证据消息。模型/端点/凭据/执行路径/系统提示由宿主决定，stdin 不可指定；allowedCompletionModels 约束包括默认模型的全部调用，首期无跨 provider fallback。必须验证 direct-provider mode、provider、请求模型及允许的 responseModel 映射；只接受 stopReason=stop，缺失/length/toolUse/error/aborted 都不是完整结果。采样 token 上限仅为 provider hint，进程输出另有硬上限。
- 单次子进程处理一个 chunk。请求包括 protocol_version、invocation_id、input_digest、schema/extraction/chunk 版本、owned/context-only evidence view IDs 及证据；响应回传身份/digest、provider/requested/response model、stop_reason、usage（可缺）与 result。Go 校验所有宿主标识、deadline、输入输出限额及退出回收；worker 用 AbortSignal。租约令牌/集成凭据/路径不进模型。固定错误码，不传播原始 prompt、响应或 stderr，也不从污染 stdout 中猜 JSON。
- 候选 schema 包含领域（健康/教育/财务/住房/其他及有界标签）、文档内 subjects/角色、原始 fields/单位币种日期/规范候选与各字段证据、coverage 和 issues。不要求预建类别/人物，不允许模型指定家庭全局 entity ID、账户或权限。宿主从不可变 parse artifact 构造版本化 typed evidence view：view_id、parent_segment_id、可选 alternative_id、component_kind、origin、Unicode scalar 半开范围与 text，并绑定 parse digest。组件限正文/单元格文本、数值原文或历史缓存、原始公式、格式代码及明确标注的派生显示；结构属性使用类型字段，不把任意 JSON 字符串都当成可摘录原文。
- 原文摘录须精确匹配父组件中的范围和文本；派生显示只接受宿主生成的 derived_value_ref，保存原件/parse digest、parser version、同格原始值和格式依赖，不伪装为原文引文。T05 的 Segment.Text 在数字格可能已是显示值，不能一律当原始值。规范数值从完整 raw/cache 十进制 token 重解析，不能从 130 截出 30；完整 token 边界在父组件验证，不能用分块边缘绕过。金额保留精度，百分比只做宿主明确支持的格式换算，不让模型补年份/币种/时区或算合计。未知证据、不匹配摘录或伪造数字使整个 chunk 失败，不能悄悄丢掉后称完整。
- 公式及属性单独作为公式证据，历史缓存不是执行结果；raw 与 CachedRaw 指向同一值时不能产生两次观测。shared-string 索引、布尔 1/0、公式常量或单元格地址不作为实测金额/指标。缺值、空字符串与零保留区别。首期日期派生值只接受解析器给出的有效完整 yyyy-mm-dd；两位年份格式不得自行补世纪，1900 日期系统产生的虚构 `1900-02-29` 不得作为 Gregorian 日期。T07 校验解析器给出的完整日期，不按 raw serial=60 拒绝 1904 系统的合法日期。当前 ParseResult 没有显式 workbook date system，不能猜纪元再声称独立核算；保存解析产物及原件出处，显示格式也不证明公式重算或原值只有显示精度。
- Core 强制继承 requires_review/native_unconfirmed，模型不能清除；冲突 alternatives 仍是一组待核对证据。医生/患者、学生/老师、房主/工程师等角色保留；“妈妈”等无参照称谓保留歧义。每个 owned fragment 或空单元格证据单元恰好归入 extracted/irrelevant/insufficient，context-only 不产生重复覆盖责任。
- 初始 chunk 限 128 evidence units/实际序列化证据 JSON 24 KiB/完整输入约 32 KiB，输出 64 KiB，单资料最多 256 chunks，经实际四领域样本校准。按页/段落/表格边界分块；最长 256 KiB 的父文本组件进一步拆成 Unicode 范围，保留 CRLF、空格和全部字符，owned ranges 精确分区，重叠上下文只属 context-only。alternative group 不拆散，单组超限明确失败；必要页眉可作重复上下文。确定性 merge 检查全部 chunk、按父组件及完整证据去除精确重复、保留冲突，不按同数值/姓名合并事件；不要求再调模型重读全档。超限明确返回，不能截断。
- T07 提供 PlanChunks/UnderstandChunk/ValidateChunk/MergeChunks 和进程适配，生产调度在 T09 接入。Task.StateJSON 上限 64 KiB，只存版本/产物引用/进度/预算；T09 另外持久化不可变 parse/chunk products、typed view 版本/组件 origin/范围映射与 chunk ownership manifest。产物键绑定 task/extraction、evidence view/chunker/prompt/schema、证据 digest 和模型配置指纹；保存产物与推进 checkpoint 同一事务。view ID 只标识本版本证据，不能代替持久 event/observation ID。
- T09 Core 单 worker 先短事务 Claim/准备授权输入，事务外调用，后短事务校验 lease/principal/policy/StateVersion/extraction 并提交产物；调用中续租，撤销/取消/续租失败终止子进程。重启同一 task，只复用已提交且版本匹配的 chunk，缺失部分重做；不能承诺 provider exactly-once 计费。有限网络退避/schema 最多一次修正，次数/预算持久化；理解完成仅推进 understood，正式发布前不把整个任务称 succeeded。
- 验收增加有效 JSON 但 length、实际模型错配、chunk 漏覆盖、OCR 冲突被擅自清除、输出洪泛/取消；T09 增加模型返回后提交前中断、checkpoint 后中断、租约重领、模型调用时 SQLite 可写、超过 64 KiB 的全产物恢复。T07 实际 DeepSeek 四领域验证记录 provider/responseModel、字段/角色/引用/覆盖质量和零工具证据；本次预审不计作模型验证。
- 事件输出为 extraction-local event_proposals，包含临时 ID、事件类型、成员字段引用与区段证据；字段的分组和引用同样验证。模型临时 ID 不具有跨版本含义，不接受模型指定现有 event_id/observation_id。Core 在 T09 持久分配并核对事件区段与观测身份；一份报告可以是 document-root 事件，交易表可逐行分组，不能每个 OCR 片段生成一次事件。

解析到理解的交接夹具覆盖：256 KiB 中文/emoji/CRLF 父组件的完整分块与实际 JSON 大小、数字处于块边界及重复真实事件、raw `151.234` 与显示 `151.23` 的精度区别、两套日期系统/两位年份/serial 60、SUM 历史缓存及 absent/empty、shared-string 索引和布尔不是金额。T09 验证重启复用同版本产物、view 版本变化使旧 checkpoint 失效，以及改变分块边界不增加业务事件。以上为实现契约，尚非模型或流水线验收。

### T08 — 实体匹配与可撤销关联

新增：`internal/archive/resolve.go`、`resolve_test.go`；`internal/store/archive_entities.go` 及迁移/测试。

红灯：两人/同名/别名/“妈妈”无参照/医生与受检人/共同房产/多被保人。独立明确主体自动创建；已有匹配需明确证据，冲突进待归属；文档可多实体多角色。合并/拆分是带版本的关联修订，不能删除证据历史。

验证人物创建不新增账户或管理员权限；旧阿布映射到已有 stable ID；单份多人文档不复制原件也不串事实。

独立归属预审收敛（设计契约，尚未实现）：

- 纯 resolver 接收已经验证的文档内 mentions、角色/身份字段证据、日期精度、OCR 冲突标志，以及 Core 从同一家庭取得的候选快照；不查库、不生成实体 ID，也不接受模型指定全局 ID。快照带家庭 `resolution_revision`、实体版本和 complete 标志。截断或匹配能力不可用不能当作候选为空。输出 link_existing/create_entity/needs_resolution/household_document、规则版本、理由码、证据和依赖版本；新实体先用计划内引用，同批新建意图参与后续匹配。
- 清楚的具名主体和角色、候选确实为空时自动建独立实体，不推断关系、性别或家庭成员身份。姓名/有证据别名加完整出生日期一致、唯一候选且无明确冲突，可以关联；唯一同名而无辅助证据仍待归属。双方完整生日明确不同且无共同强身份依据，可新建第二个同名人物；强身份相同却生日冲突则待核对。年/月生日、年龄、性别、发送者、文件名和模型 confidence 不独立证明身份，不补齐日期再匹配。OCR 冲突证据不能成为确定身份的依据。
- 完整稳定身份标识按家庭、类型、签发方和带版本的精确规则匹配，保留前导零；唯一匹配且无冲突可以关联。原值只留原件与受保护事实，仓库内有界比较，不建立裸 SHA 或普通身份号索引，不为此新增密钥系统。向 resolver 只返回匹配实体 ID、证据引用、冲突及完整性，历史身份号不进入模型上下文、普通候选 DTO、FTS、日志或 TV 摘要/导航。原件保留原字节，其独立读取遵守既定默认 TV 可见与显式私密闭包，不因识别出身份信息自动将资料设为 private。超限、未知类型或签发方不足返回 incomplete/unsupported，不能当作无候选再自动建人。
- 名称保留原文；比较键使用带版本的 NFC、首尾空白删除、连续 Unicode 空白折叠及 ASCII 大小写折叠，NFC 使用锁定版本的 `x/text`。保留标点、内部词界，不自动简繁转换、拼音化、删连字符或 OCR 近似纠错。普通 display_alias 只参与候选；confirmed_binding 保存作用域、目标、确认者、版本与证据，作用域相符、目标唯一且无冲突才自动关联。首版支持 legacy_member 和 document_mention；单份纠正不升级为永久全家身份规则。后续资料别名规则只有明确授权后才创建，亲属称谓仍需明确参照。
- 无参照的“妈妈”待归属；医生、工程师保存相关角色，不成为家庭成员。非档案主体的相关人仅保留文档局部角色，不为了每个签字人新增人物归属问题；另有明确档案主体证据时再参与实体决策。多人文件逐主体 mention 决策，字段不能全部挂到第一个人物。房屋完整地址精确匹配须保留楼栋/单元/房号差异；设备型号不是唯一身份。无个人主体的学校通知使用 household_document，不制造未知人物。
- 任何实体、别名、确认绑定或合并修订均增加家庭 resolution_revision。T09 的同一写事务除 lease/principal/policy/StateVersion/extraction 外，还检查该 revision 和受影响实体版本；过时就回滚、重取一致候选并重新运行 resolver。事务内检查同批实体和绑定冲突。两个任务同时看到空候选不能分别照旧建人；第二个重新关联或待归属。实体写助手留在 store 包内，与事实、关联、checkpoint/outbox 同事务，禁止 service 先建人再另开事务提交事实。
- 实体属性保留来源，私密证据中的姓名、别名和生日不能经合并后的展示名、TV 导航或公开搜索泄露；公开投影只取当前可见证据。仅 ingest 的回执不暴露历史候选，澄清候选详情需 read，人工改归属/合并需 read + organize。
- 合并保存旧 ID、原绑定和版本化重定向/事件，拒绝跨家庭、跨类型和循环；原绑定记录 matched_entity_id，查询解析当前 canonical entity，不重写历史外键。撤销合并恢复原绑定；合并后新增且无法区分原主体的关联转待归属，不猜拆分。身份合并不等于事件合并。legacy 使用 `(household_id,legacy_member_id)` 唯一持久映射；回填、重放、改名和后续旧记录复用它，不改 member 权限。`abu` 只在可信 legacy scope 内有权威含义，不代表未来所有“阿布”。

夹具至少覆盖空名单自动建、仅同名待归属、同名同完整生日关联、不同完整生日独立建、同名同生日多候选、部分日期不补齐、身份冲突/OCR 冲突、别名与绑定作用域、无参照称谓/相关角色、多主体房产与保单、两个任务及同批并发创建、合并/撤销/跨家庭拒绝、legacy 稳定映射与私密属性公开投影。T08 只证明决策和仓库不变量，实际模型输出到事实发布的闭环在 T09 验收。

### T09 — 原子提交、澄清与修订

新增：`internal/store/archive_extractions.go`、`internal/archive/commit.go`、对应测试及迁移；扩展任务查询和澄清 API。

在有效租约/授权下，原子提交版本化片段、分类、实体关联、事实及索引/outbox 任务。丢失租约不得提交一半。澄清沿用 task_id 和预期版本；过期回答返回冲突，缺主体不影响其他附件。

本任务还负责把 T05–T08 接到持久任务消费者，落地 T07 规定的 parse/chunk 产物与 checkpoint 原子事务、外部调用期间续租/取消、版本匹配恢复及持久预算。大产物不放入 64 KiB Task.StateJSON；在正式事实发布前，仅作为内部候选产物保存，不能被查询/统计当作已确认事实。

红灯：提交中断、并发澄清、重复模型结果、改归属、撤回、私密修订及重新提取；历史事实不能重复计入统计。增加 archive_subject_events 区分物理去重和真实事件，三种计数规则按档案设计验证。

独立发布预审收敛（设计契约，尚未实现）：

- extraction 绑定 document/task、完整 pipeline 指纹及基准 document_revision；parse/chunk/merge products 不可变并保存 digest，包含 T07 typed evidence view 版本、组件 origin、范围映射和 ownership manifest。已发布事实引用父组件/原始范围或明确的派生依赖，不依赖某次 chunk 边界；只有全部指纹相符才复用产物。SaveProductAndCheckpoint 和 PublishExtraction 留在 store 内部事务，不能拆成 service 的产品保存、建实体和 Finish。发布同时检查 lease/principal/policy/StateVersion、目标 extraction、document_revision、resolution_revision 和受影响实体版本；成功原子切换 active 发布版本、当前事实/关联、outbox 和任务状态。同文件另一个来源可复用已完成且指纹相同的产物并完成自己的任务，不转移任务身份或扩大 ingest 回执内容。
- Core 建立持久 section/event 与 observation slot 身份，保存物理锚点和规则版本。相同原件的重新提取，仅可比较且唯一的一对一原件锚点可自动复用，例如 OOXML part/table/cell 或 CSV 逻辑记录；PDF 必须验证相同页和 crop/rotation 坐标系，明确几何重合阈值、唯一性与拒绝规则。segment ID、模型临时 ID、姓名和日期不能成为跨提取稳定键。区段 split/merge、覆盖不全或新增事件区段语义不明时保留旧 active 事实，新候选等待澄清；已明确事件中的新增字段不等同新增事件。主体更正只修订关联，不换 event_id。
- 同一 observation 的值变化保存不可变 revision，发布版本显式引用当前 revision，历史不计入统计。跨文件仅在支持的完整交易标识、类型/签发方唯一且无冲突时归并；多份证明挂同一观测，冲突保存 alternatives，不能叠加金额。疑似重复披露不确定性。重新提取缺字段不自动撤回旧事实；覆盖完整且对应明确才替换。首次 partial 解析可保留有完整证据的确定结果并披露覆盖范围，不能声称整份解析成功。
- 人工更正、归属、私密为独立修订，重新解析不得覆盖；用户补充值注明 user assertion，不能伪造原文引文。发布、撤回、改归属与私密修改递增 document_revision。私密区域立即限制未脱敏原件、整页缩略图及上下文并使派生内容失效；重新解析无法对应旧私密锚点时继续限制整份原件。人工修订使用独立审计结构，不冒充 0012 中绑定 intake source 的可见性事件。
- 澄清公开投影只向有 read 的调用者提供 question_id/question_version/waiting_task_version/extraction_id/document_revision、安全证据摘要与允许选项。提交携带这些版本、resolution_key 和 typed answer；自然语言解释出的动作也须在同一仓库事务核验当前问题、允许动作/目标、依赖版本、当前 read + organize 和原始委托有效性，再保存问答并恢复同一 task_id。旧问题/过时答案冲突，回答不刷新原委托、不暴露 lease。沿用 T01 的任意 JSON ResolveWaiting 不能代替本业务验证。任务投影补齐 partial/empty/encrypted/invalid/limit_exceeded 等真实解析状态。
- 外部调用前事务预留 invocation 与最大预算，结果和 checkpoint 原子提交；调用后崩溃导致计费未知时不退还预留预算。调用、schema 修正、退避及累计预算跨重启保留，耗尽明确终止，Attempts 领取计数不能代替预算。消费者仅领取自己支持的 task kind，不能拿走索引/NAS 任务。
- T09 建立 legacy 连续 outbox 生产和幂等投影，旧记录写入同一事务入队；legacy record ID 保留修订映射，supersedes 链根对应同一事件。消费读取链当前状态，乱序回放不回退；T17 完成全量回填/恢复验收。T10 的 document_changed(household,document,revision) outbox 在消费者实现前保持待处理，不虚报索引已完成，也不阻塞已确认事实发布；检索读取仍按当前 DB 版本和 legacy 状态过滤。

故障夹具覆盖外部调用/产品提交/发布各中断点、同文件两来源并发、同文不同事件、重 OCR 值更正、root 报告与逐行交易、区段 split/merge、新字段和主体更正后事件 ID、并发/过期/越权澄清、人工值与私密保留、legacy 新增/更正/私密/撤回乱序回放，以及预算耗尽重启不恢复无限重试。

### T10 — 统一检索与证据

新增：`internal/archive/search.go`、`internal/store/archive_search.go` 及测试；FTS 迁移；查询 API。

实现实体/类别/日期过滤与原文检索；原件/片段/事实/关联先授权后排名。中文两字姓名和短词必须有别名及有界子串路径；分页稳定、字面查询参数化，不允许任意 SQL/FTS 语法注入。索引只含有效可读版本，撤回与重建一致。

合成四领域检索集 ≥ 30 个问题，每条预期档案和证据点可检查；测试姓名别名、多主体、未知日期、零结果、私密/撤销、分页和同文件多来源。具体样本与 42 项预期见[综合档案合成验收集](2026-10-03-atriumos-archive-acceptance.md)，它是待执行契约，不是通过报告。记录召回不足，必要时再增语义检索，不以加入向量库作为验收。

独立检索预审收敛（设计契约，尚未实现）：

- 小型 schema 仅含字面 query、实体/领域/资料类型、日期范围、归属状态和 limit/cursor；家庭、principal、TV/集成投影由宿主决定。默认 10 条、最多 25 条，按实际序列化字节分页，遵守现有 MCP 32 KiB 上限。返回 items/next_cursor/coverage/index_status；不完整的零结果不等于没有资料。实体筛选仅使用已确认关联。
- 当前 credential/principal/household 或 screen、发布引用闭包、可见性与 legacy 当前状态在同一短只读快照核验，不依赖 middleware 的旧身份，也不先取前 N 条再过滤。本机 modernc v1.58.0 的 BeginTx(ReadOnly:true) 会跳过 `_txlock=immediate`，实现窄只读 helper 并实测搜索期间 writer 可写。授权承诺是该读取快照时有效，不承诺网络响应后的瞬时撤销。
- 长字面查询由 Core 编码为受限 FTS 参数，短姓名/两字词使用别名与参数化 instr；引号、问号、NOT、星号等不能获得操作符含义。短词扫描有字符量、候选量和 deadline 上限，耗尽如实返回不完整/需缩小范围。索引采用稳定业务排序和 ID 次序，避免依赖包含不可见资料的全库 BM25。复用现有签名 cursor 机制，绑定主体/策略、投影、规范筛选/排序、数据与索引代次和最后排序键；条件变化明确要求重查。
- FTS/片段/标题/候选/导航只使用安全搜索投影，排除明确支持的受保护身份类型及已识别证据范围，不无条件索引全部 parsed text。投影未准备好先仅有安全元数据，测试支持的类型而不宣称通用个人信息检测；这不自动修改文档可见性，原件读取独立应用既定策略。待归属文档可检索并标 waiting/unassigned、解析状态和原文证据，未决模型字段不进入已确认事实或个人统计；已确认家庭通知不同于待归属资料。TV coverage 不泄露不可见私密资料数量。
- 证据包含 document_id/publication_revision、observation_id+revision、source_extraction_id/segment_id/locator 和有界摘录，校验属于当前发布的引用闭包。T09 可保留旧提取的当前事实，不能强制来源 extraction 等于最新 extraction。历史读取须明确且继续当前授权。partial、OCR alternatives、index pending 均保留覆盖状态。
- outbox 按当前文档版本重建，旧消息重放不得恢复旧内容；撤回/私密/改归属立即依赖 DB 过滤。重建分批新 generation 后切换或明确 indexing，不能清空后返回完整零结果。10k 文档性能同时记录总索引字符、长度分布、查询选择性、私密比例及 P50/P95，不以文档数量暗示任意 10k × 2 MiB 都满足固定延迟。

### T11 — 自然语言查询工具与来源回答

修改：`internal/homemcp/server.go`、`internal/integration/`、`internal/brain/model_tools.go` / `executor.go` / `receipt.go`、`adapters/openclaw/index.js` / manifest / tests。

工具：接收任务状态、检索、文档证据、实体查询、受控澄清/更正。宿主注入 request/operation 标识，模型只能传业务条件。回复区分原件、提取事实与解释，任何声称“已保存/已同步/已更正”必须有 Core 回执。

验证完整工具库存、参数边界、工具结果上限、模型实际查询两领域以上并正确引用；旧十三项工具回归继续通过。

实际接线须同时更新 ModelTools 白名单、executor 参数表、OpenClaw inventory/manifest、receiptResult 和 receipt-renderer.mjs；ask.mjs 最终渲染回执，仅新增工具注册不足以交付回答。模型解释问题并真实调用 search/entity/document-evidence/task/typed-clarification，T12 再加入 analysis；Core 返回结构化事实/引用，宿主渲染出处、覆盖范围与计算结果，不展示未经验证的模型最终文字。partial/coverage 放入有版本的 data schema，不滥用旧 envelope 不接受的状态。验收记录实际 OpenClaw 调用参数、Core 结果和最终引用，不用问句到预填答案的映射；既有 30 秒/8 次调用预算耗尽需明确本轮未完成。

### T12 — 基础分析与数据覆盖

新增：`internal/archive/analyze.go`、`analyze_test.go`、分析快照存储/API。

代码计算 count/sum/latest/time series；原始字段保留，单位/币种不一致分组，日期精度不足不能放到猜测日期。支出不将同笔账单/付款凭证直接重复相加；无法证明同一事件则提示候选重复并标覆盖范围。结果绑定事实版本，修订后失效。

验收：喂养 330 ml 奶粉 + 120 ml 母乳=450 ml；70 ml 是最近一次不是当日合计；教育不同满分不直接比较；金额用十进制精确表达；医疗参考范围来自报告。只说已归档范围，不推断全家全量事实。

独立分析预审收敛（设计契约，尚未实现）：首期支持 count/sum/latest/series/ratio，count 明确数文档、事件或 observation。当前 canonical event/observation slot 才参与计算，line_item/total/payment 角色须有表头、标签或公式证据；角色不明者排除并披露，不能按金额相等或模型 confidence 推定。F01 合计缓存 200 不再算一笔费用，F02 付款证明不重复增加 charge；CNY 授权范围为 250.00、TV 为 200.00，USD 20.00 单列。

使用有界十进制字符串与整数尺度/有理数运算，不沿用旧 float64 measurement 或 SQLite REAL 求和。币种、单位、项目/样本不兼容分组，换算仅支持明确的版本化规则。ratio 保存分子/分母与舍入方式；88/100 与 132/150 都为 88%，变化 0 个百分点；零分母、缺满分、比较符数值不当精确值。家庭时区与日期精度参与区间判断，粗日期重叠不能强选最近一次，未知总量不生成完整率。

分析快照绑定算法版本、规范查询、投影、输入 observation revisions 和数据代次；新增匹配事实也使结果过期，不只跟踪原有输入变化。读取快照取数后计算，保存时短事务重新核验。TV 单独计算公开集合，不能删私密明细却保留其总额。故障验收覆盖停 outbox、重建、撤回/私密/改归属、新数据和过期 cursor，及旧 extraction 保留事实的有效引用；所有数值同时有字段证据与覆盖范围。

### T13 — 微信与 Discord 可信附件接入

扩展：`adapters/openclaw/` 入站桥接、`scripts/check-openclaw-ingress.mjs`、协议和运行文档。

从实际渠道原生消息边界建立可信来源清单，校验账户/会话/原始消息/附件序号和可读根，不让模型传路径。当前 before_dispatch durable claim 与 intake 必须可恢复关联，避免 claim 成功后进程崩溃造成永远拒绝重投却没有接收任务。重投查询同一任务，不能重新执行完整归档。以下已核实的通用 hook 限制不能用假设字段绕过。

红灯：hook 字段缺失、Discord 多附件、微信图片/文件、跨渠道同文件、源文件清理前保存、路径越界、临时 ID、duplicate delivery。真实渠道最终由用户提供新合成/真实文件进行验收；不代用户发送未授权外部消息。

独立实装代码预审（微信 2.4.9、OpenClaw/Discord 2026.9.8；设计契约，尚未实现）：before_dispatch 没有 media 字段，message_received 是 fire-and-forget，inbound_claim 只交会话 owner plugin。微信当前在处理消息前保存 poll cursor，媒体只取 IMAGE/VIDEO/FILE/VOICE 首项并可能退回引用；CDN arrayBuffer 无流式上限，100 MiB 保存限制太晚。Discord 已有持久 MESSAGE_CREATE 队列及有界下载，但默认 100 MiB，preparedMedia 混入引用/转发/贴纸且丢失部分原生属性，文本还可能 debounce 合批。T13 因此使用固定版本、可重复校验的渠道桥接补丁，不假造通用 hook 能力。

- 微信冻结渠道 accountId、from_user_id、原生 direct conversation、无损 message_id 及原 item_list 下标；合法 fallback 记录具体来源字段，缺稳定身份不归档。Discord 使用原始 id/channel_id/author.id、渠道 accountId、原附件位置和 attachment ID；不能用新回复线程、显示代理身份或合批最后一条消息替代。首次槽映射不可变，失败槽不压缩编号，编辑后同槽换字节冲突。T04 来源全部由宿主填，模型只能选择 opaque message/slot refs。
- 微信在 monitor.ts 保存游标前等待 CaptureBatch 完整持久化获准消息与槽清单，之后才推进 cursor；捕获后/游标前崩溃按原生身份重放，额度不足不推进。捕获先执行渠道/本地 allowlist 和会话准入；拒绝者最多留最小身份摘要和拒绝码，不存正文、附件 URL 或解密信息。原文及恢复凭据只在有界、短期、受保护的操作收件箱，普通聊天处理后清理，不进入 Core 档案。Discord 在获准的原始消息路径做等待式交接，队列 settle 前确认成功，不依赖异步观察钩子。
- 微信 process-message.ts/media-download.ts/pic-decrypt.ts 保留所有当前槽并逐槽记录失败；下载使用受控 provider URL、重定向策略、deadline、密文/明文流式上限，解密结束和长度/校验完成才发布 plaintext，明文遵守 32 MiB。Discord 复用有界 downloader 并收紧档案上限。错误仅固定码，不回显 CDN URL、AES key 或响应体。引用/历史/转发/贴纸保留独立 provenance，首期不自动归档；明确保存引用文件时仍需原消息身份和重新授权。语音转写不冒充原始文字。
- 模型运行前，宿主从允许根中的已打开 regular-file FD 将下载内容复制到 Brain 私有 spool，逐级拒绝 symlink，流式算 hash/size、fsync、原子发布并同步目录后才标 staged。之后按 opaque spool ref 复核字节，不重信原路径；提示中的 media 路径、模型参数或字符串前缀不提供访问权。原名/声明 MIME 一次确定并持久保存，重试不换临时名或重猜 MIME。
- 当前直接文件按用户既定输入即归档的意图处理；当前可信附言为空可直接持久 inherit。附言非空先 awaiting_intent，由现有正常消息模型轮调用归档工具提交 inherit/private/needs_clarification，完成后才接收；不另建通用分类器、不逐文件要求确认，也不能超时或漏字段后先按 inherit 上传。宿主绑定消息身份、完整附言及 digest、槽清单和意图版本，保留包含否定/限定的原文证据；附件正文/OCR/引用消息不能发出可见性指令。测试“不要在 TV 展示/这是私密”和否定私密的相反表达。已持久意图的 reconcile 不再询问模型。
- 显式 private 仍需 ingest + read + organize，接收事务再次核验；缺权限明确 forbidden，受限暂存保留，不降级 inherit。已接收后的新私密要求走 T09 修订，不改原 intake intent。文字由归档工具选择宿主绑定的当前原文，普通问答/澄清不新增文本事件。模型不能自行写一段文字冒充收到的原文。
- intent 持久化后每槽 downloading/staged/submitting/core_saved 与重试/终止状态独立。先 reconcile 收件箱，再检查既有模型轮消费账本；只补接收，不重跑已消费模型轮、屏幕或其他工具。每次重试先以冻结来源 lookup，已保存则核对 hash/size，没有才上传同一 spool；Core 成功但回执丢失按此恢复。固定同一 Core principal，凭据不能悄悄换属另一 principal 重新建来源。
- ingest-only 账本只需自有 receipt/task、saved/hash/size/time，不要求 document ID 或 read。部分成功槽保留，全部 Core saved 前不说“附件全部已保存”；本地 staged 只称“已暂存，尚未进入档案”，NAS 由 Core 独立回执确认。registerService.start/stop 管理独立、有界、持久退避的 archive-only reconciler，不依赖会空闲退出的 host.js 模型进程。JSONL RPC 单条 64 KiB，不塞附件 base64，新增宿主清单/spool 专用操作和有界批次。
- 补丁保存在仓库，固定包版本、原文件 hash 和锚点并覆盖实际加载 dist；不符就拒绝启用，不由生产桥接动态导入带 hash 的内部模块。故障测试覆盖整批捕获/游标各中断点、中间槽失败、引用混入、Discord 合批来源、伪造路径/ID、下载/解密失败、symlink 替换、回执丢失、已消费消息重投仅补传、服务重启/撤权和有限重试。最后仍需新消息的真实微信/Discord 入站验证。

### T14 — TV 档案信息架构与详情

修改：`web/src/screens/FamilyScreen.tsx`、`web/src/screens/family/`、`web/src/types/family.ts`、`web/src/core/` API、`web/src/styles/family.css`；复用 `TopNav` 与 `RemoteButton`。

全家档案默认页、人物/房屋/领域筛选、处理状态、来源和时间线、原件预览；搜索可用 IM 发起+TV结果，以及 TV 遥控可操作的筛选。保留 family 路由和旧喂养记录兼容层。标题和指标为中文；日期明确到已知精度，同日事件显示时间。

验证私密子内容的原件/缩略图/片段/缓存闭包、公开统计独立计算、授权失效后清除、旧请求不覆盖新状态、焦点/返回/滚动保持、Chrome 66 内置与 CSS 限制、OnePlus 实屏。构建命令 `npm --prefix web ci` 后 `npm --prefix web run build`，按 package.json 的测试命令运行，不猜脚本。

独立 TV 入口预审收敛（基于现有页面/授权/命令代码，尚未实现）：

- 默认 `全家 + 全领域`，不自动选择第一个 legacy member；没有预登记人物也展示家庭共有和待处理资料。人物/房屋来自 T08 实体，旧成员用稳定映射兼容。新增档案列表/详情/任务摘要 DTO，不把新字段硬塞进旧 FamilyRecord；包含版本、解析/理解/归属状态、coverage、原件保存及 NAS 状态，不暴露内部 task state、来源账号或私密资料数量。首期过滤包括人物、房屋、领域和待归属状态。
- 新增配对屏幕专用只读仓库/API 路径；现有 archive 授权仅支持 integration，不能直接复用其私密读取能力。`/archives/*` 有界 GET/HEAD 在同一短读取快照核验当前 active screen、服务端 local household 和公开发布引用闭包，不只信 middleware 身份。列表、筛选、详情、证据、原件及分析固定 audience=tv，客户端不能指定家庭或扩大 audience；符合当前 cookie 只允许 GET/HEAD 的约束，首期 TV 不增加整理写入口。
- 档案展示有效期独立于全局 24 小时鉴权缓存：初始每 15 秒重验，最多 30 秒未验证展示期，其他接口成功不能续期；展开历史仍重验权限和资料版本。新增 archive generation 及 `archives` topic 的枚举/coalescer/refetch/当前页 refresh 链路。收到撤销/私密失效先清除相关内容再重取，筛选或授权变化使旧请求无效；断线到期隐藏，页面恢复可见先重验。列表、详情、摘要、图像 DOM/object URL 都清除；承诺读取快照授权和有界失效，不声称撤回已发送字节。
- T14 同时实现档案预览产物与路由；T06 OCR 坐标不是可展示页图。图片/PDF 提供资源有界的页图，文本/CSV/Office 提供保留 locator 的结构化内容预览，明确不是原版式复现；未知/加密文件显示原件已保存且暂不能预览。接口只接受 document ID、版本、页号等白名单参数，无路径/裸 hash；每次读取核对当前公开闭包、`Cache-Control: no-store`，不套用现有照片的 60 秒缓存。验证 cookie 直载和 bearer fetch + object URL，凭据不入 URL。子内容私密而无经过验证的脱敏副本时，禁止未脱敏整份原件/整页图；其他原件仍遵守用户默认可见偏好，不新增自动 PII→private 规则。
- 摘要和趋势消费 T12 公开分析 DTO 的精确十进制字符串、单位/币种、算法版本、覆盖范围及分母，不从当前分页求全家合计。图表浮点只用于坐标，显示数值和合计仍来自 Core。筛选改变重查；待归属、未决 OCR 和金额角色不明不进入确定统计。year/month/date/datetime/unknown 按精度展示，不统一 Date.parse 补日期。新档案金额代码不依赖 Chrome 66 没有的 BigInt。
- 保留 family 路由、宋式 tokens/RemoteButton，补有类型的 list/detail/evidence 状态及返回栈；保存筛选、cursor、稳定 item ID、scrollTop 和焦点。资料卡片可遥控进入详情/证据，阅读区滚动不代替卡片操作。刷新不抢焦点；目标撤回后回到最近可用项。沿用现有 legacyApis 与 CSS 兼容检查，并补实际浏览器和 OnePlus 验证。
- IM→TV 结果增加宿主授权动作，不能把当前只能开 family 首页的 navigate 当作搜索结果展示。宿主工具提交已有查询回执引用；Core 核验当前 archive.read + screens.control、家庭/屏幕范围，创建绑定目标 screen/家庭/有效期的 opaque archive_view_id。命令只携带该引用；TV 再取公开结果，不把 IM 私密结果塞进命令，公开结果重新按 TV 投影计算。使用档案专用授权分支，不沿用“允许所有 NAS source 才可导航”的照片规则；串联 ModelTools/执行器/OpenClaw 工具清单/回执渲染。实际结果加载并呈现才 ack applied，过期/撤权/失败明确返回，不能只路由切换即声称已展示。

最小行为验收覆盖无人预登记的四领域全家列表、房屋/待归属筛选、分页后统计不变、公开 CNY 200 与授权端 250 分离、展开历史/断线/过时请求/图像解码期间私密撤销、详情返回焦点、真实查询回执到 TV accepted→applied，以及 Chrome 66/OnePlus 实屏。此处为后续开发契约，不代表功能通过。

### T15 — 端到端综合档案体验

新增：`scripts/e2e/archives/` 与混合样本验收脚本（生产数据不提交）。

在隔离 Core 接收至少两个未预建人物、四领域和全部首批格式，通过真实解析、真实模型、Core 提交、检索与配对浏览器展示。模型不足不能用预填答案替代。歧义样本恢复原任务、重复不重复统计、待归属可检索。停模型/Core/NAS 分别检查降级。

### T16 — NAS 归档与可恢复清单

新增：`internal/archive/export.go` / `restore.go`、测试、CLI 支持；复用已存在挂载身份校验，保持照片目录只读。

原件、提取版本、实体与关联、事实、业务事件稳定 ID/归并证据/文档事实关联及索引重建信息的清单包含哈希及 schema；先文件后清单，全部读回成功才确认。导入到新目录，校验引用闭包、权限范围、冲突、版本与大小。恢复不重放旧操作/通知。

验收：NAS 断开/错误挂载、写一半、损坏哈希、缺文件、重复恢复、旧家人 JSON 与新归档共存、完整恢复后四领域检索及三类事件计数、账单/付款凭证归并结果一致。

独立恢复预审收敛（基于当前 SQLite backup 与 familyarchive 实现，尚未实现）：

- 承诺指定恢复点的完整档案数据；现有 SQLite VACUUM INTO + 配置副本不含新原件，旧 family restore 也只覆盖旧记录，均不能冒充新档案备份。清单闭包包含 household/local-household、blob、document/source/receipt、必要提取与 typed evidence manifest、当前 publication 引用、event/observation 稳定 ID 与修订、实体绑定/合并历史、人工修订/私密闭包、legacy 映射；当前事实引用的旧 extraction 也保留。FTS/预览/分析缓存可重建，其已确认事实和证据/规则版本不能省略。
- 恢复不按姓名/日期/hash 重建业务身份；保留 household/source key/历史 principal 引用和所有持久事件/观测 ID。新库迁移自动建立的随机家庭仅为 bootstrap，恢复初始化须在确认为空的新库中替换该身份，不能把资料迁入另一家庭。principal 外键用无凭据、无有效授权的历史占位记录满足，历史策略仅作审计，不恢复活跃集成/屏幕/管理员。家庭默认可见偏好和私密修订属于数据；provider、可执行路径、挂载路径和凭据由新主机可信配置决定，manifest 不得修改运行配置。
- 从一致数据库快照生成本机有界 manifest/分片，记录恢复点、schema、完整引用及对象 hash/size；NAS I/O 在事务外。先发布并读回全部不可变对象，再发布并读回恢复点清单，最后在短事务核验导出任务 fence 和目标版本后确认同步。期间资料/私密/checkpoint 有新版本，旧恢复点可成立，但当前版本保持 pending。上传 blob 不代表完整档案已同步；恢复明确时间边界，不因新清单损坏而静默退回旧点并称最新成功。
- 新档案占用保留命名空间 `.atriumos-archives-v1/`，旧恢复器递归扫描时明确跳过它，否则会误读新 manifest 为旧记录。新恢复器只读取已提交清单列出的对象，两套 exporter 不改对方文件，照片根仍只读。检查网络卷和预期标记内容，不能只验证 marker 存在；根及对象通过受约束句柄访问，拒绝穿越/symlink/异常文件及覆盖冲突。实际 SMB 的发布/同步/读回能力须验收；保留单槽阻塞 I/O 边界，超时不能不断启动遗留 goroutine。
- 增加 `archives restore --snapshot … --out-dir …`，目标必须不存在、在本机且不与 NAS/生产 data dir/照片源重叠。在临时目录有界流式复制，校验所有 hash/size/schema、来源键/跨表引用、冲突 ID/跨家庭边/修订图；SQLite integrity/FK、checkpoint、关闭及文件同步完成后无覆盖发布整个目录。产物按版本设置单项/累计字节和数量上限，不沿用旧 128 KiB JSON 限额。原件闭包、数据恢复及索引重建分别报告，不默认使用会替换 live DB 的旧 restore 入口。
- 导出未完成任务的已提交 parse/chunk、指纹、澄清、事件、预算和 checkpoint；恢复不保留可用 lease，也不假定未确认调用未计费。未完成任务进入明确不可领取的恢复状态，可沿用 cancelled + recovery_requires_reauthorization 并记录原状态/恢复事件；不重置已消费或预留预算。原 principal/policy 不可偷偷替换，新授权续处理创建与旧任务关联的新任务，复用核验产物及既有 document/event，并保留预算约束，不能重放 IM/屏幕操作。已完成任务保留完成历史。Brain 已消费账本及尚未 Core-saved 的 inbox/spool 不在 Core NAS 清单，整机灾备须另保护这些操作状态并明确恢复边界。

故障夹具增加对象写入/清单发布/DB 确认各中断点、同步期间私密修订、同根新旧恢复、当前事实引用旧 extraction、等待澄清及预算任务、同文两次事件/同文件多来源/账单付款归并计数、重复恢复无覆盖。恢复不自动发通知，也不自动取得当前权限。

### T17 — 迁移、性能与发布

新增：`docs/ops/atriumos-runbook.md`；迁移/降级脚本；修改构建部署脚本。

准备匹配版本的签名发布包→核验实际 schema 并一致备份/隔离升级演练→flag 关闭下原子部署→迁移与旧六条记录及更正历史核对→事务 outbox 回填/增量追平和权限核验→旧接口新增/修订/隐藏/撤回验证→最后启用新功能 flag→健康/IM/TV验收。schema 8 是已记录生产基线，不能代替部署时核验。回滚应用前核对 schema 兼容，必要时用备份恢复到独立目录，不能让旧二进制直接写新库。

回填与实时 outbox 复用同一 legacy 映射/当前修订规则并记录可恢复进度，重启或乱序不能新增事件。旧二进制及维护工具遇未知新 schema 要明确拒绝，不能只数 migration 数量。回滚前保全升级后的 DB、原件和 Brain 操作状态；只退回升级前 DB 会丢掉升级后已接收但尚未 NAS 同步的资料。发布包包含匹配的 Core/维护 CLI、Brain/MCP、渠道补丁、原生 helper 和 Web，保留既有签名身份；不顺带改挂载或自动登录配置。通用整机备份与 T16 档案恢复范围分别记入 runbook。

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
