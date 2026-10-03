package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeActualFixtures(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	dir := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("run build.sh and generate_fixtures.py; set ATRIUM_NATIVE_HELPER and ATRIUM_NATIVE_FIXTURES")
	}
	for _, name := range []string{"report.png", "report.jpg", "report.webp", "physical-90.png", "physical-180.png", "physical-270.png", "exif-1.jpg", "exif-2.jpg", "exif-3.jpg", "exif-4.jpg", "exif-5.jpg", "exif-6.jpg", "exif-7.jpg", "exif-8.jpg", "native.pdf", "scanned.pdf", "footer.pdf", "mixed.pdf", "conflict.pdf", "geometry.pdf", "crop-intersection.pdf", "nonbmp.pdf", "restricted.pdf", "blank.png", "bad.png", "multi.tiff", "oversize.png", "locked.pdf", "101-pages.pdf", "partial.pdf", "blank.pdf", "bad.pdf", "annotations.pdf"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := (NativeParser{HelperPath: helper}).Parse(context.Background(), NativeSource{f, hex.EncodeToString(sum[:])})
			if err != nil {
				t.Fatal(err)
			}
			want := ParseParsed
			switch name {
			case "partial.pdf", "annotations.pdf":
				want = ParsePartial
			case "blank.png", "blank.pdf":
				want = ParseEmpty
			case "bad.png", "bad.pdf":
				want = ParseInvalid
			case "multi.tiff":
				want = ParseUnsupported
			case "oversize.png", "101-pages.pdf":
				want = ParseLimitExceeded
			case "locked.pdf":
				want = ParseEncrypted
			}
			if evidence := os.Getenv("ATRIUM_NATIVE_EVIDENCE"); evidence != "" {
				if err := os.MkdirAll(evidence, 0700); err != nil {
					t.Fatal(err)
				}
				b, _ := json.MarshalIndent(r, "", "  ")
				if err := os.WriteFile(filepath.Join(evidence, name+".result.json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if r.Status != want {
				t.Fatalf("status %s want %s %+v", r.Status, want, r.Warnings)
			}
			if name == "101-pages.pdf" && r.Native.TotalPages != 101 {
				t.Fatal("page preflight did not report actual page count")
			}
			if name == "partial.pdf" && (r.Native.TotalPages != 2 || r.Native.ProcessedPages != 1 || len(r.Segments) == 0) {
				t.Fatal("partial page coverage lost reliable evidence")
			}
			if name == "annotations.pdf" && (r.Native.Annotations != "excluded" || r.Native.Forms != "excluded") {
				t.Fatal("annotations/forms coverage hidden")
			}
			if name == "restricted.pdf" && (!r.Native.Encrypted || r.Native.AllowsCopying) {
				t.Fatal("copy restriction metadata lost")
			}
			if want != ParseParsed {
				return
			}
			all := ""
			for _, s := range r.Segments {
				for _, a := range s.Visual.Alternatives {
					all += a.Text + "\n"
				}
			}
			if name == "nonbmp.pdf" {
				if !strings.Contains(all, "😀") {
					t.Fatal("non-BMP native text lost")
				}
				return
			}
			if name == "conflict.pdf" {
				found := false
				for _, s := range r.Segments {
					if s.Visual.RequiresReview && len(s.Visual.Alternatives) >= 2 {
						found = true
					}
				}
				if !found {
					t.Fatalf("native/OCR conflict was not grouped: %+v", r.Segments)
				}
				return
			}
			if name == "geometry.pdf" || name == "crop-intersection.pdf" {
				for _, s := range r.Segments {
					for _, a := range s.Visual.Alternatives {
						if strings.Contains(a.Text, "OUTSIDE") {
							t.Fatal("outside crop text retained")
						}
						if a.Clipped && !s.Visual.RequiresReview {
							t.Fatal("clipped line confirmed")
						}
					}
				}
				return
			}
			for _, field := range []string{"1988-03-12", "2026-10-04", "1234.50", "-2.75", "130-175"} {
				if !strings.Contains(all, field) {
					t.Errorf("missing field %s", field)
				}
			}
			if name == "footer.pdf" {
				if !strings.Contains(all, "林明") {
					t.Fatal("native footer suppressed scan body OCR")
				}
			}
			if name == "mixed.pdf" && r.Native.ProcessedPages != 3 {
				t.Fatal("did not OCR every page")
			}
		})
	}
}

