# macOS archive parser (T06)

This is a local Foundation / ImageIO / CoreGraphics / PDFKit / Vision helper and a
pure Go process adapter. It extracts evidence; it does not classify people or
financial/health domains, import documents, publish facts, or wire a durable task
consumer. T07 and T09 own those steps. Existing `archive.Parse` behavior is unchanged.

## Build and use

Requires the existing Apple Command Line Tools and macOS with Vision Accurate
revision 3 plus `zh-Hans` and `en-US`. No Swift modules or downloaded dependencies
are needed. Tested on macOS 15.2 (24C101), Apple clang 16, arm64.

```sh
adapters/macos-archive-parser/build.sh /absolute/trusted/path/atrium-native-parser
```

Trusted application configuration supplies that absolute regular executable path
(no symlinks). The authorized caller opens a **fresh** immutable original using
`BlobStore.OpenVerified`, calls the following adapter, and closes its file after
return. The adapter borrows the descriptor exclusively until it returns; it never
closes it, changes its offset, reopens `File.Name`, or accepts an original path.

```go
result, err := (archive.NativeParser{HelperPath: configuredAbsolutePath}).Parse(
    ctx, archive.NativeSource{File: verifiedFile, SHA256: canonicalOriginalSHA256},
)
```

The process receives original FD 3 through `exec.Cmd.ExtraFiles`, the single fixed
argument `--protocol=1`, fixed small stdin config `{"protocol":1}`, and only PATH /
LANG / LC_ALL. It uses `fstat` + bounded `pread`, then data APIs. The helper never
opens an original path. `NativeParserError.Code` exposes only fixed codes:
`parser_unavailable`, `parser_invalid_source`, `parser_canceled`, `parser_timeout`,
`parser_output_limit`, `parser_protocol`, or `parser_failed`. Cancellation/deadline
errors also unwrap to the context error. stderr, NSError, arguments, and recognized
text are not interpolated into errors. Missing platform/helper/revision/languages
produce `parser_unavailable`; originals remain owned by the caller.

The adapter is synchronous and admits **one helper process globally**. Its 120s
total deadline includes waiting for that slot, execution, and evidence grouping;
callers may shorten it. It is not a per-page timeout. CommandContext kills the Unix
process group on cancellation. After its one Wait, the adapter also kills any
remaining group members (including descendants holding inherited pipes after the
leader exits), before releasing the slot. WaitDelay is 500ms. Stdout is capped at
8 MiB and stderr at 64 KiB; either overflow cancels execution. No custom unbounded
pipe-reader goroutines are used. Linux builds retain the API and return unavailable.

## Evidence and protocol

Only one complete protocol-v1 JSON object is accepted after successful process
exit. All wire fields are mandatory. Unknown/duplicate fields, null scalar values,
wrong array lengths, malformed Unicode escapes, trailing objects/text, unexpected
versions, nonfinite/out-of-range rectangles, wrong page indexes, and inconsistent
counts/coverage are rejected. The helper emits source observations; Go groups them
and produces the existing ParseResult status/segments/counts/coverage plus optional
`native` metadata and `visual` evidence. T05 JSON remains unchanged when those
optional fields are absent.

- `locator.kind=page`, 1-based page, display-top-left normalized `[x,y,w,h]`.
  An image is one page. `native.pages` saves actual raster width/height, original
  EXIF orientation, PDF rotation, effective crop, and actual six-element CTM.
- Every alternative retains text, origin (`native` or `ocr`), its own rectangle,
  OCR confidence, and the native page-space bounds plus clipped flag. Confidence
  zero for native text means not applicable, not zero extraction accuracy.
- Any substantial overlap (intersection >=25% of the smaller rectangle) forms a
  connected group, including ambiguous one-to-many or repeated same-origin
  observations. A unique native/OCR pair confirms only with IoU >=0.65 and equal
  text after whitespace removal. Digits, decimal points, minus signs, punctuation,
  case and confusable scripts are never normalized to agree.
- Conflict/ambiguous groups have `requires_review=true`, empty preferred `text`,
  and every original reading in `visual.alternatives`. A native-only or clipped
  group is `native_unconfirmed` and requires review. OCR-only regions survive.
  Neither status `parsed` nor `confirmed` means OCR is error free.
