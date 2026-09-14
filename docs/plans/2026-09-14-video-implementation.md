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
