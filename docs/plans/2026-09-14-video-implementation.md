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
