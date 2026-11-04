# Home Assistant IoT 接入开发方案

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 打通真实设备 → NAS 上的 Home Assistant → Mac 上的 Atrium → TV 房屋页的只读闭环，并保留未来受控设备操作的架构边界。

**Architecture:** HA 处理设备协议和厂商账号，Atrium 将 HA 实体映射为独立于品牌的设备能力。后台同步写入本地观测存储，HouseComposer 只读取投影；TV 只连接 Atrium。

**Tech Stack:** Go 1.25、现有 coder/websocket、SQLite、React 18 / TypeScript、Home Assistant Container、群晖 Docker / Container Manager。

[English](2026-10-30-ha-iot-integration.en.md)

---

## 1. 状态、授权与决策

2026-10-30 起草；2026-11-04 已确认首批范围并进入实施。用户已授权升级 NAS、部署 HA 并接入所选米家家庭。统一模型和 HA 订阅适配器已实现，真实 HTTPS/WSS 同步已验证；存储、房屋页集成及端到端验收仍在进行。

### 2026-11-03 已验证进展

- NAS 已升级至 DSM 7.4.1-90080 Update 1，Container Manager 24.0.2-1706，Docker Engine 24.0.2。下方环境表保留升级前历史快照。
- Home Assistant Container 2026.10.4 已部署，使用固定镜像摘要、NAS 本地配置目录、host 网络及自动重启；小米官方集成 v0.5.0 已安装并完成账号授权。
- 所选家庭已导入 15 个设备、308 个实体；加湿器、温湿度计及鱼缸页面已显示有效读数。一个已检查的插座当前实体不可用。设备导入数量不等于所有设备在线或全部能力可用。
- 实际体脂秤型号为 S400，与最初口述的体脂秤 2 不同；设备清单还包含小爱音箱 mini。后续映射以实际型号和实体为准。
- 此阶段未执行设备控制；尚未验证持续状态更新、设备至 HA 延迟、HA 至 TV 延迟及故障恢复。
- 当日 HA 初始化使用局域网 HTTP；2026-11-04 已完成专用凭据、实体白名单及可验证 HTTPS/WSS，候选适配器成功同步 13 项能力。
- Atrium 的两个 NAS 照片源已恢复在线；NAS 重启后的自动重新挂载尚未验证。备份范围遵循用户要求，仅配置，不继续备份照片或存储内容。

### 已确认

- NAS 运行 HA 是当前优先部署方向；Atrium 保持在全天运行的 Mac mini 上。
- 首期只读，后续需要控制设备。TV 默认只读，控制按屏幕单独授权，并可限制到设备。
- TV 不直连 HA；Mac 与 NAS 使用局域网 API，不通过 SMB 或共享数据库交换 IoT 状态。
- 后续非米家设备、ESPHome 和 MQTT 设备可以先进入 HA，再复用 Atrium 的 HA 适配器。
- MiGPT Next 已按用户要求停止，容器和数据保留；本计划不恢复或删除它。

### 本机检查事实（历史快照，不是容量保证）

| 项目 | 2026-10-30 检查结果 |
| --- | --- |
| Mac | M4，10 核，32 GB 内存，macOS 15.2；内置盘约剩 6 GiB |
| Atrium | 正在运行；用户级 LaunchAgent，登录后启动及保活 |
| Mac 电源 | 系统自动睡眠关闭；断电恢复后自动启动未开启 |
| NAS | 实机是 **DS220+**，不是此前口述的 DS226+ |
| NAS 系统 | DSM 7.1-42661 Update 3；Intel Celeron J4025，双核，10 GB 内存 |
| NAS 容器 | Docker 套件 20.10.3-1308 已启动；检查时内存使用约 2.11 GB |
| NAS 外设 | DSM 识别到 USB UPS；尚未验证 UPS 关机及恢复策略 |

不在公共文档记录真实账号、地址、序列号、令牌或未脱敏截图。

### 已确认的首期方案

- 首批实机验收为加湿器、温湿度计和鱼缸；插座当前不可用，后续恢复后另行接入。
- 房屋页按房间分组，支持“未分配房间”；首期由配置指定房间、顺序及可展示能力。
- 首期仅一个 HA 实例、仅最新状态，无历史曲线、自动化编辑器或设备管理 UI。
- 使用独立 `iot` 字段，保留原有 `profile`、`environment` 契约；避免同时重构首页。
- 网络健康时，HA 收到新状态至 TV 呈现的目标为 5 秒内；设备至 HA 的厂商延迟单独测量，不承诺实时 SLA。

