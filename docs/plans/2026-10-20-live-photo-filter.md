# Live Photo 视频分类修复实施计划

**目标：** Live Photo 的静态照片继续进入照片库，配套 MOV 不作为独立视频展示；普通 MOV、MP4 和短视频继续展示。

**方案：** 使用 ffprobe 读取 Apple `com.apple.quicktime.content.identifier`，仅保留 `live_photo` 布尔分类，不保存标识符。该字段始终序列化在现有 metadata_json 中，无 schema 迁移。旧 ready/unsupported 记录缺字段时由现有串行 worker 重新探测一次。确认 Live Photo 后在原租约、文件版本和来源权限校验下仅发布分类，不生成视频封面。所有视频读取入口共用过滤条件；照片和 NAS 原文件不改动。

## 实施与验证

1. `internal/domain/video.go`、`internal/video/probe.go`、`tools.go`：先为 Apple 标记、空标记、普通长短视频及真实合成 MOV 写失败测试，再实现元数据读取；标记明确的 Live Photo 不依赖视频解码能力。
2. `internal/store/video_read.go`、`video_work.go`：先验证 Live Photo 在列表、详情、封面读取中不可见，再实现统一过滤与 metadata-only 发布；旧索引回填、失败退避、unsupported 终止、来源 generation 变更及 stale publication 均覆盖。
3. `internal/video/worker.go`：分类后重新验证源文件，Live Photo 跳过封面；普通视频走现有封面流程。验证不循环 claim、不修改照片以及来源重新授权后重新分类。
4. 同步 README 中英文视频能力说明，执行相关 Go 测试、race、全量 Go 测试和构建，独立代码审查后提交。
5. 在生产后端 c06c6a 基线上仅应用本次明确文件补丁，保留已部署的前端 041b483；运行同一组相关测试并构建签名。停服务后作一致性备份，原子更换 Core，验证健康、签名、schema、照片数和家庭数据。等待已有视频重新分类完成并验证 Live Photo 入口消失、普通视频仍可用。

## 边界

- 不使用时长或文件同名作为过滤依据。
- 当前 revision、source_generation 的已分类 Live Photo 才停止自动处理；来源重新授权后允许重查。
- 原有失效任务不能发布分类；新排除规则即时生效。
- 原视频、照片索引、权限、配置和配对信息保留。回滚保留原 Core 与完整操作备份。
- 使用合成测试数据；不提交私人文件名、媒体、凭据或 Apple 标识符。
