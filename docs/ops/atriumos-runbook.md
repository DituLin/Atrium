# AtriumOS 发布与恢复

状态：发布准备中，尚未部署。本文记录发布要求和已核实的脚本缺口，不表示 T17 已验收。

## 发布包

同一固定提交必须提供 Core、维护 CLI、Brain host、MCP、OpenClaw 适配器、匹配渠道版本的补丁、Web 资源，以及 macOS 原生解析 helper、固定版本 codec 依赖和许可。T13 新增的 `atrium-spool-read`、附件接入模块、补丁脚本与版本/文件摘要 pins 也属于运行依赖，不能留在开发工作区中引用。记录提交、各文件摘要、渠道版本和签名验证结果。

2026-10-11 代码检查：现有 `make release` 构建 Core/MCP/Brain 并执行 `web-sync`，但不先构建 Web，也没有打包渠道补丁和原生 helper。因此不能直接把这个目标的输出视为完整发布包。T17 需补齐构建和打包，验证脱离开发目录及 Codex runtime 后的实际运行。

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

待填写实际发布提交、包摘要、备份位置、迁移核对、渠道/TV/NAS 结果、性能数据及回滚演练。当前没有上线完成结论。