## 2. 部署前置门槛：先确认兼容，再安装

当前 HA 官方 Container 文档要求 Docker Engine 23.0.0+；NAS 显示的旧 Docker 套件不能直接视为符合要求。先核实实际 Engine、CPU 架构、DSM 可升级路径和对应 Container Manager Engine 版本。**不能假设升级到任意 DSM 7.2 就一定满足要求。**

实施前准备升级影响清单：现有容器、挂载卷、端口、套件兼容性、可恢复备份和维护窗口。DSM 升级不能假定支持原地降级。呈现具体目标版本和影响后由用户决定是否升级；安装或升级必须在整体方案确认之后。

如果无法获得合适的运行环境，暂停生产安装，继续本地模拟 HA 的开发验证；另评估 NAS 上 HAOS 虚拟机或腾出 Mac 存储后的虚拟机方案，不私自安装过时 HA 来绕过门槛。

计划部署规则：

- HA 配置目录位于 NAS 本地卷，挂载到容器 `/config`；不放在 Mac 的 SMB 挂载路径。
- 固定测试通过的 HA 镜像版本及摘要、小米集成版本；禁止生产直接追随 `latest`。
- 容器自动重启、日志轮转；HA 历史建议保留 7 天并排除不必要实体，实施时核实配置语法。
- NAS 固定地址 / DHCP 预留；HA 对 Mac 提供稳定、证书可验证的 HTTPS/WSS 地址。局域网反向代理也是候选，不能通过关闭证书校验解决证书问题。
- 首轮云接入不需要 USB 透传、宿主蓝牙或一律 privileged。网络模式与最小权限以目标 HA/集成版本验证为准。
- 备份 HA 配置及 Atrium 状态；HA 配置包含认证秘密，备份仅保存在受控位置。
- Mac 和 NAS 的开机恢复分别验证。当前 Mac 用户级 LaunchAgent 的“登录后恢复”不等于“无人登录自动恢复”。

## 3. 设备接入矩阵

| 设备 | 路径 | 首轮边界 |
| --- | --- | --- |
| 米家智能插座 3 | 小米官方 `xiaomi_home` | 开关状态；已存在功耗异常反馈，不以能耗作为验收条件 |
| 米家智能除菌加湿器 2 | 小米官方集成候选 | 工作状态；湿度、模式等以实际实体为准 |
| 小米智能鱼缸 | 小米官方集成候选 | 先盘点实体，未确认水温、喂食等支持；不操作电源或喂食 |
| 米家小米空气循环扇 | 小米官方集成候选 | 导入后核对精确型号、开关、风速及摇头映射 |
| 小爱音箱 Play / Pro | 小米官方集成候选 | 不承诺播报、播放或 Atrium 语音入口；不纳入首期验收 |
| 米家蓝牙温湿度计 2 | Xiaomi BLE，按机身型号验证 | 蓝牙覆盖及 bindkey 单独落实，不默认 NAS 或小爱提供 HA 蓝牙代理 |
| 米家智能体脂秤 2 | Xiaomi BLE / ESPHome，按型号验证 | 体重、阻抗；人员归属及健康隐私另设计，不进入公共房屋屏 |
| 自建 ESP 设备 | ESPHome 原生 API | 支持的实体复用 HA 适配器；首期不部署工具链 |
| 树莓派自定义程序 | MQTT Discovery 或 REST | MQTT Broker 需要时单独部署；不提前安装 |

产品名不能代替型号与实机验证。HA 导入后只记录脱敏能力清单：实体域、类型、单位、可用性、更新方式。小米账号授权在 HA 侧完成，用户自行登录，不在聊天中传递密码。

## 4. 统一模型、同步与故障语义

### 模型

`IntegrationInstance → Device → Capability → Observation`，房间归属由 Atrium 配置维护。

