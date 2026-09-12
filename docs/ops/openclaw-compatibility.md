# OpenClaw 接线与兼容性记录

2026-09-07。已按用户选择接入本机 OpenClaw 2026.5.20，使用用户现有的 `deepseek/deepseek-v4-flash`。专用本地 CLI 的真实状态查询已通过：`status-shared` 运行耗时 6031 ms，调用 3 个只读家庭工具，无工具失败；实际模型工具清单恰好 10 项。该耗时是一次验收记录，不是响应时间承诺。后续真实照片、首页和最近集合均已取得 OnePlus applied；正式输出改用 ask 的宿主事实回执，原生摘要不用于判断成功。

## 当前接线

```text
OpenClaw 专用 atrium-home agent → Atrium 插件 → Go Brain JSONL 宿主
                                          → home-mcp → Core → TV
```

- 专用配置：`/Users/ditu/Atrium/brain/openclaw.json`，以 `OPENCLAW_CONFIG_PATH` 指定；主 OpenClaw 配置未保留 Atrium agent。
- 插件 ID：`atrium-home`。专用配置从私有部署目录 `~/Atrium/brain/openclaw-plugin` 加载插件；这不是全局替换用户既有插件或记忆策略。
- 私有运行文件位于 `/Users/ditu/Atrium/brain`，包含宿主、MCP 配置、0600 凭据文件、工具清单和持久账本。服务身份只允许 `oneplus6t_tv` 与 `family_photos`，到期日为 **2026-10-07**。
- 专用 agent 只允许插件的 10 个模型工具，不包含 Shell、文件、浏览器、插件安装或 `home_get_operation`。operation_id、wait_ms 和恢复查询由 Go 宿主控制。
- 通过专用 CLI 使用现有 DeepSeek 认证；插件不读取或保存模型密钥。模型获得授权后的家庭元数据与工具结果，不能因此视为全离线运行。

```sh
~/Atrium/brain/ask '请查询家庭中枢、NAS 和电视的当前状态'
```

正式入口和会话/回执保留策略见 [运维文档](ai-brain-mcp-runbook.md)。原生 CLI 仅调试；ask 从私有回执生成最终文字，并在 30 秒整轮期限后取消自己的进程组。

## 本机版本的实际兼容点

本机 CLI 的 `mcp set/list/show/unset` 管理出站注册；`mcp serve` 把 OpenClaw 聊天渠道作为 MCP 服务暴露，并非本次 Atrium 接线入口。当前使用受限插件和 Go 宿主，不直接把 MCP 的全部工具交给模型。

本机 `before_agent_start` 与 `before_tool_call` 的 event/ctx 可提供 runId，后者还提供 toolCallId。插件只接受受信 agent/session/run/call 上下文，缺少或冲突时拒绝；execute 会再次校验映射，不能依赖 hook 总会运行。

实际模型运行发现：OpenClaw 的 hook 和工具 factory 来自不同注册实例，session 相同，但实例内状态不能互通。插件已改为在同进程按完整受信配置及已验证清单指纹共享控制状态；不同 agent、ledger 参数、配置和清单仍隔离。重复 service.stop 幂等，旧注册表停止不能关闭新注册表的活跃状态。跨独立模块与真实 JSONL 子进程的回归测试已覆盖。

活跃 runs 共享宿主；全部 run（包括仍启动的 run）终结、结束请求确认且在途 RPC 清空后释放空闲宿主，使 `agent --local` 能自然退出。Go 持久账本跨进程保留旧 run 墓碑，新进程不能重放旧 run。

## 证据与边界

私有证据为 `/Users/ditu/Atrium/brain/status-shared.json` 与同名 `.log`。插件只记录阶段、标识哈希及布尔状态；Brain/MCP/Core 日志记录安全追踪标识，不打印凭据或完整参数。OpenClaw 自身仍可能保留会话与最终 JSON 输出，需按家庭数据保护要求保管，不应公开整个私有目录。

完整接入命令、凭据轮换和原操作恢复见 [运维说明](ai-brain-mcp-runbook.md)。照片预览、集合切换及 TV 命令的真实模型闭环以单独动作验收为准，当前只读成功不代表这些动作已完成。
