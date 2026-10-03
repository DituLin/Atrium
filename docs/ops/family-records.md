# 家庭记录与 NAS 归档

本功能在 `codex/openclaw-family-data` 分支开发中。当前已实现记录仓库、授权 HTTP API 和后台 JSON 归档。OpenClaw 工具已实现并通过实际 Core/MCP/Brain 测试；TV 图表已实现，IM 路由和完整实机验收尚未完成，不能把下面的 HTTP 验证当作完整业务验收。

## 数据与权限

家庭成员由管理员通过 `PUT /api/v1/admin/family/members/{id}` 建立，正文为 `{"name":"阿布"}`。ID 是稳定的字母数字、下划线或连字符，显示名字可以修改。

集成身份新增 `family.read` 和 `family.write` 权限，并要求 `member_ids` 白名单。空列表不授权任何成员。CLI 的 `admin integrations issue` 和 `policy` 支持 `--members abu,parent`；更新 policy 会替换完整策略，必须同时保留仍需要的屏幕、照片来源和权限。

- `GET /api/v1/integrations/family/members`：列出授权成员。
- `GET /api/v1/integrations/family/records?member_id=abu&limit=100`：查询授权成员的记录，包含非 TV 展示记录，按实际记录时间降序；继续传入响应中的 next_cursor 到 cursor 参数可查询更早历史。
- `POST /api/v1/integrations/family/records`：记录数据。身份从认证上下文取得，不接受正文中的 principal_id。
- `GET /api/v1/family/members`、`GET /api/v1/family/records?member_id=abu`：已配对 TV 的只读入口，只返回 `display_on_tv=true` 且未被更正、未撤回的有效记录。没有设置此字段的记录默认不展示。

写入正文示例（以下仅为合成测试值，不是阿布的真实数据）：

```json
{
  "request_id": "im-event-unique-id",
  "member_id": "abu",
  "category": "growth",
  "occurred_at": "2026-10-03T08:00:00+08:00",
  "title": "身高记录",
  "notes": "合成测试数据",
  "measurements": [{"metric":"height","value":70,"unit":"cm"}],
  "display_on_tv": true
}
```

类别包括 growth、health、checkup、meal、note。记录时间必须明确；不从入库时间推断实际测量时间。测量要求有限数字、明确单位、不重复 metric。饮食计划可以用 meal 类别、计划日期和正文记录；接口不会生成医疗判断。

同一身份重复提交相同 request_id 和相同规范化正文返回原记录。相同 ID 对应不同内容返回 409，不能用它静默覆盖旧记录。更正/撤回通过新记录关联原记录，不能修改原值，具体语义见下节。

## 更正与撤回

先查询确定成员和当前记录 ID，再使用同一个 POST / `home_record_family_entry`，提交完整记录以及 `supersedes_id`（被更正的当前 ID）和 `revision_reason`（用户说明的原因）。更正是一个新的 request_id / tool call，原记录的内容和 NAS 文件保持不变。新记录仍需明确 `display_on_tv`。

撤回另设 `retracted=true`、`display_on_tv=false`，其余内容保留原记录，并填写原因。撤回保留审计历史，不是删除个人数据。已撤回记录不能再被更正；确有新的观察应建立独立记录。更正不能跨成员，过时的已更正目标返回冲突，需要重新查询，不自动把用户修改套到另一版。

集成查询返回完整历史，每条旧记录的 `superseded_by` 指向直接后续版本；`supersedes_id` 表示本条修改的目标。只有没有 `superseded_by` 且 `retracted` 不为 true 的记录才是有效观察，不能把旧值和撤回值计为额外测量。TV 在服务器端过滤旧版与撤回版；更正为私密记录时，旧版也不再返回给 TV。屏幕按现有刷新机制更新，历史翻页状态需手动刷新。

每个更正/撤回也独立进入归档队列。普通记录保持 NAS schema_version=1，更正/撤回使用 schema_version=2；动态 `superseded_by` 不写进不可变文件。恢复先读取所有记录，再验证和重建修改链，所以支持文件顺序与修改顺序不同；缺目标、跨成员、分叉和循环都会拒绝发布恢复库。旧维护工具不能恢复 schema 2，部署时必须一起更新 Core、MCP、Brain host、插件、工具 schema 和维护工具。

