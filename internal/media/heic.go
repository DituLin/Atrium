package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HEIC converter modes from config `media.heic.converter`.
const (
	ConverterSips = "sips"
	ConverterOff  = "off"
)

// ErrHEICDisabled is returned when HEIC conversion is switched off.
var ErrHEICDisabled = errors.New("media: HEIC conversion is disabled")

// Converter turns a HEIC/HEIF file into a JPEG that the standard library can
// decode. It is an interface so the pipeline can be tested without macOS and
// so a different converter can replace sips after G0 (design D8).
type Converter interface {
	// Convert writes a JPEG derived from src into dstDir and returns its path.
	Convert(ctx context.Context, src, dstDir string, maxEdge int) (string, error)
	// CreationTime reads the capture time the converter can see, used only
	// when the file carries no usable EXIF.
	CreationTime(ctx context.Context, src string) (time.Time, error)
	// Enabled reports whether conversion is available.
	Enabled() bool
}

// OffConverter refuses every conversion, which makes HEIC files land in the
// `unsupported` bucket where diagnostics can count them.
type OffConverter struct{}

// Convert implements Converter.
func (OffConverter) Convert(context.Context, string, string, int) (string, error) {
	return "", Coded(ErrCodeUnsupported, ErrHEICDisabled)
}

// CreationTime implements Converter.
func (OffConverter) CreationTime(context.Context, string) (time.Time, error) {
	return time.Time{}, ErrHEICDisabled
}

// Enabled implements Converter.
func (OffConverter) Enabled() bool { return false }

// SipsConverter shells out to /usr/bin/sips, the macOS built-in image tool
// (design D8). The subprocess inherits the job deadline and is killed on
// timeout, which is the property a cgo decoder could not offer.
type SipsConverter struct {
	// Path is the sips binary; empty means /usr/bin/sips.
	Path string
}

// SipsPath is the default binary location.
const SipsPath = "/usr/bin/sips"

// NewConverter builds the converter selected by configuration.
func NewConverter(mode string) Converter {
	if mode == ConverterSips {
		return &SipsConverter{Path: SipsPath}
	}
	return OffConverter{}
}

// Enabled implements Converter.
func (c *SipsConverter) Enabled() bool {
	_, err := os.Stat(c.binary())
	return err == nil
}

func (c *SipsConverter) binary() string {
	if c.Path != "" {
		return c.Path
	}
	return SipsPath
}

// Convert implements Converter.
func (c *SipsConverter) Convert(ctx context.Context, src, dstDir string, maxEdge int) (string, error) {
	if !c.Enabled() {
		return "", Coded(ErrCodeUnsupported, ErrHEICDisabled)
	}
	out := filepath.Join(dstDir, "heic-"+filepath.Base(src)+".jpg")
	args := []string{"-s", "format", "jpeg"}
	if maxEdge > 0 {
		args = append(args, "--resampleHeightWidthMax", fmt.Sprint(maxEdge))
	}
	args = append(args, src, "--out", out)

	cmd := exec.CommandContext(ctx, c.binary(), args...) //nolint:gosec // fixed binary, arguments are not shell-interpreted
	cmd.WaitDelay = time.Second                          // SIGKILL shortly after the deadline
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", Coded(ErrCodeTimeout, ctx.Err())
		}
		return "", Coded(ErrCodeUnsupported, fmt.Errorf("sips: %w", err))
	}
	if _, err := os.Stat(out); err != nil {
		return "", Coded(ErrCodeUnsupported, fmt.Errorf("sips produced no output: %w", err))
	}
	return out, nil
}

// CreationTime implements Converter using `sips -g creation`, the fallback
// when the HEIC container carries no EXIF Atrium can parse.
func (c *SipsConverter) CreationTime(ctx context.Context, src string) (time.Time, error) {
	if !c.Enabled() {
		return time.Time{}, ErrHEICDisabled
	}
	cmd := exec.CommandContext(ctx, c.binary(), "-g", "creation", src) //nolint:gosec // fixed binary
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("media: sips -g creation: %w", err)
	}
	return ParseSipsCreation(string(out))
}

// ParseSipsCreation extracts the timestamp from `sips -g creation` output,
// which looks like "  creation: 2026:09:05 10:12:00".
func ParseSipsCreation(out string) (time.Time, error) {
	for _, line := range strings.Split(out, "\n") {
		_, value, found := strings.Cut(line, "creation:")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		for _, layout := range []string{"2006:01:02 15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
			if t, err := time.Parse(layout, value); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("media: no creation date in sips output")
}

// IsHEIC reports whether an extension needs the converter.
func IsHEIC(ext string) bool { return ext == "heic" || ext == "heif" }
