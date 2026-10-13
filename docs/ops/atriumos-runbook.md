# AtriumOS 发布与恢复

状态更新：2026-10-13。当前本机部署基线为 `c06c6a591e9a33c6a93bc0def15d3e29061f9d07`，见 [TV 排序与奶量趋势热修验证](evidence/2026-10-11-archive-tv-hotfix/README.md)。线上源码已合入 `main`；本次整合未更换运行程序。完整 T17 验收仍未完成。下文保留 2026-10-11 首次上线及各次预检的历史状态，包括早期 `3fb9d38` 的发布结果。当前构建、部署和恢复顺序请阅读[中文发布指南](deployment-release.md) / [English release guide](deployment-release.en.md)。

## 发布包

同一固定提交必须提供 Core、维护 CLI、Brain host、MCP、OpenClaw 适配器、匹配渠道版本的补丁、Web 资源，以及 macOS 原生解析 helper、固定版本 codec 依赖和许可。T13 新增的 `atrium-spool-read`、附件接入模块、补丁脚本与版本/文件摘要 pins 也属于运行依赖，不能留在开发工作区中引用。记录提交、各文件摘要、渠道版本和签名验证结果。

2026-10-11 代码检查：现有 `make release` 构建 Core/MCP/Brain 并执行 `web-sync`，但不先构建 Web，也没有打包渠道补丁和原生 helper。因此不能直接把这个目标的输出视为完整发布包。T17 需补齐构建和打包，验证脱离开发目录及 Codex runtime 后的实际运行。

本次正式部署使用已补齐的 `deploy/build-release.sh`，从固定提交导出源码，先构建 Web 再嵌入 Core，包含匹配插件、spool reader、渠道补丁与 pins、原生 helper、codecs 和许可。最终签名后清单有 57 个文件，部署后逐一复核字节数与 SHA-256；稳定运行路径的四个二进制与发布包一致并通过严格签名验证。

同日预检：复制此前构建的 helper 与完整 `lib/` 到新临时目录，在 `/tmp` 工作目录及仅有 PATH/LANG/LC_ALL 的环境中，通过 FD 3 输入仓库合成 `report.png`，返回 `parsed`，无 stderr。移走必需 JPEG codec 后返回结构化 `unsupported/parser_unavailable`，无 stderr。摘要保留于 [native-portability.json](evidence/2026-10-11-release-preflight/native-portability.json)。这是现有 helper 的搬移与缺依赖预检，尚非最终签名发布包验收。

运行配置也须完整：`archives` 的 native helper、Node、OpenClaw、worker plugin 和 provider 路径应指向已发布资源。当前 worker 从 Core 进程环境读取 `DEEPSEEK_API_KEY`；仅 OpenClaw 网关可调用模型不能证明 Core 已具备该环境。2026-10-11 只读检查现有 Core launchd plist，仅声明 `ATRIUM_CONFIG` 环境项。启用前须通过受保护的服务启动方式提供密钥，并以实际服务环境解析合成档案验证；不得把值写入仓库、命令参数或日志。

2026-10-11 14:50 只读预检：本机 Weixin 2.4.9 与 Discord 2026.9.8 的 10 个补丁目标文件均匹配固定摘要，`patch-archive-ingress --check` 返回 verified=10、changed=10、applied=false，生产尚未应用。当前 Core 签名严格校验通过，保留的 designated requirement 为既有 `io.atrium.core` 身份；摘要见 [runtime-pins.json](evidence/2026-10-11-release-preflight/runtime-pins.json)。运行时路径与签名仍需部署时重新核验。

## 升级顺序

2026-10-11 13:24（新加坡）隔离迁移预检：以只读连接取得当时生产数据库的一致副本，再用固定 `d19bb76` 的实际 `store.Migrate` 在独立副本执行 schema 8→16。原有 22 张表的全部行内容未变，家庭记录及更正历史 80 行逐行集合一致，完整性检查通过、外键违规 0。仅无内容的汇总见 [migration-t13.json](evidence/2026-10-11-release-preflight/migration-t13.json)；含数据副本留在本机私有临时目录，不提交。此预检未启动服务、未修改生产库，也不替代最终版本的完整备份、回填与上线演练；生产已从早期 78 行增至 80 行，正式部署必须重取最新快照。

