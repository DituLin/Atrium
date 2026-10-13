# Atrium

中文（主文档） | [English](README.en.md)

Atrium 是面向家庭的本地优先信息中枢，正逐步演进为家庭 AgentOS：Mac mini 运行 Core，NAS 提供只读照片与视频源，TV 展示家庭信息；档案资料、权限、证据和任务状态由 Core 管理。

当前代码包含 Go Core、React TV 客户端、`home-mcp`、受限 Brain host 与 OpenClaw/DeepSeek 接入。AI 是可选能力；基础局域网展示和影像服务不依赖模型或云端账号。启用理解或对话时，配置的外部模型服务可收到提示词、当前请求、选定的提取文本与证据上下文，或当前请求获授权的工具结果；图片/PDF 的原生解析由本机 helper 完成。

## 当前交付状态

项目仍处于预发布与持续验收阶段。综合档案能力已有本地实现和本机部署验证，完整交付边界以[实施与验收记录](docs/ops/atriumos-implementation-status.md)为准。

| 验收项 | 当前边界 |
| --- | --- |
| 档案接收、解析、分类、归属、检索、分析与 TV 展示 | 已实现，具有任务级测试及本机验证 |
| 真实渠道接入 | 新微信文字已验证；真实附件及 Discord 新消息验收待完成 |
| T15 系统验收 | 部分通过，完整模型及系统用例尚未全部验收 |
| T16 NAS 完整备份与恢复 | 完整不可变档案快照和恢复闭包待实施 |
| T17 部署与持续验收 | 固定版本已在本机上线；一小时持续验收与一万份性能验收待完成 |
| T18 最终审计 | 待完成剩余验收、恢复与最终交付审计 |

本机和 OnePlus 开发终端的验证不能代替真实电视、重启、24 小时或七天稳定性验收，也不代表通用家庭 AgentOS 已完成全部能力。

## 家庭展示与控制

| 功能 | 当前能力 |
| --- | --- |
| 首页仪表盘 | 家庭时区的时间日期、影像展示、Core/NAS/连接状态 |
| 照片 | 只读索引、预览与缩略图缓存、合集浏览、单张查看和幻灯片 |
| 视频 | 只读影像源的视频浏览与播放；可用性受格式和本机处理能力限制 |
| 今日简报 | 展示已配置来源的家庭信息、时间和状态 |
| 房屋 | 展示已配置的房屋信息与来源状态 |
| 日历 | 保留来源接口与同步状态；当前默认未接入，不是已完成的日历服务 |
| 档案 | 人物/领域/类型筛选、证据与原件查看、待核对状态和有界分析 |
| 屏幕控制 | CLI/API 的 `navigate`、`show`、`refresh`，带实际执行回执 |

TV 可使用浏览器或独立 Android host。遥控浏览与公共投影沿用同一客户端，后台任务完成不会自动抢占当前页面。

## 综合档案

- 自动分类优先覆盖健康、教育、财务、房屋资料，保留领域、文档类型和可扩展标签。
- 本地解析支持文本、Markdown、CSV、DOCX、XLSX；macOS helper 提供支持格式的图片与 PDF 提取和预览。
- 原件、提取事实与模型建议分层保存，出处可回到文本段、页码或原始组件；支持一份资料的多人/多角色关联。
- 归属使用主体角色与原文证据。同名、缺失身份或冲突保留待核对状态，不能把发送者或模型信心直接当作主体身份。
- 检索和自然语言工具遵守当前授权与公开投影；基础统计、求和与变化分析由 Core 针对已归档数据计算，并披露覆盖范围和缺失项。
- 加密、不支持、损坏或超出限额的内容有明确处理状态；保留原件不等于成功提取全部内容，XLSX 公式与宏不会被执行。

这些能力支持资料整理与可核查分析；自主执行家庭事务、完整 NAS 恢复和全部真实渠道验收仍有独立交付门槛。

## 架构与存储

```text
TV browser / Android host ← HTTPS + WebSocket → Atrium Core → read-only NAS media
                                                  │
CLI / home-mcp / Brain / OpenClaw → scoped APIs    │ local SQLite + originals + cache
                                                  └ optional external model service
```

Core 管理持久资料、家庭作用域授权、后台任务与证据。MCP 提供受控工具，Brain host 与 OpenClaw 负责对话接入和有界编排，不能直接绕过 Core 写数据库。

