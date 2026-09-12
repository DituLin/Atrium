# 宋式房屋页：实现与验收记录

2026-09-12。M3-B 中枢状态首版已交付：B1 后端 `fcaf72d`、B2 页面 `cb1314c` 通过规范与独立质量审查；B3 浏览器、OnePlus 和 AI 回归通过。Core/Web 已从干净提交 `cb1314c` 构建并部署。房屋资料、环境传感器、真实日历和完整简报不计为已交付。

范围依据：[开发计划](../plans/2026-09-12-product-development-plan.md)、[家庭数据契约](../tech/2026-09/atrium-home-hub/family-overview-design.md)、[房屋页设计](../design/song-tv-v1/house.md)。

## B1：独立公开状态接口

新增 `GET /api/v1/family/house`，读取既有持久化观测，不读取 NAS 文件、不依赖照片统计。屏幕和管理员获得相同公开字段，integration 不获得这一端点权限，响应禁止缓存。

Core 的本次响应、各 NAS 来源的最近观测分别计时，60 秒到期即过期。读取到 offline/degraded 是有效观测，不等同于 API 失败；从未检查、来源读取失败和成功空集合分开。房屋资料与环境数据仍为未接入，不产生假房间或传感器值。

来源撤销、恢复授权和同 ID 根目录替换会清空旧健康观测与容量。内部代次存于现有 settings，随来源生命周期事务更新，无数据库迁移；旧探测不能跨代次提交。普通同根重启核对与已启用来源的重复恢复不丢失有效观测。既有挂载身份保留，重新授权不自动信任不同挂载。

探测结果中的身份、健康与容量原子提交；首次身份绑定、显式重新绑定和延迟旧结果分别有并发保护。Probe/Rebind 的数据库提交与运行时状态发布共用来源锁，避免新身份不匹配被旧“在线”覆盖。慢文件系统检查在锁外。

## 当前验证证据

| 验证 | 结果与覆盖 |
| --- | --- |
| HTTP/投影 | 公开字段与权限、OpenAPI 形状、照片表失败时 House 独立返回、来源表失败时清空旧 items、精确 60 秒过期 |
| 来源生命周期 | 撤销/恢复、撤销后配置核对、同 ID 根替换；同一时间戳也不能混淆代次；未变化配置保留观测 |
| 并发与原子性 | 延迟 Probe/Rebind 丢弃旧结果；竞争首次绑定；旧观测不能回退检查时间；写失败回滚；数据库与内存发布同步 |
| Go | `make check` 通过：vet、lint 0 问题、全仓测试和三个二进制构建；相关包 race 通过；发布同步用例另以 `-race -count=1` 通过 |
| Web | 最终 41 个文件、315 项测试通过，覆盖 House 生命周期、授权、旧响应、空列表、独立恢复和焦点；规范复审另跑 67 项，质量复审另跑 50 项 |
| Web 静态检查 | Node 24 下 lint、类型与构建通过；提交后重新构建并嵌入 Core，部署产物与检查产物分开留证 |

私有检查日志位于 `/Users/ditu/Atrium/iteration-20260912/m3-house-check-final.log`；其中构建标识为 `8dc5fd0-dirty`，代表提交前的被测工作树。正式部署前必须在最终提交后重新构建并记录版本，不能部署该检查产物后冒称干净提交构建。

## B2/B3：页面与真实设备

宋式两栏房屋页已接入导航：左侧中枢/来源独立滚动，右侧房屋资料与环境状态固定；未接入保持真实空状态。方向键、确认、返回、手动重查和跨页焦点恢复通过。可见时每 30 秒刷新，60 秒观测到期显示过期；失效请求不能复活撤销内容。HTTP-only screen_revoked、WebSocket 撤销和重新配对清空授权内容。

浏览器在 1920×1080、3840×2160、804×384 三尺寸验证 24 个长名称来源、独立滚动、空列表、读取失败、精确过期、照片接口失败、焦点和授权恢复，全部通过，无页面错误。通知与延迟响应、撤销后的迟到响应竞态单独通过。截图使用实际本地字体，已人工查看；不代表真实电视远距离验收。

OnePlus 6T 全程 ADB 方向/确认/返回，CDP 用于观测和故障注入：真实 House 来源观测、刷新保持房屋页、设置/照片往返焦点均通过。仅将照片 `/home` 响应注入 503 后，仍可经真实鉴权进入房屋页；故障已撤销。完整照片 smoke 通过：四集合、图片切换、非零滚动、原生返回、后台与进程重开、离线冷启动。启动 JS 503 自动恢复 11.223 秒，Wi-Fi 断开冷启动恢复 10.952 秒；测试结束恢复 Wi-Fi 并移除 CDP 转发。

AI 使用现有 OpenClaw + DeepSeek，查询当前房屋页、刷新、显示照片、返回首页各三次，共 12 次新调用。9 个不同控制操作均由 Core 与 Brain 台账确认 applied，三次刷新后仍为房屋页。模型入口耗时 4.384–7.375 秒；按 Core 数据库精确时间戳计算，9 次 issued→resolved 为 49.486–206.001 毫秒。只读查询回执不重复算作控制操作；这些小样本不替代 M5 性能基准。

私有证据根目录：`/Users/ditu/Atrium/iteration-20260912/m3-house/`。关键文件为 `b2-web-tests.log`、`b2-make-check.log`、`adapter-tests.log`、`browser-flow-final.log`、`browser-races-final.log`、`device-house-results.json`、`device-smoke.log`、`ai-product.log`、`ai/ai-timing-precise.json`。家庭截图与模型回执未加入 Git。

## 部署与回滚

2026-09-12T10:38:20Z 部署；健康版本 `cb1314c+cb1314c`，Web `0.1.0+cb1314c`，资源 `index-fufTWQaF.js` / `style-DiJGxgBo.css`。三个二进制均来自同一干净提交：

| 文件 | SHA-256 |
| --- | --- |
| atrium | `e66486180a07b9d387c2ac75b97eefbf7604b64c24000b580248a99639fafcb8` |
| home-mcp | `90aac169e0878454f32adc1c6ce9154650da1b8e374e5fed2ae7057c74cf56f6` |
| atrium-brain-host | `e6eb474b12a97494c206811c35ff04e51a5f1daa78fe7d6a6649737ea6ee6cf0` |

无原生改动，沿用 M2 debug APK，SHA-256 `89b530a9fead5c60795e288a458c28ecae5950b41300da391d29d8195b0181f1`。没有修改配置、TLS、launchd 参数或数据库 schema。

替换前备份三个旧二进制、配置与 SQLite 在线备份至 `/Users/ditu/Atrium/iteration-20260912/rollback-song-house/`，目录 0700，配置与数据库 0600。部署日志、哈希和健康证据见 `deployment.log`、`deployed-build.json`、`release-build.log`。家庭候选包为 `/Users/ditu/Atrium/releases/2026-09-12-song-house/`，包含三个二进制、原 debug APK、manifest 与说明；不含凭据、配置、数据库或家庭媒体。

## 未完成项与下一步

先推进提示真实更新时间/有效期和已有模块的今日汇总，再接获授权的家庭日历。房屋资料、传感器、视频、真实 TV、正式签名和 M5 长稳验收仍未完成。

NAS 在实机检查时返回过 online，但后续已有授权根的只读视频目录探测仍于 15.01 秒截止，0 个目录完成读取、0 个候选，结果为 deadline；不能判定没有视频，也不能宣称间歇性 stuck_io 已解决。证据在私有 `video-preflight-after-house/`。没有执行 NAS 自动挂载或 Mac 断电恢复。
