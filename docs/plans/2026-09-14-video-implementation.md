# 视频功能实施计划

**Goal:** 从同一 NAS 授权根交付可遥控的宋式视频列表与播放器。

**Architecture:** 保留照片语义，视频独立存储与处理；共用来源身份、排除规则和授权。Core 提供受限同源读取，WebView 按明确播放意图展示。

**Tech Stack:** Go、SQLite、既有来源 I/O 封装、受限 ffprobe/ffmpeg 子进程、React/TypeScript、Android WebView。

## V2.1 独立视频存储

新增 `internal/domain/video.go`、`internal/store/migrations/0004_videos.sql`、`internal/store/videos.go`、`internal/store/videos_test.go`。修改迁移版本断言 `internal/store/store_test.go` 与 `internal/store/integration_upgrade_test.go`。

先写失败测试：同路径身份稳定，文件变化增加 revision 并清空旧元数据，旧任务结果不可覆盖新版本；移除要两次不同的完整扫描，重复 finalize 不累计；再次出现恢复，排除不可自动恢复；事务回滚不留半成品；升级保留照片/屏幕数据。执行 `go test ./internal/store -run Video` 见失败后实现，再运行 `go test ./internal/store ./internal/domain`。提交前独立审查和格式检查。

## V2.2 混合目录扫描与处理

修改 `internal/indexer/walk.go`、`apply.go`、`batch.go`，新增 `internal/indexer/video.go` 和混合目录回归。候选按扩展名分派；视频稳定后入独立表，不进入照片任务。来源身份失败、未完整扫描不判断删除；排除规则同时覆盖视频。视频处理单独领取带 revision 的任务，元数据与封面仅能发布到仍匹配的版本。

新增 `internal/video/probe.go`、`pipeline.go` 及测试，针对超时、取消、输出上限、损坏/不支持容器、旋转、无音轨与源文件变化先写失败用例。NAS 文件访问使用已有来源边界，不能把任意路径交给子进程；需要可跳转输入的处理策略须在接入前核实。执行 `go test -race ./internal/indexer ./internal/video ./internal/source ./internal/store`。

## V2.3 HTTP 契约

修改 `docs/api/openapi.yaml` 与路由注册，新增 `internal/httpapi/videos.go` 及测试。提供列表、单项、封面和内容；逐一验证未配对、撤销、来源/路径/身份限制、取消释放、GET/HEAD、有效/无效/越界 Range，失败信息不暴露路径。使用真实处理流水线输出做集成测试，不能让模拟媒体绕过授权。

## V2.4 宋式页面与生命周期

新增 Web API 类型、视频列表与播放页、路由/导航/上报及测试。参照 `2026-09-14-video-player-design.md`：明确确认后播放、左右跳转、完整比例、返回恢复来源焦点；加载中退出、授权失效、后台与迟到 play 完成均不能续播。多尺寸视觉核查与单元测试后构建。

## V2.5 部署与验收

执行 `make check` 和相关 race；先备份程序/配置/数据库，再构建可追溯候选并部署。真实 Core 接口跑 OnePlus 视频画面、声音、跳转、返回、后台和资源占用；回归照片/简报/房屋/月历及 AI 命令。保留正式电视、人工听感和 M5 长期稳定性门槛，不因原型通过而关闭它们。

Mac 电源恢复与 NAS 自动挂载暂停；不扩大 NAS 根或写入测试文件。各任务先取得失败证据再实现，完成一批检查一批，计划持续到 V2.5 和原计划 M5 的实际验收。

## 9 月 14 日执行记录

V2.1 存储基础已完成，尚未部署。新增 videos 与 video_scan_state，观察与元数据发布绑定 revision，完整扫描按来源记录原子完成水位，防止迟到代次重新插入或恢复文件。两次不同完整扫描缺失才移除；排除不自动恢复；事务回滚同时撤回数据与水位。

