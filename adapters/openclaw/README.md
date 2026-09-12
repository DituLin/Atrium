# Atrium 的 OpenClaw 工具插件

插件 ID 为 `atrium-home`。它把专用家庭 agent 的十个模型工具接到本地 Go Brain JSONL 宿主。宿主负责持久操作 ID、授权、调用预算、MCP、恢复与确定性结果；插件不注册 Shell、文件、网络浏览或凭据工具。

已按本机 OpenClaw 2026.5.20 的 `docs/plugins/building-plugins.md`、`docs/plugins/hooks.md` 及已安装 SDK 类型编写。Node 要求 22.19 或更新。源码是可直接加载的 ESM JavaScript，无额外 npm 运行依赖。

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

## 安装前准备

先构建 Go Brain 宿主，以其 `--tools-json` 输出生成专用的 `tools.json`。文件必须是 `brain.ModelTools` 的 JSON 数组，包含恰好十个白名单工具；不得包含 `operation_id`、`wait_ms` 或 `home_get_operation`。文件使用 `0600` 权限，插件拒绝软链接、组/其他用户可访问文件和超过 256 KiB 的清单。

插件配置只接受本机维护者提供的以下字段：

```json
{
  "agentId": "atrium-home",
  "hostBinary": "/absolute/path/to/atrium-brain-host",
  "hostArgs": ["--mcp-bin", "/absolute/path/to/home-mcp", "--mcp-config", "/absolute/path/to/private/home-mcp.yaml", "--ledger", "/absolute/path/to/private/brain-ledger.db", "--principal-id", "YOUR_INTEGRATION_PRINCIPAL_ID"],
  "toolsFile": "/absolute/path/to/private/tools.json"
}
```

所有路径都由本机配置提供，工具参数无法修改它们。`hostArgs` 通过 `spawn` 数组传递，`shell: false`。不要把密钥作为命令行参数。

维护者可通过 `openclaw plugins install /absolute/path/to/adapters/openclaw` 安装；本目录的生成过程不会安装插件或修改 OpenClaw 配置。为 `plugins.entries.atrium-home` 设置上面的 `config`，并设置 `hooks.allowConversationAccess: true`，使 `before_agent_start`、`agent_end` 生命周期钩子可用。插件只读取身份字段，不读取或保留 prompt/messages。

仅在专用 agent 的工具允许列表中加入本插件的十个工具；同时排除该 agent 的内置通用工具及其他插件工具。仅启用插件无法移除 OpenClaw 已有的工具。使用已配置的 DeepSeek 模型作为该 agent 的模型，不把工具宿主当模型提供方。

## 生命周期与协议

`before_agent_start` 必须提供明确且一致的 `event.runId` / `ctx.runId`，并有受信 agentId 和 sessionId（缺少 sessionId 时使用 sessionKey）。插件立即发送 `start`，从此开始本地 30 秒期限。`before_tool_call` 绑定受信 toolCallId 与当前 run、工具名；`execute` 再次检查绑定。因此 hook 抛错或被 OpenClaw 跳过时，工具仍然拒绝执行。

JSONL 请求每行一个对象，带独立 RPC `id`：

- `start`: `run_id`
- `call`: `run_id`, `call_id`, `tool`, `arguments`
- `end` / `cancel`: `run_id`
- `tools`: 不带模型参数

wire `run_id` 是 `[agentId, sessionId或sessionKey, runId]` 的 JSON 数组 SHA-256 小写 hex。wire `call_id` 在该数组末尾追加 toolCallId 后计算。它们不从模型参数生成；操作 ULID 始终由 Go 宿主分配。

响应为 `{id,result}` 或 `{id,error:{code,operation_id?,command_id?}}`。call 的 result 为 `{tool_result,report?}`，其中 report 使用 Go 字段 `TextZH`。插件返回 MCP 文本 content，并附加确定性的 `report.TextZH`。不把私有宿主配置塞进模型结果。

活跃 runs 共享一个宿主进程；stdout 只用于 JSONL，stderr 继承宿主的安全结构化日志。所有 run（包括仍在启动的 run）均已终结、结束 RPC 已确认且在途 RPC 清空后，插件释放空闲宿主，使本地 CLI 能自然退出。后续新 run 可启动新宿主；Go 持久账本保留旧 run 墓碑。崩溃会关闭所有在途 run，只有新的 run 能重启进程。旧 run 的拒绝状态与 toolCallId 归属保留，跨 run 复用的 toolCallId 会变为歧义且拒绝执行；Go 的持久认领跨重启继续阻止重放。

即使 `agent_end` 比 `before_agent_start` 先到，插件也立即标记该 run 已关闭并发送 Go `end` 持久化 tombstone。此清理允许启动宿主以保存关闭记录，但不会执行屏幕动作。若持久关闭失败或未明确返回 `closed: true`，整个插件生命周期停止接受工具和新 run。

工具 abort 或 RPC 超时发送 `cancel`，关闭整轮，不自动重发动作。`agent_end` 仅作辅助清理；JS 30 秒与 Go 30 秒限制独立生效。取消已接受动作不表示 TV 动作被撤销；须按 Go 持久账本核实结果。

## 验证

```sh
node --test adapters/openclaw/*.test.mjs
```

测试使用 fake OpenClaw API 和 fake JSONL 子进程，覆盖工具清单、agent/session 隔离、缺失 hook、身份参数注入、失败 start、迟到 start、崩溃、新 run、agent_end、超时和 abort。真实 Go/DeepSeek/TV 验收见仓库 docs/ops/ai-brain-mcp-model-results-2026-09-07.md。

## 固定容量与恢复

每个插件生命周期最多保留 **4096 个 run**、**32768 个 toolCallId 归属记录**。记录不按 TTL 删除，也不通过淘汰旧 run 来重复使用身份。JSONL 宿主最多接受 **64 个在途 RPC**，包括 start、call、end 等事件请求；这与 Go 内部的工具并发限制分别生效。

达到任一容量后，插件拒绝新的家庭工作、关闭宿主，并输出固定的 `atrium_capacity_exceeded` 日志；需要重启插件生命周期后才能继续。未知 run 的 end 触及容量时，会先尝试保存 Go tombstone 再关闭宿主。重启不替代动作结果核实：已接受动作仍须检查 Go 持久账本，不能重发一个新操作来猜测恢复结果。

## OpenClaw 多注册表兼容

本机 OpenClaw 会分别构建 hook 注册表与工具 factory 注册表。因此同一进程内，插件按完整受信配置（agentId、hostBinary、hostArgs、toolsFile）和已验证工具清单的指纹共享控制状态。共享通过带版本的 `globalThis` Symbol 实现；不同 agent、账本启动参数、配置或工具清单不会共享。

每次注册保留独立引用，重复 stop 幂等；旧注册表停止不会关闭仍被新注册表使用的宿主，已停止注册表的旧工具闭包也不能继续执行。最后一个引用停止才关闭控制状态。每个进程最多 32 个不同控制状态、每个状态最多 128 个注册引用；触顶拒绝继续注册并要求重启。测试依赖注入默认独立，仅显式传共享 Map 的测试会共用状态。
