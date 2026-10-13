# macOS archive parser (T06)

This is a local Foundation / ImageIO / CoreGraphics / PDFKit / Vision helper and a
pure Go process adapter. It extracts evidence; it does not classify people or
financial/health domains, import documents, publish facts, or wire a durable task
consumer. T07 and T09 own those steps. Existing `archive.Parse` behavior is unchanged.

## Build and use

Requires the existing Apple Command Line Tools and macOS with Vision Accurate
revision 3 plus `zh-Hans` and `en-US`. The repository bundles pinned arm64
libjpeg-turbo 3.1.4.1, libwebp 1.6.0 and its sharpyuv dependency; the build needs
no download, package installation or Swift modules. Tested on macOS 15.2
(24C101), Apple clang 16, arm64. The native build fails on other architectures.

```sh
adapters/macos-archive-parser/build.sh /absolute/trusted/path/atrium-native-parser
```

The build also copies the three unchanged codec libraries into a fixed sibling
`lib/` directory. Relocate that complete bundle together. The helper hashes every
library against compiled SHA-256 pins before loading the fixed absolute paths;
missing, altered or incompatible libraries produce `parser_unavailable`. No Codex
Python installation or environment-selected codec is needed at runtime. Preserve
the vendored library signatures/bytes when signing the helper, or sign libraries
first, update the reviewed pins and rebuild the helper. License notices and exact
source/binary provenance are in `vendor/libjpeg-turbo/PROVENANCE.md` and
`vendor/libwebp/PROVENANCE.md`. The build copies their accompanying notices into
`lib/licenses/`; distribute the whole `lib/` directory with the helper.

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
text are not interpolated into errors. Missing platform/helper/revision/languages/codecs
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
- The fingerprint records source/build-script/public-ABI-headers/codec-bytes/compiler/SDK
  build hash, OS build,
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
| JPEG progressive virtual coefficient budget / scan cap | 512 MiB / 128 |
| PNG or RIFF chunk count | 65,536 |

ImageIO caching is disabled before type/frame/dimension/EXIF preflight. Positive,
finite, integral dimensions and division-based pixel limits precede thumbnail
decode. ImageIO applies EXIF transforms and Vision receives Up. Multiframe formats
are unsupported; the helper never calls the first frame a complete document.
The allowlist is PNG, JPEG, WebP, single-page TIFF, GIF, HEIC/HEIF and BMP as ImageIO
provides them. PNG/JPEG/WebP have independent compressed-completion validation.
BMP/GIF/single-TIFF/HEIC/HEIF keep usable OCR but report
`partial/image_integrity_unverified`, `coverage.complete=false`, and
`coverage.skipped_features=["image_integrity_validation"]`. Every retained segment
from these unverified images has `visual.requires_review=true`, included before its
stable ID is hashed. T07 consumers must inherit that flag; model output cannot
clear it. Blank or recovered no-letter images in these formats remain partial,
never `empty/complete`. Structural failures and multiframe inputs retain their
invalid/unsupported outcomes. Actual BMP/GIF/TIFF/HEIC extraction fixtures exercise
this policy; HEIF shares the conservative path but has no separate fixture.
Traditional Chinese has not been validated and is not a configured language.

A nonnil ImageIO thumbnail or a “complete” ImageIO status does not establish
compressed completeness. Before thumbnail/OCR, PNG validation checks chunk CRCs,
required ordering, zlib checksum/end, exact decompressed scanline lengths, filters
and Adam7 passes. It streams through a 32 KiB output buffer, without unfiltering
or allocating a full pixel plane. JPEG validation uses the mature libjpeg decoder,
fails on recovery warnings, drains all 1/8-scaled scanlines and requires successful
finish; appended or metadata-embedded EOI bytes cannot substitute for this check.
WebP requires exact RIFF/chunk/padding framing and successful libwebp full decode
with a four-byte, 1×1 external scaled output. Reframing truncated VP8/VP8L data
therefore cannot make ImageIO recovery count as complete. These checks establish
codec completion, not authenticity or absence of every possible pixel alteration.

Detected malformed JPEG/PNG/WebP data returns `invalid/invalid_input`, with
incomplete coverage and no OCR evidence. JPEG allocation/backing-store or scan limits return
`limit_exceeded/jpeg_resources`; WebP allocation errors return
`limit_exceeded/webp_resources`. Unsupported JPEG precision/components return
`unsupported/unsupported_jpeg_precision`. The original descriptor remains owned
by the caller. Valid baseline/progressive/grayscale/CMYK JPEG, PNG and WebP retain
the usual complete workflow.

