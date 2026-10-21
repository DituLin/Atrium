# WHO 生长参考数据来源与计算

[English](who-growth.en.md)

档案页使用世界卫生组织（WHO）2006 年儿童生长标准的每日 LMS 数值参数，在本地计算身长／身高及体重的 P3、P50、P97 参考线。数据随前端离线发布，查看档案时不会向 WHO 或其他外部服务发送人物资料。

## 来源与归属

数据归属与标准制定机构：World Health Organization。官方入口为[年龄别身长／身高](https://www.who.int/tools/child-growth-standards/standards/length-height-for-age)和[年龄别体重](https://www.who.int/tools/child-growth-standards/standards/weight-for-age)中的 “Expanded tables for constructing national health cards”。以下四个原始工作簿于 **2026-10-21** 获取；每个包含第 0–1856 天，共 1857 行参数。

| 指标 | 性别 | 原始文件 | 原始 XLSX 的 SHA-256 |
| --- | --- | --- | --- |
| 身长／身高 | 男童 | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/length-height-for-age/expandable-tables/lhfa-boys-zscore-expanded-tables.xlsx?sfvrsn=7b4a3428_12) | `c4b1c9029ab9751a5f0888e32f35c7c0287a16d361885cf911ecf23b3f7f6b4f` |
| 身长／身高 | 女童 | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/length-height-for-age/expandable-tables/lhfa-girls-zscore-expanded-tables.xlsx?sfvrsn=27f1e2cb_10) | `6aa2876319449a6b1f4d825848128902114ff53c67b92b86a0c5140846013059` |
| 体重 | 男童 | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/weight-for-age/expanded-tables/wfa-boys-zscore-expanded-tables.xlsx?sfvrsn=65cce121_10) | `b5b4748c6bfa5230e2eddafa1767629c349178b08d457f400b59422b8bfef86c` |
| 体重 | 女童 | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/weight-for-age/expanded-tables/wfa-girls-zscore-expanded-tables.xlsx?sfvrsn=f01bc813_10) | `ee3ae12cb96c6c5541cdf43665c03ce6c984f877859a183a5f6104eb06a49a6e` |

仓库只提取事实型数值参数，不收录原始工作簿、WHO 图像或长段文字；公式实现由项目独立编写，未复制第三方程序。项目代码的许可证不用于声明 WHO 原始资料的授权范围，亦不表示 WHO 对本项目的认可。

## 参数与方法

`web/src/screens/family/whoGrowthData.ts` 按工作簿 `Day`、`L`、`M`、`S` 列提取数据。每一天依次保存整数 `[L × 10000, M × 10000, S × 100000]`，读取时除回对应倍率。提取时逐项核验源值与还原值差异小于 `1e-12`；没有按月抽样或丢弃每日行。测试固定四个完整参数序列的 SHA-256，并核验男女童出生、约 12 月龄及末日的参数。

年龄按家庭时区中的观测日与出生日期计算整日差。月龄轴使用 `日龄 ÷ 30.4375`。第 0–730 天采用卧位身长标准，第 731–1856 天采用站立身高标准；图形在两种标准的交界处分段。日龄范围和转换依据 [WHO 扩展表说明](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/instructions-en.pdf?sfvrsn=5cec8c61_23)。

参考值使用 LMS 公式 `M × (1 + L × S × z)^(1/L)`；`L = 0` 时使用 `M × exp(S × z)`。P3／P97 的 z 分别为 `−1.8807936081512509`／`+1.8807936081512509`，P50 为 `M`。观测值先根据确认的姿势和单位转换，再计算 z。百分位通过独立实现的标准正态密度数值积分获得，仅在 `−3 ≤ z ≤ 3` 时显示计算百分位；范围外保留参考线及中位数差值，提示未外推百分位。方法依据 [WHO 2006 方法报告第 7 章](https://iris.who.int/bitstream/handle/10665/43413/924154693X_eng.pdf?sequence=1)。

如果已确认测量姿势与该日龄的标准姿势不同，按照 [WHO／UNICEF 测量方法](https://iris.who.int/bitstream/handle/10665/324791/9789241515559-eng.pdf?ua=1)的 `0.7 cm` 规则换算。为保留实测值，图中的参考线会换算至记录的姿势并标明换算说明。姿势未确认时仍可显示年龄对应的参考线，但不显示身长／身高的百分位、中位数差值或区间判断。

## 展示边界

- 出生日期、性别及所属人物必须来自经过确认且获准 TV 展示的人物资料；不会依据姓名、标题或测量值猜测。姿势设置应对应当前测量序列。
- 支持身长／身高的 `cm`、`m`、`mm` 和对应中文单位，以及体重的 `kg`、`g` 和对应中文单位。参考值与实测值使用同一单位。其他单位或适用日龄之外不生成 WHO 对照。
- 没有实测记录时，只显示参考线和当前年龄，实测值、实测增长及百分位留空。记录不完整或存在不安全数值时，不宣称已获得可对照的最新值。
- 若家庭时区中存在早于出生日期的历史记录，WHO 对照暂停并显示原因；原始记录和日期曲线不作修改。
- “近 3 个月已记录增长”只比较窗口内真实记录的两个日期；“标准同期增长”使用同样两个日期的 P50 差值。后者不是 WHO 生长速度标准，也不构成个体生长评估。
- P3–P97 是参考区间，不在界面中等同于诊断或“正常”的结论。
