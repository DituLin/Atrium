# Archive parser v1 contract

`Parse(ctx, ParseSource{Reader, Size, SHA256, FormatHint}, ParseLimits{})`
accepts an already authorized and verified original. The caller supplies a canonical
SHA-256 identifying those bytes. The parser deliberately does not re-authorize,
import/store, open paths, mutate originals, make network requests, or classify
people/domains. ReaderAt errors and cancellation propagate as errors (as do invalid
caller arguments). A blocking ReaderAt must be interrupted by its owner/deadline.

Inspect `status` and `coverage.complete`, not just the error. `parsed`/`empty`
mean complete within the documented body/cell extraction scope. `partial` retains
reliable supported evidence plus warnings about skipped content. `invalid`,
`limit_exceeded`, `unsupported`, and `encrypted` are document outcomes with nil
error; any prefix evidence is incomplete. Never publish their segments as complete
facts. All output fields have explicit JSON names. IDs hash original SHA-256,
parser version, and locator; they are stable within a version, not across versions.

## Supported evidence

- Text: strict UTF-8 (optional BOM), UTF-16LE/BE with BOM, validated surrogate
  pairs. No encoding guessing/replacement decoding. A single segment preserves
  every decoded CRLF, space, and repeated event; a half-open Unicode scalar range
  refers to decoded text, not original bytes. Maximum segment text is 256 KiB by
  default. A later consumer needing <=24 KiB chunks must reference the parent
  segment plus Unicode scalar subrange; it must not silently truncate the segment.
- CSV: comma only, strict quotes, streamed logical records after bounded decoding.
  Each field is a segment, including explicit empties. Missing fields in ragged
  rows are not synthesized; ragged records produce `partial`. Locators give
  one-based logical record/column and decoded-input physical line/byte column via
  Go `encoding/csv.FieldPos`. CRLF in fields becomes LF; blank physical lines are
  ignored, per that library. No formula interpretation, trimming, or zero filling.
- DOCX: Transitional namespace URI, body order, paragraphs, runs/tabs/breaks,
  table cells and nested tables. Paragraph ordinals are local to body/cell.
  Table paths alternate table ordinal, parent row, parent grid column, nested
  table ordinal. Grid spans and vertical merges retain anchors, row/column spans
  and continuation flags; merged values are not copied. Each cell paragraph is
  emitted once with its cell locator, never additionally as a flattened cell fact.
  Deleted/moved-from revision text is excluded; insertion text is marked partial.
  Headers/footers, notes, comments, images, textboxes, fields, legacy horizontal
  merge markup and unknown content are explicitly skipped/partial.
- XLSX: workbook sheet IDs/names/state and r:id relationships, sparse A1 cells,
  shared/inline/rich strings, numeric/boolean/error/ISO date values; hidden
  sheet/row/column metadata; merge ranges without copied anchor values. Absent
  cells have no segment. Explicit blank, empty string, zero, missing formula
  cache, and present-but-empty cache remain distinct. Formula text and all
  attributes (including shared-formula group IDs) are retained verbatim after
  XML decoding, along with historical raw caches. Nothing is calculated,
  refreshed, expanded, executed or fetched.
  Cell style index, xf attributes, numFmt ID/code survive. Displays only support
  `General`, `@`, `0`, `0.00`, `0%`, `0.00%`, `yyyy-mm-dd`, `mm-dd-yy` under bounded
  numeric ranges. Date displays accept whole serial days, use the workbook's
  1900/1904 system without adding a timezone, and explicitly warn that serial 60
  in the 1900 system is fictional `1900-02-29`. Serial zero maps to the system's
  arithmetic epoch. Fractional/negative/out-of-range dates, unknown formats, and
  unsupported style inheritance retain raw values and produce partial coverage.
  Date arithmetic is not an Excel formatting/locale emulator.

## Package safety and coverage

Identity comes from `[Content_Types].xml`, root relationships and main-part
content type. Main and dependent parts need not have fixed names/extensions.
Only common Transitional DOCX/XLSX are supported. Strict OOXML, macro-enabled
main parts, binary workbooks, ZIP64 and unsupported ZIP methods are unsupported.
Compound-file magic alone returns `unsupported_compound_container`, never an
unsupported claim that the file is encrypted. ZIP encryption flags produce
`encrypted_zip`; this detects declared ZIP encryption, not cipher correctness.
Worksheet protection is not file encryption. PDF/OCR is a later parser.

Before archive/zip allocation, bounded directory bytes/entry counts are checked.
Directories, symlinks/nonregular entries, duplicate or normalized-colliding names,
traversal, escaping relationships and corrupt metadata are rejected. Internal
`../` relationships are allowed when they resolve inside the package. External
relationships produce partial coverage and are never fetched. Store/Deflate only;
nested archives stay unexpanded. All package parts are read to end under actual
expansion budgets and explicit CRC/size checks, including unused parts. Local
headers, central records and data descriptors must agree. XML rejects DTDs,
custom entities, duplicate attributes and oversized depth/nodes/strings. XML
encoding follows the same UTF-8/UTF-16 BOM whitelist.

`read_parts` means CRC-checked bytes. `skipped_parts` means no semantic extraction.
Known presentation/application metadata (styles, themes, settings, core properties)
can remain in that inventory without reducing body/cell coverage. Unknown parts,
including producer template custom XML, do reduce coverage. Warnings are deduped
static codes/messages; diagnostic text does not interpolate source text or paths.
`part_uri` in warnings is currently empty; exact part URIs live in evidence and
coverage, which are internal, untrusted document content.

## Limits and actual counts

Zero values select defaults; positive overrides are available to trusted callers.
Source/part/expanded/text byte overrides cannot exceed 1 GiB.

| Resource | Default |
|---|---:|
| Original source | 32 MiB |
| ZIP entries | 4,096 |
| One expanded part | 16 MiB |
| Total actual expanded bytes | 64 MiB |
| Decoded text and emitted text (separate counters) | 2 MiB each |
| XML nesting / total XML nodes | 64 / 200,000 |
| Retained string tokens plus emitted segments | 100,000 |
| One retained string / one segment | 256 KiB |
| Cells / segments | 100,000 each |
| Relationships | 8,192 |
| Compression ratio | 1,000 |
| Normalized part-name length (fixed) | 1,024 bytes |
| XLSX merge ranges (fixed) | 4,096 |

Source bytes count actual ReaderAt bytes, once. Expanded bytes count actual
inflated bytes across all parts, not ZIP declarations; a failed actual limit can
read one probe byte over budget. XML nodes/strings/cells/relationships count
visited items, including an item that triggers a limit. XML decoded text counts
retained character-data and attribute-value bytes; emitted text counts segment
text. XML is materialized only inside the bounded package/XML budgets. The parser
is synchronous, uses standard-library codecs, and adds no dependencies.