// Check original field placement, rather than only recognizing strings. EXIF
// fixtures all display upright; physical rotations require a rotated rectangle.
func TestNativeActualFieldGeometry(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	dir := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("native fixture environment required")
	}
	var manifest struct {
		Fields []struct {
			Text string `json:"text"`
			Rect Rect   `json:"rect"`
		} `json:"image_fields"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"report.png", "report.jpg", "report.webp", "physical-90.png", "physical-180.png", "physical-270.png", "exif-1.jpg", "exif-2.jpg", "exif-3.jpg", "exif-4.jpg", "exif-5.jpg", "exif-6.jpg", "exif-7.jpg", "exif-8.jpg"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := (NativeParser{HelperPath: helper}).Parse(context.Background(), NativeSource{f, hex.EncodeToString(sum[:])})
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range manifest.Fields {
				expected := field.Rect
				switch name {
				case "physical-90.png":
					expected = Rect{1 - expected[1] - expected[3], expected[0], expected[3], expected[2]}
				case "physical-180.png":
					expected = Rect{1 - expected[0] - expected[2], 1 - expected[1] - expected[3], expected[2], expected[3]}
				case "physical-270.png":
					expected = Rect{expected[1], 1 - expected[0] - expected[2], expected[3], expected[2]}
				}
				best := 0.0
				for _, s := range r.Segments {
					for _, a := range s.Visual.Alternatives {
						if whitespaceText(a.Text) == whitespaceText(field.Text) {
							area := intersectArea(a.Rect, expected)
							iou := area / (a.Rect[2]*a.Rect[3] + expected[2]*expected[3] - area)
							if iou > best {
								best = iou
							}
						}
					}
				}
				if best < .4 {
					t.Errorf("field %q does not match its source region; IoU %.3f", field.Text, best)
				}
			}
		})
	}
}
func TestNativeActualPDFTransform(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	dir := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("native fixture environment required")
	}
	for _, name := range []string{"geometry.pdf", "crop-intersection.pdf"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := (NativeParser{HelperPath: helper}).Parse(context.Background(), NativeSource{f, hex.EncodeToString(sum[:])})
			if err != nil {
				t.Fatal(err)
			}
			clipped := 0
			native := 0
			for _, p := range r.Native.Pages {
				crop := *p.EffectiveCrop
				tr := *p.Transform
				det := tr[0]*tr[3] - tr[1]*tr[2]
				minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, corner := range [][2]float64{{crop[0], crop[1]}, {crop[0] + crop[2], crop[1]}, {crop[0], crop[1] + crop[3]}, {crop[0] + crop[2], crop[1] + crop[3]}} {
					x, y := corner[0]*tr[0]+corner[1]*tr[2]+tr[4], corner[0]*tr[1]+corner[1]*tr[3]+tr[5]
					minX = math.Min(minX, x)
					minY = math.Min(minY, y)
					maxX = math.Max(maxX, x)
					maxY = math.Max(maxY, y)
					ix, iy := (tr[3]*(x-tr[4])-tr[2]*(y-tr[5]))/det, (-tr[1]*(x-tr[4])+tr[0]*(y-tr[5]))/det
					if math.Abs(ix-corner[0]) > 1e-8 || math.Abs(iy-corner[1]) > 1e-8 {
						t.Fatal("inverse transform failed")
					}
				}
				if math.Abs(minX) > 1e-6 || math.Abs(minY) > 1e-6 || math.Abs(maxX-float64(p.Width)) > 1e-6 || math.Abs(maxY-float64(p.Height)) > 1e-6 {
					t.Fatalf("crop did not map to raster: %v", p)
				}
			}
			for _, s := range r.Segments {
				for _, a := range s.Visual.Alternatives {
					if a.Origin != "native" {
						continue
					}
					native++
					if a.Clipped {
						clipped++
						if !s.Visual.RequiresReview {
							t.Fatal("clipped confirmed")
						}
						continue
					}
					best := 0.0
					for _, b := range s.Visual.Alternatives {
						if b.Origin == "ocr" {
							area := intersectArea(a.Rect, b.Rect)
							iou := area / (a.Rect[2]*a.Rect[3] + b.Rect[2]*b.Rect[3] - area)
							best = math.Max(best, iou)
						}
					}
					if best < .75 {
						t.Errorf("native/OCR physical mismatch %q %.3f", a.Text, best)
					}
				}
			}
			if clipped != len(r.Native.Pages) || native != 3*len(r.Native.Pages) {
				t.Fatalf("missing crop evidence: native=%d clipped=%d pages=%d", native, clipped, len(r.Native.Pages))
			}
		})
	}
}

func TestNativeActualRepeatIdentity(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	dir := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("native fixture environment required")
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.png"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	f, err := os.Open(filepath.Join(dir, "report.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	source := NativeSource{f, hex.EncodeToString(sum[:])}
	p := NativeParser{HelperPath: helper}
	a, err := p.Parse(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	bResult, err := p.Parse(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Segments) != len(bResult.Segments) {
		t.Fatal("nondeterministic segment count")
	}
	for i := range a.Segments {
		if a.Segments[i].ID != bResult.Segments[i].ID {
			t.Fatal("nondeterministic evidence IDs")
		}
	}
}

func TestNativeActualImageIntegrity(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	dir := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("native fixture environment required")
	}
	files, err := os.ReadDir(filepath.Join(dir, "integrity"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(dir, "integrity", entry.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(b)
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			r, err := (NativeParser{HelperPath: helper}).Parse(context.Background(), NativeSource{f, hex.EncodeToString(hash[:])})
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(entry.Name(), "valid-") {
				if !r.Coverage.Complete {
					t.Fatalf("valid image lost complete coverage: %s", r.Status)
				}
			} else if r.Coverage.Complete || r.Status == ParseParsed || r.Status == ParseEmpty {
				t.Fatalf("damaged image declared complete: %s with %d segments", r.Status, len(r.Segments))
			}
			if strings.HasPrefix(entry.Name(), "valid-") && strings.HasSuffix(entry.Name(), ".jpg") && entry.Name() != "valid-large-progressive.jpg" {
				all := ""
				for _, s := range r.Segments {
					all += s.Text
				}
				if !strings.Contains(all, "1988-03-12") {
					t.Fatal("valid JPEG lost expected original text")
				}
			}
			if _, err := f.ReadAt(make([]byte, 1), 0); err != nil {
				t.Fatal("original descriptor lost")
			}
		})
	}
}

func TestNativeActualRelocatedCodecBundle(t *testing.T) {
	helper := os.Getenv("ATRIUM_NATIVE_HELPER")
	fixtures := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || fixtures == "" {
		t.Skip("native fixture environment required")
	}
	dir := t.TempDir()
	copied := filepath.Join(dir, "helper")
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(copied, data, 0700); err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(filepath.Join(fixtures, "report.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sourceBytes)
	f, err := os.Open(filepath.Join(fixtures, "report.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	source := NativeSource{f, hex.EncodeToString(sum[:])}
	parser := NativeParser{HelperPath: copied}
	unavailable := func(t *testing.T) {
		t.Helper()
		_, err := parser.Parse(context.Background(), source)
		var e *NativeParserError
		if !errors.As(err, &e) || e.Code != "parser_unavailable" {
			t.Fatalf("missing/incompatible codec got %v", err)
		}
	}
	unavailable(t)
	libdir := filepath.Join(dir, "lib")
	if err = os.Mkdir(libdir, 0700); err != nil {
		t.Fatal(err)
	}
	codecs := map[string][]byte{}
	for _, name := range []string{"atrium-libjpeg-3.1.4.1.dylib", "atrium-libwebp-1.6.0.dylib", "libsharpyuv.0.dylib"} {
		codecs[name], err = os.ReadFile(filepath.Join(filepath.Dir(helper), "lib", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(libdir, name), codecs[name], 0500); err != nil {
			t.Fatal(err)
		}
	}
	result, err := parser.Parse(context.Background(), source)
	if err != nil || !result.Coverage.Complete {
		t.Fatalf("relocated bundle failed: %v %s", err, result.Status)
	}
	// WebP must decode using the copied transitive dependency as well.
	webpBytes, err := os.ReadFile(filepath.Join(fixtures, "report.webp"))
	if err != nil {
		t.Fatal(err)
	}
	webpFile, err := os.Open(filepath.Join(fixtures, "report.webp"))
	if err != nil {
		t.Fatal(err)
	}
	defer webpFile.Close()
	webpSum := sha256.Sum256(webpBytes)
	result, err = parser.Parse(context.Background(), NativeSource{webpFile, hex.EncodeToString(webpSum[:])})
	if err != nil || !result.Coverage.Complete {
		t.Fatalf("relocated WebP bundle failed: %v %s", err, result.Status)
	}
	for name, bytes := range codecs {
		t.Run(name, func(t *testing.T) {
			lib := filepath.Join(libdir, name)
			if err := os.Remove(lib); err != nil {
				t.Fatal(err)
			}
			unavailable(t)
			poisoned := append([]byte(nil), bytes...)
			poisoned[len(poisoned)/2] ^= 1
			if err := os.WriteFile(lib, poisoned, 0700); err != nil {
				t.Fatal(err)
			}
			unavailable(t)
			if err := os.WriteFile(lib, bytes, 0500); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeActualUnvalidatedImages(t *testing.T) {
	helper, dir := os.Getenv("ATRIUM_NATIVE_HELPER"), os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if helper == "" || dir == "" {
		t.Skip("native fixture environment required")
	}
	for _, name := range []string{"report.bmp", "report.gif", "report.tiff", "report.heic", "blank.bmp", "truncated-1-4.bmp", "truncated-2-4.bmp", "bad.bmp", "multi.gif"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "unvalidated", name)
			bytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			sum := sha256.Sum256(bytes)
			r, err := (NativeParser{HelperPath: helper}).Parse(context.Background(), NativeSource{f, hex.EncodeToString(sum[:])})
			if err != nil {
				t.Fatal(err)
			}
			want, code := ParsePartial, "image_integrity_unverified"
			switch name {
			case "bad.bmp":
				want, code = ParseInvalid, "invalid_input"
			case "multi.gif":
				want, code = ParseUnsupported, "multiframe_image"
			}
			if r.Status != want || r.Coverage.Complete || !warning(r, code) {
				t.Fatalf("unvalidated source claimed completion: %s %+v", r.Status, r.Coverage)
			}
			if evidence := os.Getenv("ATRIUM_NATIVE_EVIDENCE"); evidence != "" {
				destination := filepath.Join(evidence, "unvalidated")
				if err := os.MkdirAll(destination, 0700); err != nil {
					t.Fatal(err)
				}
				bytes, _ := json.MarshalIndent(r, "", "  ")
				if err := os.WriteFile(filepath.Join(destination, name+".result.json"), bytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if want != ParsePartial {
				if len(r.Segments) != 0 {
					t.Fatal("unsupported or invalid source produced evidence")
				}
				return
			}
			if r.Native.ProcessedPages != 1 || len(r.Native.Pages) != 1 || !r.Native.Pages[0].OCRComplete {
				t.Fatal("completed raster OCR coverage was lost")
			}
			for _, segment := range r.Segments {
				if segment.Visual == nil || !segment.Visual.RequiresReview {
					t.Fatal("unverified source did not inherit review")
				}
			}
			if strings.HasPrefix(name, "report.") && len(r.Segments) < 10 {
				t.Fatal("usable original OCR was discarded")
			}
			if name == "blank.bmp" && len(r.Segments) != 0 {
				t.Fatal("blank source invented evidence")
			}
			if _, err := f.ReadAt(make([]byte, 1), 0); err != nil {
				t.Fatal("original lost")
			}
		})
	}
}
