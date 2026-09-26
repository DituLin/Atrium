# AI Brain / home-mcp 运维与接入

2026-09-07。实现位于 `codex/ai-brain-mcp` 工作区；本机 Core 已完成受控升级。用户已选择现有 OpenClaw + DeepSeek，专用 CLI 的真实只读状态查询已通过（6031 ms、3 次只读工具、无失败）；真实照片、首页与最近集合控制已获 OnePlus applied。早期无模型测试身份已撤销，当前使用单独的 Brain 服务身份。验收证据见 [结果记录](ai-brain-mcp-results.md)。

## 正式维护者入口

本机已安装的入口：

```sh
~/Atrium/brain/ask '家里中枢正常吗？'
~/Atrium/brain/ask '把电视回首页'
~/Atrium/brain/ask '在电视显示一张家庭照片'
```

从仓库直接运行（`--config` 必须为维护者提供的绝对路径）：

```sh
node adapters/openclaw/ask.mjs --config /Users/ditu/Atrium/brain/openclaw.json \
  --message '把电视回首页' --json
```

ask 每轮生成新的 session / invocation UUID，启动本地 OpenClaw，绝不传 `--deliver`。最终文字只由 Go 宿主验证并落盘的工具事实生成，丢弃模型自由撰写的最终摘要。整轮 30 秒到期或 Ctrl-C 时终止自己创建的进程组，最多再用 1 秒清理；不会自动重试写动作。终止不撤销已投递命令，缺失或部分回执需运行下方 `--recover` 查询原账本。

每次临时回执目录为 0700、文件为 0600，最多 8 行 / 单行 64 KiB / 总计 512 KiB，调用结束删除。`--json` 的 receipts 含范围内家庭元数据，若保存应写入私有目录。`elapsed_ms` 是整个入口耗时，包含 CLI 启动、模型与工具；不当作 TV 渲染耗时。OpenClaw 专用会话维护配置为 pruneAfter=1d / maxEntries=20；清理在运行时维护触发，**不是精确硬 TTL**。持久动作账本供恢复使用，不是家庭长期记忆。

原生 `openclaw agent --local --json` 仅用于调试，不提供最终措辞一致性保证；原生调试每次也应提供新的 `--session-id`。日常使用 ask。插件已复制到本机 `~/Atrium/brain/openclaw-plugin`，不依赖工作区持续存在。

## 构建与升级

```sh
make web
make check build-mcp build-brain-host
```

产物为 `bin/atrium`（含现有 TV Web UI）、`bin/home-mcp` 和 `bin/atrium-brain-host`。Go MCP SDK 固定 v1.7.0，要求 Go 1.25+。`make release release-mcp release-brain-host` 生成 macOS arm64/amd64 产物。

Core 新增 SQLite 0002 服务主体/凭据、0003 操作映射；启动时迁移到版本 3。升级前用运行中的旧版备份命令生成一致性快照，并保存旧二进制、配置；回滚必须同时恢复对应旧数据库。不要用新程序的初始化/备份流程在备份前打开生产数据库并隐式迁移。版本 1 → 3 保留现有屏幕凭据、序号与命令已有自动化测试。Mac 电源恢复、NAS 挂载方式仍不在本轮范围。

## 签发受限服务身份

先从既有管理员 CLI 查询真实 screen/source ID；示例中的 ID、配置路径与到期时间需由维护者替换。有效期必须在未来。密钥文件的父目录由维护者创建并保护，建议目录权限 0700。

```sh
bin/atrium admin --config /path/to/core-config.yaml integrations issue \
  --label home-brain \
  --permissions home.read,nas.read,photos.read,screens.read,screens.control,commands.read \
  --screens living_room_tv --sources family_photos \
  --expires-at 2026-12-01T00:00:00Z \
  --out /path/to/private/home-mcp.token

bin/atrium admin --config /path/to/core-config.yaml integrations list
```

签发仅将 token 写入新建 0600 文件；终端只打印主体元数据与文件位置。文件已存在或是符号链接时拒绝，不覆盖。服务器只保存凭据哈希。空权限/空来源/空屏幕列表均表示不授予对应访问。

当前 TV 集合是全局集合，navigate/refresh 要求主体覆盖所有活跃来源；只授权部分来源时允许展示范围内具体 photo_id，集合控制返回权限不足。不会让 TV 绕过授权显示其他来源。

