# AtriumOS 实施与验收记录

更新：2026-10-04。开发分支 `codex/atriumos`，隔离工作树 `.worktrees/atriumos`。所有“规划/开发中/通过”区分记录，生产版本未自动跟随开发分支。

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
| T05 解析器 | 需求审查中 | `20bfed1` + `6c17f41`；文本/CSV/DOCX/XLSX 实现端 fresh 全量 Go、archive race、Linux 编译通过，独立需求审查进行中 |
| T06–T18 | 未开始 | T06–T09 OCR、理解、归属与发布契约已预审；不将设计文档或旧 family 记录作为新能力通过证据 |

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

T10–T13 实现前核查已收敛短中文检索、当前发布引用闭包、真实回执渲染、十进制分析及渠道桥接契约。实装代码确认微信首媒体/先推进 cursor 和通用非等待式 hook 不能保证可靠多附件；计划明确固定版本补丁、准入后的整批捕获、冻结槽清单、流式下载、意图与消费分账及独立恢复。当前未修改生产渠道，也未进行真实 IM 附件验收。

## T06 格式能力补充探针

2026-10-04 将此前合成报告导出 JPEG（quality 95）和无损 WebP，用已有 Objective-C Vision 探针实跑，revision 3 均返回 13 片段，八个预期字段字符串均出现。这仅确认本机这两个格式的读取/OCR 基础能力，不是 T06 实现或复杂场景准确率验收；坐标关系、EXIF、PDF 混合页和资源/进程限制仍待正式实现。JPEG SHA-256 `5b88e0ac26b48de92fa34c871de9f06655cf656eb21abc3de373bde7a0032750`，WebP `087aec3e06912c25ce50c211f9f3c5750c9eb00dce18e107f92f1035670b7008`。合成图片、输出及结果清单位于本机 `/tmp/atriumos-image-format-probe/`，没有使用家庭文件或安装依赖。
