# 家庭提示与今日简报：实施记录

2026-09-12。N1 提示契约已在 `a583db6` 完成并通过规范与独立质量审查；N2 今日汇总接口已在 `cc62ad8` 完成并通过双阶段审查，N3 页面已在 `b025b17` 完成代码、双阶段审查与三尺寸浏览器验收，N4 已完成干净构建、备份部署、OnePlus 与照片/AI 回归。当前家庭设备运行 `28ffc09` 简报候选版；完整 M3 仍需真实日历和家庭内容。

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

## N3：宋式今日页面

`b025b17` 加入“首页、照片、今日、房屋、设置”导航。今日页面左侧完整阅读/滚动，右侧固定五类来源摘要与房屋入口，刷新栏固定。长文、状态变化、House/设置返回和远程 refresh 保留页面与焦点；本地 briefing 可上报，远程 navigate 仍只允许 dashboard/photos。

Overview 独立生命周期接入 30 秒可见轮询、前台/重连重取、通知前清空、请求中止及授权代次。HTTP 401/410 screen_revoked、WS 4002、重新配对和授权缓存失效同步清空内容。新 API 成功的来源不因 WebSocket 未建立而误标为旧数据；照片 /home 故障时仍可经真实 House 鉴权进入今日。

客户端按校时重新投影有效区间、来源 TTL 和引用排序，消费与 Go 相同的八个场景。除常规时钟 tick 外，截止定时器确保亚秒边界撤下；进入时的时钟样本与前台同步更新确保后台计时器暂停后，过期提示不会进入首帧或停留到下一个 tick。保留源内容时间，不为旧响应续期。

| 验证 | 结果 |
| --- | --- |
| Web | 最终 47 文件、347 项测试，typecheck/lint/build/postbuild 通过 |
| Go | domain/ws/httpapi/briefing 测试通过；父任务 make check 通过（最后两项 React 首帧修复后重跑完整 Web，Go 未再变化） |
| 审查 | 规范与质量各独立验证 73 项/9 文件；首帧增量分别复核 25 项/6 文件并接受 |
| 三尺寸 | 1920×1080、3840×2160、804×384 完整键盘流程均通过；长文滚动分别 1408/2816/604 CSS 像素；截图人工查看 |
| 视觉 | 长中文、24 个来源、混合状态、空/失败/过期、故障恢复；各状态按钮不裁切，列区域不覆盖刷新栏，刷新栏不覆盖导航 |
| 时间与故障 | NAS 到期新增旧状态项，提示到期撤下；未来提示通过下一次轮询取得；传输失败保留仍有效旧提示；成功空/来源失败清空内容 |
| 独立鉴权 | 冷启动照片接口 503、过期授权缓存，经 House 真正鉴权后进入今日；HTTP-only 撤销回配对并清空内容与授权时间 |
| 浏览器竞态 | 真实 mock WebSocket 通知后旧内容立即清空；两次迟到的 200 均被忽略，撤销后不会复活页面或续期授权；页面错误为零 |

首轮截图发现列高覆盖刷新/导航，已修正；浏览器返回发现 briefing 未进入通用返回栈，已补失败回归。最后首帧测试复现暂停计时器后的过期提示闪现，已修复并复审。测试脚本另修正了进入页自动刷新与手动请求合并的等待时序，并替换 Python Playwright Response.finished 的结束阶段等待，最终日志无该异步关闭噪声；这些脚本问题不列为产品缺陷。

最终私有证据位于 `m3-briefing/`：`n3-tests-final.log`、`n3-build.log`、`n3-go.log`、`n3-make-check.log`、`briefing-browser-flow-final.log`、`briefing-browser-flow-results.json`、`briefing-browser-races-final.log`、`briefing-browser-race-results.json` 及 `briefing-*.png`。检查二进制标识为 `e5a1af3-dirty`，只表示提交前检查产物。以上为部署前检查记录；随后 N4 从干净 `28ffc09` 构建并部署，APK 无改动。

## N4：备份部署与 OnePlus / AI 回归

