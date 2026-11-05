# 今日页面的数据来源

中文 | [English](today-data.en.md)

本文说明「今日」万年历与天气的数据来源。KOI 黄历已接入；和风天气的服务端适配和页面展示已实现，真实账户与地点尚待配置。发布验证状态见文末；之前的界面发布记录见[今日与房屋界面](../tv-today-house-ui.md)。

## 万年历

「今日」使用 [KOI](https://github.com/DituLin/Koi) 的通用农历表、节气表与黄历规则，来源固定为提交 `f973da1b07a8bc3a387c202fd1a134f24924a3fc`。纯 TypeScript 模块离线计算农历、年/月/日干支、节气、周历及完整宜忌，不请求外部黄历服务，也不读取 KOI 的个人记录。页面标注 `KOI 黄历 · koi-almanac/1.0`。

数据覆盖公历 **1901-01-01 至 2099-12-31**。先从家庭时区取得公历日期，再用日序运算，避免浏览器所在时区或夏令时改变日期。农历年按春节转换；黄历年柱与生肖按**立春所在的公历日**转换，月柱按十二个「节」所在的公历日转换，均不在交节的具体时刻切换。节气时间采用 KOI 原始 UTC+8 表，精确到分钟；进度条按公历日计算。

宜忌来自 KOI 的版本化简化传统规则，包含建除及神煞等规则，冲突时以忌优先；它不是某一出版机构逐日核定的黄历。范围边界缺少前后节气时，页面明确保留缺失状态，不混入近似节气。范围外只保留公历信息，并提示 KOI 支持范围。

原表、规则和测试样例保留来源提交及 SHA-256；移植通过 KOI 原有样例的一致性检查。样例一致性说明移植符合 KOI 算法，不代表全部宜忌经独立权威核定。

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

## 2026-11-05 发布验证

KOI 实现提交为 `3f9e24c`。签名 Core `koi-almanac-3f9e24c+c06c6a591e9a33c6a93bc0def15d3e29061f9d07` 已部署，OnePlus 加载 `0.1.0+3f9e24c`，完整宜忌、年/月/日柱和来源版本与 KOI 原始 reference 相符。遥控导航、确定刷新、房屋只读观测与诊断检查通过，验收后恢复档案页。尚未完成本次 TCL 实机验收。

完整前端 98 个文件、1,135 项测试，lint、类型检查、构建及 Chrome 66 输出检查通过；实际候选源码完整 Go 回归及天气/配置 race 检查通过。独立检查覆盖 72,684 个日期与 4,776 条节气；12 个代表日期完成 1920 与 804 宽度的长文本、闰月和范围边界检查。签名候选另通过 8 组后端和 8 组实际嵌入页面验收。

部署保留 schema 18、现有配置与服务身份、业务资料、NAS 来源及照片身份、屏幕配对和 2 GiB 按需预览策略。上述验收不代表真实和风账户已启用：专属 Host、认证文件与地点仍待配置，当前天气配置保持原状。