同一快照的另一独立副本上调用既有 `ProjectLegacyArchive` 处理全部 80 行，再倒序重放 80 行，72 个修订链事件 ID 均保持稳定。全部旧 ID 有映射，当前记录为链末端、根为链起点，投影正文/撤回状态/TV 可见性与当前源记录一致，原家庭记录未改，integrity/FK 检查通过。仅汇总见 [legacy-projection-t13.json](evidence/2026-10-11-release-preflight/legacy-projection-t13.json)。这验证现有投影函数处理实际历史副本；可恢复全量回填命令、并发增量追平和最终发布版本仍待 T17 验收。

1. 重新核实正在运行的版本、数据库 migration、渠道版本、NAS 挂载和设备状态。早期 schema 8 和家庭记录数量仅为历史基线。
2. 取得一致数据库备份及原件、配置、Brain 待处理状态的受保护备份。不要在终端或提交中输出凭据。
3. 在独立数据目录演练迁移，核对部署前最新快照的全部家庭记录、更正历史、原件摘要和当前权限。不得仅核对早期六条示例记录。
4. 验证签名及完整发布包，以新功能关闭状态部署。完成旧记录回填、事务 outbox 追平和原有新增、更正、隐藏、撤回回归后，再启用档案功能。
5. 验证健康检查、真实渠道接收、当前证据查询、TV 展示与私密撤销、NAS 同步和恢复。一万份合成资料性能测量及一小时持续接收/查询故障恢复需保留实际结果。

渠道验收使用用户实际发来的新消息；代理不得把本地夹具测试写成真实微信或 Discord 接收通过。TV 验收需打开应用并核对实际页面，ADB 在线本身不等于展示成功。

2026-10-11 只读设备预检：OnePlus 的 `io.atrium.tv` 为 versionName 0.2.0 / versionCode 2，当前系统 WebView 为 `com.google.android.webview` 119.0.6045.66。此结果不替代 Chrome 66 兼容验证，也不代表新档案页已运行或展示通过。

## 回滚和恢复

回滚前先保全升级后的数据库、原件、分析/处理历史和 Brain 接收队列。仅恢复升级前数据库会丢失升级后尚未同步到 NAS 的资料。

先确认旧应用支持目标数据库 schema。未知 schema 必须拒绝打开；不可仅凭 migration 数量推断兼容。需要恢复时写入独立目录，核对完整性、引用关系和权限后切换服务，不能覆盖唯一的新数据副本。

整机运行备份包括配置、凭据和接收中间状态，必须限制访问。T16 档案恢复是另一条路径：恢复原件与档案图，不自动恢复有效的 principal 凭据或授权；索引可重建，未完成处理须重新授权后才能继续。

保持现有 NAS 挂载、只读照片范围、自动登录配置与签名身份。新增归档备份使用约定的独立目录，不混入旧照片恢复流程。

2026-10-11 实际 SMB 能力预检：确认 `/Volumes/home` 为 SMB 挂载后，在 `AtriumFamily` 下新建随机私有临时目录，写入 4160 字节合成数据。文件 fsync、同目录重命名、目录 fsync 与重新打开后读回比对均成功，临时文件和目录已清理；照片目录未写入。仅汇总见 [nas-capability.json](evidence/2026-10-11-release-preflight/nas-capability.json)。这不等于 T16 清单提交/恢复验证，也不证明断电后的 NAS 硬件持久性。

## 发布结果

2026-10-11 本机正式运行版本为 `3fb9d38fdeb245515e514f28f134ea91669e15d9`，不可变包位于 `~/Atrium/releases/atriumos-3fb9d38`。`/health/live` 与 `/health/ready` 均返回 200/ok 和该提交；Core 与 OpenClaw 网关均为 launchd running。Core 通过 `--provider-env-file` 从目录0700、文件0600的单键文件读取 DeepSeek 凭据，plist 中不保存密钥。helper、worker plugin、网关插件及 spool reader 均指向正式发布资源。

