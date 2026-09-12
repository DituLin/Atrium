# 家庭今日概览：数据与展示契约

2026-09-12 · M3-A 工程契约已通过审阅并定稿；M2 验收通过，M3-B 中枢状态 API 与房屋页已在 `cb1314c` 部署并通过浏览器、OnePlus 和 AI 回归（见房屋迭代记录）。真实日历来源与公开字段已向用户询问，未获答复前不连接私人账户或读取个人日历；这一产品依赖仍待定，不妨碍先实现已有中枢状态的房屋页。

## 目标与来源

家庭今日概览由确定性代码汇总日程、家庭提示、房屋资料与中枢状态；模型不可用时仍正常展示。延续 Go Core、SQLite、React TV、既有屏幕鉴权。AI 的已有十个模型工具保持当前范围，不自动把新日历或房屋内容发送给模型。

| 模块 | 可用基础 | 尚待明确 |
| --- | --- | --- |
| 房屋 | 已授权 Core / NAS / 当前屏幕的真实观测 | 用户愿意公开的房间名、说明；传感器来源 |
| 家庭提示 | `widgets.notice` 的维护者静态提示 | 真实内容与维护者填写的时间；首期沿用单条，不增加多提示管理 |
| 日历 | 现有家庭 IANA 时区 | 一个明确授权的共享来源、允许公开的字段 |
| 简报 | 前述模块的公开投影 | 有真实来源后再验收完整今日概览 |

中枢在线、共享目录可读与房屋环境正常是不同事实。未连接温湿度或门窗传感器时不产生数值、正常结论或虚构房间。家庭位置不能由时区推断。

## 共同数据语义

采用“响应 → 来源快照 → 条目”三层，避免把不同来源的更新时间合为一个模块时间。`schema_version: 1`、`generated_at` 与 `home: {name, timezone}` 只在响应顶层出现。来源快照包含 `source_id`、`source_label`、`observed_at`、`expires_at`、`availability`、可空 `reason` 与 `items`。一个来源快照的全部条目来自同一次完整读取，共享其来源与 freshness；不同 NAS 来源各有一份快照。禁止用某个新条目的时间替整个列表续期。

| 字段 | 唯一含义 |
| --- | --- |
| `generated_at` | 本次响应生成时刻；可校准 TV 的相对时间，不证明来源更新 |
| `observed_at` | 最近一次成功取得可解释来源快照的时刻；从未取得为 null。读取到 offline/degraded 也是有效观测；失败重试或从数据库重新序列化旧记录不能刷新它 |
| `expires_at` | 该来源观测的 freshness 截止时刻；到达即 stale。静态配置无轮询 TTL，返回 null，直至配置被替换/禁用；null 不代表授权永久有效 |
| 条目 `updated_at` | 来源明确给出的该条内容修改时刻；未知为 null。与观测时刻分开，不使用响应时间、配置文件 mtime 或数据库行更新时间补造 |
| 条目 `valid_from` / `valid_until` | 内容允许进入展示/简报的半开有效区间 `[from, until)`，null 端点表示不设该端边界；到期移除，即使来源仍 available 或 stale |

以上时间点均为 RFC 3339 带偏移字符串或显式 null；全天日历日期是下节规定的例外。每条内容有稳定 `id`，通过所属来源快照继承来源、观测与 freshness，自己携带 `updated_at`、`valid_from`、`valid_until`。无需再逐条复制共同来源字段。日程开始/结束描述事件本身，不冒充来源 freshness；简报投影不能重置这些时间。

`availability` 使用 `not_connected | loading | available | stale | failed`。只有一次完整、成功且有效的读取返回 `available` + 空列表时，才能显示“没有公开事项”；未接入、过期或失败不能转换为空成功。短时故障可以保留仍允许公开的旧投影，但显式标为 stale，并显示 `observed_at`；权限撤销立即清除内容。不另加语义重复的 `last_successful_read_at`，也不把 NAS 的健康成功时间用于“最近更新”。