SQLite、索引、原件缓冲及缓存位于本机磁盘，SQLite 不放在 SMB。NAS 照片/视频目录保持只读；旧家庭记录 JSON 导出使用独立可写目录。本机操作备份和旧记录导出不能代替 T16 的完整档案备份与恢复。

## 开发快速开始

需要 Go 1.25+、Node 24+；Node 开发 mock 使用原生 TypeScript 类型剥离。

```sh
make web
make build
./bin/atrium init --config ./config.yaml --data-dir ./data --public-url https://127.0.0.1:8443
# 编辑 config.yaml：home.timezone、sources[0].root（只读 NAS 挂载目录）
./bin/atrium serve --config ./config.yaml
```

若需要 TV 访问，在 `init` 前将示例 `--public-url` 替换为 Mac mini 的实际局域网 HTTPS 地址，使证书匹配其域名/IP。纯本机开发使用 `https://127.0.0.1:8443`。

首次 HTTPS 访问前，用 `./bin/atrium tls export-ca --config ./config.yaml` 导出公共 CA，浏览器按[运维手册](docs/ops/runbook.md)配置信任；Android host 按[构建文档](android-tv/README.md)配置 `ca.crt`。信任完成后，TV 打开配置的地址并显示六位配对码；在 Mac mini 批准配对并控制显示：

```sh
./bin/atrium admin pair approve 123456 --id living_room_tv --name "Living room TV" --config ./config.yaml
./bin/atrium admin screens list --config ./config.yaml
./bin/atrium admin screen navigate living_room_tv --route photos --collection captured_today --wait --config ./config.yaml
```

管理员 token 保存在 `data/admin.token`（0600）。通过 `./bin/atrium tls export-ca --config ./config.yaml` 导出的是公共 CA 证书；私钥与管理员 token 留在受保护的本机位置。部署前阅读[部署与发布](docs/ops/deployment-release.md)及[运维手册](docs/ops/runbook.md)。

## 开发检查与构建

`make check` 检查 Go 路径：`go vet`、golangci-lint、Go 测试及三个 Go 程序的构建；需要已安装 golangci-lint。它不会执行 Web 检查。

```sh
make check
cd web
npm ci
npm run lint
npm test
npm run build
cd ..
make web-sync
```

`make web` 安装并构建前端、同步 Go 嵌入资源；`make build` 构建 `atrium`、`home-mcp` 与 `atrium-brain-host`。CI 分别运行 Go、Web、Shell 检查，并生成 macOS arm64/amd64 二进制构建产物。

## Android TV host

[`android-tv/`](android-tv/README.md) 提供基于 Android System WebView 的独立 TV/手机容器，支持方向键与返回键，不需要 Chrome 或 Google 账号。

当前 Android 交付为 debug 签名、启用 WebView 调试的开发测试 APK；不属于 CI 的公开正式发行包。家庭 CA 输入仅为公共证书 `ca.crt`，不能打包 `ca.key`、`server.key` 或管理员 token。正式签名与设备验收按 Android 文档单独执行。

## 隐私与数据边界

- 配对屏幕、管理员与集成客户端使用各自凭据和权限；TV 只读取经过授权的公共投影。
- 不跟随照片源的符号链接，不向 TV 暴露 NAS 凭据或源路径；照片预览会重新编码以移除 EXIF/GPS。
- 本地配置、数据、token、证书私钥与真实家庭资料应留在受保护且被忽略的位置；提交前检查新增文档和构建产物。
- 文档示例使用占位符或合成资料。历史文档可能保留旧机器路径与早期状态，应按当前代码和验收记录核对。

## 文档与贡献

- [文档索引](docs/README.md)
- [部署与发布](docs/ops/deployment-release.md) · [English deployment guide](docs/ops/deployment-release.en.md)
- [实施与验收记录](docs/ops/atriumos-implementation-status.md)
- [AtriumOS 系统设计](docs/architecture/atriumos-system-design.md)
- [综合档案设计](docs/plans/2026-10-03-family-agentos-archives-design.md)
- [Android TV 构建与使用](android-tv/README.md)

中文是主文档，英文作为并行完整版本维护。新增或实质改写的面向用户文档应提供中文与英文对应稿；历史文档保留原语言，直到重写时再补齐双语。设计中的规划目标、当前代码与实际验收状态应分别说明。

## 许可

[MIT](LICENSE)。
