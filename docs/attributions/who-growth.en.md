# WHO growth reference data and calculations

[中文](who-growth.md)

The Archives screen uses daily LMS numeric parameters from the World Health Organization (WHO) 2006 Child Growth Standards to calculate length/height and weight P3, P50 and P97 reference curves locally. The data ships with the frontend; viewing an archive sends no personal profile information to WHO or other external services.

## Sources and attribution

Data attribution and standard-setting organization: World Health Organization. The official entry pages are [length/height-for-age](https://www.who.int/tools/child-growth-standards/standards/length-height-for-age) and [weight-for-age](https://www.who.int/tools/child-growth-standards/standards/weight-for-age), under “Expanded tables for constructing national health cards.” These four original workbooks were retrieved on **2026-10-21**. Each contains 1857 daily parameter rows, covering days 0–1856.

| Indicator | Sex | Original file | Original XLSX SHA-256 |
| --- | --- | --- | --- |
| Length/height | Boys | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/length-height-for-age/expandable-tables/lhfa-boys-zscore-expanded-tables.xlsx?sfvrsn=7b4a3428_12) | `c4b1c9029ab9751a5f0888e32f35c7c0287a16d361885cf911ecf23b3f7f6b4f` |
| Length/height | Girls | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/length-height-for-age/expandable-tables/lhfa-girls-zscore-expanded-tables.xlsx?sfvrsn=27f1e2cb_10) | `6aa2876319449a6b1f4d825848128902114ff53c67b92b86a0c5140846013059` |
| Weight | Boys | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/weight-for-age/expanded-tables/wfa-boys-zscore-expanded-tables.xlsx?sfvrsn=65cce121_10) | `b5b4748c6bfa5230e2eddafa1767629c349178b08d457f400b59422b8bfef86c` |
| Weight | Girls | [WHO XLSX](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/weight-for-age/expanded-tables/wfa-girls-zscore-expanded-tables.xlsx?sfvrsn=f01bc813_10) | `ee3ae12cb96c6c5541cdf43665c03ce6c984f877859a183a5f6104eb06a49a6e` |

The repository extracts factual numeric parameters only. It does not bundle the original workbooks, WHO images or extended prose. The formula implementation was written independently without copying third-party programs. The project code license makes no claim about the licensing scope of the original WHO material or WHO endorsement of this project.

## Parameters and methods

`web/src/screens/family/whoGrowthData.ts` extracts the `Day`, `L`, `M` and `S` columns. Each day stores integer values `[L × 10000, M × 10000, S × 100000]`, divided by their respective scales on lookup. Extraction checks every reconstructed value against the source with an absolute difference below `1e-12`. It does not sample by month or omit daily rows. Tests pin SHA-256 hashes of all four complete parameter sequences and check sex-specific birth, approximately twelve-month and final-day anchors.

Age is the integer difference between the observation date in the household timezone and the literal birth date. The month axis uses `age in days ÷ 30.4375`. Days 0–730 use recumbent length; days 731–1856 use standing height. The chart separates the two bases at their boundary. The age limits and conversion follow [WHO expanded-table instructions](https://cdn.who.int/media/docs/default-source/child-growth/child-growth-standards/indicators/instructions-en.pdf?sfvrsn=5cec8c61_23).

Reference values use `M × (1 + L × S × z)^(1/L)`, or `M × exp(S × z)` when `L = 0`. P3 and P97 use z values `−1.8807936081512509` and `+1.8807936081512509`; P50 is `M`. Observations are converted using the confirmed posture and units before calculating z. Percentiles use an independently implemented numerical integral of the standard normal density. Exact percentiles are shown only for `−3 ≤ z ≤ 3`; outside that range the curve and median difference remain available with a notice that the percentile was not extrapolated. The method follows [chapter 7 of the WHO 2006 methods report](https://iris.who.int/bitstream/handle/10665/43413/924154693X_eng.pdf?sequence=1).

When the confirmed measurement posture differs from the age-specific standard, the calculation follows the `0.7 cm` correction described in the [WHO/UNICEF measurement methodology](https://iris.who.int/bitstream/handle/10665/324791/9789241515559-eng.pdf?ua=1). To preserve the recorded observations, the plotted reference curves are converted to the recorded posture with an explicit note. Without confirmed posture, age-specific references may still be drawn, while length/height percentiles, median differences and range comparisons remain unavailable.

## Display boundaries

- The birth date, sex and associated person must come from confirmed public archive information; names, titles and measurements are never used to infer them. The posture setting must match the measurement series.
- Supported length/height units are `cm`, `m`, `mm` and their Chinese equivalents. Supported weight units are `kg`, `g` and their Chinese equivalents. Reference and measured values share the same displayed unit. Other units or ages outside the supported range produce no WHO comparison.
- Without measurements, the screen shows references and current age only; measured values, recorded growth and percentiles remain empty. Incomplete records or unsafe numbers do not establish a latest value suitable for comparison.
- If any historical observation precedes birth in the household calendar, WHO comparisons are withheld with an explanation. Original records and the date-based measured curve remain unchanged.
- Recorded growth over the recent three-month window compares two actual observation dates. Standard growth over the same period is the difference between P50 values on those exact dates. It is not the WHO growth velocity standard or an assessment of individual growth.
- P3–P97 is a reference interval. The interface does not equate it with a diagnosis or a conclusion of normal growth.
