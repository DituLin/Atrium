# 今日页面的数据来源

中文 | [English](today-data.en.md)

本文说明代码中的接入方式。2026-11-05 的 KOI 数据补充与和风天气接入尚未完成正式配置及上线验收；已有页面的历史发布记录见[今日与房屋界面](../tv-today-house-ui.md)。

## 万年历

现有公历、农历、年与日干支、节气及周历按家庭时区计算。宜忌和月干支需要经过核对的黄历来源，不能从 UI 示例生成。KOI 源码尚未取得时，页面保留当前缺失状态，不把项目规格文档当作黄历数据库。取得实际实现后，应核对其依赖、许可、纪年、节气换月及换日约定，再说明复用的数据范围。

## 和风天气

和风天气由 Core 请求，TV 使用现有天气 widget。当前适配坐标版 Weather v1 的实时天气、24 小时预报和五天预报。QWeather 天气代码转换为现有 WMO 图标代码；未知天气保留未知状态。相对湿度从比例转换为百分比，风速从 m/s 转换为 km/h；缺失或单位不符的数值不填零。可选预报失败时仍保留有效的当前天气。

设置 `widgets.weather.provider: qweather`，填写实际坐标、地点名与专属 API Host。`qweather.timezone` 留空时使用 `home.timezone`；不同于家庭时区的天气地点应显式设置其 IANA 时区。API Host 只填主机名，不填 URL 路径。示例均为占位配置：

```yaml
widgets:
  weather:
    enabled: true
    provider: qweather
    latitude: 30.20
    longitude: 120.10
    location_label: 示例城区
    refresh_interval: 30m
    stale_after: 2h
    qweather:
      api_host: example.qweatherapi.com
      timezone: Asia/Shanghai
      api_key_file: /path/to/secrets/qweather-api-key
```

API Key 文件只包含一行密钥，由运行 Core 的用户拥有、权限为 `0600`。Core 通过 `X-QW-Api-Key` 请求头认证；文件内容不会进入配置输出或 TV 数据。这个文件独立于既有 `--provider-env-file`，不向只接受 DeepSeek 配置的文件追加内容。

JWT 可替代 API Key。将 `api_key_file` 留空，并在 `qweather.jwt` 填入 `developer_id`、`project_id`、`credential_id` 和 `private_key_file`。私钥是由 Core 用户拥有的 `0600` Ed25519 PKCS8 PEM 文件。Core 自动签发短期 JWT，无需手动保存会过期的 token。配置字段见[配置示例](../../config.example.yaml)，认证信息见[和风官方文档](https://dev.qweather.com/docs/configuration/authentication/)。

页面显示和风天气与供应商返回的来源声明。抓取时间与观测时间分开：只有响应明确包含观测时间时才显示该时间，不把刷新时刻当作观测时刻。缓存绑定来源与地点，切换配置不会把旧来源缓存重新标为和风天气。供应商限流或认证失败时退避请求；已有读数按其过期规则显示，缺失预报继续保留空态。

完成真实接入需要专属 Host、有效认证文件和确认的天气地点。接口实现与合成测试通过，不代表账户权限、额度或真实地点数据已验收。[实时天气](https://dev.qweather.com/docs/api/weather/weather-current/)、[小时预报](https://dev.qweather.com/docs/api/weather/weather-hourly-forecast/)、[每日预报](https://dev.qweather.com/docs/api/weather/weather-daily-forecast/)均按官方当前契约适配。
