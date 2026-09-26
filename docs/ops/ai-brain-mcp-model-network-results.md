# 模型网络失败隔离验收

2026-09-07。本次验证对**模型 HTTP 端点**实施故障注入，证明模型请求失败时不会触发家庭动作，且 Core 仍可读取。没有切断 Mac、NAS 或 TV 的物理网络，没有关闭真实 DeepSeek 服务，也没有更改 TV。

## 故障注入

复制专用 OpenClaw 配置到私有 `/Users/ditu/Atrium/brain/network-failure.json`。只在副本中加入 provider `atrium-unreachable`，采用 `openai-completions` 协议和无效测试 apiKey，baseUrl 指向已关闭的本机临时端口 `http://127.0.0.1:57954/v1`。创建临时端口后关闭监听；连接探测得到 macOS errno 61（连接被拒绝），实际模型 transport 日志也明确记录 `ECONNREFUSED`。

agent 与默认模型均指定 `atrium-unreachable/unreachable-model`，fallbacks 均为空。副本仅允许 3 个只读家庭工具。新建独立 agentDir、workspace、session store 和 Brain 账本，均在 `/Users/ditu/Atrium/brain/network-failure/` 下，避免写入原 agent 的 models.json 或复用原动作账本。未使用 `--deliver`。

最终命令使用新的 session UUID，避免默认 session 重用已关闭 run 的墓碑影响验证：

```sh
OPENCLAW_CONFIG_PATH=/Users/ditu/Atrium/brain/network-failure.json \
  openclaw agent --local --agent atrium-network-failure \
  --session-id NEW_UUID \
  --message '请只查询家庭中枢当前状态，不执行任何屏幕动作。' \
  --json --timeout 35
```

外层测试进程另设 50 秒硬上限；本次未触及。准备阶段曾使用端口 1，因 Node fetch 可能将其视为禁止端口而弃用该结果；最终结论只采用关闭的临时端口和新的 session 验证。

## 结果

| 检查 | 最终证据 |
| --- | --- |
| 模型请求 | provider transport 记录 `ECONNREFUSED`，CLI 报告模型网络连接错误 |
| CLI | exit 1，4.553 秒退出，未触发硬超时，stdout 无成功结果 |
| 模型 fallback | 配置为空；失败记录为指定测试 provider，无下一候选 |
| 工具执行 | 当前运行日志的 before_tool 与 execute 均为 0 次 |
| 动作账本 | 独立 `brain_actions` 为 0 条；最终新 session 已建立带 turn_id 的 run 认领，排除旧 run 拒绝导致的假阳性 |
| Core 可用性 | 模型失败后，用真实私有 CA 和 integration 凭据执行 HTTPS 只读请求，HTTP 200，136 ms，`availability=available`、`core=reachable`，返回 1 个授权屏幕 |
| Core 观察时间 | 2026-09-07 09:44:44 +08:00 |
| 原配置 | 原 `/Users/ditu/Atrium/brain/openclaw.json` 的前后 SHA-256 相同 |

因此，本次真实 OpenClaw 模型网络请求失败得到明确错误，没有执行家庭工具或创建屏幕动作，同时 Core 继续响应只读请求。此结论不证明断电、路由器故障或物理断网恢复，也不替代遥控器及真实 TV 显示验收。

## 私有证据

私有目录保留 `setup-evidence.json`、`process-evidence.json`、`outcome-evidence.json`、`core-read-evidence.json`、`model-run.log` 与隔离账本。准备阶段结果采用 `initial-` / `pre-fresh-` 前缀保留，避免与最终记录混淆。

原 DeepSeek 配置、原 agent 文件及原动作账本未被本测试改写。失败配置仅用于维护者复现实验，不是日常入口；它使用 dummy key，不应替换正常专用配置。原始 OpenClaw 日志可能包含本地会话标识，完整证据保持私有，不复制到仓库。
