# AtriumOS 实施与验收记录

更新：2026-10-11。开发分支 `codex/atriumos`，隔离工作树 `.worktrees/atriumos`。所有“规划/开发中/通过”区分记录，生产版本未自动跟随开发分支。

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
| T02 家庭授权 | 已完成任务级验收 | `53fd964` + `2c8f0ea`；需求与质量审查批准，相关 race 复验通过；未部署 |
| T03 原件存储 | 已完成任务级验收 | `c147a88` + `e40c9c7` + `474c0c1`；独立需求与质量审查批准，race/全量 Go/跨平台编译通过；未部署 |
| T04 接收 API | 已完成任务级验收 | `6fa1592` + `256aa88` + `8cbad4c`；独立需求/质量复审批准，真实临时 Core、相关 race、全量 Go 回归通过；默认关闭，未部署 |
| T05 解析器 | 已完成任务级验收 | `20bfed1` + `6c17f41` + `ab21659` + `c75659a`；最终提交独立需求/质量双审批准，全量 Go/race/Linux 编译及原失败复验通过；未接生产消费者 |
| T06 图片/PDF | 已实现并双审通过 | `7c7bee8` 获需求与质量独立批准；80 个结果、266 项定位及进程/依赖/资源检查通过，未接生产 |
| T07 分类与事实提取 | 已完成任务级双审 | `26991d1` 获最终需求/质量批准；严格提取、长文档、归并与四领域实际合成响应/图片重放通过；未接生产消费者 |
| T08 实体归属 | 已完成任务级双审 | `1a070c6` 获最终需求/质量批准；身份正反证、同批别名、归属事务与撤销、私密来源投影及迁移回归通过，未接生产消费者 |
| T09 原子发布与恢复 | 已完成任务级双审 | `ed69c61` 获完整质量复审及最终需求语义批准；持久消费/发布/修订/澄清/当前证据回归通过，未部署 |
| T10 统一检索与证据 | 已完成任务级双审 | `b670982` 需求与完整质量批准；32结构化问题及授权/证据/分页/日期回归通过，未部署 |
| T11 自然语言查询 | 已完成任务级双审 | `6e0d4cc` 需求与完整质量批准；七工具实际链路与两领域模型调用验证通过，未部署 |
| T12 基础分析 | 已完成任务级双审 | `5fa2de8` 最终需求与完整质量批准；精确分析、真实 XLSX 汇总、当前人工断言/私密及快照失效验证通过，未部署 |
| T13 可信渠道附件 | 已完成任务级双审 | `d19bb76` 最终需求与完整质量批准；原生捕获、意图工具、恢复队列与实际模型回执验证通过，真实用户新消息留上线联测 |
| T14 TV 档案 | 实施中 | 已启动唯一实施者；全家列表/筛选/详情、预览、公开分析与 IM→TV 结果待验收 |
| T15–T18 | 未开始 | 系统验收、NAS恢复与上线待实施；已做原生 helper 搬移预检，非发布完成 |

## 基线核对

`1142c9e` 的 `go test ./...` 和 `node --test adapters/openclaw/*.test.mjs` 在隔离工作树成功；JS 49 项通过。完整日志留本机 `/tmp/atriumos-worktree-baseline.log` 与 `/tmp/atriumos-worktree-baseline-js.log`（临时证据，后续关键结果会写入本记录）。原工作树的既有改动已归档为两个提交，未改生产数据库。

此前已验证微信查询恢复（用户确认）、Discord 写入、家庭记录 NAS 同步、OnePlus “档案”Tab。以上不代表新原件解析、自动归属、综合检索或通用任务已经实现。


## T00 设计审阅

独立审阅提出五项：业务事件与字节去重混淆、私密子内容泄露至原件预览、Brain/Core 两库接收恢复协议不具体、旧记录缺少持续投影、任务历史仅计数。均在档案设计“实施契约补充 / 审阅收敛”及整体设计/计划中修正；复审确认原五项已覆盖。另按复审补充：文件内业务事件在理解提交阶段创建，恢复清单包含业务事件 ID/证据/关联。具体格式质量和真实用户体验仍是后续验收，不因设计审阅通过而标为实现完成。

本机解析能力预检：Swift 6.0.3 可执行，但直接导入 Vision/PDFKit 遇到本机 CLT 的 SwiftBridging 模块重复定义，已终止该探针，未修改系统 SDK。改用 Objective-C、`clang -fobjc-arc -fno-modules` 链接 Foundation/Vision/PDFKit 后编译和运行成功，Vision 返回 zh-Hans/zh-Hant 等支持语言、PDFDocument 类存在。T06 因此优先使用 Objective-C 辅助进程，保持 JSON 协议与 Go 服务解耦。此证据仅证明运行时能力，不替代真实图像/扫描件/表格质量验证。探针位于本机 `/tmp/atriumos-vision-probe.m`，没有读取家庭文件。

本机 SQLite trigram 行为探针：对“张明 体检报告 血常规”查询“张明”“体检”返回 0，“体检报告”“血常规”返回 1，符合官方短词限制。此探针使用 Python 附带 SQLite，并非项目 modernc 构建的验收；T10 仍需在实际项目运行时覆盖短姓名/短词路径及索引重建。

随后在本工作树的真实 Go 依赖 `modernc.org/sqlite v1.58.0` 上重复探针，运行时 SQLite 为 3.53.4，结果相同：两字 MATCH 为 0，三字以上为 1；参数化 `instr(body, ?)>0` 对四个关键词都为 1。证据 `/tmp/atriumos-fts-probe.go` 和 `/tmp/atriumos-fts-probe-result.json`。这验证了短词 fallback 的必要性和基础可用性；家庭授权过滤、分页、索引重建及一万份文档性能仍须在 T10/T17 实际实现后验收。

本机真实 OCR 预检（仅技术探针，T06 尚未实现）：生成 1800×1600 的简体中文/英文合成体检图片及旋转 90° 副本，经 Objective-C Vision Accurate、zh-Hans/en-US、request revision 3 实际运行，两张均返回 13 个带坐标片段；人工对照原图的 8 项字段（姓名、出生日期、报告日期、项目、数值、单位、参考区间、医生）均出现。原图的 `130–175` 被识别为 `130-175`，空白分隔会收缩；返回顺序把右侧“参考区间”列放在末尾，**直接拼接 OCR 数组不能代表表格阅读顺序或字段关系**。T06 必须保存坐标并验证列/行关联、原文与规范值，不能以本次 8/8 推断真实照片/复杂报告准确率。未测试低清、混合 PDF 或密集表格。

探针和两份 JSON 在本机 `/tmp/atriumos-ocr-probe/`（临时证据，未读取家庭资料）。正向图片 SHA-256 `e3b74d6ccda1f6b2226f4d210412ca8371e58b1e475b1fb6407855983cdd82e8`，旋转图片 `b15f4175c224b175935fa896518fd333bbf01fa5a3b2763859e3aa235669bc62`。系统 Python 未带 Pillow，探针使用 Codex 已有依赖运行时，未安装或修改系统包。


## T01 实现与需求审查

实现提交 `fab2efe`，日期校验修复 `1b44089`。新增家庭作用域任务仓库、迁移 0009、不可变输入、检查点/结果/澄清、原子事件和独立 attempt ID。公开 JSON 不含租约令牌；领取、续租、提交、重试、等待、取消、澄清都在持久状态下工作。暂不包含解析器、HTTP 入口或生产任务调度。

红灯：实现前领域/仓库 API 缺失，测试失败。实现后 `go test -race ./internal/domain ./internal/store` 与 `go test ./...` 通过；旧迁移断言仅从 schema 8 更新到 9 并加入新表名，保留数据兼容断言。

需求复审在 `fab2efe` 发现 Go 时间解析器接受不规范时区/超长小数秒；新增七个失败用例后修复，规定最大 35 字节、两位时分秒、有效时区、可选 1–9 位小数。`1b44089` 经独立需求复审确认通过，复审实际运行 `go test -race -count=1 ./internal/domain ./internal/store -run Archive` 成功。当时质量审查尚在进行，未将 T01 标为最终验收。

质量审查独立复现两项：调用开始时尚有效的租约，在等待写锁期间过期后仍可完成；同一租约下延迟返回的旧状态可覆盖更新检查点。修复方案为仓库使用可信可注入时钟，在每次事务取得写锁后取时；工作结果带独立 `state_version` 条件，续租只增加事件版本，不使进行中的计算失效。两项修复和重新审查通过后才继续 T02，未提前接入生产。

`02b928a` 已实现上述修复：状态版本从 1 开始，Checkpoint/Finish/Retry/Wait 和澄清推进；Claim/Renew/Cancel 不推进；澄清仍用完整事件版本检测过期问题。新增测试先复现旧行为失败，再覆盖同版本竞争、续租兼容、持锁期间过期和事务重试重新取时。实现端 `go test -race ./internal/domain ./internal/store`、`go test ./...` 成功；独立需求复审实际运行 `go test -race -count=1 ./internal/domain ./internal/store` 成功并批准。独立质量复审也批准，实际运行 `go test -race ./internal/domain ./internal/store -run Archive -count=1` 成功，包含此前两个复现场景。T01 因此完成任务级验收，不代表整体档案能力上线。

## T02 家庭作用域与授权

实现提交 `53fd964`。迁移 0010 持久保存随机家庭 ID 与本机引用；旧任务事件/澄清历史保留且不自动绑定 principal，旧集成不自动授予 archive 权限。新增 admin household 查询与 CLI；issue/policy 支持 household allowlist；grant-archives 以预期策略版本原子合并明确授权，并保留原有屏幕、数据源、成员和权限。

客户端操作从受认证身份取得 principal/credential 引用，在同一事务检查当前有效性和家庭范围，再读写任务。后台使用不可变 principal + policy_version，不因凭据正常轮换取消；principal 失效或策略变化会阻止发布。Claim 在 SQL 筛选失效委托后才 LIMIT，不被旧任务堵塞。任务查询/重放提供 delegation_error_code，不悄悄刷新原委托。仅有 ingest 权限的接收回执不包含原文、提取或租约数据。

read + organize 可跨同家庭接入澄清或取消，事件分别记录原始与实际操作者；取消能终止旧授权任务，澄清恢复处理仍要求原委托有效。幂等键作用域为家庭 + principal + request_key。仓库留有包内事务方法供 T04/T09 原子提交资料、事实和任务。

实现端已观察目标 API 缺失/旧行为的红灯，完成后 `go test -race ./internal/domain ./internal/store ./internal/auth ./internal/httpapi ./internal/cli` 与 `go test ./...` 全通过，`git diff --check` 通过。迁移回归仅递增 schema/count 并加入新表，新增 schema 9→10 保留数据/事件/澄清且不扩大授权的回归。独立需求与质量审查通过后才标为任务验收。

独立需求审查实跑 `go test -count=1 -race ./internal/auth ./internal/httpapi ./internal/domain ./internal/cli ./internal/store -run 'Archive|Household|Integration'` 通过，但发现一项兼容问题：GrantArchives 校验整个合并策略时，会因旧 screen/source 已撤销而拒绝新的有效 archive 授权。需要只校验本次新增的 archive 引用及最终结构/总量，完整保留旧列表，并保持旧资源运行时仍不可访问；其他已审 T02 范围无需求阻断。

`2c8f0ea` 已修复该问题并同步 OpenAPI。新增测试先观察 revoked 旧引用导致 invalid_request 的红灯，再验证新家庭档案授权成功、全部旧列表保留、已撤销照片与屏幕命令仍 not_found；普通完整替换策略仍按原规则拒绝已撤销引用。实现端和独立需求复审的 store/auth/httpapi/cli/screen 相关 race 测试均通过，需求复审批准。独立质量审查也批准，重新实跑 Archive 授权/生命周期/迁移/稳定家庭 ID/CAS 以及 domain/auth/httpapi/cli/契约相关 `-race -count=1` 测试和提交差异检查全部通过。T02 完成任务级验收。

## T03 存储实现前预审

