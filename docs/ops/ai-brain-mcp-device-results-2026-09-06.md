# AI Brain 共用执行器 / home-mcp 实机验证

验证时间：2026-09-06 23:49–23:54，Asia/Singapore。结论：真实 Mac mini Core、群晖照片来源、OnePlus 6T 的 MCP 控制链路通过；尚未配置或调用家庭 Brain 的模型提供方，不是自然语言 AI 全流程验收。

## 升级与恢复准备

- 保留旧运行程序、配置、SQLite online backup 一致性快照于本机 `~/Atrium/rollback-mcp-20260906-234911/`，停服务后再次刷新快照；`integrity_check=ok`。目录权限 0700，另有 SHA-256 清单。
- 停止现有 launchd Core，原子替换程序后用原 plist 恢复服务；没有更改开机/断电策略、NAS 挂载、TLS、Android APK 或屏幕配对。
- 新程序构建时间 `2026-09-06T15:47:42Z`，版本 `c459876-dirty+c459876`，SHA-256 `dc7397aed2e5d94bcb2c414b333f891634e3ac67380df2baca92f7c3dd9aeb62`。这是工作区构建，不是 Git 发布版本。
- 实际 `schema_migrations` 已包含 0001、0002、0003；升级后数据库完整性检查为 ok。不要用 `PRAGMA user_version` 判断该项目迁移版本，它保持 0。
- OnePlus 自动重连，原有三个屏幕注册仍在；唯一在线测试目标为 `oneplus6t_tv`。NAS 来源仍 online，已有 17,166 张 ready 照片、2 个 pending、4 个 unsupported。

## 真实工具与执行

使用官方 Go MCP client，通过 stdio 启动本工作区 `bin/home-mcp`；Brain 的真实 `Executor`、独立持久账本和 `DescribeAction` 回执参与执行。测试宿主是本地固定场景程序，无意图解析或模型替身。临时 service principal 只允许一个真实屏幕、一个真实来源及六项工具权限。

1. initialize / tools/list：11 个 MCP 工具，经过模型工具投影为 10 个；host-only operation 查询不暴露。
2. `home_get_status`、`home_get_nas_status`、`home_list_screens`：通过实际 HTTPS 与凭据，返回 Core reachable、NAS online、目标屏幕 online；没有在调用参数中传递 NAS 路径或媒体 URL。
3. `home_list_photos(recent, limit=1)` 取得真实 photo ID，`home_show_photo` 显示该照片。第一次为 accepted；同一个宿主 call 随后查询原 operation 得到 applied，总耗时 1,014 ms（含 1 秒轮询间隔，并非精确渲染时延）。ADB 设备截图已人工检查，照片实际显示。
4. 新一轮 `home_navigate_screen(dashboard)` 恢复首页，同样 accepted → 原 operation 查询 → applied，观测耗时 1,014 ms；设备截图确认首页。
5. Core 数据库只出现这两条 integration operation，每条绑定一个 command；没有为查询重复新增命令。

| 动作 | operation_id | command_id | 最终状态 |
| --- | --- | --- | --- |
| 展示照片 | 01M1VPNM27XN7C7J7RWYR163FV | 01M1VPNM2A4KVMH7GWW34K8CSY | applied |
| 恢复首页 | 01M1VPPB9XW75Z227N6XWMNG2S | 01M1VPPBA1BJS2SPS7HF28RABQ | applied |

中文回执按观测时间分别输出“命令已接受，尚未收到屏幕执行确认”和“屏幕已确认展示照片/切换到首页”。日志中的每个 operation 对应同一个 turn；首次命令与后续查询有独立 call 关联。

## 隔离与清理

- MCP client 关闭后，进程清单中没有剩余 home-mcp 子进程。
- 撤销临时 principal，管理接口确认 enabled=false；使用原 token 重连后工具返回 permission_denied，测试宿主按失败退出。
- MCP 停止且身份撤销后，原管理员 CLI refresh 仍得到 applied（503 ms 含轮询，command `01M1VPQPX984C7AZ290QKDEKPW`）。
- 同样在 MCP 停止后，ADB 注入实际 Android 遥控按键：右键进入照片列表，确认键进入照片预览，返回两次回首页；各阶段截图已检查。页面、NAS 与实时会话继续工作。
- 最终 OnePlus 在线并位于首页。所有测试配置、日志、账本、截图与机器可读 evidence.json 留在本机私有 `~/Atrium/mcp-validation-20260906/`；家庭截图没有复制进 Git。临时凭据已撤销，不能继续用于工具调用。

## 尚未证明

模型运行时工具白名单与接线、真实自然语言意图/澄清、模型中文回答与回执一致性、模型断连场景，均待模型路线及提供方确定后验证。真实设备长稳及完整 M5 故障矩阵也不能由上述短测替代。此证据补齐了 MCP 到真实设备的一段，完整目标保持未完成。
