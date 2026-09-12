# 家庭提示与今日简报：实施记录

2026-09-12。N1 提示契约已在 `a583db6` 完成并通过规范与独立质量审查；N2 今日汇总接口开始实现，N3 页面与 N4 实机验收尚未完成。运行设备仍为已验收的 `cb1314c` 房屋候选版，本记录不代表简报已上线。

依据：[开发计划](../plans/2026-09-12-product-development-plan.md)、[简报细化方案](../plans/2026-09-12-briefing-foundation.md)、[宋式今日页设计](../design/song-tv-v1/briefing.md)。设计已独立审阅，来源到期重排与未来提示轮询发现的语义已明确。

## N1：提示真实时间与有效性

`widgets.notice` 新增可选 `updated_at`、`valid_from`、`valid_until`。未知值在响应中为 null；不再在每次 home 请求时填当前时间。非空字段要求 RFC3339 和明确偏移；截止必须晚于开始，禁用提示中的非法配置也拒绝装载。旧 enabled/text 配置继续有效，纯空白内容不展示。

`widget.CurrentNotice` 是 home 与后续 overview 的共享投影：开始时包含，截止时排除；没有有效条目时不保留旧文本。Go/TS、OpenAPI 与示例配置同步，现有 home wire 自动序列化新增 nullable 时间，实际 Snapshot JSON 往返已验证。未在生产配置写入家庭提示或样例文本。

`Config.LoadedAt()` 单独记录成功文件装载后的观测时刻，返回副本。Defaults、Parse 与单独 Validate 没有文件成功装载证据，返回 nil；不会用文件 mtime 或请求时间补造观测。未引入配置热加载机制。

| 检查 | 结果 |
| --- | --- |
| 失败回归 | 初始缺字段/LoadedAt、伪造 updated_at，以及开始前/截止后仍出现提示的用例失败，随后修复通过 |
| 配置 | 老配置、空值、日期/无偏移/非法偏移/非法日期、正反范围、禁用非法配置、真实装载时间与元数据副本通过 |
| 投影与 wire | 纳秒级 `[from, until)` 边界、混合时区、未提供更新时间、空白/禁用、home 与共享投影一致、JSON 往返通过 |
| 完整检查 | `make check`：vet、lint 0 问题、全 Go 测试、三个二进制构建通过 |
| 并发检查 | `go test -race ./internal/config ./internal/widget ./internal/httpapi` 通过 |
| Web | Node 24 `npm --prefix web run typecheck` 通过 |
| 独立审查 | 规范和质量审查均接受，各自重新运行上述三个 Go 包测试；规范审查另验证 typecheck |

私有证据：`/Users/ditu/Atrium/iteration-20260912/m3-briefing/n1-make-check.log`、`n1-race.log`、`n1-typecheck.log`。检查构建发生在提交前，标识 `da322df-dirty`，不作为部署版本；没有替换正在运行的三个二进制或 APK。

## 剩余交付

N2 接独立汇总 API，N3 接宋式今日页与授权/有效期生命周期，N4 完成三尺寸、OnePlus、照片与 AI 回归后再构建部署。当前 Dashboard 不显示 notice，因此 N1 API 修正不能当作家庭提示已在 TV 可见。

真实日历来源、公开字段与家庭内容仍待明确，完整 M3 尚未完成；视频样本与正式发布门槛继续保留。Mac 断电恢复、NAS 自动挂载保持暂停。