## NAS 配置

SQLite 保留在本机 `storage.data_dir`。照片来源仍保持只读。另建专门的可写归档目录，例如 `/Volumes/home/AtriumFamily`，不能与照片根目录或本机数据目录重叠。

配置示例：

```yaml
family_archive:
  enabled: true
  dir: /Volumes/home/AtriumFamily
  identity: your-unique-archive-id
  allow_local: false
```

部署者必须在该目录建立 `.atrium-family-archive` 文件，内容为配置中的 identity。后台不会自动创建根目录或身份标记，避免 NAS 掉线后在本机冒充成功。生产环境保持 allow_local=false，只有测试目录可以显式开启。

后台每 15 秒尝试处理最多 50 条待归档记录。目录结构为：

```text
AtriumFamily/
  .atrium-family-archive
  abu/2026/10/<record-id>.json
```

年份/月按记录时间的 UTC 日期归档；文件内保留完整时间，TV 按家庭时区显示。JSON 包含 schema_version 和 record，member_id 为稳定关联。每个文件用临时文件写入、Sync、Rename、读回校验，然后确认本机同步状态。目录和文件请求权限分别为 0700 和 0600，NAS 共享 ACL 仍由 NAS 自身控制。

写入正文与待归档队列在同一 SQLite 事务中提交。NAS 断线时记录保留本地，nas_status 为 pending；只有文件写入/校验并确认后为 synced。崩溃后发现同名同内容文件会补做确认，不会重复生成记录；同名异内容报冲突并保留待处理状态，不覆盖既有文件。

SMB I/O 最多占一个归档工作槽，调用等待上限 20 秒。无法取消的内核 I/O 会继续占槽，避免无限堆积；不阻塞 HTTP 录入。NAS 修复后自动重试；如果系统的 SMB 调用永久卡住，需要恢复挂载/服务。API 不返回 NAS 路径或凭据。

## 当前验证与剩余事项

已有测试覆盖 API 权限、成员范围、私密记录投影、重复消息、冲突正文、重启后持久数据、并发重放、NAS 身份错误、意外本地盘、损坏档案和崩溃重试。完整 IM → OpenClaw → Atrium → NAS → TV 验证未完成。

基础模型写入、NAS 实际导出、配对浏览器及 OnePlus 显示已验证；NAS 恢复和持久 IM 去重已有测试。更正/撤回已部署，隔离真实模型、NAS 和配对浏览器验收均通过。实际 IM 收发和用户 TV 硬件仍未验证。不要把测试示例导入家庭真实记录。

### TV family page

The `家人` home card/top navigation opens `family`. The screen reads only TV-visible records, separates trend lines by metric and unit, and shows archive pending/synced state. Browsing earlier pages pauses automatic refresh until the user selects Refresh; new-record view polls every 15 seconds. Member changes clear the prior record view. OpenClaw can request `home_navigate_screen` with `route: "family"` and no collection, under the existing screen-control permissions. Production Android test-device acceptance is recorded below; user television hardware remains unverified.

### 本机部署进度（2026-10-03）

已将签名的新 Core/Web 部署到 `~/Atrium/atrium`，服务和 HTTPS 检查通过。生产库已完成家庭表迁移，已建立成员 abu（阿布），现有 OpenClaw principal 增加 family.read/family.write 和 abu 白名单。生产 family_records 当前为 0，未录入合成个人数据。

NAS `/Volumes/home/AtriumFamily` 已建立身份标记，生产配置启用归档并禁止本地盘替代。正式成员尚没有真实记录。另建隔离库和 NAS 测试目录后，真实 DeepSeek 写入的合成记录已完成 NAS 读回校验及配对浏览器图表展示；未把合成数据放入生产成员。回滚备份在 `~/Atrium/rollback-family-20261003`，含原程序、配置及 SQLite 一致性备份（integrity_check=ok）。回退旧表结构须使用备份，不要直接让旧版本写新库。

