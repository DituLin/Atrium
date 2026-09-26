# Android 真机验证与 PRD 后续工作

日期：2026-09-06。对象：本机 Mac mini + 群晖 NAS + OnePlus 6T（作为 TV 测试终端）。本轮为功能冒烟，不是完整 G0/V1 验收；未重启 Mac、卸载 NAS、修改原始照片或开展长稳测试。

## 实测基线

- OnePlus 6T / ONEPLUS_A6013，Android 11，Chrome 119.0.6045.66，物理屏幕 1080×2340，DPR 2.8125。
- 通过 USB ADB 检查和 Chrome 远程调试读取真实 DOM；应用流量通过家庭 Wi-Fi 访问 Mac mini，未用 USB 转发替代 LAN。
- 部署 Core 自报版本 `0.1.0+unknown`；Web 自报 `0.1.0+dev`。尚不能证明部署二进制与当前 Git 提交一致。
- NAS online、挂载身份 bound、stuck_ops=0；最近扫描 17,172 个文件，23,639 ms，errors=0。
- 可用照片 17,142，pending 26，preview_failed 4，unsupported 4（这些计数不应简单相加）；缓存约 11.89 GB / 21.47 GB，剩余磁盘约 35.50 GB，未因空间暂停。
- jobs queued=2、running=69、failed=4；recent_errors 持续出现 health_write_failed、share_stats_failed。需排查运行态任务与数据库写入问题；本轮未确定根因。

## 本轮结果

| 项目 | 结果及限制 |
| --- | --- |
| 网络 | 初始 Wi-Fi 关闭、Chrome 离线；开启后自动连接已保存的家庭网络，访问 Mac 的 ping 样本约 11.6 ms |
| TLS | Chrome 显示隐私错误；仅临时继续访问已确认的本机地址。正式 CA 信任未通过，不等于安全部署完成 |
| 配对 FR-03/13 | 测试机最初显示配对页；新建 `oneplus6t_test` 配对成功，旧屏幕记录保留 |
| 首页 FR-01/02/07 | 时钟、家庭时区、真实照片、Core/NAS/Link 在线状态正常；照片在两次观察间轮换 |
| 集合 FR-11 | all 集合返回首批 50 条，已观察到可见缩略图成功解码；未逐张验证全库，未验证分页到底 |
| 控制 FR-14/15/18 | 首次 navigate 返回 HTTP 500；复测 navigate/all applied，CLI elapsed 253 ms；refresh applied 503 ms；show applied 503 ms；navigate/dashboard applied 4737 ms |
| 指定照片 FR-14/15 | show 后 DOM 中图片 naturalWidth=1920，单张页出现上一张/下一张/返回操作 |
| 返回交互 FR-04 | ADB KEYCODE_ESCAPE（111）从单张页返回 all 集合；不是实体遥控器 Android Back 键完整验证 |
| 重载与配对 FR-13/16 | Page.reload 后首页、照片、Link Online 恢复，无需重新配对；不代表进程重启/设备重启后的持久化已通过 |
| 布局与全屏 FR-01/04 | 竖屏首页 layout viewport 曾扩至约 913 CSS px；横屏集合 layout viewport 804×284，visual viewport 约 413×146、scale≈1.95。浏览器缩放、地址栏、翻译栏影响截图；需在重置缩放与全屏后重新验证，不能直接认定所有裁切均为 CSS 缺陷 |

以上耗时为 CLI 端到端 elapsed，包含 API、轮询等开销，不是精确的“接受到渲染”延迟，也不是 P95。仅四次成功样本和一次失败，不能宣布性能达标。当前队列未清空，不满足正式性能测试前置条件。

## 后续优先级（对应 PRD）

| 顺序 | 需要做什么 | 完成标准 |
| --- | --- | --- |
| P0-1 | 排查 HTTP 500、慢命令和持续数据库写入错误；核对 jobs.running=69 与实际 worker/任务锁状态，调查 26 个 pending 的停滞原因；确认部署版本来源 | 可复现原因、针对性修复与回归；任务可正常结束或延后；稳定期不再出现同类错误。对应 FR-08/13–18、§7 |
| P0-2 | 完成 Android 终端部署：本地 CA 安装与 HTTPS/WSS 信任、重置站点缩放、横屏/全屏路径、方向/确认/返回操作 | 无证书绕过；显示不裁切；配对在浏览器及设备重启后保持；待机/切后台再返回可恢复。对应 FR-01/03/04/13/16 |
| P0-3 | 收尾媒体任务：核查失败/不支持样本，验证 HEIC 与方向；复现既有 stuck 消耗重试次数问题，必要时改为延后；优化首批预览等待 | 合法照片正常生成；不支持项有明确原因；长时间 NAS 故障不耗尽正常照片重试。对应 FR-08–11 |
| P0-4 | 完成无人值守部署：NAS 专用只读账号、GUI 登录后自动挂载、FileVault 解锁与断电后启动策略 | 真实重启可恢复服务和 NAS；记录需要人工解锁的边界。对应 FR-06/07、§7.2 |
| P0-5 | 完整 G0 功能/恢复验证：最近新增与 baseline、今天拍摄时区、随机轮播、增量照片、离线与恢复、授权撤销、备份恢复 | 按 acceptance.md / failure-matrix.md 保存证据；不可把单次成功当正式门槛通过 |
| P0-6 | 正式性能与长稳验收 | LAN 客户端读 API 各 1000 样本 P95≤200 ms；初始页面≥20 次 P95≤3 s；控制≥100 次 P95≤1 s、正常样本≤3 s；TV 重连≥10 次≤60 s；NAS 恢复≥5 次≤120 s；Core kill 与重启各3次≤120 s；24h 展示和7天实际展示窗口/可用率≥99.5%，其余指标见 acceptance.md |
| 后续 P1 | 天气、提示、日历、主题等；基础版验收后再进入可选 V0.4 home-mcp/AI 集成 | 外部功能不影响本地基础能力；停用 AI 不影响照片与屏幕控制 |

执行顺序建议：后端稳定性 → Android 可信且可持续的展示路径 → 恢复/长稳验收 → 可选功能。OnePlus 6T 的结果仅覆盖此测试终端，不替代未来真实 Android TV 的遥控器、分辨率和系统生命周期验收。

## 本轮留下的状态

- 测试机 Wi-Fi 保持开启，Chrome 留在 Atrium 首页，新增测试屏幕配对保留。
- 横屏测试后恢复原始自动旋转设置（accelerometer_rotation=1、user_rotation=0）。
- 临时浏览器证书例外仍需正式 CA 部署替代。ADB 调试端口转发在测试结束后移除。
- 未修改应用代码、生产配置或 NAS 内容；未运行 24h/7天监控。