- Sorting is stable geometry order, **not semantic reading order** or table/unit
  inference. A later consumer must use locations and review state. IDs hash source,
  engine-version fingerprint, group position and evidence; no Vision UUID is used.
  OS/helper/settings changes change the fingerprint and IDs. Actual repeated local
  runs are tested, but cross-OS OCR reproducibility is not promised.
- The fingerprint records source/build-script/compiler/SDK build hash, OS build,
  helper version, Vision revision, languages, Accurate and language correction off.
  Geometry/grouping policy is scoped by `archive-native/1`.
- No character offsets are emitted. PDFKit's internal `selectionForRange` is
  UTF-16 code units; it is not exported as T05 Unicode scalar or original byte offsets.

Every readable PDF page is rasterized and sent through OCR, including native-only,
scanned-body/native-footer and blank pages. Native text is extracted per line.
`isLocked`, not `isEncrypted`, determines blocked encryption; no password is accepted.
`allowsCopying=false` is metadata and does not prevent render/OCR. CoreGraphics draws
page content; annotations and form values are explicitly `excluded`. Presence of
annotations/widgets makes coverage partial. Per-page failures retain successful
page evidence and `partial`; all completed blank pages become `empty/no_text_detected`.

PDF rasterization uses only CGPDFPageGetDrawingTransform (extra rotation 0) plus
CGContextDrawPDFPage. E is CropBox ∩ MediaBox; page Rotate swaps displayed dimensions.
An unflipped bitmap gets explicit bounded scale before concatenating the drawing
transform. That actual CTM maps all four corners of each native bounds rectangle;
then `(minX/W, 1-maxY/H, width/W, height/H)` places it in the OCR frame. Entirely
outside-crop lines are excluded. Partially clipped lines retain full native text
and original page bounds but carry a clipped visible rectangle and review flag.
The helper does not subtract crop origin or apply rotation twice, and never mixes
PDFPage.drawWithBox into this transform.

## Resource limits and scope

| Resource | Limit |
|---|---:|
| Original regular file | 32 MiB |
| Image input pixels / maximum edge | 60,000,000 / 16,384 |
| Decoded page pixels / maximum edge | 16,000,000 / 4,096 |
| PDF pages | 100 (preflight, before page OCR) |
| Retained source-observation text | 2 MiB UTF-8 |
| Result text including preferred text and all alternatives | 2 MiB UTF-8 |
| Source observations / result groups | 20,000 / <=20,000 |
| stdout / stderr | 8 MiB / 64 KiB |

ImageIO caching is disabled before type/frame/dimension/EXIF preflight. Positive,
finite, integral dimensions and division-based pixel limits precede thumbnail
decode. ImageIO applies EXIF transforms and Vision receives Up. Multiframe formats
are unsupported; the helper never calls the first frame a complete document.
The allowlist is PNG, JPEG, WebP, single-page TIFF, GIF, HEIC/HEIF and BMP as ImageIO
provides them. Actual format validation here covers PNG/JPEG/WebP and multi-TIFF
rejection; other allowlisted codecs are not claimed tested. Traditional Chinese
has not been validated and is not a configured language.

PDF pages use individual autorelease pools. Input/raster limits **do not create a
hard bound on PDFKit/ImageIO/Vision internal allocations**. Very complex or malicious
PDFs can still exhaust framework resources; process failure/deadline is a failed
parse, never a successful empty result. The initial benchmark below supports the
120s default for these small fixtures, not a promise for 100 complex pages.

## Reproduce native verification (synthetic data only)

Use the bundled Python, with Pillow/pypdf/reportlab, solely to generate/evaluate
fixtures. It is not a Go runtime dependency. On this host:

```sh
PY=/Users/ditu/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3
"$PY" adapters/macos-archive-parser/generate_fixtures.py /tmp/atriumos-t06-fixtures
adapters/macos-archive-parser/build.sh /tmp/atriumos-native-helper
adapters/macos-archive-parser/build.sh /tmp/atriumos-fixture-renderer --fixture-renderer
ATRIUM_NATIVE_HELPER=/tmp/atriumos-native-helper \
ATRIUM_NATIVE_FIXTURES=/tmp/atriumos-t06-fixtures \
ATRIUM_NATIVE_EVIDENCE=/tmp/atriumos-t06-evidence \
go test ./internal/archive -run TestNativeActual -count=1 -v
"$PY" adapters/macos-archive-parser/evaluate_fixtures.py \
  /tmp/atriumos-native-helper /tmp/atriumos-fixture-renderer \
  /tmp/atriumos-t06-fixtures /tmp/atriumos-t06-visual
```

