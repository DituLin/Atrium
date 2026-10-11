# Synthetic XLSX F01 analysis fixture

This committed fixture is copied byte-for-byte from the independent review's synthetic `/tmp/t12-spec-f01.xlsx`. It contains no real family data.

The worksheet has charges 128.10 and 71.90 CNY, separate TEST-BILL-001/002 identifiers, an example payee, distinct transaction dates, and a labelled total with original formula `SUM(B2:B3)` and historical cache 200.00. The real archive parser/chunker generates four chunks; the repository regression validates and merges every chunk before publication and analysis.

The cache is retained evidence, never a second expense. Tests also revise current amounts, hide members, withdraw the document, and replace formula/reference/currency inputs in memory. The fixture and independent `/tmp` inputs themselves are not modified.