2026-09-12 19:41（Asia/Singapore）从干净 `28ffc09` 构建 Web 并执行 `make web-sync build`，备份后替换 Core、home-mcp 和 Brain Host。健康检查为 `28ffc09+28ffc09`，Web 为 `0.1.0+28ffc09`；OnePlus 实际加载 `index-CRYbAiQ0.js`。配置、CA、launchd 和 Android APK 未改变，没有增加数据库迁移。

| 验证 | 结果 |
| --- | --- |
| 真实今日页 | 804×384 CSS / 2262×1080 实屏；本地字体、两栏、固定来源与导航无溢出或覆盖。真实 NAS 有新在线观测；日历/提示未接入、资料未填写、环境未接入均准确显示 |
| 遥控与恢复 | 全程 D-pad/确认/返回完成今日与房屋/设置/照片往返；来源焦点恢复，手动与远程刷新保持简报页和焦点 |
| 截止与独立读取 | 只在测试机已鉴权的 200 响应中注入明确标记的短期提示，到期自动撤下；不改真实家庭配置。冷启动 /home 503 时，经 House 新鉴权仍能打开今日 |
| 照片完整 smoke | 四合集、单图上下张、非零滚动与原卡片恢复、操作层/长按/原生菜单、设置三页、前后台、进程重开保持配对均通过；照片页 refresh 保持预览 |
| 故障恢复 | JavaScript 503 后自动恢复 11.444 秒；关闭测试机 Wi-Fi 后离线冷启动，恢复 Wi-Fi 到页面连通 10.876 秒。结束时 Wi-Fi 为 1，CDP 转发已移除 |
| DeepSeek 联动 | 新收据 12 次：查询/刷新/展示/回首页各 3 次全部通过；查询报告 briefing，3 次刷新后仍是 briefing；9 个不同控制均为 applied |
| 耗时与账本 | AI 入口耗时 4.463–7.114 秒；Core 精确 issued_at→resolved_at 为 102.322–193.570 毫秒。只读恢复核对后 Brain 9 条对应动作均 applied，没有补发未确认控制；这些样本不作为 M5 P95 验收 |
| 适配器 | OpenClaw 44 项测试通过，0 失败 |

备份：`~/Atrium/iteration-20260912/rollback-song-briefing/`，含原三个二进制、配置及 SQLite 在线备份，目录 0700、敏感文件 0600。部署辅助程序核验旧文件与 launchd 参数后执行原子替换，并具备失败回滚；部署和最终健康检查均成功。

候选包：`~/Atrium/releases/2026-09-12-song-briefing/`，只含三个 Mac ARM64 二进制、未变更 M2 debug APK、README 与哈希清单，不含凭据、数据库或家庭素材。APK SHA-256 为 `89b530a9fead5c60795e288a458c28ecae5950b41300da391d29d8195b0181f1`。尚未正式签名，不能替代真实电视验收。

| 运行文件 | SHA-256 |
| --- | --- |
| atrium | `d36a4e62c4dddc959ed7850906c632856057fc54166615ff1911691301230d59` |
| home-mcp | `d9df2f890039dc485cf6af5c588173ace24c64ec5c5fd4b0ef123038a207653c` |
| atrium-brain-host | `7b85e09b4a1ddc77e8996346ba508aa088f12b6c070f1077ecf6dd3ed892c182` |

私有证据：`m3-briefing/release-build.log`、`deployment.log`、`deployed-build.json`、`device-briefing-results.json`、`device-briefing-*.png`（实屏已人工查看）、`device-smoke.log`、`ai-product.log`、`adapter-tests.log`，以及 `ai/` 内的 12 份原始收据、命令最终结果、恢复结果和 `ai-timing-precise.json`。测试结束恢复真实首页并确认 Core 健康。

## 剩余交付

N1–N4 提示/简报基础已完成，在 OnePlus 验证并交付家庭候选包。它只证明已接入模块的真实汇总与页面流程，不代表真实日历、家庭提示内容或正式 TV 已验收。

真实日历来源、公开字段与家庭内容仍待明确，完整 M3 尚未完成；视频样本与正式发布门槛继续保留。Mac 断电恢复、NAS 自动挂载保持暂停。