失败回归包括旧元数据覆盖、重复扫描计数、迟到记录恢复及首次插入。v3→v4 升级验证原照片实体与屏幕 token 保持不变，重复迁移无操作。`go test -race ./internal/store ./internal/domain` 与最终 `make check` 通过；独立审查发现的来源水位 P2 已修复并复审接受。私有检查日志：`~/Atrium/iteration-20260912/video-store-check-final.log`。

V2.2–V2.5 未完成。当前仓库新增的视频存储尚无生产调用者，运行中的 Core/数据库仍保持先前版本；视频索引不能计作真实 NAS 已导入，HTTP、封面、页面与生命周期仍按后续任务验证。

### V2.2 扫描接入进展

混合目录扫描已接入独立视频表：MP4/MOV 按配置分派，不创建图片元数据/预览任务；同目录照片继续原链路。既有排除规则会同步到已发现视频。文件稳定前不发布新 revision，旧记录只维持存在性，后续读取必须核对实际文件 size/mtime 才能使用其元数据。

视频缺失判断只覆盖本轮配置的扩展名；子目录或文件信息读取有错误的部分扫描不累计视频缺失，正常两次完整扫描才移除。video_scan_state 与来源 scan_generation 在同一事务提交，提交成功后才增加本次视频 Missing/Removed 统计。

验证：`go test -race ./internal/indexer ./internal/store ./internal/source` 和 `make check` 均通过；混合目录、不创建照片任务、部分目录无权限、重复缺失、排除、扩展名禁用/重新启用及未稳定文件用例通过，两轮独立审查接受。检查日志：`~/Atrium/iteration-20260912/video-indexer-check.log`。

尚未部署；运行配置仍是原照片扩展名列表。生产启用视频时需在原来源的 include_extensions 加入 mp4/mov，并同时部署处理流水线。V2.2 的元数据/封面处理、V2.3–V2.5 仍未完成。下一批需实现受限可跳转输入、子进程超时/取消与输出上限、revision 和授权版本发布校验，不直接将 NAS 绝对路径交给任意外部读取。

### V2.2 处理器进展

`internal/video` 已完成 fd 输入的 ffprobe 元数据与 ffmpeg 封面处理，12 个已授权 NAS 样本全部成功，最终 `make check` 与重复 race/独立复审通过。见 [处理器证据](../ops/video-processor-results-2026-09-14.md)。

后台任务、重试、缓存与授权版本发布仍未接入，V2.2 尚未全部完成。下一步优先实现这些连接，再接 V2.3 HTTP 与 V2.4 宋式视频界面；保持完整目标，不将孤立处理器视作产品交付。

### V2.2 持久任务与发布协议

已新增 `0005_video_work.sql` 与 `store.VideoWork`。Claim 在写事务中选择已授权且在线来源的任务，为视频 revision 和来源 observation generation 生成持久租约；并发领取互斥，过期租约可替换。文件版本或授权版本变化立即允许新任务，重试次数按新版本重置。

Publish 在同一事务核对视频 revision、任务 token、租约期限、来源状态/根/授权代次，再一起提交元数据和封面引用。Retry 只更新仍有效的任务，错误码限制为不含路径的短码。缓存对象将由任务 token 命名；发布返回 false 时后续 worker 必须删除对应对象。数据库层不验证实际 JPEG 文件存在，因此不能把此协议单独当作缓存已生成或已上线。

验证覆盖并发领取、未知来源不领取、超时替换、旧 token 发布拒绝、文件变化后的旧发布/重试拒绝、来源撤销后恢复也拒绝旧结果、延迟重试与次数重置。`go test -race ./internal/store`、独立重复 race 审查与最终 `make check` 通过；日志 `~/Atrium/iteration-20260912/video-work-check-final.log`。旧数据库迁移测试继续保留原照片与屏幕数据。

下一批连接 worker 运行循环与实际处理器，加入封面文件写入、总缓存预算与孤儿清理、授权/身份及源文件 tuple 再核对，然后启用后台运行。V2.2 尚未整体完成，当前 Core 和数据库未部署这些迁移。

### V2.2 Worker 与缓存串通