- 实例：稳定 ID、provider、连接状态、同步代次、最近成功同步时间。
- 设备：稳定 Atrium ID、显示名、room_id、来源实例。不得以可变显示名作为身份。
- 能力：稳定 ID、kind、数据类型、标准单位、provider 实体绑定。首批支持 `power`（boolean）、`humidity`（number/%）、`temperature`（number/°C）、`operating_state`（白名单枚举），并按已确认设备补充 `battery` 和 `filter_life`（number/%）。
- 观测：类型化值或 null、原始状态变化时间 `source_updated_at`、明确存在才填写的采样时间 `sampled_at`、接收时间 `received_at`、同步确认时间 `confirmed_at`、可用状态及公开原因。
- 实体重命名：保留 Atrium ID，更新显式绑定；无法匹配时标记缺失，不按名字自动关联其他设备。多平台重复设备首期手工映射，不自动合并。

连接状态与单实体状态分开。`unknown` 不转成 0 或 false；`unavailable` 不解释成关机；单位不符或非法值标记不可用，不静默猜测。温度转换只接受已知单位。

### 配置草案（全部为合成示例）

```yaml
iot:
  enabled: false
  home_assistant:
    id: ha_primary
    url: https://ha.example.invalid
    token_file: /path/to/private/ha.token
    devices:
      - id: demo_plug
        name: 示例插座
        room_id: demo_living_room
        room_name: 示例客厅
        capabilities:
          - id: power
            kind: power
            entity_id: switch.demo_plug
```

配置校验拒绝重复 ID、跨设备重复实体绑定、未知能力、URL 用户信息和内联 token。`token_file` 限制服务账号读取；日志及配置打印必须脱敏。HA 使用专用非管理员账号，验证其实际可读范围。普通长期令牌**不自动等于只读令牌**；第一期 Atrium 客户端仅实现读取消息白名单，不实现通用代理或动作调用。

配置首期重启生效；删除映射后，启动及每次投影均按当前白名单过滤，清除已撤销的缓存观测。禁用接入和失去授权不得继续显示旧的敏感数据。

### 同步算法

1. 后台 worker 鉴权；建立状态订阅后缓存事件，再请求全量状态，避免“先快照后订阅”丢更新。
2. 以连接代次隔离旧连接；全量响应与缓存增量按照 provider 版本/时间合并，不能让旧快照覆盖较新事件。无可靠顺序时重新同步，不猜测。
3. 首个完整同步后原子提交；每次有效状态变化提交最新值并发布 `iot` 变更通知。
4. 订阅心跳建议 20 秒、超时 10 秒；指数退避 1–60 秒并加入抖动。401/鉴权拒绝进入需重新授权状态，避免高速重试。
5. 建议每 5 分钟完整对账；这些时间是可配置设计默认值，需要用目标 HA 版本验证。
6. 断线保留带时间的旧状态，标记连接失效；重连完成全量同步后才恢复可用。重启读到的持久化缓存也先标旧。
7. HA 删除实体则清除当前值并标记缺失；取消 Atrium 展示授权则删除整个展示条目。

“连接活着”“实体可用”“读数刚采样”是不同事实。心跳不能更新采样时间；长期不变的开关不因 `last_changed` 较旧就失效。没有真实采样时间时标“最近同步”，不标“刚测量”。传感器采样过期只在有可靠采样依据和该能力阈值时判定。

持久化只保存白名单设备最新观测，不保存完整 HA payload 或长期历史。使用单张观测表，主键为实例/设备/能力；状态提交成功后再通知 TV。NAS/HA 故障不得阻塞 Core 启动、照片或档案 API。

## 5. API 与 TV

- 在 `/api/v1/family/house` 增加可选 `iot` 模块；旧字段语义保持不变，schema version 1 的兼容性通过旧客户端回归确认，否则实施前调整版本方案。
- `iot` 包含接入可用性、房间/设备列表和逐能力状态。响应不包含 token、HA URL、外部实体 ID 或原始错误。
- `profile`、`environment` 首期仍为原占位契约，避免改变 `OverviewSources`。新版房屋页在有 IoT 模块时使用它展示环境读数，避免同时显示误导性的“环境未接入”。
- 客户端兼容没有 `iot` 字段的旧服务器，继续显示原房屋页。
- 新增 `data.changed` topic `iot`，仅触发房屋数据刷新，不引起照片统计重读。沿用通知合并、请求代次和授权清空机制。
- 正常状态通知不得每次清空整个页面导致闪烁；权限撤销必须立即清空。轮询继续作为兜底，可见页每 30 秒，后台停轮询。
- 建议按房间展示设备卡片，未知房间归“未分配房间”；中枢/NAS 保留独立状态区。首期没有开关按钮、不可用的控制入口或虚构正常值。
- TV 断开 Atrium 时保留明确标旧的公开快照；配对撤销/401 清空。遥控焦点与返回行为保持现有规则。