独立预审已核实本机 Go 文档、x/sys API 与 macOS link 手册：os.Root 允许跟随根内 symlink，Rename 会覆盖目标，因此选目录 fd + no-follow 与 Linkat 无覆盖发布，并在重复目标上重验完整内容。文件发布先于数据库引用，失败不能删除可能已被并发接收引用的原件。最小表/事务交接/故障矩阵已写入开发计划 T03。当前实际 Go 为 1.27.1，模块基线 1.25；实现保持已锁定依赖与 macOS/Linux 支持，不依赖新增 Root 方法。这是实现契约，不是存储能力已完成的证据。

## T03 实现与审查

实现提交 `c147a88`。BlobStore 固定受信任本机根，以流式上限、内容 MIME 与 SHA-256 校验原件，临时文件同步后用 Linkat 无覆盖发布；目录逐层 no-follow，已有原件全量校验后复用，失败不删除已发布内容。Close 等待正在进行的操作，返回的读取句柄仍可使用。迁移 0011 保存不可变 blob 元数据和家庭限定的逻辑文档；相同字节允许多个文本事件文档，默认 TV 可见、显式 false 保持私密。事务辅助方法仅供 store 包内后续接收事务使用。

实现端观察了原件操作与元数据缺失的红灯，并在自检发现 SQLite 文本长度/GLOB 忽略 NUL 后缀的约束漏洞；先加入失败用例，再以 TEXT 类型与完整字节长度校验修复哈希约束。最终 `go test -race ./internal/archive ./internal/store ./internal/app`、`go test ./...`、变更包 Linux amd64/Darwin amd64 无 CGO 编译及差异检查通过；本机为 Darwin arm64。独立需求审查已启动，之后进行质量审查；当前不将实现端通过等同于任务验收。未接入 HTTP、解析、NAS、GC 或生产服务。

独立需求审查 fresh race 与 Linux 编译通过，但临时 Go overlay 探针确认 `home-a` / `HOME-A` 两个合法家庭 ID 在本机大小写不敏感卷上写入后指向同一 inode。审查因此未批准。修复采用精确 ID 字节的小写无 padding base32 目录组件，保留数据库 ID 语义且支持最长 128 字节；增加物理隔离与长度回归后重新审查。未发现其他 T03 需求阻断；修复验证中。

修复提交 `e40c9c7`。大小写变体的普通与 128 字节 ID 用例均先在 APFS 失败，再确认物理 inode 不同且原件可完整读取；另从实际目录名反向解码验证精确 ID 和 205 字节上限。所有受影响的链接/损坏/清理测试适配实际编码路径，未删减检查。实现端 fresh 目标 race、全量 Go、Linux amd64/Darwin amd64 编译全部通过；独立需求复审运行 fresh Blob race 和此前失败的独立 APFS 探针，均通过并批准。独立质量审查进行中。

质量审查发现迁移约束仍接受恰好 64 字节但包含 NUL 的哈希：仅完整字节长度不足以弥补 SQLite 文本 GLOB 截断。当前 Go 校验和包内方法已阻止该输入，问题属于数据库不变量缺口。`474c0c1` 在保留类型/GLOB/字节长度条件上增加字符长度，原始 SQL 回归的 NUL 位置 0/1/31/63 均先失败再通过，合法 64 位十六进制正例保持有效。实现端 fresh 相关 race 与全量 Go 通过；独立需求复审批准，质量复审独立重现已拒绝异常值并批准。质量审查另实跑完整 Blob race、并发重复发布与 Close 20 轮、元数据/回滚/迁移/目录布局测试，均通过。T03 完成任务级验收。

## T04 接收 API 实现与审查

T03 两类独立审查通过后启动 T04，实现提交 `6fa1592`。archives.enabled 默认 false，与旧 family_archive 导出独立；默认不创建原件目录。开启后运行时接入单槽上传、来源回执 lookup 和安全任务查询三接口，退出时实际终止阻塞上传并关闭 BlobStore。单一接收事务重新验证凭据/家庭/策略代次后处理来源幂等、文档与任务和可见性审计；原件发布不占数据库写锁，提交失败保留可复用原件。

同文件跨渠道复用逻辑文档，每个来源自己的任务委托不转移；相同文字的不同消息保留不同逻辑文档。仅 ingest 回执固定六个自有字段，read 才增加文档引用与白名单任务投影；无原件绑定的通用任务不能被误报 saved。显式 private 统一组合授权、原子版本与审计；冲突先于可见性副作用，inherit 不取消既有私密。

实现端观察并修复了缺失服务/配置、通用任务虚假 saved、关闭遗留 staging、大小写别名 JSON 字段、取消变 500、普通 curl metadata 文件名参数与声明超限错误码等红灯。可选 multipart filename 现在对两 part 均允许但完全忽略，原名只取严格 JSON；其他字段/part/header/结束及 epilogue 约束保留。凭据轮换测试曾因到期时间超出 principal 而在轮换前失败，自检纠正夹具并断言实际 unauthorized/forbidden，还增加新凭据查回旧回执，避免泛化 Error 造成假阳性。

实现端 `go test ./...` 与 `go test -race ./internal/archive ./internal/store ./internal/httpapi ./internal/app ./internal/config ./internal/domain` exit 0；最后轮换/回执/二进制 PNG/MIME/审计回归也通过，OpenAPI JSON/引用和差异检查通过。可重复脚本 `scripts/e2e/archive-intake.py` 实际构建并启动临时 Core，以 CLI 授权、curl 上传→lookup→task 查询得到 200/200/200；26 字节 UTF-8/CRLF 原文哈希 `7e75b5c52b2e85cd0ad35beb1f789a062b49d032c7d417ab9d1bc4430e3fa988` 一致，恢复同一回执，状态 saved/queued/NAS pending。只使用临时凭据、目录和合成资料。独立需求审查已启动；尚未完成任务级验收，也未验证真实 IM 附件绑定或部署。

独立需求审查在真实临时 Core 复现两个缺口：显式提供 `expected_sha256:""` 或 `visibility_intent:""` 均返回 200/saved。它们分别违反已提供哈希必须为 64 位小写十六进制、已提供可见性意图只能为 inherit/private 的契约；仅字段省略可以采用默认行为。审查未批准，要求补充 HTTP 400 且来源/任务不提交的回归，再修复并复审。空 declared_mime 在当前 schema 中允许，不计作缺陷。审查另外实跑六包 fresh 测试及 CLI/curl 200/200/200 基线均通过；通过基线不能代替边界校验。

修复提交 `256aa88`，仅在严格 JSON 入口区分上述可选字段的显式空值，保留内部存储 API 和省略默认值。两个真实 HTTP 回归先观察 200 红灯，再验证 400 且没有 source/task 提交；同时覆盖省略、合法声明、空 declared_mime 正例及重复/大小写字段绕过。实现端 focused archive HTTP/upload race 测试通过；独立需求复审进行中。

独立需求复审批准 `256aa88`：临时 Core 再次确认两个显式空值均 400 且新增 source/task 为零，正确断言及空 declared_mime 均 200 且各新增一条 source/task；原始上传→lookup→task 仍为 200/200/200，精确 26 字节、哈希和同一回执一致。fresh focused HTTP 测试及差异检查通过，无新增需求阻断；随后启动独立质量审查。

独立质量审查未批准：Go multipart 接受带尾随空白的 closing delimiter，而 wire tracker 只识别规范 CRLF 版本，因此“空白 closing + epilogue + 后续规范 closing”被错误接受并提交来源；另总请求上限为最大内容加 128 KiB，但没有验证实际 wire bytes 减实际内容大小，5 字节内容附 524,847 字节 overhead 仍被接受。两项独立 overlay 回归已复现，要求在接收事务前拒绝异常结束和超额 overhead。审查实跑 intake/HTTP/runtime race 及阻塞上传退出均通过；8 个同来源并发汇聚到一条回执/任务/文档，8 个不同来源的同文件则有 8 个任务和 1 个文档。并发基线通过不抵消两项协议缺口。

修复提交 `8cbad4c`。常量空间的行状态扫描器与本机 Go multipart 的关闭语义对齐，支持空格/tab、由首个实际 opening delimiter 选择的 CRLF/LF 及 EOF，记录第一个关闭位置；实际 wire 减 content 字节数不得超过 128 KiB，检查在接收事务前完成。两项独立探针先重现 200/新增来源，再得到 413/零来源和 400/零来源；新增测试同时断言无任务，验证 overhead 上限前/等于/超过一字节、各种分块位置、长行与 marker-like 原文字节完整保留。实现端 targeted 和 focused HTTP/upload/envelope race、原始 overlay 全通过；重新进入独立需求与质量复审。

独立需求复审批准 `8cbad4c`，fresh focused race 覆盖前次空值回归，另独立运行 24 种 HTTP 组合（CRLF/LF、空白/EOF 结束、epilogue、精确 hash/size 和来源/任务计数）全部通过，真实临时 Core smoke 再次 200/200/200、26 字节及同一回执一致。独立质量复审也批准该提交：原先两项失败 overlay 在 fresh race 下分别返回 413/零来源、400/零来源，相关 envelope/上限/原文保留/空值测试通过，扫描器与 Go delimiter 语义对照后无新增可操作问题。根任务随后执行 `go test ./...` exit 0，改动影响的 app/cli/httpapi 重新运行，未变包使用 Go 正常缓存。T04 完成任务级验收；这仅证明接收基础，未完成解析、模型、实际 IM 或生产发布。

T05 独立解析预审已补充到开发计划：严格编码白名单、OOXML 容器证明与读取限额、partial/coverage、可定位表格及合并单元格、公式历史缓存/缺值/原始与显示值区分。T04 双审通过后启动 T05 实现；目前尚无解析实现的测试通过证据。已验证本机既有依赖运行时含 python-docx/openpyxl，可辅助生成合成 Office 夹具；不是 Go 生产解析器的运行依赖。

T06 预审明确继承文件句柄、统一页/坐标、每页 OCR 与 native 文本证据组、冲突不擅自择一、资源/进程/协议限制；仍未实现。T07 在本机 OpenClaw 2026.9.8 文档和实现中核对公开 direct-provider completion，选择专用 CLI 插件复用 DeepSeek 路由与凭据；它保留 stopReason/responseModel，优于当前会丢掉截断终止原因的 isolated 路线。只接受完整 stop，零工具与模型身份须实测。T07/T09 的产物/checkpoint 原子持久化交接已明确，避免把大产物塞入 64 KiB task state。此阶段未调用 provider、未读取凭据、未修改生产 OpenClaw 配置，不能作为模型质量证据。

T08/T09 预审补充具名新主体自动创建、同名证据规则、范围明确的身份绑定、候选快照代次与原子发布。身份号只在受保护仓库有界精确比较，不新增普通索引或密钥系统。事件区段/观测由 Core 分配持久 ID，重解析和改归属不靠模型临时 ID 重建事件；问题/答案带版本并在同事务验证。人工修订/私密、有限预算、连续 legacy outbox 与索引交接的契约已写入计划，仍未实现或验收。

## T05 实现中的证据

2026-10-04 首批文本/CSV/限制行为测试在 API stub 上观察到 RED：精确 Unicode/CRLF、带 BOM 的 UTF-16、CSV 引号内换行/尾空字段及物理 FieldPos、源/文本/字符串/片段上限、取消和 ReaderAt I/O。实现进行中，尚不能称格式支持已通过。OOXML 夹具将移动主 part 的位置，验证沿 content types/relationships 定位而非依赖固定文件名，并覆盖稀疏、公式缓存、合并、日期及异常包。

实现提交 `20bfed1`，原件夹具换行修正 `6c17f41`。新增纯 Parse API、显式 JSON schema 字段、范围/表格定位、原始单元格/公式/缓存/样式、覆盖与资源计数；无新依赖、数据库调用或生产消费者。UTF 文本/CSV 首轮通过后，DOCX/XLSX 按精确段落/格子/合并/缓存/缺值/隐藏/日期夹具观察 RED→GREEN。CSV 尾空字段预期曾将中文物理字节列写错，按原始三字节汉字、引号和逗号核对后纠正为第 10 字节，未修改解析行为凑断言。

