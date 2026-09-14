package video

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"

	"github.com/DituLin/Atrium/internal/domain"
)

// Tools invokes administrator-configured ffprobe/ffmpeg executables. Its input
// must be a fresh regular read-only file obtained through source.FS.Open after
// source authorization and identity checks. Each operation consumes its offset.
type Tools struct {
	FFprobe string
	FFmpeg  string
}

func inputArgs(protocols string) []string {
	return []string{"-v", "error", "-max_alloc", "33554432", "-protocol_whitelist", protocols,
		"-f", "mov", "-enable_drefs", "0", "-use_absolute_path", "0", "-probesize", "8388608",
		"-analyzeduration", "10000000", "-fd", "3", "-i", "fd:"}
}

// Probe derives only the metadata retained by the video index.
func (t Tools) Probe(ctx context.Context, input *os.File) (domain.VideoMetadata, error) {
	args := append(inputArgs("fd"), "-show_entries", "format=duration:format_tags=major_brand:stream=codec_type,codec_name,width,height:stream_disposition=attached_pic:stream_side_data=rotation", "-of", "json")
	data, err := runTool(ctx, t.FFprobe, args, input, 512*1024)
	if err != nil {
		return domain.VideoMetadata{}, err
	}
	return parseProbe(data)
}

// Cover returns one bounded JPEG, preserving display rotation and aspect ratio.
// It never creates files or transcodes the full video.
func (t Tools) Cover(ctx context.Context, input *os.File) ([]byte, error) {
	args := append([]string{"-nostdin", "-threads", "1"}, inputArgs("fd,pipe")...)
	args = append(args, "-map", "0:V:0", "-an", "-sn", "-dn", "-frames:v", "1", "-filter_threads", "1",
		"-vf", "scale=w='min(640,iw)':h='min(640,ih)':force_original_aspect_ratio=decrease",
		"-threads", "1", "-c:v", "mjpeg", "-q:v", "4", "-f", "image2pipe", "pipe:1")
	data, err := runTool(ctx, t.FFmpeg, args, input, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 640 || cfg.Height > 640 {
		return nil, ErrToolFailed
	}
	return data, nil
}