数据可用性与被观测对象的健康是两个字段。例如已成功读取到 NAS 降级状态，来源可以是 `available`，其中条目的 `health` 为 `degraded`。`health` 仅使用现有 `online | offline | degraded | unknown`，只有明确来源证据才赋值；available 不推导 online，更不推导“房屋正常”。NAS `last_success_at` 表示最近可读检查，可能远早于 `observed_at`，不能代替 freshness。现有 admin-only 的卡住 I/O 详情、挂载身份与路径继续只留在维护端。

页面文案按以下状态表落地，避免各页自行解释：

| 模块状态 | 内容处理 | 日历示例 |
| --- | --- | --- |
| not_connected | 不含事件，不计算“零条” | 尚未连接家庭日历 |
| loading | 首次读取中，不作空成功结论 | 正在读取公开日程 |
| available，空 | 本次成功读取覆盖旧列表 | 当前范围没有公开日程 |
| available，非空 | 展示公开投影和来源 | 展示议程 |
| stale | 保留仍获授权且在有效区间内的旧投影并标最近成功观测时间；旧空列表也不算当前空成功 | 日历暂未更新，显示上次日程 |
| failed，无旧投影 | 不展示旧/示例事件 | 暂时无法读取日历 |
| 授权撤销 | 清空投影、缓存和待展示状态 | 该日历已停止共享 |

状态判断顺序为：授权/配置检查 → 是否有旧快照 → 最近读取结果与 TTL → 条目有效区间筛选。首次请求进行中且无旧快照为 loading；首次读取失败为 failed；有旧快照时刷新中不改成 loading，失败立即标 stale，成功则整体替换（包括成功空列表）。`reason` 首期只需要 `not_configured | not_provided | not_supported | sharing_stopped | read_failed | expired | not_observed` 或 null，不返回原始错误详情。来源已配置但 NAS 尚未完成首个检查为 loading + not_observed，不能由旧空 health 推断 online。

撤销不是第六种可用性状态：固定模块（如日历）清空后返回 `not_connected` + `sharing_stopped`；不能变成可长期保留内容的 stale。NAS 使用下节的集合语义例外：撤销来源从数组移除，不返回该来源的 tombstone。来源配置代次包含来源删除、替换、重新授权和公开字段收紧；后台工作提交结果前必须核对代次，原始缓存、公开投影、简报引用和待展示状态同时清除。代次是内部竞态控制，不要求对 TV 暴露新的权限模型。

TV 新增 family 状态必须接入现有 `ApiClient.invalidateAuthorization()` 与 `AppProvider` 清除生命周期：401、screen_revoked/WS 4002、重新配对、切换 Core、24 小时授权缓存失效时同步清空 family 内容并使旧请求失效。请求响应和延迟 JSON 解析都检查授权代次；同一授权内的刷新再使用请求序号，旧请求不能覆盖较新的撤销或成功空列表。离线无法获知远端撤销时受现有授权时限约束，不声称能立即远端清除。首期 family 内容仅存内存，HTTP 返回 `Cache-Control: no-store`，不写 localStorage、IndexedDB 或 Service Worker 缓存。

所有面向 TV 的对象只包含经过公开字段筛选的内容。原始订阅 URL、凭据、NAS 路径、账户邮箱、参会者、备注、附件、会议链接和未经允许的地点不进入 TV API、日志或模型工具。数据更新不抢夺遥控焦点，也不跳出正在浏览的照片或视频。

## 日历适配边界

先提供统一的只读适配接口，再按用户选择实现一个来源。若选择 ICS，使用受限读取和完整解析库；不要用按行拆字符串代替日历解析。网络订阅地址只来自维护者配置，禁止 TV 或 AI 参数改变目标地址；密钥不放 URL 日志或仓库。设置单次大小、事件数量、重复展开窗口和超时上限，后台同步，首页请求不等待外网。