自检补上零 CRC、实际解压计数过冲、local header/central/descriptor 一致性、重复 XML 属性/值容器、无语义解析的 parts 覆盖和不支持样式继承产生虚假显示等回归。原件、ZIP/解压、XML、字符串/单元格/片段/关系均有显式限额；超限计入至多一个探测字节。纯文本首版保留单一完整片段，默认单段 256 KiB，T07 须使用父片段及 Unicode 范围分块，不静默截断。

独立生产工具夹具也通过：python-docx 1.2.0 的合成健康报告保留受检人/医生与 `/word/document.xml` table[1]/row2/col2/paragraph1 的“周青”；模板 customXml 未语义解析，状态如实 partial。openpyxl 3.1.5 的“家庭设备”工作簿为 parsed，地址/型号、原始日期 46904→2028-05-31、A5:B5 合并与隐藏第 7 行均核对。生成脚本、固定包时间和版本说明在 testdata/archive，Go 测试不依赖 Python。

实现端 `go test ./... -count=1`、`go test -race ./internal/archive -count=1`、Linux/amd64 archive 测试二进制编译均 exit 0；10 秒有界 fuzz 执行 6,646 次无失败。提交检查发现 Git 自动将 feeding.txt 的 CRLF 归一化，已先针对实际入库字节观察失败，再加该文件的窄 `-text` 属性和精确回归；`git show HEAD:testdata/archive/feeding.txt` 确认 56 字节、三个 CRLF，最终 HEAD 的 archive fresh 测试与差异检查通过。独立需求审查已启动，任务尚未验收；PDF/OCR、模型和发布不在本次实现范围。

独立需求审查未批准 `6c17f41`，只读 overlay 四项探针均复现失败：DOCX 的 `tcPr/cellDel`、`trPr/del` 删除内容仍被当作当前内容且 coverage 完整；合法 UTF-8 BOM 的 Office XML 被拒绝；以普通字符 `PK` 开头的文本/CSV 被当作 ZIP；Excel 有限数字格式使用浮点 ties-to-even，将 `2.5` 显示为 `2`、`0.125` 百分比显示为 `12%`。修复须分别排除删除内容并保留物理定位/如实修订覆盖、接受合法 BOM、按实际 ZIP 签名分派、用精确十进制中点远离零舍入且不修改原值/历史缓存。探针在本机 `/tmp/atriumos-t05-spec-review-6c17f41/`，实现者已独立重现；当前仍待修复与需求、质量双审，不能用先前基础通过覆盖这些失败。

修复提交 `ab21659` 在原四项探针上 RED→GREEN，增加删除格内嵌表排除、物理行列保持、修订合并关系、BOM 计数以及正负中点/指数/百分比原始缓存不变回归。修订表的纵向合并不能确定时给出 `vertical_unresolved=true`，未知锚点/行跨度为零，同时保留横向跨度和 continuation；不受影响的嵌套表仍正常解析。实现端原 overlay、针对回归、fresh `go test ./... -count=1`、`go test -race ./internal/archive -count=1` 和暂存差异检查通过。需求复审已启动；未获双审前仍不标任务完成。

独立需求复审批准 `ab21659`：原四项探针全部通过，追加删除/物理定位/内外嵌套合并独立性、XLSX 七个 XML part 同时有 BOM 的证据与计数、真实 ZIP 签名和普通 PK 内容，以及 13,290 项精确有理数舍入对照均通过，负数/指数/百分比/下溢边界与公式缓存保留也核对。只读探针 `/tmp/atriumos-t05-spec-review-ab21659/repair_test.go`，未修改仓库。已派发新的独立质量审查，需求批准不替代该审查。

独立质量审查未批准 `ab21659`，新增四项有复现的 XLSX 问题：日期在 float64 转换后才检验整数/符号，`61.000000000000001` 和 `-1e-999` 被误转确定日期；合法可重复的 cols 只读取第一组，漏第二组隐藏列；重复 workbookPr/sheets/cellXfs/mergeCells 单例只选第一份且声称完整；非 inlineStr 格子内的 is 内容被静默忽略。普通 fresh archive 测试通过不能覆盖这些缺口。根任务实跑 `/tmp/atriumos-t05-quality-review-ab21659/overlay.json` 的 `TestQualityReview` 全部按报告失败，已交实施者修复并补回归；cols 与单例 cardinality 必须区分，不以拒绝合法多组规避支持。当前无质量批准，T06 尚未开始。

质量修复提交 `c75659a`：日期改用共享有界十进制 lexer 检查原始符号/整数/范围，超出 float64 的合法十进制日期序号同样 raw/partial，公式缓存不变；全部 cols 保留，隐藏状态冲突 partial 并新增 `column_hidden_unresolved`，用差分计数保证 O(ranges + columns + cells) 且有取消检查；已识别单例重复及冲突 inline 表示提前拒绝。原失败与永久回归已 RED→GREEN，fresh 全量 Go、archive race、原需求探针、质量日期/多列/sheets 探针及 Linux/amd64 编译通过。原质量 scratch 部分用例在检查 invalid 前调用 cellAt，会错误要求被拒绝结构仍输出片段；复审须按既定允许 invalid 的标准调整断言顺序，永久回归已采用状态优先。独立质量复审和共享 lexer/元数据的针对需求复核已启动，尚无最终任务批准。

最终 `c75659a` 获独立双审批准。质量复审保持原输入、仅修正允许早期 invalid 的断言顺序后原探针全部通过；独立 oracle 核对全部 16,384 列可见性、fresh archive 测试及修复差异检查通过。需求复核两份旧 overlay（含 13,290 舍入对照）、16,000 精确日期对照（其中 3,830 个支持的整数）、冲突列 JSON 往返和针对测试均通过。无未解决阻断发现，仓库代码与评审 SHA 一致，后续仅状态文档。T05 完成任务级验收；尚未验证真实模型或生产档案发布。T06 已交新实施者，仍沿用单一代码实施者及需求→质量独立审查。

T05→T07/T09 证据交接已按实际 API 补充 typed components、原值/派生显示、完整父范围分块、空格证据、ownership manifest 与版本复用；独立文档复核确认原四处交接缺口已覆盖，并纠正日期措辞：1900 系统的虚构 1900-02-29 不能成为有效公历日期，不能误拒绝 1904 系统的合法 serial 60。此处仅为后续实施契约。

T07 公共 CLI 宿主合成探针已验证 OpenClaw 2026.9.8 的 package/manifest、definePluginEntry/registerCli/discovery action 和 api.runtime.llm.complete 可用；独立 OPENCLAW_HOME/state/config/cwd、环境白名单、禁 bundled plugins/shell-env、网络阻断下，echo 一条 JSON 且 stderr 为空。诊断 console 转 stderr，未捕获错误却会在 JSON stdout 与 stderr 传播原 message；根任务已读取四类输出及插件源码。计划因此明确省略请求 execution（direct-provider 仅为返回归因）、纯 machineOutput descriptor、捕获固定错误码与完整宿主生命周期。探针目录 `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-openclaw-cli-probe-pw1kd33w`；没有 provider 调用、现用凭据/配置读取或仓库/安装包修改。这不证明实际 DeepSeek 路由/模型质量，仍须 T07 四领域真实调用验收。

后续本机配置元数据核对：`/Users/ditu/.openclaw.pre-migration/openclaw.json` 的默认 primary 为 `deepseek/deepseek-v4-flash`，普通会话 fallback 为 `deepseek/deepseek-v4-pro`；DeepSeek 插件已 enabled、auth mode 为 api_key。实际公开 provider 包 `@openclaw/deepseek-provider` 版本 2026.9.8 位于 `/Users/ditu/.openclaw.pre-migration/npm/projects/openclaw-deepseek-provider-2481ed984b/node_modules/@openclaw/deepseek-provider`，manifest 的 runtime entry 为 `dist/index.js`、providerCatalogEntry 为 `dist/provider-discovery.js`，端点 `https://api.deepseek.com`。受保护的 state `.env`（0600）存在 DEEPSEEK_API_KEY 键名，未输出密钥、未调用模型或修改运行配置。T07 可显式加载该受信 provider 并按需提供单个密钥，不能依赖独立 worker 自动发现普通 state/npm 安装，也不能复制普通会话/fallback/完整环境；实际 responseModel 与提取质量仍未验证。

独立 provider 加载探针位于 `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-deepseek-catalog-probe-7x_8woxc`：空配置/state/workspace、环境白名单、无 API 密钥且 Node 网络阻断下，显式 load.paths + allowlist 的 `plugins list --json` 返回 archive probe 和 DeepSeek 2026.9.8 均 loaded，后者 providerIds=[deepseek]、无 tool/channel/service。禁 bundled plugins 不妨碍这个显式 provider。无凭据的 models list/refresh 返回空本地目录，不能据此否定插件加载，也不能作为实际模型可调用的证据。仅在临时目录写入探针配置/缓存，未改生产；T07 真实调用门槛不变。

T10–T13 实现前核查已收敛短中文检索、当前发布引用闭包、真实回执渲染、十进制分析及渠道桥接契约。实装代码确认微信首媒体/先推进 cursor 和通用非等待式 hook 不能保证可靠多附件；计划明确固定版本补丁、准入后的整批捕获、冻结槽清单、流式下载、意图与消费分账及独立恢复。当前未修改生产渠道，也未进行真实 IM 附件验收。

T14 独立代码预检确认现有页面默认首位成员、历史展开暂停刷新、档案授权仅 integration、媒体只服务照片、远程命令只能打开 family 首页。开发计划已明确全家默认 DTO、screen 公共快照、独立展示有效期与失效 topic、预览产物、Core 公开分析、返回焦点，以及绑定 screen 的查询回执展示动作。尚未修改前端/生产，也未将已有“档案”Tab 更名视为综合档案能力。

T16/T17 独立预检补充完整恢复点/证据引用闭包、历史身份而非当前权限、版本化同步确认、NAS 保留命名空间与旧恢复器共存、新本机目录无覆盖恢复、未完成任务与 Brain 操作状态边界。部署顺序修正为匹配签名包、备份演练、flag 关闭部署、迁移/回填/权限核验后启用；回滚须保全升级后新接收资料。以上只检查代码和设计，未访问生产或执行 NAS 恢复/部署。

## T06 实现与能力验证

实现提交 `280fb4c`。正式 helper 的 33 个实际夹具已通过，覆盖 PNG/JPEG/WebP、三种物理旋转、EXIF 1–8、文本/扫描/页脚/混合 PDF、数字冲突、裁切旋转、非 BMP、受限/锁定/损坏/超限、空白/部分页失败和批注表单等结果。14 张格式/方向图片上的 19 项字段共 266 项，通过仅忽略空白的文字比较和框 IoU ≥ 0.4（实际最低 0.459）；PDF 四角/逆变换、CropBox 与 MediaBox 交集及 native/OCR IoU ≥ 0.75（完整行实际 0.852–0.949）检查通过。重复实际 OCR 的片段 ID 稳定。这是合成样本上的提取和定位证据，不代表真实家庭资料的通用 OCR 准确率。

原件在 `/tmp/atriumos-t06-fixtures/`，Go 解析结果在 `/tmp/atriumos-t06-evidence/`，实际 raster 与红色 native/蓝色 OCR 框叠图在 `/tmp/atriumos-t06-visual/`，代表性合成原件/叠图/结果已提交至 `adapters/macos-archive-parser/evidence/`。根任务已查看报告原图、EXIF 7、geometry PDF 第 2 页和 crop-intersection 第 1 页：方向、各列框和裁切边界符合预期，四条部分裁切 native 行保持待核对；数字 9.80/9.30 冲突保留同组两个读数且不选择首选文字。实现端进程回归曾复现 leader 退出后子进程仍持有 pipe；增加 Wait 后的进程组清理后取消/leader 退出两种场景通过。最终协议收尾覆盖必需字段/null/数组长度、孤立 UTF-16 escape、覆盖计数矛盾、首选文字与 alternatives 合计上限，以及能力缺失时读取计数为零且返回 parser_unavailable。

