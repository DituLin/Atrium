# 普通月历交付记录

2026-09-13。用户将日历要求明确为普通日历，外部账号与事件同步移出本轮必要交付。代码 `fd30e88` 已从干净工作区构建、备份部署，Core 健康版本 `fd30e88+fd30e88`，Web `0.1.0+fd30e88`；OnePlus 实际加载 `index-X8I8mmcu.js`。没有原生 APK、数据库、NAS 根或挂载配置变更。

## 功能与验证

- 纸白青瓷公历月历，周一开列、固定六周、今日标记，上个月/回到本月/下个月；家庭时区、跨年、闰年和世纪规则通过。
- 使用已授权响应中最新的家庭时区；不以旧 Home 覆盖新 House 刷新结果。审查发现此问题，补失败回归后修复，独立复核接受。
- 本月模式跨月跟随；主动浏览别月时保留月份；前台恢复立即按最新时钟更新今日。无家庭时区时显示等待，不补造日期；沿用时间未经核验提示。
- 本地 calendar 路由/上报/返回栈同步 Go、TS、OpenAPI；远程 navigate 白名单仍仅 dashboard/photos。远程 refresh 通过独立 House 请求取得时区和校时，保持月份与焦点。今日来源中的外部日历标为“日程同步 · 未接入”，不影响普通月历。
- 最终 Web 49 文件、355 项测试全部通过；lint 与 build/postbuild 通过，make check 完成全 Go 测试与构建。相关 Go domain/ws/httpapi 另行通过。三尺寸 1920×1080、3840×2160、804×384 浏览器检查通过，0 页面错误，六周、三个控制及六个导航无裁切；1080p/OnePlus 等效截图人工查看。
- 真实 OnePlus 只用 ADB 遥控按键完成前后月/本月、设置往返、返回首页原焦点；浏览其他月份时远程刷新 applied 后月份/焦点保持；Core 上报 calendar。实际 804×384 CSS、2262×1080 屏幕无覆盖或横向溢出，截图人工查看。结束移除 CDP 转发，设备留在日历页供查看。

## 交付与证据

备份 `~/Atrium/iteration-20260912/rollback-song-calendar/`：原三个运行二进制、配置及 SQLite 在线备份；目录 0700、敏感文件 0600。部署前验证当前哈希、健康和 launchd 参数；原子替换后健康检查通过。

候选包 `~/Atrium/releases/2026-09-13-song-calendar/`：三个 Mac ARM64 程序、原 M2 debug APK、README、哈希清单；无配置、凭据、数据库或家庭素材。运行/发布哈希在 `manifest.json` 与 `m3-calendar/deployed-build.json` 中逐一核对。

私有验收文件位于 `~/Atrium/iteration-20260912/m3-calendar/`：`web-tests.log`、`web-lint.log`、`make-check.log`、`release-build.log`、`browser.log`、`browser-results.json`、三尺寸截图、`deployment.log`、`device.log`、`device-calendar-results.json`、`device-calendar.png`。构建前检查产物带 dirty 标识；实际部署由干净 fd30e88 重新构建。

## 视频与剩余范围

用户确认图片、视频在同一 NAS 授权目录 `/Volumes/home/Photos`，无需拆目录，不增加根。9 月 13 日同根只读探测仍 15.01 秒截止回收，未取得目录或视频候选；这是读取阻塞，不是没有视频。证据 `video-preflight-20260913/result.json`。播放器、真实 TV、正式签名和 M5 完整验收尚未完成。Mac 断电恢复、NAS 自动挂载保持暂停。
