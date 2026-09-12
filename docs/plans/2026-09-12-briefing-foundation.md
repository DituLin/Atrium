# 家庭提示与今日简报基础实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在已部署的照片与房屋中枢状态基础上，让 TV 展示有效家庭提示和需要关注的来源状态，为授权日历接入保留明确边界。

**Architecture:** 延续家庭快照契约。配置负责提示的真实时间与有效区间；独立 overview composer 复用 House 公开投影和提示规则，生成确定性排序引用。TV 自行渲染中文，模型不参与计算，不增加 MCP 工具。

**Tech Stack:** 现有 Go / React / TypeScript / OpenAPI / Android WebView，无新运行时或数据库迁移。

---

2026-09-12，N1 开始实现，N2/N3 为工程细化方案，尚未交付。范围依据 [开发总计划](2026-09-12-product-development-plan.md) 与 [家庭数据契约](../tech/2026-09/atrium-home-hub/family-overview-design.md)。用户已确认简报内容为家庭今日概览：日程、家庭提示、房屋状态。当前缺少明确授权日历来源，先交付已有模块的真实汇总，不把它算作完整 M3。

## 实现选择

选择在 Core 独立汇总公开来源与排序引用，页面按同一份来源快照取内容。这既能独立于照片统计和模型运行，也避免简报条目复制后丢失原始有效性。单纯由 TV 拼接多个请求容易得到不同代次的数据；让模型生成简报会把确定性状态与模型可用性绑定。两者均不作为本批主路径。

## N1：提示规则

修改 `internal/config/{config,load,validate}.go` 与对应测试、`internal/widget/{types,composer,wire}.go` 与对应测试、`web/src/types/api.ts`、`docs/api/openapi.yaml`。按实现需要提取共享提示函数，供 widget 与 overview 调用。

1. 先写失败用例：旧 enabled/text 配置、缺省时间、非法/无偏移时间、结束不晚于开始、开始时刻包含与截止时刻排除。
2. 加入可选 updated_at/valid_from/valid_until；未知序列化为 null，不能以请求时刻或文件 mtime 代替。
3. 成功 Load 的观测时间单独记录；Defaults/仅 Parse 的配置没有成功装载证据，不补造 observed_at。
4. 共用 enabled、非空文本与 `[from, until)` 过滤；校验未通过不能进入运行配置。禁用或替换配置后新投影不得保留旧文本。
5. 核对真实 home JSON，而不只测内部结构。执行 `go test ./internal/config ./internal/widget ./internal/httpapi`，Web 类型检查与相关测试；规范与质量审查后独立提交。

目前 Dashboard 只渲染 clock/photo，N1 不因 API 中 notice 修正就宣称 TV 已展示提示。可见入口由 N3 完成。

## N2：独立今日汇总 API

新增 `internal/briefing/{types,composer,composer_test}.go`，扩展 `internal/httpapi/family.go` 及测试、`server.go`、`routes.go`，同步 `docs/api/openapi.yaml` 与 Web API/types/tests。具体依赖构造复用既有 House composer，不读取 NAS 文件或先调用 widget.Compose。

响应仅有一个根 `schema_version/generated_at/home`。`sources` 包含 `core/nas/profile/environment` 的现有公开快照，以及 `notice`、`calendar`。NAS 从 House 公开投影复用；不能把 failed/空数组转换为健康。notice 来源固定 ID `notice`、标签“家庭提示”，条目固定 ID `notice`，保留真实 updated_at/valid_from/valid_until 和文本；observed_at 是成功配置装载时刻，expires_at 为 null。启用但当前无有效提示时是 available + []；未配置/禁用是 not_connected + not_configured。日历暂为固定 `calendar`、not_connected + not_configured、null 时间、空 items；不预建日历 handler、适配器或假事件。

