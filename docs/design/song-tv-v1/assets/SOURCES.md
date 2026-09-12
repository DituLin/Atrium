# 本地示例资产与字体

四幅 SVG（landscape / portrait / panorama / courtyard）为本次原型用代码创作的原创几何插画，不含家庭照片、人物、真实地理地点或网络素材。它们只验证构图、比例与状态；不能替代真实照片色彩和远距离可读性验收。

字体来自 Google Fonts 官方仓库，2026-09-12 获取；原始 SIL Open Font License 1.1 分别完整保存在 `notoserifsc-OFL.txt` 与 `notosanssc-OFL.txt`。

| 文件 | 来源 | 来源 blob | 本地 SHA-256 |
| --- | --- | --- | --- |
| title.woff2 | [Noto Serif SC](https://github.com/google/fonts/tree/main/ofl/notoserifsc) / NotoSerifSC[wght].ttf | eab063faf229160a52d3760f5555150e4eb9e5bf | 6c19d533f0d17c87713b9ae7190b25a79674af038b5d724b14399eecca7fe565 |
| body.woff2 | [Noto Sans SC](https://github.com/google/fonts/tree/main/ofl/notosanssc) / NotoSansSC[wght].ttf | fb0637bafbcd804fe32152370a1225990745b4bc | 44e5d7d5120431731b4743e052bdbaaefc08d60aa7e3b341e62cdc2f9bc19fb9 |

处理：fontTools 4.65.0，静态化 weight 400；按 GB2312 常用字集合与 U+0020–00FF、U+2000–206F、U+3000–303F、U+FF00–FFEE 子集化，输出 WOFF2。确切请求字符集合保存在 `subset-unicodes.txt`。此子集不保证覆盖所有动态私人标题、生僻字、繁体字或 emoji；CSS 提供系统中文与 serif/sans-serif 回退。字体应只标注为本地子集，不声称完整中文覆盖。

原型只通过本地相对路径加载字体和 SVG，不请求 Google Fonts 在线服务。字体组合：Noto Serif SC 标题、Noto Sans SC 正文和时间。字体来源与许可已核验；真实 WebView 的加载性能、字形回退和渲染仍需 M2 验证。
