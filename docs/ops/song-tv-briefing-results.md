# 家庭提示与今日简报：实施记录

2026-09-12。N1 提示契约已在 `a583db6` 完成并通过规范与独立质量审查；N2 今日汇总接口已在 `cc62ad8` 完成并通过双阶段审查，N3 页面与 N4 实机验收尚未完成。运行设备仍为已验收的 `cb1314c` 房屋候选版，本记录不代表简报已上线。

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

## N2：今日汇总接口

新增 `GET /api/v1/family/overview`。只读 House 的公开投影，与提示的共享有效期函数组合；请求不调用照片统计、NAS 文件或外部服务。响应只含一个 schema/generated_at/home 根，sources 保留 Core/NAS/资料/环境来源，并加入家庭提示与未连接日历。notice 使用 House 的同一生成时刻过滤，避免截止边界出现两种解释。

排序条目只保存稳定引用，不复制文本或刷新来源时间。读取失败和有效异常优先，其次旧观测，再到未完成检查/未知，最后有效提示；同级保持配置来源顺序。旧 offline 先按 stale 分类，仍有效的 stale 提示可保留。无配置装载证据的启用提示不暴露文本，返回 loading/not_observed；未启用为 not_connected；成功装载后未来/过期/空提示为 available + []。

Screen 和 admin 获得同一公开投影，integration 为 403，响应 no-store。照片表故障时 overview 仍可响应；来源授权读取失败保留其他模块，但清空 NAS 条目；撤销来源直接移出。没有新增日历适配器、页面路由或 MCP 工具。

验证包括 HTTP 权限/撤销/隐私、真实提示截止边界、成功空数组与显式 null、OpenAPI 实际响应校验及拒绝维护字段。Web API 的独立请求、延迟 JSON 授权失效与 AbortSignal 三个用例通过，API 文件共 20 项测试。`testdata/overview-ordering.json` 提供 8 个已投影场景，Go 排序已消费；TS 客户端的时效投影与排序一致性将在 N3 完成。

`make check`、briefing/httpapi/family/config/widget 相关 race、Web lint/build 通过。规范审查独立通过相关五包测试、三个包 race 与 20 项 API 测试；质量审查接受，独立重跑 briefing/httpapi（非缓存）与 Web API 20 项通过。检查产物标识 `fbdff8c-dirty`，未部署。私有日志为 `m3-briefing/n2-make-check.log`、`n2-go-race.log`、`n2-web-api.log`。

## 剩余交付

N1/N2 已完成代码与审查，下一批 N3 接宋式今日页与授权/有效期生命周期，N4 完成三尺寸、OnePlus、照片与 AI 回归后再构建部署。当前 Dashboard 不显示 notice，因此 N1 API 修正不能当作家庭提示已在 TV 可见。

真实日历来源、公开字段与家庭内容仍待明确，完整 M3 尚未完成；视频样本与正式发布门槛继续保留。Mac 断电恢复、NAS 自动挂载保持暂停。
