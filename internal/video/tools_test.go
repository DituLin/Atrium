package video

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolsRealDescriptorProbeAndCover(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	path := filepath.Join(t.TempDir(), "sample.mp4")
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=96x64:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", path).CombinedOutput()
	require.NoError(t, err, string(out))
	tools := Tools{FFprobe: ffprobe, FFmpeg: ffmpeg}
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	m, err := tools.Probe(context.Background(), f)
	require.NoError(t, err)
	require.Equal(t, 96, m.Width)
	require.Equal(t, 64, m.Height)
	require.Equal(t, "h264", m.VideoCodec)
	cover, err := os.Open(path)
	require.NoError(t, err)
	defer cover.Close()
	data, err := tools.Cover(context.Background(), cover)
	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.LessOrEqual(t, img.Bounds().Dx(), 640)
	require.Positive(t, img.Bounds().Dy())
}