```sh
bin/atrium admin --config /path/to/core-config.yaml integrations policy PRINCIPAL_ID \
  --permissions home.read,screens.read --screens living_room_tv
bin/atrium admin --config /path/to/core-config.yaml integrations rotate PRINCIPAL_ID \
  --out /path/to/private/home-mcp-next.token
bin/atrium admin --config /path/to/core-config.yaml integrations revoke PRINCIPAL_ID
```

policy 替换完整策略，省略的 allowlist 变为空。rotate 保留主体与操作归属，立即使旧凭据失效；默认继承主体到期时间，不延长主体寿命。切换 MCP 的 token_file 后重启其进程。撤销不会撤回已经送到 TV 的动作。

## home-mcp 配置与宿主

复制仓库 `home-mcp.example.yaml` 为独立私有配置，设置 `core_url`、`ca_file`、`token_file`、`request_timeout_ms`。Core URL 必须为 HTTPS origin；不接受工具参数指定任意主机，禁止重定向与环境代理，不忽略证书错误。CA 是公共证书，不能传服务器私钥。

```sh
bin/home-mcp --config /path/to/home-mcp.local.yaml
```

此命令供 MCP client 启动；标准输出仅协议，日志/启动错误写 stderr。它不是交互聊天入口。client 关闭 stdin 时正常退出，取消传到 Core HTTP，已接受命令仍可能执行。

通用 MCP host 配置示例（不是已应用的 OpenClaw 配置）：

```json
{
  "mcpServers": {
    "atrium-home": {
      "command": "/absolute/path/to/home-mcp",
      "args": ["--config", "/absolute/path/to/home-mcp.local.yaml"]
    }
  }
}
```

## 本机 OpenClaw 专用入口

用户已选择本机 OpenClaw 2026.5.20 与现有 `deepseek/deepseek-v4-flash`。专用配置是 `/Users/ditu/Atrium/brain/openclaw.json`，通过环境变量显式选择；主配置未保留 Atrium agent。该配置只启用 Atrium 插件及专用 agent，不修改原有 agent 的工具或记忆策略。部署插件从 `~/Atrium/brain/openclaw-plugin` 加载，实际模型清单已验证为恰好 10 个家庭工具。

```sh
OPENCLAW_CONFIG_PATH=/Users/ditu/Atrium/brain/openclaw.json \
  openclaw agent --local --agent atrium-home \
  --message '请查询家庭中枢、NAS 和电视的当前状态，不执行屏幕动作。' --json
```

不要加 `--deliver`；此入口在本地返回结果，不发送到聊天渠道。当前服务身份仅允许 `oneplus6t_tv` 与 `family_photos`，到期日为 **2026-10-07**。凭据轮换不得延长主体寿命；到期前须安排新身份签发和受控切换，旧主体未确认操作先完成查询，不复制新操作重发。

宿主参数由维护者配置，模型不能更改：

```sh
/Users/ditu/Atrium/brain/atrium-brain-host \
  --mcp-bin /Users/ditu/Atrium/brain/home-mcp \
  --mcp-config /Users/ditu/Atrium/brain/home-mcp.local.yaml \
  --ledger /Users/ditu/Atrium/brain/state/brain.db \
  --principal-id PRINCIPAL_ID
```

`PRINCIPAL_ID` 必须使用已签发主体的 ID，可通过管理员 `integrations list` 核对。同样的参数加 `--tools-json` 可生成模型工具数组，保存为 0600 的 `model-tools.json` 后供插件同步验证。宿主无 `--config` 参数；MCP 配置传给 `--mcp-config`。

活跃 runs 共享宿主，全部 run 结束及在途 RPC 清空后释放空闲进程。OpenClaw 多注册表共享按完整受信配置隔离的控制状态；run 和 call 身份不从模型参数推导。版本证据与适配说明见 [兼容性记录](openclaw-compatibility.md)。

## 操作结果与故障恢复

