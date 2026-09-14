# OnePlus 视频样本与遥控播放

2026-09-14。当前结论：现有 WebView 可以继续作为视频实现基线；已修复遥控播放的用户手势阻塞。本文是兼容性原型证据，正式视频索引、接口和页面尚未交付。

## 样本范围与方法

在已授权 `/Volumes/home/Photos` 只读探测 9 个目录，取得 12 个样本，ffprobe 均成功。没有要求分开图片/视频目录，没有写 NAS。完整文件名、路径、元数据和家庭画面只保存在本机私有验证目录，不提交仓库。

从中选 5 个覆盖不同特征的样本，在 OnePlus 6T、WebView 119 上测试。临时同源 HTML 通过 CDP Fetch 拦截加载，分段读取真实 NAS 文件。未安装额外服务、关闭 TLS 验证或放宽 origin；这些实验响应不是 Core 视频接口。

| 样本 | 容器 / 视频 / 音频 | 显示尺寸 | 时长 | 普通遥控原型结果 |
|---|---|---|---|---|
| v01 | MP4 / H.264 / AAC stereo | 1920×1080 | 239.01 秒 | 通过 |
| v02 | MOV / H.264 / AAC stereo | 1080×1920 | 7.34 秒 | 通过 |
| v03 | MP4 / H.264 / AAC mono | 1280×720 | 20.48 秒 | 通过 |
| v04 | MOV / H.264，−90°旋转 / AAC stereo | 1080×1920 | 11.74 秒 | 通过 |
| v06 | MOV / HEVC / AAC stereo | 1920×1080 | 22.03 秒 | 通过 |

“通过”限定为：元数据加载、确认播放且时间推进/视频帧增加、跳至 60% 并产生 seeked、暂停后时间停止、再次播放、Android Home 后暂停、返回前台仍暂停、返回退出清空媒体。设备 screencap 的 5 张截图均已人工式视觉检查，确认有画面，竖屏与旋转方向正确。音频解码字节均大于零；没有人工听感证据，不声称声音输出、音画同步或正式电视兼容性通过。

## 遥控阻塞与修复

旧 APK 对全部 5 个样本都能加载元数据，却报 `NotAllowedError: play() can only be initiated by a user gesture.`。原生 sendKey 使用 evaluateJavascript 合成 KeyboardEvent 并 click，不携带浏览器用户激活。先用 CDP userGesture 单独确认解码路径，再给容器设置 `setMediaPlaybackRequiresUserGesture(false)`，重建安装后使用原始 ADB 方向/确认/返回按键复测，未使用 userGesture 绕过播放。

修复只改变容器媒体手势配置。播放意图仍由页面负责；后续正式播放器需处理加载期间切后台/退出等竞态，不能只依赖 WebView.onPause。Android 单元测试、lint 与 debug 构建通过，独立代码审查接受最小修复。

## 实验限制

- CDP 截图没有包含视频层，设备 screencap 有画面；不能把前者黑屏判为视频解码失败。
- 首次设备截屏超过 Node 默认输出缓冲，脚本报错并恢复首页；提高截屏缓冲后完整重跑成功。
- 最终原型记录 48 次媒体分段请求，另有 3 次 `Invalid InterceptionId`（v06/v01）。没有媒体错误且上述断言完成，但没有足够事件追踪精确归因这些失效拦截；保留为实验工具限制，不将本次结果计作生产 HTTP Range/取消读取验收。
- 只跳到一处时间点，没有验证任意双向拖动、完整长视频播放、字幕、多音轨、HDR、4K 或所有 HEVC profile。
- 临时页面实现了基本生命周期，正式播放器还需独立单元测试与真实 Core 接口回归。本次不是 M4 或 M5 全部通过。

私有证据：`~/Atrium/iteration-20260912/video-samples-20260914/` 的 metadata.json、device-video-results.json（修复前）、device-video-decoder-results.json（隔离解码）、device-video-remote-fixed-results.json（修复后）、video-*-device.png 与各日志。结束后禁用拦截、移除调试端口转发并恢复 Atrium 首页。

## 原有功能与安装包

修复 APK 上 `android-tv/scripts/smoke.mjs` 退出码 0，照片四合集、上下张、来源卡片与滚动恢复、返回归属、长按确认、设置三面板、原生菜单、远程 show/refresh/navigate、前后台与进程重启配对均通过。今天拍摄合集为空，未把它计作上下张样本。网络开关和启动故障注入本轮未启用。

候选包在 `~/Atrium/releases/2026-09-14-video-remote/atrium-tv-debug.apk`，SHA-256 为 `329b66ecec7a61851789faec8f028d9b7d8e1f433552da209a8264e1ce96a724`，已用设备安装路径 sha256sum 核对一致。Core 保持 `dbbca5a+dbbca5a`；此 APK 是容器修复候选，不带正式视频页面。USB 复制仍未完成。

下一步见 [播放器实现基线](../plans/2026-09-14-video-player-design.md)。
