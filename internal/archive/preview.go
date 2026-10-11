package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type PreviewPage struct {
	PNG                             []byte
	Page, TotalPages, Width, Height int
}

// Preview uses the same bounded FD renderer and orientation/crop transform as
// extraction. It never executes OCR and accepts no output path or document args.
func (p NativeParser) Preview(ctx context.Context, s NativeSource, page int) (PreviewPage, error) {
	var out PreviewPage
	bad := func(code string) (PreviewPage, error) { return out, nativeError(code, nil) }
	if page < 1 || page > 100 || s.File == nil {
		return bad("preview_invalid_request")
	}
	if runtime.GOOS != "darwin" {
		return bad("preview_unavailable")
	}
	st, e := s.File.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 32<<20 {
		return bad("preview_invalid_source")
	}
	sum := sha256.New()
	if _, e = io.Copy(sum, io.NewSectionReader(s.File, 0, st.Size())); e != nil || hex.EncodeToString(sum.Sum(nil)) != s.SHA256 {
		return bad("preview_invalid_source")
	}
	hi, e := os.Lstat(p.HelperPath)
	if !filepath.IsAbs(p.HelperPath) || e != nil || !hi.Mode().IsRegular() || hi.Mode().Perm()&0111 == 0 {
		return bad("preview_unavailable")
	}
	timeout := p.Timeout
	if timeout <= 0 || timeout > 20*time.Second {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case nativeSlots <- struct{}{}:
	case <-ctx.Done():
		return bad("preview_timeout")
	}
	defer func() { <-nativeSlots }()
	cmd := exec.CommandContext(ctx, p.HelperPath, "--protocol=1")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}
	cmd.Stdin = strings.NewReader(fmt.Sprintf("{\"protocol\":1,\"preview_page\":%d}\n", page))
	cmd.ExtraFiles = []*os.File{s.File}
	cmd.WaitDelay = 500 * time.Millisecond
	configureNativeProcess(cmd)
	stdout := &nativeBoundedBuffer{limit: 8 << 20, cancel: cancel}
	stderr := &nativeBoundedBuffer{limit: 64 << 10, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	e = cmd.Run()
	finishNativeProcess(cmd)
	if e != nil || ctx.Err() != nil || stdout.exceeded || stderr.exceeded {
		return bad("preview_failed")
	}
	var wire struct {
		Protocol   int    `json:"protocol"`
		Page       int    `json:"page"`
		TotalPages int    `json:"total_pages"`
		PNG        []byte `json:"png"`
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.data.Bytes()))
	dec.DisallowUnknownFields()
	if dec.Decode(&wire) != nil || dec.Decode(new(any)) != io.EOF || wire.Protocol != 1 || wire.Page != page || wire.TotalPages < page || wire.TotalPages > 100 || len(wire.PNG) > 6<<20 {
		return bad("preview_unavailable")
	}
	config, e := png.DecodeConfig(bytes.NewReader(wire.PNG))
	if e != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 16000000 {
		return bad("preview_protocol")
	}
	out = PreviewPage{PNG: wire.PNG, Page: page, TotalPages: wire.TotalPages, Width: config.Width, Height: config.Height}
	return out, nil
}