视频 Worker 已连接持久任务、真实 fd 处理器、来源/文件校验和独立封面缓存。隔离数据库的 12 个 NAS 样本全部完成 ready/cover 发布，缓存 461,495 字节；未改变生产配置、数据库或 NAS。见 [后台流程验证](../ops/video-worker-results-2026-09-14.md)。

尚需运行时预算分区/启停/依赖配置及失败终态、封面丢失恢复，再接 HTTP 与页面；生产应用尚未自动启动该 Worker。继续保留 V2.2 未全部完成、V2.3–V2.5 未完成状态。

### V2.2 封面丢失恢复

后台处理基础已提交为 `8b3de24`。本批补充本地封面丢失恢复：扫描保留 token 对应的缓存文件，文件不存在、为空或不符合封面文件边界时，仅清除该 token 的已发布封面引用，使任务重新领取。检查和清除期间持有缓存写锁，避免旧的缺失检查清除刚写入的新结果；数据库清理不影响进行中的 claim，也不影响替换 token。访问错误保持原记录并返回错误，不把权限失败当成文件不存在。

回归覆盖真实缓存文件删除后重新生成、完整封面不重复生成，以及 active/旧 token 不影响新发布。首次运行恢复测试复现缺失后不再处理的问题，再实现修复。此批仍未部署；生产运行时接入、总预算分配、失败终态、HTTP 和宋式视频页面继续待办。

验证结果：`go test -race ./internal/video ./internal/store` 和最终 `make check` 通过，独立复核接受；完整检查日志为 `~/Atrium/iteration-20260912/video-cover-recovery-check.log`。

### V2.2 运行时与预算接入

Core 在现有授权来源包含 mp4/mov 时装配并启动串行视频 Worker，随后台上下文取消退出。新增 `media.video.ffprobe` / `ffmpeg` 指定本地可执行文件，留空使用 PATH；launchd 部署宜填写绝对路径，示例配置已说明。

从原缓存总额预留 10%，上限 512 MiB，供视频封面使用，其余为照片额度；诊断返回两者合计及原总预算。已有照片尚未收敛到新额度时，封面写入额外核对总占用。总预算缩小时，启动前按生成时间回收超额的自有封面文件；不支持识别的文件不会被删除，无法收敛则明确报错。关闭视频扩展名但仍留封面时也执行降额和占用统计。封面缺失的数据库引用沿用已完成的自动恢复协议。

验证包含纯图片启动、混合来源装配、诊断额度与字节合计、历史照片超额时拒绝封面新写，以及预算缩小后启用/禁用来源的封面回收。本机真实 ffmpeg 生成临时 MP4，经 Runtime.Start 的后台循环自动完成 ready/cover 发布，无手动调用 RunOnce；Shutdown 正常返回。测试只使用临时目录及隔离数据库。

独立审查发现历史封面在降额后无法收敛的 P2，已补充启动回收与回归。运行中 Core 未部署本批。失败终态/用户重试、接口、宋式页面与真实部署验收仍未完成；继续按原计划推进。

最终 `go test -race ./internal/app ./internal/media ./internal/video ./internal/config` 及新增 Runtime 回归通过，`make check` 通过，独立复审接受。最终日志：`~/Atrium/iteration-20260912/video-runtime-check-final.log`。

### V2.2 失败终态与显式重试协议

Worker 对明确的无效/不支持元数据错误，在再次验证实际来源身份和文件 size/mtime 后，使用当前任务租约守卫提交 unsupported 终态，不再自动领取。工具不可用、工具执行失败、NAS I/O、缓存压力等不能据此断定文件不支持的错误保留 pending/既有状态，采用 2 分钟起、最长 1 小时的指数退避。

新增内部 RequestRetry：调用者必须先做认证与屏幕/来源 scope 校验；数据库继续拒绝已撤销来源、排除规则与已移除文件，并用客户端所见 revision 做 CAS。成功后 revision 增加、元数据清空并重新排队，因此重复旧请求和迟到旧任务都不能覆盖新处理。此批只是底层协议，尚未有面向用户的重试 API/按钮。

