# Synthetic archive parser corpus

All people, organizations, addresses, bills and reports here are fictional. Do not
replace these with real family files. `generate.py` uses Python's standard library
and fixed ZIP metadata. `generate_native.py` additionally uses python-docx 1.2.0
and openpyxl 3.1.5 (versions recorded in `native-producers.txt`); the generated
files are checked in, so Go tests have no Python/runtime dependency. To regenerate
the producer controls locally, use the bundled Python containing those libraries.

| Fixture | Required evidence/status |
|---|---|
| `feeding.txt` | UTF-8 exact CRLF and two separate `喂养70ml` occurrences; 张明/周青 roles remain raw text |
| `home.txt` | 测试苑1号测试室 / DEMO-AC / 2028-05-31, exact decoded range |
| `school.txt` | 示例学校运动会2026年10月; no manufactured personal identity |
| `f02.csv` | TEST-BILL-001 128.10 CNY and TEST-BILL-003 20.00 USD; record3/column3 physical row3/byte21 |
| `e01.docx` | `/content/main.xml`; paragraph1 student李妍/DOB2012-11-06/2026春季; table[1] row1,col2=88/100 and row2,col2=91/100 |
| `merges.docx` | anchor row1,col1 span2x2, row2 continuation, nested path[1,2,3,1], run text `A\tB\nC` |
| `partial.docx` | current body preserved, deleted text excluded, header/textbox/image/revision/external-link warnings |
| `e02.xlsx` | sheet7 成绩 `/sheets/data.xml`, rich shared A1=学生李妍, A1:B1 merge, C3=132,D3=150; huge dimension allocates no grid |
| `f01.xlsx` | 128.10+71.90 CNY; SUM cache200.00; A10 blank, B10 empty string, C10 zero, D10 missing, E10 formula no cache, F10 empty cache; F11/F12 shared group7; hidden row/col, booleans/errors/dates; unknown format means partial |
| `f01-missing-cache.xlsx` | B4 formula retained, cache absent, no invented total |
| `dates-1900.xlsx` / `dates-1904.xlsx` | serial0/59/60/61, both date systems; fictional1900-02-29 warning only in1900 system |
| `native-health.docx` | Independent python-docx control: 6 segments, `/word/document.xml` table[1] row2,col2,paragraph1=周青. Partial for untouched template customXml, not lost body text |
| `native-home.xlsx` | Independent openpyxl control: parsed, sheet家庭设备, B1 address, B2 model, B3 raw46904/styleyyyy-mm-dd/display2028-05-31, A5:B5 merge, C7 zero/hidden row |
| `badpackages/*` | Invalid paths/escaping relationships/DTD/truncation; unsupported wrong main type and unproven compound container |

Go tests derive single-condition adversarial variants in memory: duplicate names
and percent-normalized collisions, symlinks, CRC and local-header/data-descriptor
mismatches, under-declared expansion, encrypted ZIP flags, Strict/macro main
parts, bad shared indexes, duplicate XML attributes/value containers, unsupported
style inheritance, UTF-16 XML, unused footnotes and nested packages, arbitrary
main-part extension, and every configurable resource class. Tests assert exact
segments, locations, raw values, cache/merge/missing states and warning codes.

`parse_review_test.go` adds in-memory revision/BOM/dispatch/rounding regressions:
deleted rows/cells with surviving physical coordinates; revised outer merges and
unrevised nested merges; property revision coverage; BOM-bearing identity,
relationship, main and worksheet parts; `PK` prose versus corrupt ZIP signatures;
and exact positive/negative decimal ties, exponent/percent scaling and untouched
formula cache/raw values.

`xlsx_quality_test.go` derives exact-date fraction/sign/range regressions with
untouched formula caches, integer decimal/exponent controls in both date systems,
repeated column groups and conflicting versus agreeing overlaps, duplicate
recognized singleton containers, and incompatible inline cell representations.
Invalid structural cases assert status before looking for cell evidence; rejected
ambiguous containers must not manufacture a selected value.