时间模型区分全天日期与带时区的时间点。全天事项保留日期边界，结束日期按非包含边界处理；重复事件按限定议程窗口展开并处理例外。浮动时间使用明确配置的家庭时区，并保留该解释信息。跨日事件按家庭日界投影，不以设备时区截断。这些基础语义依照 [iCalendar RFC 5545](https://www.rfc-editor.org/rfc/rfc5545) 的 DATE / DATE-TIME、VEVENT、RRULE、EXDATE 与 RECURRENCE-ID 定义；实现前按所选库的支持范围补兼容清单。

公开事件字段候选为稳定公开 ID、标题或“家庭日程”占位、开始/结束、全天标记、来源标签和数据状态。候选不是已经获准公开：来源、是否可公开时间/标题及其他字段仍待用户确认。未确认前返回 not_connected，不读取后再以占位标题绕过授权。确认后只投影获准字段；原始 UID 不能直接用作可能含邮箱的公开 ID。取消与例外更新要能撤下旧投影；来源成功为空也须清空旧事项，防止旧日程长期残留。

M3-C 工程默认：议程窗口为家庭时区今天起 7 个自然日，响应显式返回 `range_start` / `range_end`（结束不包含）。定时事件使用带偏移的 `start_at` / `end_at`；全天事件使用 `start_date` / `end_date` 的 `YYYY-MM-DD` 日期，二者用 `all_day` 区分，不用午夜 UTC 替代全天日期。后台每 5 分钟同步一次，成功观测 15 分钟后过期；失败立即 stale。当前窗口之外的旧缓存不能用于新一天的空成功结论。若所选来源需要不同 cadence、解析上限或端点安全限制，在实现适配器前补充来源专项契约；这些工程默认不替用户选择来源。

## 房屋与提示

房屋页分开显示“房屋资料”“环境数据”和“中枢与来源状态”。M3-B 首版只接已有中枢真实观测：没有用户提供的资料则 `profile` 为 not_connected + not_provided，显示“尚未填写”；`environment` 为 not_connected + not_supported，显示“未接入”。不生成房间名、温湿度或占位数值，也不增加传感器运行时和设备控制。公开资料编辑配置与持久化留到用户提供内容后再定义，首版不为占位区迁移数据库。

中枢区区分三个事实：成功 House 响应仅能证明 Core 本次能响应；NAS 展示已授权来源的最近检查；当前屏幕连接状态来自现有 TV connection/session 状态。不要从 GET 成功推断 WebSocket 在线或全部数据库功能正常，也不向屏幕返回其他家庭屏幕列表。Core 响应观测可用请求处理时刻，`updated_at` 仍为 null。

NAS freshness 工程默认固定为 60 秒，取 `LastCheckAt + 60s`；服务端每次读取廉价持久化快照后重新计算，客户端亦按服务器校时在到期时降为 stale。现有 `source.Manager` 每 15 秒检查，但只在 health/detail/mismatch 改变时发布 `nas/home` 通知，因此 TV 房屋页可见时每 30 秒重取 House API，在收到 `nas/home` 变更、页面进入、回到前台或重连后立即重取；合并并发请求，页面隐藏停止周期请求。仅依靠变更通知会让稳定在线来源误过期，不作为实现方案。TV 网络请求失败不延长观测时间：有仍允许展示的旧投影则 stale，无旧投影则 failed；服务端无法复核来源授权时按下节的 failed/清空规则处理，不能落成空成功。

当前 `domain.Source.ShareStatsAt` 与 `LastCheckAt` 不同，检查失败后容量可能仍是旧值。M3-B House API 与页面省略 `share_free_bytes` / `share_total_bytes`；已有 `/home` 与 `/nas/status` 容量行为不在本批改动范围。日后确需展示，容量须独立使用 `ShareStatsAt` 的观测与过期状态，不能沿用健康检查时间标为新鲜。这一选择不需要迁移。

原 `internal/widget/composer.go` 每次 Compose 把静态 notice 的 `UpdatedAt` 设为当前时间；N1 已在 `a583db6` 修正并完成审查，尚未部署。`widgets.notice` 现支持可选 `updated_at`、`valid_from`、`valid_until`；未提供均为 null，旧 `enabled/text` 配置继续有效，首期保持一条。非空时间必须是带偏移 RFC 3339，若两端均有值则要求 `valid_until > valid_from`，非法配置拒绝装载。成功装载配置的时刻仅作为来源 `observed_at`，不是内容发布时间；配置无 TTL，禁用或变更立即替换投影。

提示只在 enabled、非空文本和有效区间内出现；未到生效时间或已到期时来源为 available + 空列表，不能显示旧提示。Go `Notice.UpdatedAt` 已改为可空、与现有 TS 的 `string | null` 对齐，并同步 wire/OpenAPI；`/home` 和新简报必须使用同一有效区间过滤，避免一边撤下另一边仍保留。此修复是提示进入 M3-D 之前的独立任务，不要求先改 M2 UI。

## 简报排序与页面

建议优先级：明确异常或同步失败 → 当前及近期公开日程 → 有效家庭提示 → 简洁来源摘要。每条记录携带来源和有效性；没有事项时保持留白并给真实来源状态。单个模块失败不让整个首页失败，不使用“家里一切正常”兜底。

页面继续宋式字体、留白、焦点和返回规则。M3 完成真实联调前不在生产导航开放演示数据入口；无日历来源时真实状态页可以显示“尚未连接”。日历与简报先做议程列表，无需首期实现密集月历。

## 已选 API 形状与隔离

House 端点已实现并部署；overview 已在 `cc62ad8` 实现并完成审查，尚未部署到家庭设备；calendar 仍是后续目标，目前不可调用。首批只新增 `GET /api/v1/family/house`，以 `requireScope(auth.ScopeScreen, ...)` 接入现有鉴权；admin 可按现有兼容规则访问同一公开投影，不能因此附加维护字段。integration/MCP 不新增权限、端点或工具。概览使用 `GET /api/v1/family/overview`，具体 sources 与排序引用见简报实施计划；未来日历使用 `GET /api/v1/family/calendar`，来源确认后实现，不为未接入功能提前添加空 handler。

House 响应形状如下；这是类型草图，非真实家庭数据：

```ts
type Availability = 'not_connected' | 'loading' | 'available' | 'stale' | 'failed';
type Reason = 'not_configured' | 'not_provided' | 'not_supported'
  | 'sharing_stopped' | 'read_failed' | 'expired' | 'not_observed';
type ItemMeta = {
  id: string;
  updated_at: string | null;
  valid_from: string | null;
  valid_until: string | null;
};
type SourceSnapshot<T> = {
  source_id: string;
  source_label: string;
  observed_at: string | null;
  expires_at: string | null;
  availability: Availability;
  reason: Reason | null;
  items: Array<ItemMeta & T>;
};
type HouseResponse = {
  schema_version: 1;
  generated_at: string;
  home: { name: string; timezone: string };
  core: SourceSnapshot<{ responding: true }>;
  nas: Array<SourceSnapshot<{
    health: 'online' | 'offline' | 'degraded' | 'unknown';
    last_success_at: string | null;
  }>>;
  profile: SourceSnapshot<never>;
  environment: SourceSnapshot<never>;
};
```

`core` 使用稳定来源 ID `core` 和单条 `response`，`observed_at` 为本次响应观测，`expires_at = observed_at + 60s`；这只记录“最近响应”。每个 NAS 快照以已有 source ID/name 为来源，单条 ID 为 `health`，观测来自 `LastCheckAt`，条目三个时间元字段为 null。`profile` / `environment` 使用各自固定来源 ID，首版 items 恒为空、时间均 null、状态按上节定义；`never` 仅表示本版还没有内容类型，不预建通用房屋/传感器 schema。它们不显示“零条资料”或“零个异常”。

NAS 来源集合从当前配置与授权状态确定，只保留 active 来源。`nas: []` 统一显示“没有可展示的照片来源”，不推断从未配置、全部撤销或健康；首版不为区分这些空集合原因再加 schema。数据库来源清单读取失败时，无法复核来源授权：按已配置 ID/name 返回各来源 failed + read_failed、items 为空、两个快照时间为 null，客户端必须替换并清除对应旧 items，不能将 failed 自动降为保留内容的 stale，也不能把失败序列化为空数组。这条授权复核失败规则优先于普通网络失败保留 stale。配置移除或来源撤销立即移出列表并清掉对应内存记录。HTTP 200 可同时含 available 的 Core 与 failed 的 NAS；鉴权失败继续返回现有错误码，不能包成模块失败。

House 投影在独立 composer 中读现有配置和 source repository，可复用提取后的安全 NAS 映射；禁止先调用 `widget.Composer.Compose` 再裁剪字段。现有 Compose 先读照片统计，照片查询故障会导致整个调用失败；房屋 API 应能在照片计数失败时仍返回 Core 与 NAS。请求路径不触发 NAS probe、扫描、传感器或外部网络 I/O，亦不更改 `/home` 的现有 widget 包络。

客户端房屋页数据加载不能依赖 `/home` 成功或照片 widget 存在。当前连接引导使用 `/home`，M3-B 必须验证照片统计失败时，已有有效屏幕授权仍可进入房屋并独立读取 House；必要调整只隔离 family 页面加载与连接展示，不绕过鉴权，不改变照片命令的确认条件。当前屏幕的 name/status 可复用 `/screens/me`，WS 状态继续取本地 connection/session；不在 House 响应虚构一个由 HTTP 推导的在线屏幕。

只新增本地 `house` 路由及其状态上报支持：React route/ScreenName、Go `RouteName.Valid()`、WS 状态类型和 OpenAPI 的可上报 route 同步增加 house。`NavigableRoute()`、navigate 命令校验与 MCP 白名单仍仅允许 dashboard/photos。房屋返回到原导航焦点；刷新与数据通知不抢焦点、不切页；现有 `show` / `refresh` 照片语义继续回归。

## 下一批任务与门槛

| 次序 | 具体工作与文件落点 | 完成证据 |
| --- | --- | --- |
| M3-A 文档审阅 | 核对本文与 `internal/widget/{types,composer,wire}.go`、config/domain/source/httpapi、TS API 类型的差异；只定契约 | 来源级 freshness、公开字段与未决事项无歧义；不宣称已有功能 |
| M3-B1 投影与 API | 新增 `internal/family/{types,house}.go` 和 `internal/httpapi/family.go`，注册 House Screen 路由；同步 `docs/api/openapi.yaml`、`web/src/types/api.ts` 和 API client | 照片查询失败隔离；NAS 已知异常仍为有效观测；无来源、未首检、过期、查询失败分别验证；screen/admin 输出均无维护字段，integration 不获新权限 |
| M3-B2 页面与生命周期 | 新增 `web/src/screens/HouseScreen.tsx` 与最小 family 状态/hook；接现有导航、route 上报、auth purge 与请求代次 | 30 秒轮询与 60 秒过期、通知刷新、前后台/重连；旧响应不可复活内容；无资料/传感器时准确显示尚未填写/未接入；全程遥控往返 |
| M3-B3 验证与交付记录 | M2 出口通过后执行 B1/B2；按涉及包做 Go 测试/race、HTTP/OpenAPI 鉴权测试及 Web lint/test/build，再进行 OnePlus 回归 | House 可独立使用；source 撤销、24 小时 auth 失效、延迟 JSON、照片查询失败与 route 白名单有行为证据；记录为“中枢状态首版”，不冒充完整 M3 |
| 提示契约补齐 | 扩展 `internal/config` 时间校验；修复 widget notice 时间/有效期，同步 Go/TS/wire/OpenAPI | 老配置兼容；未提供更新时间为 null；到期从 home 与简报一致撤下；不需要迁移 |
| M3-C 来源接入 | 用户确认共享来源及公开字段后，新增 `internal/calendar/` 只读适配、公开投影与受限缓存，再做 Calendar API/页面 | 家庭日界、全天/跨日/重复与例外、成功空数据、窗口换日、失败旧缓存和权限收紧清除；用一个真实授权来源联调 |
| M3-D 简报 | 新增 `internal/briefing/` 确定性汇总与 Briefing API/页面，保留原来源时间与有效性 | 单模块失败隔离，无 AI 时可用，不向既有模型工具自动暴露新数据 |

M3-B1/B2 不需要数据库迁移，不增加 NAS 扫描根，不连接私人日历，也不顺带扩展生产 AI 工具。持久化日历前先完成来源授权与缓存销毁设计，再创建必要迁移和升级测试；只保存允许公开的投影及最少同步状态。M3-C 网络/文件来源尚未选择，具体库、凭据存放和解析限制不是已完成项。

M3 出口仍要求一个真实授权日历来源和真实家庭提示/公开资料场景。仅文档、模拟日历或未连接页面不能算完整家庭概览交付。Mac 断电恢复、NAS 自动挂载继续暂停。