正式切换前取得停接入、跨库静止的完整操作备份 `~/Atrium/releases/predeploy-20261011-164054`（schema8、81条记录、836文件），热修前再取得 `predeploy-20261011-165314`（schema17、81条记录、852文件）。备份包含配置、TLS/凭据、程序、Brain 与 OpenClaw 接收和操作状态；均有清单且限制访问。首次迁移 schema8→17 后，原22张表全部行内容一致；最后核对时，原81条家庭记录逐行集合均保留，新增真实微信记录后共82条，integrity=ok、外键违规0。旧屏幕、来源、成员 allowlist 和已有权限保持，只为既有服务身份合并四项 archive 权限及家庭范围。

Weixin2.4.9/Discord2026.9.8 的固定补丁均已安装；最终 `--check` 为 verified=10、changed=0。实际渠道 probe 显示微信 configured/running、两个 Discord 账号 connected/running，均无 lastError。用户发送的新微信文字喂奶消息已被原生接收桥捕获并完成普通消息处理，新增家庭记录及健康档案投影，授权检索命中。这证明该文字消息链路；没有附件槽，不能据此宣称真实文件上传或 Discord 新消息通过。

OnePlus 正式 `io.atrium.tv` 已显示新版公开档案及该新增记录详情；实际 Core→MCP→Brain host→WebSocket→设备 DOM 回执为 applied。该投屏验证使用类型化 host 调用，未冒充自然语言模型触发。上线时复现宽范围分析超过32KiB响应上限导致整个档案页不可用，`3fb9d38` 增加精确的 `analysis_response_limit` 原因；普通列表仅对该原因保留已重新核验代次的资料并说明统计暂不可用，其他授权/代次冲突仍失效。指定日期范围的实际统计与设备查询页通过。最终 Web65文件/465测试、typecheck、store/httpapi及有界响应回归通过；此前组合版本全量 Go 通过。最终签名 native 包搬移后 PDF/PNG/JPEG/WebP 解析通过，故意缺 codec 返回不完整的 parser_unavailable。

外部 schema guard 的实际演练允许旧版本打开 schema8 备份，拒绝旧版本打开 schema17 活库；没有用旧程序直接打开升级后的资料。完整 NAS 档案快照/恢复、剩余 T15 模型用例、一小时持续验收、一万份资料性能及 T18 最终审计仍未完成。此次“本机已上线”不代表全部家庭 AgentOS 目标或 T15–T18 验收完成。

可提交的脱敏证据见 [部署摘要](evidence/2026-10-11-local-deployment/deployment-summary.json) 和 [验证边界](evidence/2026-10-11-local-deployment/README.md)。包含真实资料的截图、API结果与接收标识仅保留本机私有目录 `~/Atrium/releases/verification-20261011-3fb9d38` 及受保护临时证据，不提交仓库。

## 2026-10-11 开始部署时的记录（历史）

用户明确要求开始部署后，已创建受保护在线预部署快照 `~/Atrium/releases/predeploy-20261011-161230`：实际 schema 8、81 条家庭记录，共 33 文件约 114.7 MB；Core 与两个 Brain SQLite 各自使用在线一致备份，完整性和外键检查通过。包含现有程序、配置、TLS/凭据、插件与 launchd 文件；没有复制约 12 GB 的可重建预览缓存。此快照未跨库停机，正式切换前还需短暂停接入并保全最新操作状态；不将它称为最终灾备点。脱敏摘要见 `deployment-start.json`。

从固定 `078c167` 独立源码导出构建完整暂存包，先 Web build 再 embed，包含 Core、Brain/MCP、spool reader、插件与 worker 资源、native helper/codecs/许可。五个可执行文件使用既有本机证书签名并严格验证，codec 原始字节未重签。签名 native 包搬移到独立目录、净化环境后实际解析 PDF/PNG/JPEG/WebP，通过；故意缺失 codec 返回 `parser_unavailable` 且 complete=false。根自写首个探针把协议 `code` 误写为 `error_code`，更正探针后实际重跑通过，并未修改产品协议。证据见 `signed-staged-native.json`。

生产 Core 的单键模型凭据文件已准备于 `~/Atrium/service-env/atrium-core.env`（目录0700、文件0600），没有把密钥写入 plist/参数/日志。暂存配置 archives.enabled=false；生产配置和服务尚未切换。发布 guard/回填 CLI/保护环境文件入口在独立工作树实现；T15 两项关键审查修复仍须纳入最终组合包。NAS 完整档案恢复、一小时持续运行等未完成项不会因为开始部署而被标为通过。