回归先复现无效元数据仍 pending，再验证 unsupported 不自动领取、显式重试后成功恢复 ready、旧任务与旧 revision 拒绝、撤销/新增排除规则阻止重试、处理中改变文件及工具不可用不误标 unsupported。相关 store/video race、最终 make check 与独立审查通过；额外新增排除规则回归单独 race 通过。检查日志：`~/Atrium/iteration-20260912/video-failure-check.log`。

本批仍未部署。接下来进入 V2.3 的授权列表、封面、内容 Range 和显式重试接口，再完成宋式视频页面及 V2.5/M5 验收。

### V2.3 列表与详情接口

新增 `GET /api/v1/videos` 与 `GET /api/v1/videos/{id}`，沿用屏幕配对/管理员读取权限。运行时来源绑定身份必须与数据库一致，并核对配置根；已知身份不匹配时不纳入查询。查询再次过滤 active 来源、根、允许扩展名、移除/排除状态和即时排除规则；离线仍允许查看已授权索引。

列表按发现时间与 ID 倒序使用 keyset 游标分页，上限 100 条，响应只含 ID、来源 ID、revision、状态、发现时间及就绪元数据，不返回 NAS 路径或原文件名，使用 no-store。新授权下必须有当前任务的成功发布记录才显示 ready 元数据；审查发现只判断新 Claim 会提前显示旧数据，已先复现再增加成功发布条件，覆盖重新授权→领取→发布的完整状态转换。

配对拒绝、真实屏幕 token、分页、不暴露路径、新增排除规则、撤销来源、配置根/扩展名过滤和重新授权元数据隐藏已测试。OpenAPI 增加已实现路由与 Video schema。此批不提供尚未完成的媒体 URL；封面、内容 GET/HEAD/Range、读取取消/撤销与显式重试 API 仍待后续实现，V2.3 尚未全部完成。生产仍未部署。

最终 `go test -race ./internal/store ./internal/httpapi`、`make check` 和独立复审通过；日志 `~/Atrium/iteration-20260912/video-read-api-check-final.log`。后续继续媒体读取与页面，不将元数据接口视作视频可播放验收。

### V2.3 封面接口

新增 `GET/HEAD /api/v1/media/videos/{id}/cover`，ready DTO 增加同源 cover_url，运行时注入专用封面缓存。GetCover 只返回当前授权代次、revision 和成功发布的 token。HTTP 读取前后均校验来源/发布状态；缓存读取通过 os.Root 限定目录，最多读取 2 MiB，并核对 JPEG 尺寸与发布字节数。返回 no-store，不将原始 NAS 路径、文件名或缓存路径传给屏幕。

离线仍可读取已授权缓存；缺失或损坏的封面清除对应 token 引用，在线返回 202/Retry-After 5，离线无缓存返回 503。来源撤销返回 404。HEAD 具有相同授权与状态，成功时保留 Content-Length 且无响应正文。OpenAPI 已记录 GET/HEAD 与 DTO 字段，路由契约测试正确处理 HEAD 无正文。

真实临时 MP4 经 ffmpeg→Worker→SQLite/封面缓存→HTTP 完整流程验证，覆盖已配对屏幕、未配对拒绝、JPEG 解码、HEAD、离线缓存、缺失重建、损坏重建及撤销；另有超限文件和越界符号链接读取回归。store/video/httpapi/app race、最终 make check、独立审查全部通过。日志 `~/Atrium/iteration-20260912/video-cover-api-check.log`。使用临时源与隔离数据库，没有修改 NAS 或生产服务。

接下来仍须实现原视频 GET/HEAD/Range、撤销和取消期间的资源释放、重试 API、宋式列表/播放器以及真实 Core 部署验收；封面通过不等于视频已可播放。

### 原视频读取前的文件句柄清理修复