最终 helper 指纹 `170fbb014fb3582957b6e40b4f5d36ee7d46b3a32e59ab58c2e64af31c6c8c7a`，OS `24C101`、Vision revision 3、Accurate、zh-Hans/en-US。暖启动 helper-only 的 33 个小样本耗时中位数 0.302 秒、最大 0.446 秒，不是 Core 端到端或复杂百页资料性能保证。实现端 fresh `go test ./...`、`go test -race ./internal/archive -count=1`、Linux archive 测试编译和差异检查通过；根任务已核对 clean 提交与最终 33/266 结果摘要。新独立需求审查已启动，之后才进行质量审查，当前尚未完成任务级验收；本轮未接生产或模型。

独立需求审查未批准 `280fb4c`：将合成 report.jpg / report.png 截为一半字节，ImageIO 仍返回缩略图，helper 与公开 Go API 都给出 parsed/coverage.complete=true，原 19 个字段只剩 8 / 4 个。根任务分别重跑正式 helper 和 `/tmp/atriumos-t06-spec-probes/overlay.json` 的 `TestSpecReviewTruncatedImage`，均复现。该 macOS 上 ImageIO source/frame status 在截断样本解码前后仍为 0，单加状态判断不能证明修复，需有界、可实证的完整性检查并保留正常格式支持。实施者已收到唯一已验证阻断及回归输入，修复和复审待完成。审查另 fresh 运行带实际环境的全部 TestNative（33 个结果及 266 个位置对照）和 focused race 均通过，额外截断 PDF 探针返回 invalid；这些通过不抵消图片完整性缺口。

修复进行中：PNG 的有界 chunk/CRC + 32 KiB 流式 zlib/行与交错 pass 字节验证，在实施端 16 个真实损坏/正例上通过，包括重新计算 CRC 后截断 IDAT、缺失 IEND、Adler/行数/filter 错误及正常 split/零长 IDAT、Adam7、1-bit/16-bit 灰度。JPEG 不以所有有效图片统一 partial 的方式回避完整性；独立 scratch 探针验证成熟 libjpeg-turbo 3.1.4.1 的严格 warning/error 回调、1/8 IDCT 全 scanline 消费与 finish，可接受 baseline/progressive/灰度/CMYK/含 EOI 的合法 COM，并拒绝比例截断、伪造 EOI 和真正末尾缺失的 6 个损坏样本。结果/代码在 `/tmp/atriumos-strict-image-probe/`；这仍是依赖能力证据，未替代正式 helper 修复验收。

JPEG 依赖最小闭包仅 arm64 libjpeg dylib 与系统 libSystem，复制、相对 install ID、签名后独立运行已实测。实施将固定来源/版本/ABI/头文件/许可/原始及产物哈希并验证搬移运行，避免生产依赖 Codex 目录；缺失或不兼容返回 parser_unavailable。6000×6000 正常 progressive 在 64 MiB 虚拟系数预算下失败、128 MiB 下通过（约 105 MiB RSS），因此正式方案采用明确的 512 MiB 系数预算以保留既定 60 MP 常见输入范围，另有扫描数量/120 秒 deadline；这不是总 RSS 硬上限。正式修复端已通过原失败 overlay（JPEG/PNG 均 invalid/complete=false）、36 项完整性用例、3600 万像素 progressive 与搬移/缺失/篡改依赖检查，仍待完成补充修复和双审。

WebP 初始只调整外层 RIFF 的截断样本被 framing 拒绝，不能证明压缩流完整。随后同时重写 RIFF 和 VP8/VP8L chunk 长度的半截 lossless/lossy 文件，仍被 ImageIO 误报 parsed/complete，分别只剩 4/3 片段；根任务在中间 helper 指纹 `95956350fca5513ad2f8d8818a493aefe443014b64a2e2585ae3a30ed0f990f8` 实跑复现。成熟 libwebp 1.6.0 的完整解码状态校验已在 scratch 拒绝两者（status 3/7），并接受正常 lossy/alpha/odd chunk 与 3600 万像素 lossless。实施因此加入固定 libwebp + libsharpyuv 依赖闭包及公开 API 校验，保留正常 WebP 完整提取；外部 1×1 输出不代表内部 lossless 无源图大小分配。新增缺口修复中，尚无修复提交、需求复审或质量批准。

新实施者已完成首轮 Go 进程边界与证据分组的 RED→GREEN：未链接路径的原件通过 FD3 定位读取，调用者句柄/当前偏移保持；稳定片段 ID、未知/重复/截断/多份 JSON、坐标和计数、空白差异与数值/标点冲突、一对多分组、挂起/输出洪泛/异常退出/取消的 `TestNative` 实现端通过。测试 helper 使用 ReadAt，不以 cat 当前偏移迫使生产代码 Seek/reopen。实际 Objective-C helper 和完整合成格式矩阵正在实现，本轮不代表真实 OCR/PDF 验收。

2026-10-04 将此前合成报告导出 JPEG（quality 95）和无损 WebP，用已有 Objective-C Vision 探针实跑，revision 3 均返回 13 片段，八个预期字段字符串均出现。这仅确认本机这两个格式的读取/OCR 基础能力，不是 T06 实现或复杂场景准确率验收；坐标关系、EXIF、PDF 混合页和资源/进程限制仍待正式实现。JPEG SHA-256 `5b88e0ac26b48de92fa34c871de9f06655cf656eb21abc3de373bde7a0032750`，WebP `087aec3e06912c25ce50c211f9f3c5750c9eb00dce18e107f92f1035670b7008`。合成图片、输出及结果清单位于本机 `/tmp/atriumos-image-format-probe/`，没有使用家庭文件或安装依赖。

PDF crop/rotation 窄探针（macOS 15.2、Apple clang 16）使用非零 MediaBox/CropBox、0/90/180/270 度、两倍显式 raster scale。四页有效 crop 均映射到实际像素框，8 条 native/Vision 几何 IoU 为 0.903–0.957；文字仅 5/8 完全一致，三处拉丁 B 被 OCR 识成西里尔 В，不能凭视觉相似归一化后自动合并。根任务读取结果并查看 0/90 度页图，方向/红色定位点与变换一致。探针确认 CGPDFPageGetDrawingTransform 已处理页旋转，单独放大目标 rect 不上采样，应显式 scale 并复用实际 CTM；计划已加入同框公式及裁切文字规则。代码、四张 raster 和结果在本机 `/tmp/atriumos-pdf-transform-probe/`，仍非 T06 实现验收，正式 overlay/裁切/非整数缩放/非 BMP 夹具待补。

T06 完整性修复提交 `c92e58c1af9f2e496db13372749baea6bf0fcf5b`，根任务确认提交后工作区干净。正式 helper 指纹更新为 `bcd463b9318ea0ee7ff17884fc46c8d46d1d02140798e4cab105877331cef390`；根任务读取最终两份 summary，确认同一指纹覆盖 33 个基础和 38 个完整性样本。实施端报告 266/266 字段定位通过，原未改动需求 overlay、重新封装的截断 VP8/VP8L、完整包搬移、三个依赖逐一缺失/篡改、36 MP progressive 与资源/ABI 探针均通过。PNG 使用有界结构/CRC/完整 scanline 膨胀验证；JPEG/WebP 使用固定版本成熟解码器的完整完成状态，保留有效格式支持。依赖原始字节、公开头文件、来源、许可证和签名限制随构建分发，不依赖 Codex 运行时路径。

实施端 fresh 全量 Go、archive race、Linux 编译与差异检查通过，最终 native/process suite 为 26.743 秒。最终基础样本 helper-only 中位数 0.307 秒、最大 0.462 秒；这些仍不是端到端性能或真实家庭资料准确率结论。根任务已把修复提交交原独立需求审查者复核，随后才开展独立质量审查；T06 尚未任务级验收，T07 自动分类/理解和生产发布仍未实现。arm64、JPEG 8-bit/128 扫描及系数预算等能力边界详见 parser README；不声称总 RSS 硬限额。

独立需求复审仍未批准 `c92e58c`：原 JPEG/PNG 探针、全部实际 native 测试（23.709 秒）、资源/ABI 和另外 12 个重新封装的截断 lossy/lossless WebP 均通过；但允许列表中的 BMP 绕过完整性检查。合成报告转 BMP 后原件有 19 个字段，截为 25% 返回 empty/complete=true/0 组，截为 50% 返回 parsed/complete=true/7 组。根任务运行 `/tmp/atriumos-t06-spec-probes/repair/bmp-overlay.json` 的 `TestSpecReviewBMPCoverage`，在最终指纹 bcd463b 上独立复现同样失败。已交原实施者作窄修复：未独立验证完整性的其他图像路径返回明确不完整/不支持及待核对状态，保留已验证 PNG/JPEG/WebP 正常支持，不要求再实现其他完整解码器；README 的未验证说明不能替代机器可读覆盖状态。质量审查继续等待需求复审批准。

BMP 覆盖窄修复已在未提交实现上由根任务独立复测：不改原 overlay/input，helper 指纹 `9c9c4ac321b6debbf9909e42469671f4ee6f726f557a7712afb6e657828cdf1c` 下，25% BMP 为 partial/complete=false/0 组，50% BMP 为 partial/complete=false/7 组，`TestSpecReviewBMPCoverage` GREEN。实现保留未独立验证格式的可读 OCR，以 `image_integrity_unverified` 与 `image_integrity_validation` skipped feature 明确限制，并在片段 ID 哈希前继承 requires_review。实施端另报告正常 BMP/GIF/单页 TIFF/HEIC、空白 BMP 和结构无效/多帧保留行为通过；最终提交、全量检查及独立复审仍待完成，不把此窄探针当 T06 验收。

上述窄修复已提交 `7c7bee8b8c01fed7622d5de4d132e7c896c1d29f`，根任务核对工作区干净。实施端最终验证 80 个夹具结果、266/266 字段定位、两份未改动审查 overlay、全量 Go、archive race、Linux 编译及差异检查通过；native/process suite 25.938 秒，race 12.314 秒。指纹保持 9c9c4ac。HEIF 使用同一保守策略但无独立实际夹具，未扩大格式验证声明。已提交原审查者针对需求复审，质量审查仍待该阶段通过。

独立需求复审批准 `7c7bee8`，原 JPEG/PNG 和 BMP 两项发现均关闭。审查者 fresh 运行完整 native suite 23.877 秒，80 个结果、266 项定位、几何/协议/依赖/进程测试无 skip；两份原 overlay、focused race 6.232 秒及差异检查通过。确认未验证图片保留 OCR、zero-text 仍 partial、review 标记在 ID 哈希前传播；没有剩余可操作需求偏差。已派新的独立质量审查者审阅 T06 全部代码范围 `383167b..7c7bee8`，任务仍未最终验收，不提前启动 T07 实施。

T06 独立质量审查批准完整代码范围 `383167b..7c7bee8`，无可操作 Critical/Important/Minor 发现。审查者新建 `/tmp/atriumos-quality-review/helper` 完整包，真实夹具 TestNative 24.508 秒、archive 回归 4.882 秒、进程/取消/输出限制/分组 deadline focused race 6.187 秒、JPEG 资源/ABI 检查均通过；额外 C harness 对 2,000 个不同压缩级别/IDAT 边界的合法 PNG 流验证通过。仓库未被审查者改动。T06 至此完成任务级双审；不代表 T09 调度、T17 部署完成。

## T07 实施与真实模型入口

根任务使用独立临时 home/state/config/cwd/workspace、仅 worker 与必要 DeepSeek 插件的 allowlist、环境白名单和单个受保护 DEEPSEEK_API_KEY，实际调用公开 `api.runtime.llm.complete`。脚本 `/tmp/atriumos-deepseek-live-probe.py` 不含常量密钥；临时目录 `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-deepseek-live-probe-nmjb2_n4` 保存无密钥配置及合成响应，未改现用 OpenClaw 配置/渠道或发送 IM。请求 `deepseek/deepseek-v4-flash`，返回 provider=deepseek、model=deepseek-v4-flash、responseModel=deepseek-flash、stopReason=stop、execution.mode=direct-provider、owner=provider/deepseek，exit 0、stderr 0 字节。合成体检文本的受检人林明/医生王华、145.6 g/L、两项日期和参考区间正确；用量 118 input + 69 output = 187 tokens。该实际别名映射已交实施者，不能只按请求 model 字符串等同 responseModel。