- 动作调用必须由 Brain 宿主生成并持久保存 canonical ULID operation_id；模型不得负责随机 ID 或自行换 ID 重试。同主体、同 ID、同参数只创建一条命令。
- 新 ID 时效为过去 24 小时至未来 5 分钟；已存在映射可在保留期内查询。映射及命令至少保留七天；清理后旧 ID 过期拒绝，不能重新执行。
- MCP 每次动作最多一次 POST；默认等待 3 秒，范围 0–5 秒。accepted 仅为待执行，applied 才是 TV 确认。
- POST 或后续轮询结果不确定时保留 operation_id（已知时保留 command ID），报告 outcome_unknown。由宿主先调用 home_get_operation，不能创建替代动作。Brain 重启只恢复查询，不自动重放排队动作。
- failed、expired、unknown 均为工具业务失败；断网不推断电视关机。当前路由不能证明历史命令执行成功。
- 工具并发最多 4；同 MCP 实例同屏最多一个动作，忙时明确返回。不同实例的重复动作仍依赖 Core 持久幂等与限流。
- 工具完整输出上限 32 KiB，含结构化与文本输出；超限显式报错。读取需缩小分页，不截断 JSON。当前读取默认 20、最多 50 条。

### 宿主重启后的原操作查询

先确认同一账本对应的活跃轮次已结束，再运行一次恢复页查询；不要并行启动另一轮屏幕控制：

```sh
/Users/ditu/Atrium/brain/atrium-brain-host \
  --mcp-bin /Users/ditu/Atrium/brain/home-mcp \
  --mcp-config /Users/ditu/Atrium/brain/home-mcp.local.yaml \
  --ledger /Users/ditu/Atrium/brain/state/brain.db \
  --principal-id PRINCIPAL_ID --recover
```

若返回下一页游标，用相同命令追加 `--recover-after OPERATION_CURSOR`。恢复只按账本中的原操作查询，不自动重发屏幕动作。当前模型清单不暴露 `home_get_operation`，该恢复能力属于宿主维护入口。

## 数据边界

只有 Core 受限 integration 路由可被调用：照片为元数据 ID，NAS 为健康摘要，没有原始路径、媒体 URL 或图片字节。TV 回报的 route、collection、error_code 采用白名单，其他字符串不透传；来源外照片 ID 被隐藏，Observed 不返回。

屏幕名称等授权文本仍是不可信数据，Brain 必须把它们当内容而非指令。家用 Agent 不启用通用 Shell、文件系统、浏览器或插件安装工具。现已使用用户选择的 DeepSeek 云模型，授权的家庭元数据与工具结果会进入模型请求；未发送图片字节或媒体地址。OpenClaw 可能保留会话及最终输出，私有目录与验收 JSON 不应公开；本接入未更改原有 agent 的记忆策略。普通家庭概览和现有 TV 功能继续独立于模型。

## 调用追踪

Brain 共用执行器为每个实际工具调用生成 `call_ref`，通过 MCP `_meta` 中的 `io.atrium/turn_ref`、`io.atrium/call_ref` 传递。home-mcp 转成 `X-Atrium-Turn-Ref`、`X-Atrium-Call-Ref` HTTP 头，Core 另生成自己的 `request_id`。动作还带持久 `operation_id`，Core 成功记录后关联 `command_id`。这些标识只用于排查，权限始终来自 integration 凭据和当前策略。

三层结构化日志可以按 turn/call 串联；MCP HTTP 日志关联 Core request，命令日志与审计关联 operation/command。宿主恢复使用账本中的原 turn，并生成新的 call。仅接收规范 ULID 追踪值；无合法 call 时 MCP 自行生成。日志只记录固定事件、稳定错误码、耗时、状态和标识，不记录完整工具参数、返回正文、凭据或原始错误原因。Core 的 integration 错误出口也遵循该限制。Brain/MCP 默认日志写 stderr，stdio stdout 保留给协议。

跨层追踪已有自动化测试及专用 OpenClaw + DeepSeek 的真实只读调用证据。私有 `status-shared.json` / `.log` 记录了 3 个只读工具的成功调用；照片控制与 TV 动作追踪仍以正在进行的独立验收为准。

## Brain 中文动作回执

Go 宿主已用原始 `brain.Action` 和 `Turn.Call` 返回的 result/error 调用 `brain.DescribeAction`，OpenClaw 插件原样附加 `TextZH` 及关联标识。该函数与账本共用命令证据校验；调用错误优先于伴随的响应。accepted 不是成功，applied 仅表述观测时刻的确认；过期、未知或缺乏可信结果有独立措辞。屏幕/照片 ID 来自原动作，不采用响应的任意名称或错误文本作为指令。自然语言意图由已选 DeepSeek 处理；确定性回执不替代尚在进行的真实 TV 动作验收。

当前家庭设备证据及回滚快照位置见 [实机记录](ai-brain-mcp-device-results-2026-09-06.md)。