独立测试配置位于 `~/Atrium/family-staging`：真实 DeepSeek 状态查询和成员/记录查询均取得完整回执，failed=false。日常 `~/Atrium/brain/ask` 已更新为十三项工具，并显式使用独立状态目录；IM Gateway 的现有 main agent 已加载家庭工具，微信和 Discord 沿用现有私聊权限。新建了有效至 2027-10-03 的接入身份供维护者入口和网关使用，旧身份保留供已有账本恢复，按原到期时间失效。

### 完整工具链验收（2026-10-03）

隔离环境 `~/Atrium/family-acceptance` 运行同一签名 Core，NAS 使用 `/Volumes/home/AtriumFamilyAcceptance-20261003`。真实 DeepSeek 调用成员查询后创建一条明确标注合成的成长记录（70 cm、8 kg），回执 failed=false。NAS 归档内容一致；配对 Chrome 通过真实 API 取得记录并显示“已同步到 NAS”，远程 family 导航状态 applied，1920×1080 / 804×384 截图已留存。该测试不代表已验证实际 IM 入站。

生产 Android 真机原来保留旧页面，首次 family 导航失败 invalid_route；重新启动应用加载新版 Web 后，命令 applied 并显示阿布的空记录页面。OpenClaw main agent 通过网关执行成员查询成功；模型发出的 family 导航命令 `01M40KHSWXN9Q60YJ58PNRYD3T` 最终在 Core 查得 applied。它最初只报告 accepted，已补充助手指令要求自动查原命令回执，不重发动作。

微信插件从 2.4.3 更新为 2.4.9，消除了旧 plugin-sdk/channel-runtime 导入错误。状态为 running=true，但 lifecycle=starting；Discord 两个账号 connected=true，主动 HTTP probe 出现 ECONNRESET。因此暂不声称微信/Discord 实际收发验收完成，需真实私聊验证。已请求维护者发送只读查询；本任务没有向任何 IM 联系人主动发消息。

待完成：实际 IM 入站与回复验证。OnePlus 测试机不等同于用户电视，未验证长期运行或厂商遥控差异。

## 从 NAS 恢复家庭记录

`atrium family restore` 校验整个归档后创建一个**全新的本机 SQLite 文件**。不覆盖任何现有输出，也不修改 NAS。已安装的维护工具为 `/Users/ditu/Atrium/atrium-maintenance`，源码构建产物为 `bin/atrium`。这只恢复家庭记录；不是整个 Core 的备份恢复，不包含照片索引、屏幕配对、凭据或原 IM 请求幂等身份。

准备成员映射 JSON，例如 `[{"id":"abu","name":"阿布"}]`，ID 必须与归档一致。成员姓名不会根据文件名猜测。输出必须位于归档之外的本机磁盘，文件尚不存在。identity 使用源目录 `.atrium-family-archive` 的已知标识。

```sh
~/Atrium/atrium-maintenance family restore \
  --archive /Volumes/home/AtriumFamily \
  --identity '<已核实的归档标识>' \
  --members-file /absolute/path/members.json \
  --out /absolute/local/path/recovered-family.db
```

所有 JSON 记录必须具有支持的 schema、合法记录字段、匹配的成员/UTC年月/记录ID路径，并且成员在映射中。未知字段、额外 JSON、符号链接、损坏文件和身份不符均会中止；只在完整校验和 SQLite checkpoint 后发布输出。源记录保持 ID、实际记录时间、创建时间、值、备注及电视可见性；同步时间表示本次验证 NAS 归档的时间。限制为最多 100000 条记录、每个文件 128 KiB。`--allow-local` 仅供本地测试源，默认要求网络卷。

恢复出的数据库用于独立检验或重建实例。不要直接替换正在运行的家庭库，也不要混入现有库；需要合并时应另做带冲突审查的迁移。它不恢复历史 IM 幂等身份，不能用重放旧消息的方式补数据。