The test renderer is a separately compiled binary with a distinct build fingerprint.
It shares the actual decode/raster implementation, and only this build accepts an
output directory and writes raster PNGs. Do not configure the renderer as the
production helper. Production builds contain no raster output path. Overlay blue
boxes are OCR, red are native; public Go results are saved separately. All originals,
wire results, output results and overlays remain synthetic. Fonts are existing macOS
Arial Unicode / Helvetica; package versions and original hashes are in the manifest
and recorded summary. PDF generation uses invariant mode except pypdf encryption,
whose salt may vary. Full OCR snapshots are diagnostic evidence, not golden tests.

The committed `evidence/` contains a bounded representative original, selected
actual overlays and the run summary; the complete local run remains under the
paths above. Availability-gated tests skip without both environment variables;
that skip is not native verification. Run them on an actual supported Mac.

Go failure tests independently exercise positional FD inheritance with an unlinked
original, unchanged offset/ownership, environment isolation, stable IDs, strict
protocol/Unicode/geometry/count validation, exact conflict/one-to-many grouping,
text budgets, output flood, hanging/abnormal/half JSON helpers, cancellation and
no surviving descendant processes. Native fixture tests additionally verify 19
field rectangles across 14 image variants (266 relationships), every PDF page's
coverage, the physical transform's four corners and inverse, crop intersection,
clipped text and non-BMP text. These are controlled reports, not real document
accuracy or table-association claims.

## Recorded run — 2026-10-04

Final helper build fingerprint:
`170fbb014fb3582957b6e40b4f5d36ee7d46b3a32e59ab58c2e64af31c6c8c7a`.
OS build `24C101`, Vision revision 3, zh-Hans/en-US, Accurate, language correction
off. Fixture generator versions: Pillow 12.3.0, pypdf 6.10.0, reportlab 4.4.9.

- 33/33 actual native document outcomes passed, including partial-page coverage,
  excluded annotation/widget coverage, copy restrictions, and blank/no-text PDF.
- 266/266 exact whitespace-only field matches passed their original-region IoU
  test across 14 image variants; minimum measured IoU 0.459 (threshold 0.4).
- Four rotated nonzero-crop pages and the crop-beyond-media intersection passed
  four-corner/inverse/raster-frame checks. The four-page native/OCR line pairs had
  IoU 0.852–0.949; all eight intact pairs agreed on this engine build. Four clipped
  lines remained `native_unconfirmed`. The separate invisible-native `9.80` versus
  raster `9.30` fixture returned one review group, no preferred text, both readings.
- Repeated actual report parses produced identical evidence IDs.
- Helper-only 33-file warm run: median 0.302s, maximum 0.446s, total 7.881s. This
  excludes Go startup, fixture generation and overlay rendering; no cold-cache,
  full 100-page workload or hard-memory claim is made.
- Visually inspected report original, EXIF7 overlay, rotated PDF 90-degree overlay,
  and crop-intersection overlay using the image viewer; text direction and both
  source rectangles aligned, with visibly cropped text marked for review.

Fresh verification completed successfully:

```sh
# Native helper, real fixtures, public API and subprocess failure tests:
ATRIUM_NATIVE_HELPER=/tmp/atriumos-native-helper \
ATRIUM_NATIVE_FIXTURES=/tmp/atriumos-t06-fixtures \
ATRIUM_NATIVE_EVIDENCE=/tmp/atriumos-t06-evidence \
go test ./internal/archive -run TestNative -count=1
# Repository regressions, race coverage and cross-platform compile:
go test ./...
go test -race ./internal/archive -count=1
GOOS=linux GOARCH=amd64 go test -c ./internal/archive -o /tmp/atriumos-t06-linux.test
git diff --check
```

The actual fixture suite completed in 17.606s; archive race run completed in
12.293s. The process tests verified no surviving child PID after timeout or after
an exited leader left a descendant holding its pipes. No production configuration,
database, household document, credential, network provider or deployment was used.
