# HA IoT 部署与接入

[English](ha-iot-deployment.en.md)

## 交付状态

截至 2026-11-04，NAS 上的 HA 和小米官方集成已部署，三类首批设备已完成读数及实体标识盘点。专用非管理员账号的令牌认证及三类设备 API 读取已验证。候选适配器已通过真实 HA 的 CA 校验及 WSS 全量同步，13 项能力全部可用；TLS 配置已由管理员确认保留；持久化、House API 和 TV 卡片已实现。经用户批准，系统级服务隔离验证通过后已切换正式服务，程序仍以普通用户运行。生产 HA 已连接、13/13 能力可用，两个 NAS 来源在线；OnePlus 实机房屋页与方向键滚动已验证，完整动态与恢复验收尚未完成。详见[验证记录](evidence/2026-11-04-ha-iot/README.md)。导入设备不等于所有实体可用，静态读数不等于持续更新验证。

已验证版本：DSM 7.4.1-90080 Update 1、Container Manager 24.0.2-1706、Docker Engine 24.0.2、HA 2026.10.4、小米官方集成 v0.5.0。硬件实测为 DS220+、10 GB RAM。此记录不代表其他型号或未来版本的兼容性保证。

## 部署边界

- HA 负责协议和厂商认证，NAS 本地目录挂载为 `/config`；IoT 不通过 SMB 交换数据。
- Mac 上运行 Atrium；TV 仅连接 Atrium。照片共享挂载独立于 HA。
- 使用[固定摘要 Compose 示例](../../deploy/iot/compose.example.yaml)，首次云接入没有 USB、D-Bus 或 privileged 权限。
- 配置目录应限制访问；不要将认证文件、真实实体列表、账号或截图提交到仓库。示例中的 `/srv/ha` 是占位路径。
- 配置备份只包含 HA 配置及部署描述，不复制照片共享、媒体或整个存储卷。备份包含秘密，须保存在受控目录。
- MiGPT Next 保持停止，不因部署 HA 恢复其他已停止容器。

## Atrium 接入准备

1. 在 HA 创建专用非管理员账号，仅限本地访问。普通用户凭据不能视为只读凭据；Atrium 客户端必须只发送读消息。
2. 通过专用账号生成长期访问令牌，直接保存至 Mac 私有文件（例如 `/private/atrium/ha.token`，权限 0600）。不要发送到聊天或写入 YAML。
3. 建立 HTTPS/WSS 端点及信任链。可使用受信任反向代理或 HA 原生 TLS，证书须匹配实际访问主机名/IP。客户端配置 `ca_file` 时将指定 CA 加入系统信任根，不关闭证书校验。
4. 从 HA 实体详情读取实际 ID、单位及属性；在私有配置中映射温湿度计温度/湿度/电量、加湿器电源/湿度/温度、鱼缸水温/水泵/滤材。不要以设备昵称猜测 ID。
5. 检查未知、不可用、关闭和合法零值显示，并分别记录源更新时间、最近同步时间。缺少真实采样时间时不显示“刚测量”。
6. 候选版本通过测试后，按[发布流程](deployment-release.md)备份 Atrium 数据库并部署。新增 schema 后不能直接回退旧二进制；恢复时配套恢复兼容数据库。

HA 2026.10.4 的 HTTP/TLS 参数由“设置 → 系统 → 网络”管理。更改会重启 HA，并需要管理员在限定时间内确认。安装前必须再次核对目标版本文档。

## 验收与回退

- 至少两类真实设备状态更新抵达 TV；分别记录设备→HA 与 HA→TV 延迟，后者目标不超过 5 秒。
- 验证 HA 断线、重连、Atrium 重启、配置撤下设备及屏幕撤权；照片和档案服务不应因 HA 故障不可用。
- 默认关闭 IoT。遇到问题时关闭接入配置，保留 NAS 设备及数据；遵守数据库 schema 回退门禁。
- HA 降级使用配套配置备份；不承诺 DSM 原地降级。Mac 登录后启动不代表无人登录恢复。NAS 重启后照片共享自动重挂载仍需单独验收。

## 依据

- [HA Container 安装](https://www.home-assistant.io/installation/linux/)
- [HA HTTP/TLS 配置](https://www.home-assistant.io/integrations/http/)
- [HA WebSocket API](https://contributors.home-assistant.io/core/internals/websocket-api/)
- [小米官方集成](https://github.com/XiaoMi/ha_xiaomi_home)
