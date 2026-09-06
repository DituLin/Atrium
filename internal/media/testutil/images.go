package testutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// Gradient builds a deterministic RGBA image. A gradient compresses poorly
// enough to make byte-budget tests meaningful, unlike a flat colour.
func Gradient(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*3) % 256),
				G: uint8((x*13 + y*29) % 256),
				B: uint8((x*x + y*y) % 256),
				A: 255,
			})
		}
	}
	return img
}

// Marker paints a distinctive bright block in the top-left quadrant so tests
// can tell whether an orientation transform was applied.
func Marker(img *image.RGBA) *image.RGBA {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Min.Y+b.Dy()/4; y++ {
		for x := b.Min.X; x < b.Min.X+b.Dx()/4; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return img
}

// JPEG encodes img, optionally splicing an EXIF APP1 segment after the SOI
// marker so the file carries real metadata rather than a stub.
func JPEG(img image.Image, opts ExifOptions, quality int) ([]byte, error) {
	if quality <= 0 {
		quality = 90
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("testutil: encode jpeg: %w", err)
	}
	raw := buf.Bytes()
	if opts.Empty() {
		return raw, nil
	}
	seg, err := app1Segment(buildTIFF(opts))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(raw)+len(seg))
	out = append(out, raw[:2]...) // SOI
	out = append(out, seg...)
	out = append(out, raw[2:]...)
	return out, nil
}

// PNG encodes img. PNG carries no EXIF here, which is what makes it the
// "capture time unknown" fixture.
func PNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("testutil: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// HugePNG writes a PNG whose header declares enormous dimensions without
// carrying the pixels. image.DecodeConfig reads only the header, so this is
// the fixture for the max_pixels guard.
func HugePNG(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, width)
	_ = binary.Write(&ihdr, binary.BigEndian, height)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA, no interlace
	writeChunk(&buf, "IHDR", ihdr.Bytes())
	writeChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writeChunk(w *bytes.Buffer, kind string, payload []byte) {
	_ = binary.Write(w, binary.BigEndian, uint32(len(payload)))
	body := append([]byte(kind), payload...)
	w.Write(body)
	_ = binary.Write(w, binary.BigEndian, crc32.ChecksumIEEE(body))
}

// Corrupt returns bytes that look like a JPEG but cannot be decoded.
func Corrupt() []byte {
	out := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0}
	return append(out, bytes.Repeat([]byte{0x42}, 200)...)
}

// SeedOptions describes one file written by WriteTree.
type SeedOptions struct {
	RelPath string
	Bytes   []byte
	ModTime time.Time
}

// WriteTree materialises files under root, creating directories as needed.
func WriteTree(root string, files []SeedOptions) error {
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("testutil: mkdir: %w", err)
		}
		if err := os.WriteFile(full, f.Bytes, 0o644); err != nil {
			return fmt.Errorf("testutil: write %s: %w", f.RelPath, err)
		}
		if !f.ModTime.IsZero() {
			if err := os.Chtimes(full, f.ModTime, f.ModTime); err != nil {
				return fmt.Errorf("testutil: chtimes %s: %w", f.RelPath, err)
			}
		}
	}
	return nil
}
