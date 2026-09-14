// Package video probes and derives bounded video assets from already authorized
// read-only descriptors. It never opens source paths or grants authorization.
package video

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/DituLin/Atrium/internal/domain"
)

// ErrMetadata means the probe did not describe a usable video stream.
var ErrMetadata = errors.New("video: invalid or unsupported metadata")

func parseProbe(data []byte) (domain.VideoMetadata, error) {
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
			Tags     struct {
				Brand string `json:"major_brand"`
			} `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Type        string `json:"codec_type"`
			Codec       string `json:"codec_name"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Disposition struct {
				Attached int `json:"attached_pic"`
			} `json:"disposition"`
			Side []struct {
				Rotation *float64 `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
	}
	var m domain.VideoMetadata
	if err := json.Unmarshal(data, &raw); err != nil {
		return m, ErrMetadata
	}
	seconds, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 7*24*3600 {
		return m, ErrMetadata
	}
	m.DurationMS = int64(math.Round(seconds * 1000))
	m.Container = "mp4"
	if strings.TrimSpace(raw.Format.Tags.Brand) == "qt" {
		m.Container = "mov"
	}
	for _, s := range raw.Streams {
		if s.Type == "audio" && m.AudioCodec == "" {
			m.AudioCodec = s.Codec
		}
		if s.Type != "video" || s.Disposition.Attached != 0 || m.VideoCodec != "" {
			continue
		}
		if s.Width <= 0 || s.Height <= 0 || s.Width > 16384 || s.Height > 16384 || s.Codec == "" {
			return domain.VideoMetadata{}, ErrMetadata
		}
		m.Width, m.Height, m.VideoCodec = s.Width, s.Height, s.Codec
		for _, side := range s.Side {
			if side.Rotation == nil {
				continue
			}
			angle := *side.Rotation
			if math.IsNaN(angle) || math.IsInf(angle, 0) || math.Abs(angle) > 360 || math.Abs(angle/90-math.Round(angle/90)) > .001 {
				return domain.VideoMetadata{}, ErrMetadata
			}
			m.Rotation = int(math.Round(angle/90)) * 90
		}
		if m.Rotation%180 != 0 {
			m.Width, m.Height = m.Height, m.Width
		}
	}
	if m.VideoCodec == "" || m.DurationMS <= 0 {
		return domain.VideoMetadata{}, ErrMetadata
	}
	return m, nil
}
