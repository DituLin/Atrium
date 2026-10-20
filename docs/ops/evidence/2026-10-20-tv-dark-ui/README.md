# TV 深色 UI 部署验证

[English](README.en.md)

2026-10-20，经用户明确授权部署。

- 前端固定提交：`041b4833678109735c5799de6d1f65ac88b11067`。
- 后端继续使用线上 `c06c6a591e9a33c6a93bc0def15d3e29061f9d07` 源码、依赖和迁移；以新的生产 Web bundle 重建 Core。这不是完整主分支或模型/渠道运行时升级。
- 运行版本：`tv-ui-041b483+c06c6a591e9a33c6a93bc0def15d3e29061f9d07`。
- 现有完整包校验通过。候选包只替换 Core，新增来源记录；其余三个程序、native/helper、codecs、插件与运行时绑定保持原字节。包清单明确保留后端基线提交，并由 `build-provenance.json` 单独记录前端提交、覆盖范围与 bundle 摘要。
- Core 使用既有 `io.atrium.core` 身份签名，designated requirement 与线上一致。稳定维护 CLI、配置、worker/helper 路径和 provider 绑定未修改。

上线前以隔离数据启动实际候选 Core。九条合成奶量/身高/体重记录下完成 56 次 API 请求：六页、成长曲线、周奶量、设置刷新、WebSocket 在线检查通过，无页面错误。前端 69 个测试文件、486 项测试，lint、类型检查、生产构建与 Chrome 66 CSS 构建检查通过。

切换前停止 Core 与 OpenClaw 网关，确认数据库写入句柄释放。私有一致备份保留数据库、档案原件、配置、TLS/凭据、Brain 接收/ledger/spool、渠道状态及旧程序，排除可重建照片缓存和未修改的外部运行时目录。SQLite backup、integrity/FK 与家庭业务表摘要核对通过。原子切换后 `live` / `ready` 均为目标版本；实际服务 CSS/JS 摘要匹配候选，schema 17 与既有家庭记录保持，Core/网关 running。

OnePlus6T 正式 `io.atrium.tv` 已刷新，WebView 119 实际加载新 CSS/JS。六页深色背景、视口边界、原生 ADB 遥控菜单、领域筛选及客户端版本通过，无页面错误，结束后返回首页。只有 OnePlus 完成此次设备验收；真实 TCL / Chrome 66 的视觉、性能及视频解码未复验。

NAS 照片源在部署前的备份中已为 offline，部署后相同；最近成功读取时间仍为 2026-10-19。未将既有 NAS 离线标记为本次部署回归或已修复。

机器可读脱敏结果见 [verification.json](verification.json)。完整清单、备份、API 记录和含家庭内容的截图仅存于受保护本机目录，不进入仓库。回滚保留当前数据库，只退回同 schema 的旧 Core；不能用旧备份覆盖新接收资料。
