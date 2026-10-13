package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNativePreviewRejectsInvalidPageAndHelperOutput(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "original")
	require.NoError(t, e)
	defer f.Close()
	_, e = f.WriteString("source")
	require.NoError(t, e)
	helper := filepath.Join(t.TempDir(), "helper")
	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\necho '{\"protocol\":1,\"page\":1,\"total_pages\":1,\"png\":\"c2VjcmV0\"}'\n"), 0700))
	p := NativeParser{HelperPath: helper}
	src := NativeSource{File: f, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("source")))}
	_, e = p.Preview(context.Background(), src, 0)
	require.Error(t, e)
	_, e = p.Preview(context.Background(), src, 101)
	require.Error(t, e)
	_, e = p.Preview(context.Background(), src, 1)
	require.Error(t, e, "non PNG output must not be delivered")
}

func TestNativePreviewActualRaster(t *testing.T) {
	helper := os.Getenv("ATRIUM_T14_PREVIEW_HELPER")
	if helper == "" {
		t.Skip("native helper opt in")
	}
	for _, name := range []string{"report.png", "geometry.pdf"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("../../adapters/macos-archive-parser/evidence", name)
			b, e := os.ReadFile(path)
			require.NoError(t, e)
			f, e := os.Open(path)
			require.NoError(t, e)
			defer f.Close()
			p, e := (NativeParser{HelperPath: helper}).Preview(context.Background(), NativeSource{File: f, SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}, 1)
			require.NoError(t, e)
			require.Greater(t, p.Width, 100)
			require.Greater(t, p.Height, 100)
			require.NotEmpty(t, p.PNG)
		})
	}
}

func TestNativePreviewCancelsAndRejectsWrongSource(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native process")
	}
	f, e := os.CreateTemp(t.TempDir(), "source")
	require.NoError(t, e)
	defer f.Close()
	_, e = f.WriteString("source")
	require.NoError(t, e)
	helper := filepath.Join(t.TempDir(), "helper")
	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\nsleep 60\n"), 0700))
	src := NativeSource{File: f, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("source")))}
	p := NativeParser{HelperPath: helper, Timeout: 50 * time.Millisecond}
	started := time.Now()
	_, e = p.Preview(context.Background(), src, 1)
	require.Error(t, e)
	require.Less(t, time.Since(started), 2*time.Second)
	src.SHA256 = "wrong"
	_, e = p.Preview(context.Background(), src, 1)
	require.Error(t, e)
}
func TestNativePreviewActualPDFGeometry(t *testing.T) {
	helper := os.Getenv("ATRIUM_T14_PREVIEW_HELPER")
	if helper == "" {
		t.Skip("native helper opt in")
	}
	path := "../../adapters/macos-archive-parser/evidence/geometry.pdf"
	b, e := os.ReadFile(path)
	require.NoError(t, e)
	f, e := os.Open(path)
	require.NoError(t, e)
	defer f.Close()
	src := NativeSource{File: f, SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}
	for page := 1; page <= 4; page++ {
		p, e := (NativeParser{HelperPath: helper}).Preview(context.Background(), src, page)
		require.NoError(t, e)
		require.Equal(t, 4, p.TotalPages)
		width, height := 481, 322
		if page%2 == 0 {
			width, height = height, width
		}
		require.Equal(t, width, p.Width)
		require.Equal(t, height, p.Height)
	}
	_, e = (NativeParser{HelperPath: helper}).Preview(context.Background(), src, 5)
	require.Error(t, e)
	fixtures := os.Getenv("ATRIUM_NATIVE_FIXTURES")
	if fixtures == "" {
		return
	}
	for _, name := range []string{"locked.pdf", "restricted.pdf", "101-pages.pdf", "bad.pdf"} {
		path = filepath.Join(fixtures, name)
		b, e = os.ReadFile(path)
		require.NoError(t, e)
		f, e = os.Open(path)
		require.NoError(t, e)
		_, e = (NativeParser{HelperPath: helper}).Preview(context.Background(), NativeSource{File: f, SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}, 1)
		f.Close()
		require.Error(t, e, name)
	}
}