检查流式读取基础时发现 OSFS.Open 的历史问题：受限调用超时后，底层 os.OpenFile 若迟到成功，原路径会丢失文件句柄且不关闭；打开后的 Stat 也在调用者线程执行，没有受同一 I/O 超时保护。

本批将打开与类型检查放入同一个受限 worker，使用无缓冲交接明确句柄所有权。请求超时后迟到句柄由 worker 关闭，清理完成前继续保留原 I/O 名额；成功交付后先释放名额再让调用者返回，避免正常连续请求误触 degraded。

真实本地 FIFO 回归先在旧实现下复现：超时后放行底层 open，写端仍有读端连接。修复后待清理完成，写端返回 EPIPE，证明遗留读句柄已关闭。另验证 MaxInflight=1 下连续 1000 次正常打开不会误触限额。没有写入 NAS、改变挂载或部署生产。

这项修复是后续原视频取消/超时处理的前置基础；尚未提供原视频字节、Range 或播放器，不将此测试作为播放验收。

source/video/media/indexer race、额外连续打开回归与最终 make check 通过；独立审查重复相关 race 三次后接受。完整检查日志：`~/Atrium/iteration-20260912/video-open-cleanup-check.log`。接下来继续原视频读取与取消验证。

### 原视频读取的有界流组件

新增 `video.ReadPool/Stream`，为后续内容 HTTP 提供 io.ReadSeeker：固定容量（最多 8）限制活跃文件及遗留 I/O，每次打开/读取/跳转有独立超时（最多 30 秒）。每个 stream 只有一个 actor 持有描述符，Close 只发出取消并及时返回；底层打开、读取或关闭若仍卡住，名额一直保留到实际清理完成。

读取使用独立的 64 KiB 上限缓冲区。调用者超时退出后，迟到 I/O 不会再写入其缓冲区。每次 Read/Seek 前后执行调用方提供的权限守卫，读取期间失效则丢弃该次结果；对外错误不包含 OS 路径。该组件本身不授予来源权限，后续 HTTP 必须提供授权 opener/guard。

测试覆盖正常读/跳转/关闭、读超时仍占容量且调用者缓冲区不变、读后权限失效不返回数据、打开超时后迟到文件关闭、底层 Close 阻塞时不能释放容量。当前仅组件层通过，还没有原视频内容路由，不能据此认定 GET/HEAD/Range 或实际屏幕播放已验收。

相关 Stream race 通过，独立审查重复五次通过；首次全量检查发现导出注释缺失的 lint 问题，补齐后最终 make check 通过。最终日志：`~/Atrium/iteration-20260912/video-stream-check-final.log`。没有修改运行配置、生产程序或 NAS。

### V2.3 原视频内容接口

新增 `GET/HEAD /api/v1/media/videos/{id}/content` 和 ready DTO 的 content_url。Core 每个 API 实例限 4 条流，打开/读取/跳转超时 10 秒；每次网络输出刷新 10 秒写超时。使用原 MP4/MOV 字节及正确 MIME，支持单 Range、尾段、开放结束位置、If-Range 和 ETag；多段请求拒绝为 416。

授权 opener 与每次 Read/Seek 前后守卫重新检查凭据、来源配置/绑定、当前成功发布 token、在线状态、实际挂载身份以及文件 size/mtime；打开后的真实描述符也核对类型与 tuple。取消通过 ReadPool 及时结束请求，迟到 I/O 仍占用容量至清理完成。传输开始后权限撤销会停止后续读取，客户端得到截断响应，不继续发送剩余视频。

同时修复并验证 OSFS.Open 的祖先符号链接逃逸：旧 O_NOFOLLOW 仅保护最后一级，新增真实回归复现后，用 os.Root 限定整个打开操作的根目录，并拒绝发现的祖先符号链接。打开/关闭仍处于原受限 worker 内。

独立审查发现视频与后台身份探测共用单槽时可能互相误报不可用，已改为探测在同一截止时间内等待槽；普通 I/O 的限额行为保持原逻辑。等待槽超时不增加 kernel stuck 计数。并发探测回归先复现 ErrDegraded 后修复。