PDF pages use individual autorelease pools. Input/raster limits **do not create a
hard bound on PDFKit/ImageIO/Vision or codec internal allocations**. JPEG's virtual
coefficient budget is advisory, not total RSS; lossless WebP may allocate
source-size internal structures even with scaled output. Very complex or malicious
PDFs can still exhaust framework resources; process failure/deadline is a failed
parse, never a successful empty result. The initial benchmark below supports the
120s default for these small fixtures, not a promise for 100 complex pages.

## Reproduce native verification (synthetic data only)

Use the bundled Python, with Pillow/pypdf/reportlab, solely to generate/evaluate
fixtures, plus the existing `/usr/bin/sips` ImageIO encoder for a synthetic HEIC
positive. These are not Go runtime dependencies. On this host:

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
xcrun clang -Wall -Wextra -Werror -O2 \
  adapters/macos-archive-parser/test_codec_resources.c -o /tmp/atriumos-codec-resources
/tmp/atriumos-codec-resources /tmp/lib/atrium-libjpeg-3.1.4.1.dylib \
  /tmp/atriumos-t06-fixtures/integrity/valid-large-progressive.jpg
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

The committed `evidence/` contains bounded representative originals, selected
actual overlays, public results, the run summary and integrity/policy summaries; the
complete local run remains under the paths above. Availability-gated tests skip without both environment variables;
that skip is not native verification. Run them on an actual supported Mac.

Go failure tests independently exercise positional FD inheritance with an unlinked
original, unchanged offset/ownership, environment isolation, stable IDs, strict
protocol/Unicode/geometry/count validation, exact conflict/one-to-many grouping,
text budgets, output flood, hanging/abnormal/half JSON helpers, cancellation and
no surviving descendant processes. Native fixture tests additionally verify 19
field rectangles across 14 image variants (266 relationships), every PDF page's
coverage, the physical transform's four corners and inverse, crop intersection,
clipped text and non-BMP text. These are controlled reports, not real document
accuracy or table-association claims. Integrity regressions also retain valid
format variants, a 36 MP progressive JPEG, forged/embedded JPEG EOI, PNG CRC/zlib/
scanline/Adam7 cases, and WebP truncations with both outer and inner framing
repaired. The relocation test copies the helper plus all three libraries into an
isolated directory, parses JPEG and WebP, and verifies typed unavailability when
each dependency is removed or modified. The C resource test distinguishes the
same valid progressive JPEG failing at 64 MiB from succeeding at 512 MiB; it also
checks the ABI error maps to unavailability without diagnostic text. Nine additional
unvalidated-format policy fixtures cover valid BMP/GIF/single-TIFF/HEIC extraction,
blank and quarter/half-truncated BMPs, a broken BMP header and animated GIF. The
Go protocol test covers partial results with zero evidence, explicit limitation
metadata, review inheritance and its effect on stable evidence identity.

## Recorded run — 2026-10-04

Final helper build fingerprint:
`9c9c4ac321b6debbf9909e42469671f4ee6f726f557a7712afb6e657828cdf1c`.
OS build `24C101`, Vision revision 3, zh-Hans/en-US, Accurate, language correction
off. Fixture generator versions: Pillow 12.3.0, pypdf 6.10.0, reportlab 4.4.9.
The HEIC positive uses the existing `sips-311` encoder; its encoded bytes may vary
with the OS. Actual original hashes are recorded in `unvalidated-summary.json`.

- 33/33 actual native document outcomes passed, including partial-page coverage,
  excluded annotation/widget coverage, copy restrictions, and blank/no-text PDF.
- 38/38 additional integrity cases passed: 26 damaged JPEG/PNG/WebP inputs
  returned invalid with incomplete coverage; all 12 valid variants remained complete.
  The 36 MP progressive JPEG completed as a blank image in 0.701s. Original
  half-JPEG/half-PNG review probes and fully reframed VP8/VP8L truncations were
  independently reproduced as RED before their respective repairs, then GREEN.
- 9/9 unvalidated-format policy cases passed: seven retain partial coverage
  (including zero-evidence blank/recovered BMPs), broken BMP remains invalid and
  animated GIF remains unsupported. Useful OCR stays present with review required.
- 266/266 exact whitespace-only field matches passed their original-region IoU
  test across 14 image variants; minimum measured IoU 0.459 (threshold 0.4).