实测：隔离 NAS 合成记录恢复为 `~/Atrium/family-acceptance/recovered-family.db`，记录数 1、已验证归档数 1、integrity_check=ok；重复输出被拒绝。生产库未被替换。

### IM 重复投递

已在 OpenClaw `before_dispatch` 使用可信原始消息身份做持久接收去重，跨进程重启和并发测试通过。首次消息身份先落宿主私有账本，然后才允许模型处理；重投不会重写。首次处理崩溃时也不自动重试，只提示用新的查询确认。正文相同但 ID 不同的新消息不被视为重复。该账本不可删除或与 NAS-only 恢复混同。实际 IM 入站尚需用户只读查询确认；不能把合成 hook 测试写成真实消息验收。


## 部署与验收核对（2026-10-03 18:52，Asia/Singapore）

新 Core、MCP、Brain host、插件、十三项工具 schema 和维护工具已同步部署。正式库 schema=8、integrity_check=ok、family_records=0，HTTPS 返回 200；安装文件与验证构建哈希一致。备份位于 `~/Atrium/rollback-family-20261003/revisions`，包含更新前二进制、工具文件、助手指令及 SQLite 一致性备份。插件 generation 7 重载时报告旧 provider 资源清理超时，但随后 main 网关实际 DeepSeek 查询 status=ok、工具 failures=0；Discord 两账号保持 ready/connected。微信仍为 running/starting，没有把它记作收发成功。

| 原目标/计划项 | 当前证据与边界 |
| --- | --- |
| 新建分支 | `codex/openclaw-family-data` |
| 最新 stable OpenClaw | 安装 2026.9.8；当次 `npm view openclaw dist-tags.latest` 同为 2026.9.8 |
| 使用 DeepSeek | 安装后的 main 网关查询 provider=deepseek、model=deepseek-v4-flash，status=ok |
| 双向记录与读取 | 隔离真实模型创建成长、健康、体检、饮食记录，均取得完整回执并查回；正式库只读查询成功 |
| 连续历史、更正、撤回 | 真实模型 70→71 cm 更正及撤回通过；旧值保留，TV 排除旧版与撤回版 |
| NAS 结构化归档与恢复 | 六个合成历史文件实际写入 SMB；三条成长修改链恢复后 integrity=ok、链关系正确、TV 有效记录为零 |
| TV 分类、图表、既有风格 | 配对浏览器无 API mock：成长更正/撤回刷新、心率图、体检事项及饮食计划，分类键盘操作及无页面错误；1920 截图已检查。此前 804 分辨率也通过 |
| Android TV 容器部署 | 已连接 OnePlus 测试设备 sequence=303 family 命令 applied，实屏显示阿布空记录；未接用户电视，不声称厂商遥控或长期运行验证 |
| 权限与重复投递 | Go 全量测试、store/archive/HTTP race、49 项插件测试、go vet、Web/Go 构建通过；同成员权限、私密投影、持久入站去重和实际安装 mapper 检查通过 |
| 真实 IM 输入/回复 | **未验证，阻塞完整验收**。实际频道无入站消息；CLI、框架合成上下文不替代此项。需要用户向现有机器人发“查询阿布已有的家庭记录”，确认回复并核对入站轨迹 |

验收文件保存在私有 `~/Atrium/family-acceptance` 与 `~/Atrium/family-staging`，未提交私人配置或凭据。`model-correction.json`、`model-retraction.json`、`model-categories.json` 是真实模型回执；`revision-browser.log`、`categories-browser.log` 和相应 PNG 是实际配对页面证据。隔离 Core 已停止。为完成测试清理，NAS 上的 `AtriumFamilyAcceptance-20261003` 已移除，七个文件先逐一验证哈希并转存本机 `family-acceptance/nas-evidence`；正式归档 `/Volumes/home/AtriumFamily` 未受影响。旧验收配置仅保留为证据，重跑需重新建立隔离根目录，不能误用正式成员。

正式入口当前仅授权已建立的阿布；其他家庭成员应先按“数据与权限”说明创建稳定 ID 并加入接入身份白名单。没有猜测家人姓名、关系或录入任何真实健康数值。