## 6. 后续控制边界（本期不实现）

在读取接口之外增加独立 Controller，不在首期提前暴露 HTTP 写接口。未来调用路径为 TV → Atrium 权限和参数校验 → HA 动作 → 设备状态回读。

- 每台 TV 默认无控制权；权限同时限制设备与动作，不能透传任意 HA service。
- 操作 ID 与参数一起持久化去重；`accepted` 不等于 `confirmed`。超时显示结果未知并读回状态。
- 不承诺跨设备 exactly-once。使用 set-on/set-off 而非 toggle；喂食、启动清扫等非幂等动作禁止自动重试。
- 断网不排队补执行；恢复时仅同步状态。执行结果不明确时不伪造成功。
- ESPHome/MQTT 等设备只要提供已支持能力，复用现有投影；新能力或特殊动作需要显式扩展契约。

## 7. 分阶段开发任务

所有任务先写关键行为测试、确认失败，再实现并确认通过。各阶段形成独立提交；不把生产安装与代码提交混在一起。以下新文件路径是计划，迁移编号必须执行时重新确认。

### T0 — 环境兼容与部署评审（生产安装门槛）

交付：新建 `docs/ops/ha-iot-deployment.md` 及 `.en.md`，包含目标版本、兼容证据、套件影响、备份、维护窗口和恢复路径；仅合成路径。

1. 只读盘点 DSM、Engine、现有容器/卷/端口、空闲资源；不能输出环境变量里的秘密。
2. 核对官方升级路径及目标 Engine，确认现有 Photos/SMB/容器兼容和数据备份。
3. 明确 Mac↔NAS 地址、TLS 信任、网络访问策略及自启动；列出需用户参与的账号授权步骤。
4. 用户批准具体部署方案之后，才执行升级/安装；若不可行返回架构决策，不强行推进。

通过标准：目标版本和恢复策略明确；不是“套件可打开”就算兼容通过。

### T1 — HA 设备盘点（可与本地实现准备分离）

计划文件：`deploy/iot/compose.example.yaml`、`docs/ops/ha-iot-deployment{,.en}.md`。

1. 在满足 T0 的环境运行固定版本 HA，验证重启与备份恢复；不恢复 MiGPT Next。
2. 安装固定版本小米官方集成，由用户授权对应地区/家庭。
3. 盘点插座和加湿器的真实实体，核对开关语义、湿度单位及不可用状态；不采用功耗为验收指标。
4. 在米家 App 操作并观测 HA，记录 HA 侧延迟和脱敏能力清单。

通过标准：至少两类设备的所需状态在 HA 侧真实更新；不能以模拟数据替代实机结论。

### 2026-11-04 开发前接入清单

现有代码复核：`internal/config/config.go` 尚无 IoT 配置，`internal/family/house.go` 的环境投影仍为 `NotSupported`，`web/src/screens/HouseScreen.tsx` 仅显示环境占位状态；当前最高数据库迁移为 0017。

首批设备范围已于 2026-11-04 获用户确认：加湿器、温湿度计、鱼缸。不可用插座暂不作为闭环验收前提。

| 能力 | 首批展示内容 | 读取方式 |
| --- | --- | --- |
| 温湿度计 | 温度、湿度、电量 | 各独立 sensor 实体 |
| 加湿器 | 开关/工作状态、当前湿度、目标湿度、环境温度 | humidifier 状态及属性、独立 sensor |
| 鱼缸 | 水温、水泵状态、滤材剩余寿命 | 各独立 sensor 实体；不提供喂食/重置入口 |

实施前必须从 HA 实体详情读取真实 `entity_id`、单位和有效状态；不能根据设备名称猜测 ID。真实映射仅存私有运行配置，仓库示例用合成 ID。记录 `last_updated` 与采样时间的差异，避免把重新同步时间误写为传感器采样时间。