- Four rotated nonzero-crop pages and the crop-beyond-media intersection passed
  four-corner/inverse/raster-frame checks. The four-page native/OCR line pairs had
  IoU 0.852–0.949; all eight intact pairs agreed on this engine build. Four clipped
  lines remained `native_unconfirmed`. The separate invisible-native `9.80` versus
  raster `9.30` fixture returned one review group, no preferred text, both readings.
- Repeated actual report parses produced identical evidence IDs.
- Helper-only 33-file run: median 0.308s, maximum 0.454s, total 8.023s. The
  separate 38-case integrity run took 4.669s of helper execution. This
  excludes Go startup, fixture generation and overlay rendering; no cold-cache,
  full 100-page workload or hard-memory claim is made.
- Visually inspected report original, EXIF7 overlay, rotated PDF 90-degree overlay,
  and crop-intersection overlay using the image viewer; text direction and both
  source rectangles aligned, with visibly cropped text marked for review.

Fresh verification completed successfully after the integrity repair:

```sh
# Real fixtures, public API and process failures:
ATRIUM_NATIVE_HELPER=/tmp/atriumos-native-helper \
ATRIUM_NATIVE_FIXTURES=/tmp/atriumos-t06-fixtures \
ATRIUM_NATIVE_EVIDENCE=/tmp/atriumos-t06-evidence \
go test ./internal/archive -run TestNative -count=1
# Repository regressions, race coverage and cross-platform compile:
go test ./... -count=1
go test -race ./internal/archive -count=1
GOOS=linux GOARCH=amd64 go test -c ./internal/archive -o /tmp/atriumos-t06-linux.test
git diff --check
```

The final native/process suite passed in 25.938s, covering all 80 actual fixture
outcomes. Repository regressions passed separately; the archive race run passed
in 12.314s. Both unchanged external review probes passed using their hardcoded
`/tmp/atriumos-native-helper-spec-review`, rebuilt with the complete sibling bundle:

```sh
go test -overlay=/tmp/atriumos-t06-spec-probes/overlay.json ./internal/archive \
  -run TestSpecReviewTruncatedImage -count=1 -v
go test -overlay=/tmp/atriumos-t06-spec-probes/repair/bmp-overlay.json ./internal/archive \
  -run TestSpecReviewBMPCoverage -count=1 -v
```

The permanent regressions live in `TestNativeActualImageIntegrity` and
`TestNativeActualUnvalidatedImages`; external review overlays are not needed to
reproduce them. The resource/ABI test above
passed, as did isolated relocation and each missing/modified dependency case.
The process tests verified no surviving child PID after timeout or after
an exited leader left a descendant holding its pipes. No production configuration,
database, household document, credential, network provider or deployment was used.

### Production single-page preview protocol (T14)

The normal helper also accepts `{"protocol":1,"preview_page":N}` on stdin,
with the immutable, hash-verified original on FD 3. `N` is an integer from 1
through 100. This production mode does not invoke Vision OCR or write files.
It returns `protocol`, `page`, `total_pages`, and a base64 PNG on stdout, with
an 8 MiB response / 6 MiB PNG limit. Image EXIF orientation and PDF crop/media
intersection and rotation use the same renderer as extraction. The Go boundary
checks PNG dimensions (4096 maximum edge, 16 million pixels), limits the child
to 20 seconds, shares the one-process semaphore, and kills descendants on cancel.
Encrypted PDFs, unsupported formats, malformed sources and out-of-range pages
return unavailable. The fixture-only raster-output directory is not used.

Core only exposes `GET|HEAD /api/v1/archives/documents/{id}/versions/{revision}/pages/{page}`.
The household, current active screen and current public document closure are
server-derived. Authorization is checked again after rendering, with `no-store`
responses. Documents containing private child content cannot serve a whole-page
raster unless a separately verified redacted artifact is implemented; no such
artifact is claimed here. No original path, blob hash, bearer token or arbitrary
output filename appears in a preview URL.

Run the production preview checks with the rebuilt helper:

```sh
sh adapters/macos-archive-parser/build.sh /tmp/atriumos-t14-preview/helper
ATRIUM_T14_PREVIEW_HELPER=/tmp/atriumos-t14-preview/helper \
ATRIUM_NATIVE_FIXTURES=/tmp/atriumos-t14-native-fixtures \
go test ./internal/archive -run '^TestNativePreview' -count=1 -v
```

The optional fixture directory is generated by `generate_fixtures.py`; it adds
locked/restricted/encrypted, over-100-page and malformed PDF rejection checks.
The committed report PNG and four-page cropped/rotated PDF always exercise
actual production raster output when the helper opt-in is supplied.
