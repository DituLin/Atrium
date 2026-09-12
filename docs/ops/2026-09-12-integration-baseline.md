# 2026-09-12 集成基线

状态：M0 源码集成、构建与部署回归通过。此记录不表示 M1–M5 已交付。

## 源码来源

集成工作区 `.worktrees/product-iteration`，分支 `codex/product-iteration`，共同基点 `c459876`。

- `79ad91c` 保存主工作区 Android / NAS / 设计与计划基线（31 文件）。
- 第二组集成保存 AI Brain、home-mcp、OpenClaw 与 TV generation 修复（104 文件；提交见 Git 历史）。
- 逐文件核对：102 个 AI 独有文件、27 个相同改动文件、1 个主目录独有计划文件均与来源一致。
- 三个文档差异按内容解决：README 与 AI 技术方案采用 AI 工作区的实接状态；旧路线保留主目录的 9 月 12 日新计划指引。
- 原主目录、AI 工作区、私有运行数据及旧回滚目录保留。清单和补丁存于私有 `~/Atrium/iteration-20260912/`；家庭图片与凭据不进入 Git。

## 检查

2026-09-12 集成源码检查：

| 检查 | 结果 |
| --- | --- |
| make check（vet / lint / 全部 Go 测试 / 三程序构建） | 通过，lint 0 问题 |
| go test -race ./... | 通过 |
| Web lint / test / build | 通过，216 项测试 |
| node --test adapters/openclaw/*.test.mjs | 通过，44 项测试 |
| 分组差异与敏感文件检查 | 源码保留与集成质量两轮独立审查通过 |

首轮 Web 检查使用本机 Node 23.11.0，npm 报 eslint-visitor-keys engine 不支持警告。后续构建切换到随工作环境提供的 Node 24.19.0；不修改全局 Node。嵌入资源须执行 `make web-sync build`，不能仅依靠 web/dist。

审查后补齐 Go 模块直接依赖与 checksum（未变更版本），CI 保留 Core / MCP / Brain 的双架构共六个二进制；`go mod tidy -diff` 无差异，`make release` 六个产物构建通过。历史 AI 计划同步最终验收索引。

## 既有运行部署回归

旧 Core SHA-256：`736a2011e42d7602589660239d4c38df8be8ac3506963678a5595d32594681b5`，对应 9 月 7 日 generation 修复构建。安装中的 ask / host / plugin / renderer 与本次集成源码逐文件一致。

9 月 12 日 OnePlus `oneplus6t_tv`：健康检查 OK、屏幕在线；D-pad 右移/确认、Android Back、show 图片解码、refresh 保持页面、回首页、Home 后恢复及进程重开保留配对均通过。四条 CLI 控制均 applied，含 CLI 的观测耗时为 264 / 519 / 522 / 272 ms；不作为正式延迟基准。私有记录 `~/Atrium/iteration-20260912/baseline-device.jsonl`。

正式 ask 查询成功：Core reachable、屏幕在线、NAS 来源 degraded；没有控制动作。不将 degraded 改写成“家庭正常”。这是既有部署回归，新的集成构建部署结果须独立记录。

## 保留边界

Mac 断电恢复、NAS 自动挂载仍暂停。OnePlus 是开发终端；真实电视远距离可读性、正式签名、24 小时展示及 7 天可用性仍未通过本次记录证明。

## 集成构建部署验收

提交 `7f43094`，构建时间 2026-09-12T07:50:15Z。先保存旧二进制、配置与 SQLite online backup 一致性快照至私有 `~/Atrium/iteration-20260912/rollback-integration/`，随后替换三个可执行程序并重启原 Core launchd 服务。没有修改 launchd 配置、配对、NAS 或电源设置；未重建 Android APK（原生源码未变）。

| 产物 | SHA-256 |
| --- | --- |
| Core | b4adae878e277792347bb28dc9f144566af4d82083d5817ff9b88edac95527e6 |
| home-mcp | 989cb3fdd6584ef6b91400c31048089577b5682317cb2e0c1d53e507ec58afaf |
| Brain host | dc81b4161e85171c953d69e8ccd0b95105f8302c93b0382fae13425b41ab2796 |

健康检查返回 `7f43094+7f43094`。该次嵌入 Web 资源来自提交前已核验的集成源码，UI 构建标签仍为 `0.1.0+79ad91c`（9 月 12 日实读确认）；此标签不能当成纯 79ad91c 源码包。M2 发布时在最终提交后重新执行 Web build，再嵌入 Core，以统一构建对应关系。集成部署再次通过同一 OnePlus 全流程 smoke：D-pad/确认/返回、show、refresh、首页、Home 后恢复与重开保留配对；四次控制 applied，观测耗时 266 / 271 / 523 / 269 ms。正式 ask 查询在线状态并回首页成功，回执保留在私有 `integrated-ask.txt`；不将模型等待计入 TV 渲染延迟。

证据：私有 `~/Atrium/iteration-20260912/{deployed-builds.json,integrated-device.jsonl,integrated-ask.txt}`。M0 通过并进入 M1；不以此提前完成产品体验或正式 V1 验收。