`entries` 是排序引用，不复制内容：`id`、`kind: source_status | notice`、`module: nas | notice`、`source_id`、可空 `item_id`。source_status 引用来源状态，item_id 为 null；notice 引用实际 notice 条目。来源与条目的时间只保留在 sources，UI 不得给引用重新续期。ID 由模块、来源、类型稳定组合，不能用本次响应时间。

排序：读取失败与当前可用观测中的 offline/degraded → stale → loading/unknown → 有效提示，同级按配置来源顺序；一来源只产生一条状态引用。stale 优先于其中旧健康值，不能把旧 offline 当本次已确认异常。在线 Core/NAS、未接入资料/环境/日历在来源摘要区展示，不制造告警或“正常”的总判定。没有 entries 不意味着所有来源正常。

1. 失败用例覆盖上述排序、边界时刻、稳定引用、缺省/禁用提示与未接日历；Home/NAS 失败隔离与来源撤销清空。
2. 使用一个确定时刻构成响应；复用已读 House 快照的生成时刻筛选提示，避免请求跨过截止时一份响应出现两个解释。
3. `GET /api/v1/family/overview` 使用 Screen 鉴权，admin 同公开投影，integration 403，Cache-Control no-store。鉴权失败不包成来源失败。
4. HTTP 测试核对 JSON 空数组/null、照片表故障独立返回、来源表失败保留提示但清空 NAS 内容、无路径/凭据/原始错误泄露。
5. Web getOverview 支持 AbortSignal，并沿用授权代次检查；运行相关 Go/race、Web API 与 lint/build，再独立审查提交。

## N3：宋式简报页面

先写 `docs/design/song-tv-v1/briefing.md` 冻结布局、焦点和状态规则，再新增 `BriefingScreen.tsx` 与 family overview 生命周期。纸白青瓷作为可调整基线，不当作用户已批准的视觉定稿。

主体为“今日”事项列表，侧栏为来源摘要。列表为空时说明已接入模块当前没有可展示的提示，同时清晰呈现未连接日历。提示文本必须可完整滚动阅读；焦点/选中与内容状态分开，不让刷新改变当前位置。不得用真实生产页展示样例家庭事项。

本地新增 briefing 路由与上报枚举，远程 navigate 白名单不扩展；refresh 保持当前简报页。与 House 一样独立于 /home、可见时 30 秒刷新、前台/重连重取、通知先清除旧投影再合并请求。客户端按服务器校时在 valid_until 到期撤下内容，即使离线；未来提示尚未随响应下发时，不从空列表猜测生效时间；由可见页面的下一次 30 秒轮询发现，前台恢复立即重取，实际延迟还取决于请求完成。禁止提前展示，首期不增加下一次变更时间字段。未连接/已撤销/过期/读取失败不混成空成功。

entries 表示生成时的排序。客户端先按服务器校时与网络状态计算来源当前可用性，再使用与服务端相同规则重建展示引用及排序：原先在线来源到期须新增 stale 状态项，旧 offline 到期须降为历史观测；仍在有效区间的提示可保留但标旧。实现独立纯函数，以共享 JSON 场景核对 Go 与 TS 的排序结果，避免两端规则漂移。

先测试在线→过期、异常→过期、传输失败但提示仍有效、未来提示轮询发现，以及旧响应不能覆盖成功空/撤销、授权到期清除、单来源失败清空、返回焦点和后台停止轮询，再接 UI；三尺寸长提示/多来源截图与方向键验证后审查。

## N4：交付门槛

执行总计划第 7 节检查，OnePlus 全遥控简报/房屋/照片往返、提示截止边界、断线/重新授权、照片接口失败时独立入口；现有 AI 查询/刷新/show/navigate 每类三次。原生未改则不重建 APK。

有通过的干净构建后，先备份再部署，记录版本/哈希/截图/回执，家庭素材留私有目录。真实日历、真实家庭提示内容与完整 M3 仍需真实授权数据验证；视频样本、正式 TV/签名、性能与长稳属于后续门槛。Mac 断电恢复与 NAS 自动挂载继续暂停。