真实临时 MP4 经处理流水线发布，再由 HTTP 验证完整原字节、HEAD、前段/尾段/开放 Range、无效与多段 416、条件请求、离线拒绝、未配对拒绝、请求取消及来源撤销。两条内容请求各读五次，与后台 Manager.Probe 同时运行，相关 race 多轮通过；16 MiB 临时测试文件验证已开始的响应在撤销后截断。仅修改隔离临时文件，没有写入 NAS或部署生产。

剩余：真实 NAS 内容接口与 OnePlus 播放验证、用户重试 API、宋式视频页面、部署回归及完整 M5。当前接口通过隔离测试不等于真实电视端已交付。

httpapi/source/video race 与最终 make check 通过，独立复审接受。全量检查中的测试错误包装 lint 已修正；最终日志为 `~/Atrium/iteration-20260912/video-content-api-check-final.log`。

### V2.3 真实 NAS 验证与重试 API

基线 `177f69c` 上，12 个真实 NAS 样本经新隔离数据库和处理流水线发布后，36 个首/中/尾 Range 请求均返回 206 且逐字节一致，合计 9 MiB；服务在 loopback 运行并随测试成功退出。详见 [NAS HTTP 证据](../ops/video-http-nas-results-2026-09-14.md)。不把系统缓存参与的 3–4 ms 请求时间当作冷读性能，也不替代 OnePlus/TLS/持续播放验收。

新增管理员 `POST /api/v1/videos/{id}/retry`，沿用图片重新处理的权限边界。严格接收正整数 revision，经当前来源可见性与 SQL CAS 后返回 202、新 revision 和 pending；旧版本/重复请求 409，屏幕凭据无权操作，新增排除规则立即阻止重试。请求只改任务状态，不在 HTTP 中执行媒体处理，并记录管理审计。

相关 retry/Video race、make check 和独立审查通过。日志 `~/Atrium/iteration-20260912/video-retry-api-check.log`。尚未部署；下一步进入 V2.4 宋式视频列表与播放器、状态上报及遥控生命周期，再做真实 Core/OnePlus 部署回归。完整计划仍包含正式电视、声音听感及 M5 稳定性验收。

### V2.4 播放器组件与客户端接口

新增 VideoPlayer 组件及墨色、宋体标题、青瓷焦点样式。进入预览不设置内容 src，明确确认后才请求播放；左右跳转 10 秒，上下切换播放/返回焦点。支持加载、暂停、结束、读取失败和设备不支持提示，返回先暂停、清除 src 并 load，再通知导航。视频 ID/revision 改变会卸载旧会话，新会话保持待播。

播放意图按代次管理：隐藏、pagehide、卸载和取消均使旧请求失效，迟到 play 完成再核对意图/可见性并暂停。独立审查指出旧 pause 事件可能在新 play 后到达，已用失败回归复现并增加 video.paused 实际状态核对；复审接受。

客户端新增 Video DTO、listVideos/getVideo，沿用当前授权传输，使用 no-store、AbortSignal 和授权代次保护。新增 12 项播放器及 4 项 API 测试，最终前端 51 个测试文件、371 项测试全部通过，类型检查、lint 和生产构建通过。日志 `~/Atrium/iteration-20260912/video-player-web-tests-final.log`。

此批是未接入路由的组件与客户端基础，生产构建尚不会包含未引用的播放器。尚未完成视频列表、导航/状态上报、返回卡片与滚动恢复、授权失效触发播放器卸载、错误状态与 Core 的进一步联动、真实浏览器多尺寸视觉检查及 OnePlus 播放验收。组件测试的媒体 API 为 JSDOM mock，不能替代真实媒体事件/声音/画面验收。媒体元素使用现有同源 Cookie 模式；后续集成须保持该边界，不把 bearer 凭据放入 URL，也不整文件下载来伪装 Range 播放。没有部署、修改 NAS 或运行配置，V2.4、V2.5 和 M5 均继续待完成。
