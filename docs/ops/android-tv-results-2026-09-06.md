# Android TV 应用与后端修复实测

2026-09-06。继 `android-validation-2026-09-06.md` 的 Chrome 冒烟之后，本轮交付独立 Android TV 应用，并修复观察到的 Core 写入争用。结果仅覆盖所述测试，不将冒烟推断为长稳验收。

## 已交付

- `android-tv/`：原生连接设置 + 全屏横屏 WebView，Android TV Leanback/手机双启动入口，Android 8+。依赖 Android System WebView，不依赖 Chrome、外部浏览器或 Google 账号。
- 家庭公共 CA 在构建时显式注入，仅该 CA 被信任；HTTPS、WSS 正常验证，不绕过 SSL 错误。地址可在应用内修改，设备配对使用原有独立 screen Cookie。
- 方向键/确认键发给当前焦点元素，Android Back 返回应用内上级；菜单键或双击返回可打开设置/退出；Home 和用户切后台照常工作。
- 原生离线冷启动页、1–30秒重试；前台保持屏幕、后台停止原生重试；配对 Cookie 落盘；WebView 渲染退出有恢复入口。
- `dist/android-tv/atrium-tv-0.1.0-debug.apk`：当前实际安装的测试包（构建产物不提交 Git）。SHA-256 `913ad1d4199871f64ce93884170e7ed5cb5b22fa7924f141bc836fa3eabd07bc`。调试版可用于设备验证；公开分发需维护者长期发布签名。

## Core 修复和部署

1. **扫描持有写锁时等待 NAS**：原 batch 在两次文件处理之间保持事务，ReadDir / DirEntry.Info / Stat 的网络延迟因此占用 SQLite writer。新增复现测试在各 I/O 入口尝试并行写入，旧实现稳定报 SQLITE_BUSY。新实现先缓冲候选处理，再仅在 flush 的数据库阶段开启短事务；常规扫描和稳定性轮次均通过回归。
2. **NAS 卡顿消耗照片重试**：ErrStuck / ErrDegraded 改为 Defer，利用既有任务延后及预览 pending 路径，不增加照片失败次数；有失败→通过的回归证据。
3. 高 DPI 小 CSS 视口的固定字号下限造成内容超宽；调整小视口字号、时钟尺寸和状态栏换行。真机横屏 layout viewport 与 scrollWidth 都为 804，scale=1；截图显示时钟、照片与状态栏完整。
4. 部署前保留旧二进制、配置及 SQLite online backup 一致性快照到运行目录下 `rollback-tv-<timestamp>/`。Core 部署版本 `c459876-dirty+c459876`。
5. 更新签名触发 macOS 网络卷重授权。日志明确提示旧 code requirement 不匹配；维护者手动允许后，重启同一二进制释放旧阻塞 I/O，NAS online、stuck_ops=0，预览任务继续完成。没有修改 TCC 数据库或放宽系统安全设置。

## 验证证据

设备：OnePlus 6T / Android 11，Android System WebView `119.0.6045.66`，物理屏幕 1080×2340，横屏应用 CSS viewport 804×384、DPR 2.8125。测试经过家庭 Wi-Fi 访问真实 Mac/NAS；USB 仅负责安装、输入和调试。

| 检查 | 实测 |
| --- | --- |
| Android 构建/安全地址测试/lint | Gradle assembleDebug、3项 JUnit、lintDebug 通过 |
| Go | 全部包 go test、go vet 通过；golangci-lint 报告 0 issues |
| Web | 28文件、214测试通过，eslint 与构建通过 |
| 配对/TLS | 独立应用新配对成功；无证书警告或 SSL 绕过 |
| 全屏 | 无 Chrome 地址栏、无横向页面溢出；时钟和真实照片可见 |
| D-pad/Back | 右键改变焦点、确认打开照片、Android Back 返回集合，自动断言通过 |
| show/refresh | 图片 naturalWidth>0 后 show 成功；refresh 保留单张页面 |
| 切后台 | Home 后重新打开恢复展示，未夺取前台 |
| 应用重启 | force-stop 后重开回到首页，不重新配对 |
| 启动资源故障 | HTML 200、首个JS 503，旧版25秒无法自动恢复；新增原生启动检查后11,373 ms恢复Dashboard，真机断言通过 |
| 离线冷启动 | 关闭 Wi-Fi 后冷启动出现原生“正在重新连接”；最终APK恢复 Wi-Fi 后10,872 ms回到在线首页，自动断言通过 |
| 控制100样本 | 100次 dashboard navigate 全部 applied，CLI端到端 P50=276 ms、P95=279 ms、max=280 ms；零失败、无HTTP500 |
| NAS恢复 | 授权后online、stuck_ops=0；完整扫描17,172文件、22,742 ms、errors=0；ready=17,166，queued=2（历史decode_failed延后）、running=0，unsupported=4 |

100次控制测量来自 Mac CLI（含轮询开销），且测量时 NAS 因权限处于 degraded、缓存仍可用。这证明该环境下控制不被 NAS 阻塞，不替代 `acceptance.md` 中所有正常运行基线/路由/性能门槛。恢复后还需结合实际家庭长稳窗口验收。

可复现命令与环境变量见 `android-tv/README.md`，脚本 `android-tv/scripts/smoke.mjs`。网络故障测试需显式开启 `ATRIUM_TEST_NETWORK=1`，finally 会恢复 Wi-Fi。

## 仍需单独验收的运行边界

- 本轮没有完成真实电视机的分辨率/厂商 WebView/遥控器兼容性验证，也没有24小时/7天稳定性数据。
- Mac FileVault 解锁、断电自动启动、NAS 自动挂载与专用只读账号属于后续无人值守部署工作。
- 库中既有不支持/解码失败文件保留真实失败及重试状态，不伪造成功、不修改原图。
- 开发签名的 Go 二进制更新可能再次触发 macOS 权限提示；正式维护应采用稳定的可信签名流程。

最终核验：从设备读取已安装base.apk，与交付APK的SHA-256完全一致；独立TV停留Dashboard且online=true，open_commands=0；NAS online、stuck_ops=0，当前进程recent_errors为空。临时ADB端口转发已全部移除。
