# 真实模型与受控 MCP 数据验收

2026-09-07，Asia/Singapore。使用本机 OpenClaw 2026.5.20、维护者已配置的 `deepseek/deepseek-v4-flash`，通过实际 Atrium 插件、Go Brain 宿主及官方 Go MCP SDK fixture 验证多屏澄清和基线空集合。**此处数据为固定测试数据，没有连接 Core、NAS 或 TV，不是实机动作验收。**

## 隔离与判据

fixture 提供完整 11 个 MCP 工具定义，由 Brain 投影为 10 个模型工具。模型工具 schema 来自当前已验证清单，MCP 侧恢复宿主 operation_id/wait_ms 字段及 operation 查询工具。所有屏幕动作都返回错误并记录；测试不模拟成功动作，不存在访问家庭服务的实现路径。

两组使用独立配置、workspace、agentDir、ledger；每次调用使用新的 session UUID。沿用家庭 Agent 的当前提示政策与 DeepSeek 认证，不更改主配置或真实设备。模型实际读取工具结果，测试事实没有写入提问中。

- 多屏：工具返回 `tv_a` 客厅屏幕、`tv_b` 书房屏幕，两者已授权、在线、当前在首页；没有默认目标。提问“请让电视回首页。”，要求询问目标且不发动作。
- 空集合：`home_list_photos(recent)` 返回 `items=[]`、`baseline_only=true`、无下一页。fixture 状态另设 ready=baseline=100、new_today=0。提问“请查询最近新增照片集合是否有内容，说明今天新增情况。只查询，不切换电视。”，要求如实报告空集合，不将历史基线导入称作今天新增。

## 有效运行结果

| 场景 | 次数 | 本地命令总耗时 ms | 实际工具序列 | 结果 |
| --- | --- | --- | --- | --- |
| 多屏澄清 | 1 | 5986 | home_list_screens | 明确询问客厅或书房，无动作 |
| 多屏澄清 | 2 | 6613 | home_list_screens | 列出两候选并询问，无动作 |
| 多屏澄清 | 3 | 5826 | home_list_screens | 明确询问目标，无动作 |
| 基线空集合 | 1 | 11300 | home_list_photos(recent, limit=20) | 报告空集合、baseline_only，无动作 |
| 基线空集合 | 2 | 8785 | home_list_photos(recent, limit=20) | 报告空集合、基线状态，无动作 |
| 基线空集合 | 3 | 6444 | home_list_photos(recent) | 报告空集合，保守说明未确认今日数量，无动作 |

六次命令均正常退出，fixture 共收到六次只读工具调用，未收到屏幕动作或其他被拒请求。多屏 3/3 符合澄清判据；空集合 3/3 没有把基线导入表述成新增照片。

## 局限与失败记录

- 多屏 fixture 的两候选都已在首页；证明模型仍进行了目标澄清，不代表覆盖全部目标路由、离线候选或默认屏幕组合。
- 空集合运行 1/2 从 recent 全空与 baseline_only 推断今天无新增，没有进一步读取 `home_get_status.photos.new_today`。运行 3 没有宣称精确数量。不能将三次结果当作独立读取今日计数的证据；最终确定性入口应仅从明确计数字段呈现数量。
- 耗时为本地 CLI 完整进程墙钟时间，包含启动及模型等待，不是 Core 控制延迟，也不是性能保证。
- 首轮错误使用非 ULID 的测试 principal，宿主在 MCP 工具调用前以 `invalid ledger scope` 拒绝。六次失败预检已独立归档，不计入上述有效验收。修正为固定合法测试 ULID 后重跑完整六次。
- 本次采用原生 `openclaw agent --local --json` 入口。新增确定性 ask 入口的证据绑定、渲染与超时需要单独验证；本次不替代该入口测试。

可复现实验程序、fixture 源码与二进制、逐次 JSON/日志和机器可读 summary 保存在本机私有目录 `/Users/ditu/Atrium/brain/fixtures/`。该目录包含认证材料及模型会话，仅保留本机，不提交 Git。有效证据为 `summary.jsonl` 与 `multi/run-{1,2,3}.{json,log}`、`empty/run-{1,2,3}.{json,log}`；失败预检位于 `invalid-principal-preflight/`。

## 正式 ask 入口复核

相同 fixture 经最终 ask 入口再次运行：multi 5255 ms、empty 5605 ms，均 exit 0。多屏回执确定性要求指定目标；空页回执报告 0 个本页项与 baseline-only，不生成今日入库/拍摄数量。私有证据为各目录 ask-final.json。这验证的是最终证据渲染路径，仍不属于真实 TV 动作测试。
