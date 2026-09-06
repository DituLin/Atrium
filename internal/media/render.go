package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG for image.DecodeConfig and imaging.Decode
	"io"

	"github.com/disintegration/imaging"
)

// Preview error codes stored on photos.preview_error. They are stable strings
// shared with the CLI, the API and the runbook.
const (
	ErrCodeDecodeFailed = "decode_failed"
	ErrCodeUnsupported  = "unsupported"
	ErrCodeTooLarge     = "too_large"
	ErrCodeTimeout      = "timeout"
	ErrCodeIO           = "io_error"
	ErrCodeStuck        = "stuck"
	ErrCodeEncodeFailed = "encode_failed"
)

// jpegQualitySteps is the quality ladder from design §6.3 step 3: the first
// encode that fits the byte budget wins.
var jpegQualitySteps = []int{85, 75, 65}

// RenderOptions bounds a derived image.
type RenderOptions struct {
	MaxEdge  int
	MaxBytes int64
	// Quality pins a single quality instead of walking the ladder.
	Quality int
}

// Rendered is one encoded derived image.
type Rendered struct {
	Data          []byte
	Width, Height int
	Quality       int
}

// CodedError carries a stable preview error code alongside the cause.
type CodedError struct {
	code  string
	cause error
}

// Error implements error.
func (e *CodedError) Error() string {
	if e.cause == nil {
		return e.code
	}
	return e.code + ": " + e.cause.Error()
}

// Code returns the stable error code.
func (e *CodedError) Code() string { return e.code }

// Unwrap exposes the cause.
func (e *CodedError) Unwrap() error { return e.cause }

// Coded builds an error carrying a preview error code.
func Coded(code string, cause error) *CodedError { return &CodedError{code: code, cause: cause} }

// CodeOf extracts the preview error code from an error chain.
func CodeOf(err error) string {
	var c *CodedError
	if errors.As(err, &c) {
		return c.Code()
	}
	return ErrCodeDecodeFailed
}

// InspectConfig reads only the image header so the guards can reject an image
// before its pixels are ever allocated (design §6.3).
func InspectConfig(r io.ReadSeeker, maxPixels int64) (image.Config, string, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return image.Config{}, "", Coded(ErrCodeIO, err)
	}
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		return image.Config{}, "", Coded(ErrCodeUnsupported, err)
	}
	if maxPixels > 0 && int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return cfg, format, Coded(ErrCodeTooLarge,
			fmt.Errorf("%dx%d exceeds max_pixels %d", cfg.Width, cfg.Height, maxPixels))
	}
	return cfg, format, nil
}

// Decode reads a full image, applying the EXIF orientation. Re-encoding the
// result is what strips EXIF and GPS from every preview by construction.
func Decode(r io.ReadSeeker) (image.Image, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, Coded(ErrCodeIO, err)
	}
	img, err := imaging.Decode(r, imaging.AutoOrientation(true))
	if err != nil {
		return nil, Coded(ErrCodeDecodeFailed, err)
	}
	return img, nil
}

// Render fits img inside opts.MaxEdge and encodes it as JPEG, lowering the
// quality until the byte budget is met.
func Render(img image.Image, opts RenderOptions) (Rendered, error) {
	fitted := Fit(img, opts.MaxEdge)
	bounds := fitted.Bounds()

	steps := jpegQualitySteps
	if opts.Quality > 0 {
		steps = []int{opts.Quality}
	}
	var last Rendered
	for _, q := range steps {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, fitted, &jpeg.Options{Quality: q}); err != nil {
			return Rendered{}, Coded(ErrCodeEncodeFailed, err)
		}
		last = Rendered{Data: buf.Bytes(), Width: bounds.Dx(), Height: bounds.Dy(), Quality: q}
		if opts.MaxBytes <= 0 || int64(len(last.Data)) <= opts.MaxBytes {
			return last, nil
		}
	}
	// The lowest quality still exceeds the target. Returning it is better than
	// failing: the budget is a target, not a hard limit (FR-10).
	return last, nil
}

// Fit downscales img so its longest edge is at most maxEdge. Images already
// within the bound are returned unchanged, never upscaled.
func Fit(img image.Image, maxEdge int) image.Image {
	if maxEdge <= 0 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return img
	}
	if w >= h {
		return imaging.Resize(img, maxEdge, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, maxEdge, imaging.Lanczos)
}