此探针只证明隔离公开宿主的实际路由与完整响应，不是正式 T07 schema/typed evidence/覆盖校验，也不构成四领域质量验收或完整零工具调用审计。新的单一实施者已接手 T07：完整分块、类型化证据、严格引用/数值校验、局部主体和事件提议、真实公共 CLI 进程适配，最终必须通过正式 worker 对四领域合成资料实测。T08–T18 仍未实施，目标保持完整。

T07 实现进行中，尚未提交/双审。根任务检查初版 handoff 后明确两项：长 Office 单元格同样须完整 Unicode 分块，不能把全部 cell components 当不可拆组；多人同一事件须有逐字段的文档内 subject_refs，不能仅靠事件主体列表分配生日/数值。实施者已加入相应代码与回归。根任务在当前未提交代码上实跑 `TestUnderstandingLongCellAndNumericFabrication`、`TestUnderstandingMultipleSubjectBinding`、`TestUnderstandingRequiredPropertiesAndRepeatedEvents`，全部通过（0.261 秒）：长字符串保留、130 不得截为 30、未知主体及不一致事件拒绝、两个独立 70 ml 事件保持两项、必需引用属性缺失拒绝。此为针对性开发证据，不是正式模型、全量或任务级验收。

T07 真实模型开发验证：实施者报告最终提示下健康、教育、原始普通银行记录、明确归属财务对照、房屋 5 个案例均通过（17.43 秒），包含精确 raw/date/dimension、角色与人物绑定、事件、引用和覆盖、真实 direct-provider/deepseek receipt/usage；原银行格式保留，没有用更容易的改写替代。此前不准的模型 offsets/标签与 raw 不一致被严格拒绝，加入宿主精确 quote hints 后改进。根任务另运行 quote hints、混币种维度及完整输入预算三项针对回归通过（0.380 秒）。这些仍为未提交开发结果，正式证据归档、全量及双审待完成。

根任务继续检查真实 T06 图片产物时发现未提交 T07 分块的衔接缺口：`adapters/macos-archive-parser/evidence/report.png.result.json` 的 19 个片段被切为 16+3，两块 context 都为空；第一块独占 `-2.75`，第二块只有“变化 Delta”“mmol/L”和医生名，不能在该数值的可用输入内验证它的项目/单位，也缺患者上下文。只读 overlay `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-understanding-handoff-f9b7rr2t/overlay.json` 的 `TestRootNativeReportChunks` 增加同一数值所属块可访问原行 label/unit 的断言后，根任务实跑 RED（exit 1）。已交实施者按既定页/段落/表格边界和有界 context-only 契约修复，保留唯一 ownership/实际输入限额；不能仅凭短文本 5 例 GREEN 宣称普通图片档案已完成。

图片分块窄修复的原只读 overlay 已由根任务复跑 GREEN（0.490 秒）：19 片段现按 9+9+1 分配 ownership，context 分别 3/7/7，`-2.75` 所属块能访问原项目名与 mmol/L。实际模型图片提取仍待结果。根任务同时要求检查通用结构边界：有明确行定位的表格优先按行分块；长父文本/单元格的所有碎片不能在 context 阶段又被当成不可拆组，导致邻近上下文因总量超限全部消失。保留有界、唯一 ownership 和可验证原文范围，不以单个 3 邻居样本通过替代完整分块契约；实施与全量验收继续进行。

通用结构修复后，根任务运行 `TestUnderstandingSharedReportAnchorMerge` 与 `TestUnderstandingStructuralRowsAndLongBoundaryContext` GREEN（1.667 秒），覆盖可容纳的结构行同块、长父范围有界上下文和共享报告锚点。实施者随后报告真实 OCR 三块均通过严格字段/引用/覆盖验证且负数绑定 mmol/L，但最终合并暴露同一中英标题的短引用与全引用不一致；正在加入窄范围的原生报告标题锚点规则，原始提议仍保留，不能推广为模糊合并交易。另已在代码中核对 worker 按实际加载资源计算 configuration fingerprint、Go 校验并从原响应保留该值，重放不得用当前版本重新标记旧调用。以上仍为开发检查；最终实际证据、固定提交、完整回归与独立需求/质量审查尚未完成。

T07 首次固定实现提交 `f3af3d2bbe00c539fab896559f8151c44882b400`，根任务核对提交后工作区干净。实施端全量 Go、archive race、Linux 编译、Node、原生交接/上下文和离线图片重放均通过。最终实际配置指纹 `0fd2243da057e295d4b430caeadf7b4aca4f6d8e07dfc194ca1083e7cbcac72b` 的健康、原始银行记录、房屋及三块图片报告通过；教育因未知 schema 属性严格拒绝，明确财务对照把“元”误分为 unit 而非 currency，故真实模型验收仍未完成。健康第一次仅证据目录未创建导致 harness 失败，修复后单独重跑通过；未重跑另两项模型失败。证据位于 `internal/archive/understanding/evidence/2026-10-04/`，根任务独立验证 15 个文件哈希、提示词/schema 哈希和 7 个响应实际配置指纹。已启动新的独立需求审查，未启动质量审查或 T08 实施；较早配置的成功不能替代最终配置门槛。