推荐继续使用独立 HA WebSocket 适配器：全量同步加事件订阅，统一能力映射后供房屋 API 读取。REST 轮询可用于诊断，但不作为第二套生产同步机制；TV 内嵌 HA 页面不符合既定权限和展示边界。

生产连接尚缺：可验证 HTTPS/WSS、专用 HA 非管理员账号及本地凭据文件、已核对实体白名单。HA 账号/token 不等于只读凭据；Atrium 客户端必须限制为读取消息，不能发送控制服务。禁止从浏览器会话提取 token 或要求用户把 token 粘贴到聊天。凭据在本机私有文件中交接，TV API 不下发凭据或 HA 原始地址。

代码阶段仍按 T2–T6 执行；有了真实实体与连接条件后再启用生产配置。T7 保留两类以上设备真实更新、断线恢复与 TV 延迟测试，当前读数截图不能替代这些验收。

### T2 — 统一模型和配置

新增：`internal/iot/types.go`、`normalize.go`、`normalize_test.go`。
修改：`internal/config/config.go`、`load.go`、`validate.go` 及对应测试、`config.example.yaml`。

1. 编写 unknown/unavailable、合法零值、单位转换、重复映射和秘密脱敏测试。
2. 实现上文类型及禁用默认值；现有无 `iot` 配置继续启动。
3. 新增只读接口 `SnapshotReader.Read(ctx)` 与适配器 `Run(ctx, sink)`；具体签名随编译确定，不能引入写动作或全功能 SDK。
4. 运行 `go test ./internal/config ./internal/iot`，预期通过。

### T3 — HA 订阅适配器

新增：`internal/iot/homeassistant/client.go`、`sync.go`、`client_test.go`、`sync_test.go`。

1. 使用 Go httptest 和现有 coder/websocket 建立合成 HA 服务。
2. 测试鉴权拒绝、快照/事件交错、旧连接晚到、实体移除、断线恢复、取消退出和带抖动退避。
3. 实现读消息白名单、消息大小/队列上限、超时和全量对账；队列溢出触发重同步。
4. `go test -race ./internal/iot/...`，预期通过；同时断言未发送 `call_service`，日志无 token。

### T4 — 存储及后台生命周期

新增：`internal/store/iot.go`、`iot_test.go`、`internal/app/iot.go`、`iot_test.go`。
新增迁移：`internal/store/migrations/0018_iot_observations.sql`（当前最大 0017；实施时重定编号）。
修改：store 迁移注册/版本门禁、`internal/app/runtime.go`、`lifecycle.go`、`internal/domain/enums.go`、`internal/app/events/coalesce.go` 及对应测试。

1. 测试原子快照替换、设备撤下、重启标旧、配置白名单过滤和旧代次拒绝。
2. 实现只存最新白名单状态的仓库及 worker 生命周期；网络失败不阻塞主服务。
3. 添加 `iot` topic；状态持久化后发布，去重高频无变化通知。
4. `go test -race ./internal/store ./internal/app/... ./internal/iot/...`，预期通过。

### T5 — House 投影和契约

修改：`internal/family/types.go`、`house.go`、`house_test.go`、`internal/httpapi/server.go`、`family_test.go`、`docs/api/openapi.yaml`、`docs/api/ws-protocol.md`、`internal/briefing/composer_test.go`。

1. 测试未配置、首次同步、部分实体不可用、HA 故障、授权撤销及 NAS 故障隔离。
2. 注入 SnapshotReader，新增 `iot` 公共 DTO；不能因 NAS 读取提前返回而遗漏独立 IoT 投影。
3. 确认 API schema、旧字段和首页投影兼容；按仓库现有方式补英文说明或独立对应文档。
4. `go test ./internal/family ./internal/httpapi ./internal/briefing ./internal/ws`，预期通过。

### T6 — TV 展示与刷新

修改：`web/src/types/api.ts`、`types/ws.ts`、`app/house.ts`、`app/dataChanged.ts`、`app/useConnection.ts`、`screens/HouseScreen.tsx`、`styles/home.css`、`test/houseFixture.ts` 及相关 mock。
测试：`web/src/app/house.test.ts`、`dataChanged.test.ts`、`houseLifecycle.test.tsx`、`screens/HouseScreen.test.tsx`。

