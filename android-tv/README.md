# Atrium Android TV

独立安装的 Android TV / Android 应用。无需 Chrome、浏览器登录或 Google 账号；使用设备提供的 Android System WebView（Android 8+，需可运行当前前端的现代 WebView）。同时注册 TV Leanback 与手机启动入口，OnePlus 6T 可作为真机测试终端。

## 构建与安装

需要 JDK 17+、Android SDK 35、Node 24+（仅真机冒烟脚本）。首次构建 Gradle 会下载官方构建依赖。

```sh
export JAVA_HOME='/Applications/Android Studio.app/Contents/jbr/Contents/Home'
export ANDROID_HOME="$HOME/Library/Android/sdk"
# 导出的家庭公共 CA；绝不能传入 ca.key 或 server.key。
./gradlew :app:testDebugUnitTest :app:lintDebug :app:assembleDebug \
  -PatriumCa=/absolute/path/to/ca.crt
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n io.atrium.tv/.MainActivity
```

命令在 `android-tv/` 中执行。APK 绑定这一个家庭 CA，但地址由连接页输入。CA 变更后需要重建 APK；服务端证书只要仍由该 CA 签发且域名/IP 匹配，无需重建。Gradle 验证输入确为有效 CA，将公开证书放在忽略的生成目录，不保存家庭地址或私钥到仓库。

当前交付为开发测试 APK，开启 WebView 调试、使用 Android debug 签名。家庭正式发布可构建 `assembleRelease` 并用维护者的长期签名密钥签名；release 自动关闭 WebView 调试。不要把 debug 包作为公开商店发行包。更换签名必须重新安装和配对，同签名更新可保留配对。

## 首次使用

1. 在原生连接页输入 `https://<mac-mini-address>:8443`，选择“连接家庭中枢”。
2. 应用显示六位配对码，在 Mac 执行 `atrium admin pair approve <code> --id <screen-id> --name 'Atrium TV' --config <config>`。
3. 自动进入全屏首页。应用保存地址与屏幕 Cookie；退出、强制结束进程后重新打开仍能使用配对。

## 遥控器

- 首页左右键进入照片集合，确认键打开当前轮播照片。
- 集合方向键移动焦点，确认键打开照片。
- 单张页左右键切换、返回键回到集合/首页。
- 菜单键打开“继续展示 / 连接设置 / 退出应用”。没有菜单键的遥控器可快速双击返回键。
- Android Home 键仍正常退出到系统桌面；应用不会夺回前台。

## 生命周期与网络

- 横屏、沉浸式展示，前台使用 KEEP_SCREEN_ON；不阻止用户关机、待机、Home 或系统节能，不请求后台保活/开机夺取前台权限。
- 已加载页面断线时由现有 Web 客户端保留内容并重连；无法加载页面时原生界面按 1–30 秒退避重试。
- 暂停时停止原生重试，恢复前台后继续；渲染进程退出时提供“恢复展示”。
- HTML 成功但启动脚本失败时，10秒启动检查会触发原生重试；普通照片失败不会触发整页重载。
- TLS 错误显示处理提示并取消连接。没有 `SslErrorHandler.proceed()`，没有信任所有证书、HTTP 降级或系统 CA 导入。
- WebView 仅接受配置的 HTTPS origin；禁用文件/内容访问、第三方 Cookie、混合内容和弹窗；无 JS-to-native 桥接，关闭 Android 数据备份。

## 真机回归

安装 debug APK，完成配对，设备连到家庭 Wi-Fi 并保持 Atrium 在前台。脚本会导航、展示照片、切后台、强制结束并重启应用，最后回首页；不会改 NAS 文件。

```sh
ADB="$ANDROID_HOME/platform-tools/adb" \
ATRIUM_BINARY=/absolute/path/to/atrium \
ATRIUM_CONFIG=/absolute/path/to/config.yaml \
ATRIUM_SCREEN=your_test_screen \
node scripts/smoke.mjs
```

加 `ATRIUM_TEST_NETWORK=1` 会短暂关闭测试机 Wi-Fi、离线冷启动后恢复 Wi-Fi，验证原生重连页与自动恢复。仅在指定测试机执行。脚本使用 localhost:9223 临时转发，退出时移除。输出只记录结果和耗时，不输出凭据、照片路径或内容。

OnePlus 6T 的通过结果不等同于真实电视遥控器、TV 厂商节能策略、24 小时或七天稳定性验收。

`ATRIUM_TEST_BOOT=1` 使用 WebView 调试协议让首个 JS 请求返回一次503，验证原生启动恢复；后续请求恢复正常。可与网络测试同时开启。
