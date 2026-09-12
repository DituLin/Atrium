# OpenClaw / DeepSeek / OnePlus 实接记录

日期：2026-09-07，Asia/Singapore。运行时 OpenClaw 2026.5.20，实际模型 deepseek/deepseek-v4-flash，无 fallback。用户已确认复用其现有 DeepSeek。所有调用均为本机维护者 CLI，无 `--deliver`。

## 部署与隔离

专用配置 `~/Atrium/brain/openclaw.json`，通过 `OPENCLAW_CONFIG_PATH` 使用，不在日常主配置中保留 Atrium agent。只加载 DeepSeek 与 Atrium 插件；agent 仅允许 10 个 home 工具，无 Shell、浏览器、文件、聊天投递、长期记忆或其他技能。原生 MCP 注册关闭，工具只能经 Go Brain 宿主。家庭元数据在调用时送入用户选择的模型；工具不暴露 NAS 原路径、媒体 URL 或凭据。专用 integration 只允许 oneplus6t_tv / family_photos，凭据文件 0600，期限至 2026-10-07。

首个真实状态查询 status-shared：6031 ms，3 个只读工具，无工具失败；Core reachable，NAS degraded，OnePlus online。工具清单实测恰为 10 项。原生模型措辞仍有质量问题，见后文；工具成功不等于回答质量通过。

## 原生 CLI 固定中文基线（修复前）

每场景 3 个独立 session，15 次共计。耗时为 OpenClaw durationMs，包含模型多轮与工具等待；不是单独推理时延，也不是 TV 渲染延迟。

| 场景 | 3 次耗时 ms | 结果 |
| --- | --- | --- |
| 中枢状态 | 4398 / 4398 / 4000 | 查询成功；UTC 标注、降级原因与整体正常措辞存在偏差，回答质量未通过 |
| 展示真实照片 | 6172 / 9784 / 6900 | 2 次 applied，1 次 accepted 后 ack_timeout；模型没有把未确认说成成功 |
| 电视回首页 | 5789 / 7234 / 5906 | 3 次 applied，工具顺序 list_screens → navigate → get_command |
| 最近新增照片 | 5684 / 6606 / 7380 | 3 次 applied；有一次把今日入库与今日拍摄混说，表述需收紧 |
| 视频 / 妈妈照片 / 日历 | 7683 / 2732 / 4931 | 均说明能力未接入、没有执行写动作；部分回答冗长 |

追加提示规则并禁用通用技能后，状态 / recent / 引用数据注入各 3 次。所有工具均受白名单约束，引用文字中的读私密文件 / Shell 指令没有执行。但 status-2/status-3 仍有无依据概括或猜测原因；不能把提示词改进当作最终解决。正式维护者入口改用宿主证据生成最终回执，原生模型摘要仅用于调试。

## 实测暴露的 TV 问题与修复

原照片命令 `01M1WR3Q333TTHZAZSC1B0H1BG` 已投递、截图能看到照片，但 Core 最终为 unknown / ack_timeout。此前离开同照片页面没有清除 viewer 的 renderedId/status；再次 show 同图时 effect 依赖不变，未发送新的确认。没有用截图倒推该历史命令 applied。

修复为每次 show 开启新的图片 generation，清除旧渲染状态并重建图片；仅当前 generation 的 onLoad 可确认，迟到旧回调被拒绝。真实 PhotoScreen + useConnection 组件生命周期测试先红后绿，216 项 Web 测试、lint、build 通过，独立审查通过。

修复部署前备份旧 Core / 配置 / SQLite 至 `~/Atrium/rollback-tv-ack-20260907/`。新 Core 构建时间 2026-09-07T01:43:39Z，SHA-256 `736a2011e42d7602589660239d4c38df8be8ac3506963678a5595d32594681b5`。这是工作区构建，不是 Git 发布版本。重启 Core 并重新打开原 Android APK，保留配对；没有修改 NAS 挂载和电源策略。

管理员 CLI 独立实机回归：show 同图 → show 同图 → dashboard → show 同图，序列 133–136 全部 applied；含 CLI 启动与轮询的观测耗时 267 / 280 / 279 / 280 ms。证据为私有 `tv-ack-fixed.json`。这段验证 UI 确认链路，最终自然语言入口复测另列。

## 故障解耦

模型连接拒绝注入详见 [网络失败记录](ai-brain-mcp-model-network-results.md)：真实 ECONNREFUSED、CLI 退出、无工具/动作，Core 同时可达。此为单独配置的模型端点故障，不是断开家庭网络。

Brain/MCP 进程全部退出后，原管理员 show/navigate/refresh 继续 applied；ADB 遥控返回、方向和确认键可进入照片、返回列表与首页，截图已检查。证据仅留在本机私有 `~/Atrium/brain/`，家庭截图不进入仓库。

## 正式入口最终验收

正式 ask 入口已安装到 `~/Atrium/brain/ask`，插件源复制到 `~/Atrium/brain/openclaw-plugin`，宿主为独立私有部署二进制。每轮新 UUID；Go 验证并 fsync 的私有证据生成最终文字，模型自由摘要不进入最终输出。缺失/截断证据保留可信前缀与恢复 ID，并标记未完成；回执数与 OpenClaw 调用数不一致也拒绝完整成功。完整 30 秒期限由入口控制，另有最多 1 秒进程组清理。

正式固定场景各 3 次：

| 场景 | 完整入口耗时 ms | 最终结果 |
| --- | --- | --- |
| 状态 | 6055 / 6635 / 6409 | 3/3，保留 Core/NAS/TV 状态与 ISO 时区，不推测降级原因 |
| 实际照片 | 8082 / 8486 / 10531 | 3/3 applied，包括已展示过的照片；真实 ID、命令与屏幕一致 |
| 首页 | 7514 / 7972 / 7303 | 3/3 applied，明确目标与首页动作 |
| 最近集合 | 7203 / 7523 / 9244 | 3/3 applied，不虚构照片数量或今日新增 |
| 未接入视频/人物/日历 | 7143 / 4093 / 6685 | 修复零工具兼容后 3/3 明确能力边界，无屏幕动作 |

未接入能力首轮曾有 2 次安全拒绝（5307 / 4708 ms）：OpenClaw 在零工具调用时省略 toolSummary，入口错误地视为元数据损坏。核对本机 buildTraceToolSummary 源码后，以当前新 session、未中断且正常完成的零回执轮识别该兼容情况；加真实子进程回归，模型正文仍丢弃。首轮失败样本保留，不伪报为通过。

独立正式入口补验：多候选 fixture 5255 ms，仅列两候选并要求指定；空 recent fixture 5605 ms，仅报告返回空页和基线语义，不生成今日计数。两者无动作，详见 [fixture 记录](ai-brain-mcp-fixture-results.md)。模型端点失败时 ask 4459 ms 退出 1，报告 AI 本轮暂不可用，没有成功回执。

部署后的便利命令 `~/Atrium/brain/ask '把电视回首页'` 通过，真实命令 `01M1WWCYCRYZYA5VB88K4K0XVM` applied，OnePlus 最终处于首页。恢复命令只查询旧的 2 个未确认动作，将其账本状态记录为 Core 的 unknown；没有重发历史动作。

最终检查：make check 全仓 Go 测试 / vet / lint / 三程序构建通过；receipt 三包 race 通过；Web 216 项测试 / lint / build 通过；插件与 ask 共 44 项 Node 测试通过。全部实现保持在独立工作区，尚未提交 Git 或合并；部署是本机验证构建，非正式发布。