1. 合成 fixture 覆盖开关关闭、湿度 0、未知、过期、部分失败和设备移除。
2. 实现房间卡片和来源状态，兼容无 `iot` 的响应，不增加控制按钮。
3. 添加 IoT 通知处理，保留授权代次、防晚到响应、后台停轮询和焦点稳定性。
4. `npm --prefix web test -- src/app/house.test.ts src/app/dataChanged.test.ts src/app/houseLifecycle.test.tsx src/screens/HouseScreen.test.tsx`。
5. `npm --prefix web run typecheck`、`npm --prefix web run lint`、`npm --prefix web run build`，均应通过。

### T7 — 综合验证与真实 TV 闭环

新增：`scripts/e2e/iot/` 合成测试工具、`docs/ops/evidence/<execution-date>-ha-iot/README.md` 和 `.en.md`。

1. 运行 `go test ./...`、`go vet ./...`、`npm --prefix web test`；按当前仓库发布要求运行 `make check`。缺少 lint 工具时明确记录并补齐，不声称完整检查通过。
2. 浏览器验证 1920×1080、3840×2160、804×384，长名称、多房间、滚动与遥控路径。
3. 按既有发布流程部署候选；TV 依次验证加湿器、温湿度计及鱼缸状态变化、离线、恢复、撤下设备与授权撤销；插座不作为首批门槛。
4. 分别测量设备→HA 和 HA→TV 延迟；HA→TV 目标 ≤5 秒，至少重复 10 次记录实际结果，失败要保留。
5. 验证通知丢失后 30 秒兜底刷新（另加请求耗时）；重连后完整同步；首次连接失败不影响照片和档案。
6. 经维护安排验证 HA 重启、Atrium 重启和主机重启后的恢复；区分登录后恢复和无人登录恢复。
7. 仅在真实闭环完成后更新 `README.md`、`README.en.md` 与实施状态文档；证据只入库脱敏结果。

## 8. 回退与完成标准

- IoT 功能开关默认关闭。故障时关闭 IoT 配置并重启 Atrium，恢复原房屋展示；不删除设备或 NAS 数据。
- 数据库迁移后不能直接运行不识别新 schema 的旧二进制。遵循现有 release schema guard，必要时恢复匹配备份；不能覆盖期间新增家庭数据。
- HA 回退使用匹配的镜像与升级前配置备份，不能只降级镜像而复用不兼容数据库。
- DSM 升级的恢复另行评审，不能以容器回退代替系统恢复。
- 完成必须同时具备：目标环境兼容、两类真实设备状态、TV 展示、故障恢复、权限清除、测试及脱敏证据。模拟测试通过仅代表软件阶段通过。

## 9. 评审清单

- [x] NAS 升级及 HA 部署已获授权并完成（2026-11-03）。
- [x] 首批改为加湿器、温湿度计和鱼缸（2026-11-04）。
- [ ] 按房间分组，支持未分配房间；配置文件管理映射。
- [ ] 独立 IoT 模块、最新状态存储、首期只读范围已确认。
- [ ] 网络/TLS、启动恢复、维护窗口和实机验收目标已确认。
- [x] 用户已授权按三类设备范围开始开发（2026-11-04）；生产凭据和实机验收单独完成。

## 10. 官方依据（2026-10-30 查阅，实施时复核）

- [HA Container 要求](https://www.home-assistant.io/installation/linux/#install-home-assistant-container)
- [群晖安装路径](https://www.home-assistant.io/installation/alternative)
- [小米官方集成](https://github.com/XiaoMi/ha_xiaomi_home/blob/main/doc/README_zh.md)
- [插座 3 功耗问题报告，不是所有固件的定论](https://github.com/XiaoMi/ha_xiaomi_home/issues/1788)
- [HA WebSocket API](https://developers.home-assistant.io/docs/api/websocket/)
- [HA REST API](https://developers.home-assistant.io/docs/api/rest/)
- [Xiaomi BLE](https://www.home-assistant.io/integrations/xiaomi_ble/)
- [ESPHome](https://www.home-assistant.io/integrations/esphome/)、[蓝牙代理](https://esphome.io/components/bluetooth_proxy/)
- [体脂秤](https://esphome.io/components/sensor/xiaomi_miscale/)
- [MQTT](https://www.home-assistant.io/integrations/mqtt/)、[REST](https://www.home-assistant.io/integrations/rest/)