审查期间的只读接口研究（2026-10-04）：本机公开 SDK 文档 `docs/plugins/sdk-runtime/models.md` 允许 direct completion 的 responseFormat，已安装 transport 会传递该选项；这并不证明远端支持任意 schema 约束。[DeepSeek Chat Completions 官方文档](https://api-docs.deepseek.com/api/create-chat-completion/) 的 response_format 当前只列 text/json_object，JSON 模式保证语法而非指定 schema；同页说明 temperature 在 thinking mode 不生效。因此现行 reasoning low + temperature 0 不构成确定性承诺，也不能将开启 JSON 模式视为教育未知字段问题已修复。[Responses API 官方文档](https://api-docs.deepseek.com/api/create-response/) 另列 json_schema，但本机公开 provider 是否支持该 transport 尚未验证，未修改生产或改走自定义 HTTP。已将限制交独立审查者，待事实驱动的修复结论。

T07 独立需求审查中已复现三项实现缺口，根任务原样运行 `/tmp/atrium-t07-spec-review/overlay.json` 的 `^TestReview` 同样全部 RED（0.217 秒）：raw 日期 2026-10-01 附另一日期证据后可被错误规范为 2026-10-04；同父同范围 4972:5228 的 section 引用在 owned 完整 view 与裁剪 context view 中因 ViewID 不同而合成两份报告且未标 review；约定的 bounded open labels 在 schema/type 缺失并被严格拒绝。审查者继续检查其余边界，尚未出最终报告；这些缺口和既有真实模型失败均未关闭。

独立需求审查最终 NOT APPROVED `f3af3d2`：上述两项 P1、一项 P2 及教育/财务维度真实验收缺口均待修复。审查 fresh 19 项理解定向、process/协议/receipt/隔离、Node 四项、实际 native replay/context 通过；没有把这些成功扩大为完整批准。同一实施者已收到完整修复任务，要求原探针输入保留、跨视图锚点使用原始证据身份、日期绑定实际 raw 来源、补齐开放标签，并在新配置下保存新的实际验证证据，禁止重标旧调用。质量审查和 T08 继续等待需求复审。

审查修复进行中：根任务在未提交改动上实跑日期 raw/derived/partial 三项回归 GREEN（0.242 秒），再原样运行独立 overlay 的 DateCrossBinding 与 SameParentAnchorDifferentViews 两项 GREEN（0.368 秒）。代码将日期候选绑定字段 raw，并以 parse/parent/component/alternative/origin/原始范围等证据身份归并和检查重叠，保留原提议引用。标签与最终版本/实际模型验证仍待完成。实施者对旧教育配置做三次有界合成诊断均返回结构合规，原 unknown-schema 子类型未复现，不能据此编造根因或消除旧失败；财务对照已确认“元”的维度种类误分。后续 v2 结构示例与维度指导须以新证据验证，不再重复旧配置抽样等待成功。

v2 未提交修复补齐有界开放标签及分块来源，root 原样复跑独立 overlay 全部三项 GREEN（0.247 秒）。真实验证仍未通过：thinking-off 配置五个文本案例通过、图片覆盖失败；恢复 low 的一致配置五个文本及四个图片 chunk 严格校验通过，但图片费用被单独分组为事件。root 重新查看原合成图，费用与指标同属唯一报告表、无独立账单/交易区段，支持补充按真实区段分组的指导；该指导保留独立账单和重复记录例外。随后一轮五文本通过、图片再次覆盖失败，不能声称指导已证明有效。完整回归另发现增长的提示词及重复 schema 导致 256 KiB Unicode/长单元格超过 maxChunks；已要求先修复输入资源回归，不抬高限额或删除夹具，暂停进一步 live 抽样，定位具体覆盖矛盾后再继续。所有配置结果须分别保留，T07 仍未验收。

输入压缩修复后 prompt 为 6,620 字节，schema 使用共享本地定义，限额不变。root 独立实跑 Partition、LongCellAndNumericFabrication、CompleteInputBudget、StructuralRowsAndLongBoundaryContext 四项 GREEN（2.193 秒）。最终 compact 配置 `88ed68580fe3ceaf4ee700fc3118e26955fbf465136fb469adc341ab7b3ad961` 的五文本实际案例通过、图片两个 chunk 的字段/引用/覆盖均通过；标题单独引用与标题加日期引用最初产生两个报告，原失败日志保留。宿主新增窄 `native-report-header-section/1`：只处理全页唯一支持的原生报告标题及同抬头区唯一明确报告日期，保留原提议、其他证据和不同报告/日期。root 对正常及 10 个保守反例全部复测 GREEN（0.271 秒）。另实测 5,000 不重叠报告检查从实施者报告的 >2 秒降至约 0.01 秒，按原组件排序区间扫描，未改变模型配置。

root 对 `internal/archive/understanding/evidence/2026-10-04-v2/compact` 独立核对 17 文件哈希、当前 prompt/schema 哈希及 7 响应实际配置指纹一致，复制到 `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-root-v2-replay-ywv4vxg4/evidence` 后运行 NativeReportReplay GREEN（0.200 秒），未改仓库留存产物。加强后的重放验证两日期、145.6 g/L、1234.50 CNY、-2.75/变化 Delta/mmol/L、患者/医生角色、字段归属/覆盖和单一报告。这里无新模型调用或旧响应重标；原采样与宿主修复重放明确区分。最终全量、固定修复提交和独立复审仍待完成。

修复已固定提交 `a19ed7a89e82fa729eca30177218343c1b5f080d`，root 核对工作区干净，另验证 thinking-off/thinking-low/section-guidance 三批历史资源哈希、文件哈希与响应指纹一致。实施端最终 full Go、archive race、Linux 编译、Node 六项、原审查探针与 native 回归通过。已交原独立需求审查者复审全部修复、新增 section policy/区间扫描、共享 schema 及最终真实证据；暂无需求批准，质量审查与 T08 未启动。

独立需求复审 APPROVED `a19ed7a`：原三探针、全部 TestUnderstanding、Node 六项、11 项 native section 正反例、独立 10,000 区间 oracle 通过；从临时副本按原 evaluator 重放五文本响应（包括 education 和“元”为 currency 的明确财务对照）及完整 native 报告通过，17 文件/7 响应/当前资源哈希一致。root 另建 10,000 seeded oracle `/var/folders/9n/qh4533dn6db5y_wc4q5v6kr40000gn/T/atriumos-root-overlap-oracle-q4dtqq0w/overlay.json` 对照朴素判定同样 GREEN（0.390 秒）。无剩余需求阻断项，不将有限合成结果表述为消除随机失败。已启动新的独立质量审查，审完整 T07 范围 `d023044..a19ed7a`；T08 仍待双审完成。

独立质量审查 CHANGES REQUESTED `a19ed7a`：非数字 raw="Alice" 可借附加 quote="70" 获 decimal=70（P1）；Go JSON 接受同时 domain/Domain 并由大小写别名覆盖值（P2）；非空正文可用零宽空 quote/raw 伪造 extracted 空字段（P2）。三者均可通过原校验，其中错误事实仍 complete=true/review=false。root 原样复跑 `/tmp/atrium-t07-quality-review/overlay.json`，前两项 RED（0.185 秒），新增空值项 RED（0.212 秒）。原 TestUnderstanding 和 Node 六项 GREEN 不能覆盖这些缺口。已交同一实施者窄修复，要求数字始终绑定 raw 来源、递归精确 JSON tag 匹配、拒绝非空组件零宽引用但保留真正空组件/derived 协议，并重放保存的真实响应；不额外调用模型，T08 继续等待。

质量修复提交 `26991d13d783bd5feb115c1557e4a35d4f92ae98`，仅校验器及新增回归，root 核对工作区干净。数字普通来源始终等于 raw；JSON 递归精确 tag 白名单；零宽普通引用只允许真实空父组件。root 原样运行质量 overlay 三反例与合法对照全部 GREEN（0.257 秒）。实施端全量 Go、race、Linux 编译、Node 六项、原需求/质量 overlays、临时副本 compact 重放通过，空组件/派生/百分比正例保留。prompt/schema/model config/captures 未变且无模型调用，已交原质量审查者复审，尚未批准最终提交。

最终 `26991d1` 获需求与质量双审 APPROVED。质量审查原 overlay 三反例及正对照 GREEN（0.258 秒），全部 TestUnderstanding GREEN（3.647 秒）、Node 六项通过，无剩余可复现缺陷。需求审查独立核对最终窄差异，原需求 overlay、Quality 正反例、typed cells/数值边界/cache/derived/日期绑定均通过，确认合法空值、百分比与历史缓存保留。T07 至此完成任务级验收，不表示 T09 消费者、生产或 42 查询系统验收已完成。

## T08 实体归属实施

新的单一实施者已开始 T08：依据已验证 mentions 和同家庭一致快照自动 link/create/pending/household，覆盖人物与房屋、多主体、相关角色、同名/别名/完整日期/强身份、版本围栏、可撤销合并、legacy 映射及私密属性公开投影。遵守原完整 T08 契约，不预设家人名单、不创建账号权限；真实模型到事实的原子发布闭环仍属 T09。尚无代码验收或部署结论。

T08 开发中检查发现并交实施者补回归的边界：单文确认不能成为家庭级永久别名；同批主名 A、别名 B、再次 B 不能把重复的 B 提升为主名；生日 A/B/A 聚合必须保留冲突，一致年份与完整日期则保留有证据的精度；部分日期已知分量矛盾应阻止强身份自动关联；全局旧成员仅属于可信 local_household。当前代码已新增相应测试，并包含并发空快照、回滚、当前授权、私密投影与 schema 12→13 升级用例。实施者仍在收尾，尚未固定提交或进行独立双审；测试存在不等于验收通过。

进一步检查发现 undo merge 仅将新关联设为待归属，但对应文档属性仍可能进入候选、公开投影和强身份比较。实施者确认在固定版本前补齐具体 link/mention 来源闭包：解除关联时对应姓名、生日及受保护身份依据失效，不能按整文档过滤误伤同份材料的其他已确认主体。实施者报告此修复前全量 Go 通过；该结果不能覆盖正在进行的闭包修改，修改后仍需重新验证和独立审查。

T08 首次固定实现提交 `0c90a0d598b0c0c2f36f9745617f11e117497c95`，root 核对工作区干净。实施者最终报告 `go test ./...`、store/archive race（34.688/44.877 秒）、Linux amd64 全包编译、archive/store vet 和差异检查全部通过。root 已独立运行重复别名、生日聚合及 undo mention 身份闭包三项回归 GREEN（0.235 秒），最后一项同时验证同文其他独立主体仍可公开、旧合并上下文确认绑定不可继续自动匹配。组件衔接说明位于 `internal/archive/README.resolve.md`；没有模型调用或生产修改。已启动新的独立 T08 需求审查，尚未批准，质量审查及 T09 未启动。

独立需求审查 CHANGES REQUESTED `0c90a0d`：同名同生日可忽略候选已有不同 national ID 的矛盾而关联（P1）；同强 ID 三个 mention 的生日为空/A/B 时，空值者先创建，组内矛盾未覆盖全组（P1）；同批新别名经强 ID 关联后未进入后续临时候选，第三次无 ID 的别名又被创建成新人（P2）。root 原样运行 `/tmp/t08-spec-review-overlay.json` 前两项 RED（10.398 秒），第三项 RED（0.181 秒）。独立基线 archive/store 测试通过，不能替代反例。已交同一实施者修复：候选范围的稳定身份负证据、先核验同身份组一致性、关联后更新临时候选且别名不升级主名。尚无需求批准，质量审查和 T09 继续等待。

修复固定为 `5c276f6867b7b86ed4e3eec0fc07974fc1569fdc`，root 核对工作区干净。规则升级 `entity-resolution/2`，保护身份比较只输出候选/局部 peer 反证，不输出原值或哈希；先检查同身份组，再决策；link 后更新候选日期和 display_alias。增加同批空 DB 不同稳定 ID、输入排列及无关候选/真实新实体正反对照，去除原三层 peer 扫描。root 原样复跑三项 spec overlay GREEN（0.403 秒）；实施者最终原 overlay、full Go、race（store 35.441 秒/archive 45.966 秒）、Linux 编译与 vet 均通过。已交原需求审查者复审完整修复，尚无批准，质量审查与 T09 未启动。

独立需求复审 APPROVED `5c276f6`：原三项 overlay GREEN（0.253 秒），新增 `/tmp/t08-spec-recheck-overlay.json` 的六种组冲突排列、正常强身份关联、无关负证据、别名权威、年份精度、输入不变与重复决策稳定性 GREEN（0.454 秒）。全 Go 退出 0（缓存），非缓存 race archive 44.645 秒/store 35.290 秒通过。审查者核对当前 HEAD 仅多 root 状态文档，代码未变、工作区干净。已启动新的独立质量审查，覆盖两次 T08 代码提交的完整范围；T08 尚未双审验收，T09 未启动。

独立质量审查 CHANGES REQUESTED `5c276f6`，两项 P2：property 写入无条件设为 full_address，把单文确认的新地址别名提升为后续全家匹配依据；合并期间针对合并前旧 link 新增/修订的确认没有自身 merge context，undo 可能漏隔离，重新将原 B 静默指向 A。root 原样运行 `/tmp/t08-quality-overlay.json` 两反例均 RED（0.250 秒），独立原 archive/store 基线通过但未覆盖这两项。已交原实施者修复 property 别名权威及绑定修订时的合并上下文，要求保留历史、合并前有效确认和同文其他主体；质量复审与最终需求回归仍待完成，T09 未启动。

质量修复固定为 `1a070c679bf326064071bf2003c2981de2802d46`，root 核对工作区干净。property 保留 display_alias；绑定引入独立 confirmation_version 与不可变的每次确认合并上下文，undo 按当前确认版本隔离，历史记录保留。新增原地址/当前限域确认、合并前绑定、同文其他主体、过期及嵌套重确认对照，更新未部署 schema 13。root 原 quality 两项独立 GREEN（0.351 秒）；实施者最终 quality/spec/recheck overlays、full Go、race（store 42.161 秒/archive 49.220 秒）、Linux 编译和 vet 均通过。已交原质量审查者复审，并交原需求审查者核对最终差异；最终双审批准尚待确认，T09 未启动。

最终 `1a070c6` 获需求与质量双审 APPROVED。需求审查相关质量/升级/撤销/绑定/legacy 用例 GREEN（0.357 秒），原 spec overlay 0.222 秒、recheck 0.192 秒通过。质量审查原两反例 GREEN（0.290 秒），新增 `/tmp/t08-quality-recheck-overlay.json` 验证事务回滚、上下文不可改删、逆序嵌套撤销和拆分后重确认 GREEN（0.560 秒），非缓存 archive/store 回归通过（8.729/3.431 秒）。没有新的可复现缺陷；T08 完成任务级验收，不代表真实发布消费者或生产验收完成。

## T09 原子发布、澄清与恢复实施

新的单一实施者已开始完整 T09：接入 T05–T08 持久任务消费者，保存不可变解析/理解产物和检查点，在同一受租约与授权保护的事务内提交实体关联、版本化事实、发布状态和 outbox。范围包括稳定事件/观测身份、人工与私密修订、版本化澄清、持久模型预算、中断恢复和 legacy 连续投影；不缩减为存储骨架。T10 索引消费者未实现前索引 outbox 保持 pending。仍使用隔离库和合成资料，尚无 T09 验收、生产或 42 查询完成结论。

首批开发检查：root 独立运行 ClaimKinds 不抢索引任务、产物/checkpoint CAS 与租约丢失后预算保留、extraction 来源/文档版本绑定、发布 outbox 故障全回滚与同文件第二来源不重复事件、人工值/私密修订重提取保留五项定向 store 测试，全部 GREEN（0.342 秒）。编译发布候选重验 Plan/Merge 并转换父证据物理锚点测试 GREEN（0.354 秒）。当前发布样本为无人物家庭资料，不能替代完整 T08 人物/房屋桥接或消费者端到端验收。typed 澄清、legacy 连续投影、消费者、角色/身份映射、重新核对文档版本及资源总量限制仍在实施，尚未固定 T09 代码版本或独立审查。

root 独立运行首个 `TestWorkerRealTextPipelineOfflineModelBoundary` GREEN（0.583 秒）：真实临时库/blob/intake → Parse → PlanChunks → ValidateChunk → Merge → 发布任务完成，第二次领取为空。模型边界使用测试函数提供合成 extraction，明确没有调用 provider；这证明当前单一无人物文本流程，不能代表图片、真实保留响应、完整身份桥接或故障矩阵通过。T09 仍在继续开发，尚无固定提交或独立验收。

下一增量 root 实跑：typed 澄清过期与 ingest-only 拒绝（当前 keep_current 路径）、legacy 撤回链乱序投影、同文件并发来源重新核对版本且复用推理、每 operation 持久次数上限及 extraction 文档绑定，五项 store GREEN（0.356 秒）；doctor_name 只保留相关角色且生日证据传入实体决策测试 GREEN（0.573 秒）。此前一次定向执行遇到新增 RebaseArchiveExtraction 测试尚缺实现的编译 RED，实施完成后以上复跑通过。typed 答案幂等/新主体、子区域私密、完整强身份映射及消费者故障矩阵仍在收敛；未据此声称 T09 完整或全量回归通过。

进一步定向复验：root 跑 PDF 同页/坐标系唯一物理匹配（IoU ≥ 0.90，跨页/旋转拒绝）与纯文本 patient/student 主体、doctor/engineer 数值反例 GREEN（archive 0.794 秒）；辅助程序字节及插件目录资源变化使 pipeline 指纹改变两项 GREEN（archiveworker 0.328 秒）；同物理 mention 重发布复用原 link 且不接受悄悄改目标 GREEN（store 0.502 秒）。这些仍是开发期定向证据；子私密派生身份闭包、typed 完整回答路径、partial 字段分离、保护身份/交易证明、runtime/API 与完整故障验收尚待完成。

root 独立复验下一稳定增量五项 store 测试 GREEN（0.312 秒）：子区域私密保留文档 TV 标记且限制原件、partial 的独立完整证据发布、第二来源复用已发布产物而不转移任务身份、typed create_subject 将“妈妈”澄清为用户指定新主体并继续发布、同一物理 mention 无生日再次接收不重复询问。此前新增测试先于 ArchiveAnswer.Name 实现造成一次编译 RED，字段及流程补齐后本次通过。当前子私密测试仅验证文档标志，不能代替公开事实/身份/证据的完整可见性闭包；T08 公开身份属性暂保守排除该受限文档。完整强身份、交易证明、多事实私密投影、故障与原生保留响应全链、全量/跨平台和独立双审仍待完成，T09 未验收、生产未部署。

root 新增复验：CurrentArchiveObservations 私密排除、同文独立公开字段保留且原件不可公开、dimension 私密引用闭包、完整交易身份匹配四项最终 GREEN（0.316 秒）。dimension 反例首次实际 RED：公开 80 的单位引用带出私密 70；修复将 Dimensions 转为 CanonicalEvidence，并另设 VisibilityAnchors 包含其物理依赖，保留值 SlotKey 身份不变，root 核对代码后原样复跑通过。交易测试只证明仓库辅助函数按完整 type/issuer/identifier 关联、缺 issuer 不关联；未证明实际消费者跨文档金额去重或冲突处理。隐私测试仍不替代全部格式/身份派生闭包、人工归属、故障矩阵、全量及独立双审；T09 继续实施。

root 独立运行两项发布事务定向 GREEN（0.338 秒）：ProtectedIdentityFullPublicationKeepsLeadingZeros 使用两份不同主体显示名但相同受保护身份 00012/issuer 的材料，最终一个实体，保护表保留前导零，普通 observation revision 不含原身份值；TransactionPublicationDeduplicatesAmountAndKeepsConflict 使用三份相同完整交易身份的 70/70/80 SGD 材料，只有一个金额 observation，冲突保留在 alternatives，当前授权事实读取不输出未决金额。这是预构造且经验证的 bundle→真实发布事务证据，不是实际模型/worker 全链。另已交实施者验证私密冲突凭证不能改变公开事实，以及跨文档共享事实在来源撤回/私密后的证据闭包；尚未以该定向通过认定这些边界完成。

root 复跑交易公开隔离增量 GREEN（0.299 秒）：原公开 70/70/80 冲突用例仍通过，新增 PrivateConflictingProofDoesNotSuppressPublicAmount 将第三份 80 凭证整文设私密后，前两份公开资料仍返回 70。当前过滤按冲突来源文档的 withdrawn/tv_visible/original_restricted 判定；此证据不涵盖局部私密的独立公开冲突、首次来源私密/撤回后的跨文档引用、冲突凭证新发布替代旧版本。相关完整闭包仍需实施与独立审查，不能扩大为全部交易公开隔离已完成。

root 独立执行 TestWorkerActualNativeWithRetainedCompletion GREEN（0.699 秒，无 skip）：原件 /tmp/atriumos-t06-fixtures/report.png，真实辅助程序 /tmp/atriumos-quality-review/helper，保留响应 internal/archive/understanding/evidence/2026-10-04-v2/compact/native-report.validated.json。实际临时 DB/blob/intake→真实 native parse→两块 plan（manifest 必须与保留模型输入完全一致）→保留 completion→实体决策→原子发布，task succeeded，1 entity、1 event、7 observations，new_provider_calls=0。测试在理解边界注入已保留 ChunkUnderstanding，发布仍重验 Plan/Merge；这是实际解析与存储消费者衔接，不是本轮重新经过 OpenClaw 进程/远端模型。故障与修订边界、完整全量回归及双审仍未完成。

root 独立复验人工归属边界 GREEN（0.309 秒）：FieldReassignmentDoesNotSilentlyReconfirmOldSubject 将 Alice 文档单条 observation 改归 Bob 后，同文件重提取进入 waiting_input，当前 observation 保留 Bob；同组 SamePhysicalNamedSubjectWithoutDOBDoesNotAskAgain 正对照仍 succeeded。此为单字段更正与窄文档关联复用的实际发布回归，不证明所有多字段/整主体修订与后续回答路径已覆盖；T09 完整恢复与最终双审继续待完成。

root 实跑产物写入两项边界 GREEN（1.394 秒，无 short/skip）：第 261 项与持久 128 MiB 字节越界均拒绝，任务 state_version 不变、产物数量不增加。ByteLimit 用 SQLite zeroblob 构造接近上限的既存数据，证明仓库写入限额事务，不代表该种子本身是合法可复用解析产物。完整产品来源绑定、恢复故障与最终回归仍待完成。

root 复验交易区域可见性三项 GREEN（0.285 秒）：公开冲突、整文私密冲突排除、UnrelatedPrivateRegionDoesNotHidePublicTransactionConflict（无关备注私密不隐藏公开金额冲突）。实施者将粗粒度 original_restricted 过滤改为冲突自身依赖区域判定。当前性检查仍在收敛：旧提取的事实可被新 active publication 保留，不能普遍用最新 extraction 等同性代替有效引用闭包；root 已要求保留旧事实/明确修正移除冲突两种对照，尚未视为完成。

root 新增独立复验：worker UnknownCallSurvivesDatabaseRestart / RevocationCancelsExternalCall / CallBudgetRemainsExhaustedAfterRestart 三项 GREEN（0.682 秒），用真实临时 DB/worker 与可控理解函数，注入 chunk commit trigger 中断、关闭重开库/过期租约恢复；原 reserved 预算不退款，权限撤销取消上下文且无事实发布，两次失败后重开库终止为 call_budget_exhausted。未以函数替身声称真实外部进程取消重测。ConflictReferencesSurvivePartialAndExpireOnReplacement 已改为 Parse/Plan/Validate/Merge/Publish、不同 extraction 的 uncertain 再完整纠正流程，root GREEN（0.565 秒）：旧当前冲突继续有效，纠正后移除当前引用且保留历史 alternative。HTTP revision strict shape/auth 四个拒绝子例 GREEN（0.555 秒），覆盖未知参数、缺 reason、ingest-only 和不存在文档；并非成功修订端到端验收。最后全量/跨平台与独立双审仍待完成。

共享人工修订最终增量 root 独立 GREEN（0.591 秒）：扩展 ManualRevisionOfSharedObservationUsesFreshRevision 验证两份当前凭证均返回人工 AssertedValue=71、Field.Raw=70 和真实 SourceDocumentID；修订字段私密后另一凭证 public70/授权private71；新增第三份公开70不继承私密断言；撤回人工来源后授权读取恢复70。实施者报告曾复现第三份凭证错误继承 user71 的 RED，现发布仅保留本来源人工版本，跨文档优先由当前可见断言选择。此新增证据补充早先仅写入成功测试，不取代多人工断言冲突/完整双审。最终回归与文档仍在收尾。

root 多证件增量三项独立 GREEN（0.282 秒）：合法 national_id+passport 同一主体现在 succeeded 并保存两项保护事实，替代早先仅 waiting 的保守实现；单身份前导零/跨显示名归属与并发 typed answer 单赢家对照继续通过。实施者使用内部有界 Additional probe 聚合多身份正反证；已要求多身份指向不同候选的矛盾与原 T08 强身份反证复验，最终受影响完整/race/Linux 尚待汇总。

T09 首次完整固定提交 `b91c49bafa64e9eb64d2a2cd69ea811f292ae5f6`，root 核对工作区干净（46 个 internal 路径文件）。实施者汇总全量 Go、最终受影响 store/worker/httpapi/app 回归、race、Linux amd64 编译、Node 55/55、原 T08 四 overlay 通过；最终 Native 无 skip，2 chunks/1 entity/1 event/7 observations/0 新 provider 调用。日志与真实覆盖范围保留在 internal/archiveworker/evidence/2026-10-04-t09/，不是独立审查批准。root 已启动新的独立需求审查者覆盖完整 T09 契约及触及的 T08 多身份聚合，要求实际反例验证；质量审查与 T10 继续等待需求审查。生产、42 查询、索引消费及 NAS/TV 系统验收均未据此完成。

T09 独立需求审查进行中已复现两项交易当前证据缺陷，root 原样执行 /tmp/t09-independent-spec-overlay.json 的 ^TestReview 同样 RED（0.285 秒）：同文件物理锚点一致的完整金额 70→80 重提取被当作跨来源冲突，进入 waiting_input 且当前金额消失；两来源70/80冲突后撤回70来源，唯一有效80仍被冲突过滤抑制。分别涉及 amountConflict 未区分同来源修订与跨来源证明、当前冲突未按有效可见来源集合重算。完整审查仍在继续，代码保持固定 b91c49b，尚未派修复或启动质量审查/T10，不因原基线测试通过忽略这些反例。

独立需求审查新增两项反例，root 对同一 /tmp/t09-independent-spec-overlay.json 的 ^TestReview(Incomplete|Shared) 原样复跑 RED（0.252 秒）：同文原有两个物理事件，重提取仅覆盖一个时错误 succeeded 并切 active，未因旧事件未匹配而保留原发布/澄清；共享观测 A 人工值71 后 B 仅改归属，当前读取按整条最新 user revision 替换，导致既有 AssertedValue 丢失回到原70，违反值与归属独立修订。当前共四项已复现，完整审查仍在进行；审查者真实 native 保留响应链通过，另旧 archive 进程取消测试一次 pid 文件时序失败正在单独复跑，未将其判为本次代码缺陷。

T09 固定实现独立需求审查最终结论为 CHANGES REQUESTED，共五项已复现缺陷。除前述四项外，分区审查与未决主体同时出现时，生成的问题允许 create_subject，但尚未建立主体 link，提交合法答案返回 sql: no rows in result set；root 原样复跑第五项同样 RED（0.307 秒）。原实施者已接收完整五项反例与原 overlay，开始修复并增加仓库回归；必须保留原反例，固定修复提交后重新需求审查，通过后再做质量审查。审查者的 store/worker/httpapi/app、四份 T08 overlay 和实际 native helper 加保留 completion 链均通过；旧 archive 进程测试单项及完整包重跑通过（完整包 8.849 秒），不列为本次缺陷。

修复增量验证：root 首轮原样执行五项独立反例，四项通过、独立归属覆盖金额仍失败（0.652 秒）；实施者继续拆分金额与归属断言后，root 再次原样执行同一 overlay，五项全部通过（0.657 秒）。这是未提交修复的局部证据，扩展顺序/私密/撤回/来源回归及固定提交复审尚待完成，T09 不据此标记批准。

root 扩展回归复验：金额/归属先后顺序、来源金额一致/不同、两轴分别私密或撤回的 16 组合，以及回答主体后仍保持旧 active 并继续分区澄清，两项测试通过（1.575 秒）；三来源在 replace/private/withdraw 后重算当前金额、核对真实来源并保留旧70历史的测试通过（0.352 秒）。这些为实施者新增仓库测试的独立执行，非新的独立设计反例；完整回归和固定版本双审仍未结束。

2026-10-11 恢复实施：核实原 T09 实施子任务因额度错误已终止，修复仍保留为七个未提交路径；此前完整回归不能声称完成。本次已指派新的唯一实施者接手原有 diff、验证证据并收尾固定提交，随后重新需求审查，再质量审查。原件、生产数据库与部署未改动；T10–T18 仍待实现。

2026-10-11 固定修复提交 `4ae4db657c3a033a2d3863b379a592b11bb949db`，root 核实工作区干净。本次实现端重新执行修复矩阵、全量 Go、archive/worker/store/httpapi/app 五包 race、Linux amd64 构建及重建 native helper + 提交合成 PNG + 保留模型响应回放均通过。新证据位于 internal/archiveworker/evidence/2026-10-11-t09-repair/；原 T09/T08 临时 overlays 已丢失，未冒称重跑。已启动新的独立需求复审，复核原五项缺陷及修复影响；质量审查仍待需求通过，未部署。

固定 `4ae4db6` 需求复审发现新 P1：仅人工改归属后，同源完整重提取70→80仍读70，发布端整条 user revision 冻结源证据。`61c2fe94f8e4983a08d9063288ad04f6acaca190` 已将当前 source 与人工值/归属审计分离，原独立 overlay 保持不变并先 RED 后 GREEN；新增10场景、全Go、五包race、Linux、实际native保留响应回放均通过。原审查者正在复审固定提交，质量审查未开始。证据 internal/archiveworker/evidence/2026-10-11-t09-source-repair/。用户明确要求按完整目标持续推进，不等待逐次催进度。

`61c2fe9` 独立需求复审 APPROVED：原反例未改SHA通过（0.548秒）；独立新增连续场景“归属→原文80→人工75→原文90→新凭证90→原断言区域私密”通过（0.399秒），公开结果Raw90无私密断言，授权读取保留人工75和归属。修复矩阵回归通过（1.254秒）。现已派新质量审查者检查 T08基线1a070c6至61c2fe9的完整T09实现，尚未取得质量批准。

完整T09质量审查（1a070c6..61c2fe9）CHANGES REQUESTED：①value-only人工修订绕过主体当前链接验证，实际Merge/UndoMerge后仍返回失效人物归属；②同文交易0012→0013更正后旧映射使独立0012凭证错误合并和假冲突；③指纹拒绝受信执行文件symlink，本机Homebrew Node因此在Claim前冲突。原独立store overlay三测试与worker两测试均RED，root原样复现store三项（0.538秒）。已交原实施者处理，保持原overlay并补当前证据关系回归；质量及必要需求复审通过前不启动T10。

质量修复固定 `628907dd632773c0bc1926200778c6350d7d8ca2`：值修订按实际来源校验当前人物关联；交易ID更正只在无共享/人工依赖时自动更新当前证明，其他情况保留旧active澄清，合入既有ID移除旧金额current引用；受信执行入口允许symlink并计算实际内容指纹，保留资源树边界。root原质量store三项GREEN1.133秒、worker两项GREEN0.345秒；实施端最终全Go、受影响race、Linux、原质量及需求overlay通过，证据 internal/archiveworker/evidence/2026-10-11-t09-quality-repair/。原质量审查者已开始固定SHA复审，通过后仍需需求语义回归。

T09最终固定 `ed69c615a9cff1291b30048fde1c0e8dc4a20f3f`：修复“更正至已有交易但缺金额时删除当前事实”回归，迁移前要求完整且安全替代；缺字段/主体未解/待确认保留旧active。原质量四store+两worker反例通过（0.352/0.571秒）、相关回归1.567秒，质量APPROVED。最终需求复审也APPROVED：原source反例0.551秒、连续人工/区域私密0.403秒、相关回归0.601秒、新独立迁移→缺金额→旧ID新凭证组合0.387秒通过。独立验证迁移后当前80保留，旧ID新70使用不同observation，无错误复合。实施端本轮全Go、四包race、Linux、native回放通过，证据 internal/archiveworker/evidence/2026-10-11-t09-migration-repair/。T09完成任务级双审，未等于上线。已启动T10唯一实施者，按完整检索契约继续；42项系统验收尚未执行。

T10固定提交 `9585d2488c2d6d2d289091dde2f23be68d1c6fc8`：四领域统一search/entity/evidence接口，schema15安全索引与FTS，当前只读快照授权、签名分页、中文短词、protected证据范围、waiting/household区分、当前legacy与outbox重建。root独立执行32问题/读快照writer并行/partial FTS NULL回归通过0.312秒。实施端最终全Go、检索race20.159秒、受影响race（store125.625秒、HTTP61.639秒、app17.951秒、worker4.695秒）、Linux构建通过。证据 internal/archive/evidence/2026-10-11-t10/README.md。已启动新独立需求审查，质量待其通过；32合成结构化问题不等于T15真实模型42验收或IM/TV/NAS上线。

T10日期修复 `b670982ef9ff6e3667bfe42e20b2ee3d27e8ff0d` 已获独立需求APPROVED：当前人工日期优先、ISO日/月/年精度、非法/未知不回退旧日期、私密隔离及原文normalization证据。原overlay与新增边界0.857秒、独立闰日/零年/年精度/时间戳及直接证据5场景0.484秒通过，现启动完整质量审查。

2026-10-11 用户再次明确最终目标为上线。只读部署基线核验：launchd com.atrium.core 运行，HTTPS /health/ready 返回ok、版本3d51eff-dirty+3d51eff；生产schema_migrations最大8，family_records总行数78（不等同78个当前事件），NAS SMB /Volumes/home 已挂载，AtriumFamily与Photos目录存在；OnePlus设备2b9d849a在线，但pidof io.atrium.tv无进程，不据此声称TV应用已运行。T17核对范围由早期六条记录更新为部署前最新完整快照全部记录/修订，防止遗漏新增数据。未改生产配置、数据库、挂载或设备状态。

T10完整质量APPROVED：ed69c6..b670982全范围无Critical/Important；store/http搜索与snapshot独立回归1.430/0.777秒，新独立字面特殊字符、证据分页终止无重复、三类受保护身份不入公开/授权检索摘录0.618秒通过。T10完成任务级双审；已启动T11唯一实施者，要求完整MCP/Brain/OpenClaw inventory/executor/receipt-renderer接线及隔离Core合成资料的真实两领域模型调用，不冒称T15全链或T17上线。

T11固定 `6e0d4cc3941beae4b02b4c7e26d81b11a2c7ca3f`：七新工具使模型库存13→20，host控制身份与resolutionkey，版本化coverage回执，真实Brain→MCP→TLSCore→SQLite联测含stale澄清/新主体/修订/撤权。真实隔离OpenClaw自主查询education数学及health血红蛋白两次工具调用，2份Core回执，9.434秒，failed/timed_out均false；root实际读取capture和最终来源文本确认。用户呈现中文领域/定位+紧凑R.D.E来源，完整ID保留structured receipts；original不当作待确认候选。最终全Go、六包race（store130.549秒）、Linux、Node62/62通过。证据 internal/brain/evidence/2026-10-11-t11/，首轮并行native超时诊断保留，串行全Go复跑通过。已启动独立需求审查，之后质量，未部署。

续跑状态核验：get_goal返回usageLimited（非active），但账户工具ordinaryUsageAllowed=true；当前用户明确继续上线，本轮执行与子任务继续，不声称自动目标续跑已恢复，且不擅自消费额度重置。

T11需求和质量均APPROVED于6e0d4cc：需求独立Brain/MCP/TLSCore/Node62通过，七工具伪造host字段/coverage/null/revise边界overlay0.351秒通过；质量独立授权/typed调用及Node档案ask24/24通过，新错误taskID/保存同步状态/null时间/非法版本/超32KiB回执均拒绝。已启动T12唯一实施者，完整基础分析+T11工具回执接线；保留精确数值、当前证据/人工断言/隐私闭包和快照失效要求，未部署。

T12 固定 c95d31c231b5a6ceaf57d95bc0886b61b24dd303，root 核实工作树干净。实施端最终全Go、五包race及最终分析专项race、Node63/63、Linux构建通过；实际TLS Core→official MCP→Brain调用分析返回450ml及两份来源。真实CSV解析发布后的128.10+71.90=200.00不再加合计，授权CNY250/公开CNY200与USD20独立；新增/撤回/私密/重建/撤权、人工值与归属、pending legacy及错配查询回执均有回归。证据 internal/archive/evidence/2026-10-11-t12/。已派新独立需求审查者，之后仍需质量审查，未部署。

发布准备已修正旧回滚说明并新增 atriumos-runbook.md，明确保全升级后资料与完整打包缺口。root将现有native helper连同lib搬至独立临时目录，在清洁环境实际解析合成PNG成功；移走JPEG依赖后结构化parser_unavailable，证据 docs/ops/evidence/2026-10-11-release-preflight/native-portability.json。此为预检，非最终签名包或T17通过。

T12需求审查CHANGES REQUESTED，三个独立反例root原样复跑均RED（archive0.205秒/store0.373秒）：原文>70合法提取70后被精确求和；同文件CNY明细使无关USD账单合计被删除且覆盖完整；sample人工serum→urine后仍按原serum分组。原反例 /tmp/t12-spec-overlay.json、/tmp/t12-spec-store-overlay.json 保留，已交原实施者修复并补扩展回归。质量审查/T13等待需求修复通过，未部署。

T12修复c7c533c使原三反例通过，新增相邻等额不同账单INV-A/INV-B仍被合并，root原样复跑RED0.263秒；eff4c2b修复当前显式账单/交易等边界，原三个overlay全GREEN，人工改号/私密闭包及全Go复跑、analysis race、Node63/Linux通过（原生取消PID时序失败及重跑保留）。

root要求核对完整F01而非简化CSV后，新独立真实XLSX反例经Parse→4chunksValidate→Merge→Publish→Analyze输出400.00而非200.00：两明细分别有TEST-BILL-001/002，原SUM(B2:B3)与历史缓存200存在，但汇总实现未使用公式证据。root原样复跑RED0.308秒；/tmp/t12-spec-formula-overlay.json、/tmp/t12-spec-formula_test.go与/tmp/t12-spec-f01.xlsx保留，已交原实施者补有界公式成员关系，需同时保持独立账单不误合并。仍未需求通过，不开始质量审查/T13。

T12最终公式修复50c4305cfd3662b8da8197eaa44aba58bf034cbd，真实XLSX仓库fixture与原/tmp一致，F01为200、人工明细更正后250、私密成员公开71.90且无旧128.10/200泄漏。只识别有界same-sheet literal SUM成员，不执行任意公式；历史cache不作为新charge，未知或缺失成员披露覆盖不足。最终全Go/analysis四包race/Node63/Linux通过。需求审查APPROVED：全部四个原overlay原样GREEN，追加公式/账单/真实Core→MCP→Brain/完整F01故障通过。已启动新的独立完整T12质量审查；T13及部署仍未开始。

T12完整质量审查发现后置70左右仍被精确计算，root原样独立overlay RED0.254秒。5fa2de8修复有界同component后置限定符/原证据绑定单位、分母及句界隔离，算法/5 schema/1；全Go、analysis四包race、Node63/Linux及五个原overlay通过。原质量复审APPROVED（原overlay0.305秒，分析真实MCP/Node7通过）；最终需求语义复验APPROVED（原四overlay、后置/人工更正、F01/账单/私密闭包通过）。T12完成任务级双审，已派新的T13唯一实施者开展可信微信/Discord附件接入，明确先隔离nativeboundary验证，真实用户新消息及生产部署仍待后续，未冒称上线。

T13实施中发现T12遗漏的真实CLI接线：cmd/atrium-brain-host/main.go:72仍要求len(tools)==20，而加入analysis后库存21，实际启动会返回unexpected model tool inventory。root读代码确认；既有TLSCore→MCP→Brain executor测试并未覆盖CLI startup，前述验收范围据此明确收窄。已交T13唯一实施者一并修复，要求共享工具名称集合完整性验证及实际CLI→MCP子进程启动回归，避免另一代码写手冲突。未部署此回归。

T13固定提交474edb817c2fff1a83d8ef57fbbaaec0823abb2e，root确认工作树干净并启动独立需求审查，质量审查待其通过。持久native capture/inbox/spool/reconcile、正常消息意图工具与回执、固定渠道补丁及CLI库存完整性修复已交审。实施端全Go、Node87/87、四包race（store177.833秒）、Linux构建通过；实际TLS Core与构建CLI/MCP子进程启动通过。微信实际monitor隔离副本验证capture先于cursor、两槽崩溃恢复；Discord实际dispatcher/SDK隔离验证消费后仅补两槽上传，模型次数保持1。两次实际隔离OpenClaw/DeepSeek正常工具轮以合成可信上下文分别选择private/inherit，root读取原始回执确认。证据 adapters/openclaw/evidence/2026-10-11-t13/；native transport/admission doubles和模型wrapper边界明确，仍未真实用户新消息验收或生产启用。

T14准备：root已按锁文件安装Web依赖，旧FamilyScreen四项测试及生产构建/Chrome66 CSS guard通过，未改Web源码。npm audit生产依赖无报告；开发工具依赖存在5项报告（含Vitest/Tinypool），未自动执行破坏性升级，发布前按实际开发工具使用范围处理。这些为基线，不代表新TV档案体验通过。

T13最终d19bb760491d9e15da0b9f0f054f87e6cabe1617获完整质量复审及需求语义APPROVED。需求发现32待澄清消息阻塞下载、全局256条阻塞其他账号，3eb61f3改SQL先筛选due/可执行或目标channel/account再限量；root两原/tmp反例RED→GREEN。质量发现openStaged先于attempt和lookup，缺失spool占前8槽并阻止已保存回执恢复；d19bb76先持久attempt/backoff→lookup严格hash/size→仅notfound读取spool，文件异常保留terminal/archive_spool。root两质量原反例RED→GREEN，完好尾槽上传1，lostack缺spool由第二次lookup恢复core_saved。实施Node93/93、native双入口/TLSCoreCLI通过；质量独立24测试、最终需求22测试及原队列反例通过。已派新T14唯一实施者，继续全家TV档案及结果展示，未部署。

OnePlus实际基线：root已打开io.atrium.tv并用方向键进入旧档案页，确认应用可连接家中服务并显示记录；不是仅ADB在线。顶部档案命名已生效，但首页菜单仍称家人、默认首位成员、部分指标英文。截图仅存本机/tmp，不提交家庭数据。T14须统一入口与中文标签并验证新页面，旧页面实屏不算新功能验收。
