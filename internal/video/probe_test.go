package video

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseProbeDisplayDimensionsAndAudio(t *testing.T) {
	m, err := parseProbe([]byte(`{"format":{"duration":"11.741667","tags":{"major_brand":"qt  "}},"streams":[{"codec_type":"video","codec_name":"mjpeg","width":8,"height":8,"disposition":{"attached_pic":1}},{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"side_data_list":[{"rotation":-90},{"side_data_type":"other metadata"}]},{"codec_type":"audio","codec_name":"aac"}]}`))
	require.NoError(t, err)
	require.Equal(t, "mov", m.Container)
	require.Equal(t, "h264", m.VideoCodec)
	require.Equal(t, "aac", m.AudioCodec)
	require.Equal(t, 1080, m.Width)
	require.Equal(t, 1920, m.Height)
	require.Equal(t, -90, m.Rotation)
	require.EqualValues(t, 11742, m.DurationMS)
}

func TestParseProbeRejectsUnusableMetadata(t *testing.T) {
	for _, raw := range []string{`invalid`, `{}`, `{"format":{"duration":"NaN"},"streams":[{"codec_type":"video","codec_name":"h264","width":10,"height":10}]}`, `{"format":{"duration":"2"},"streams":[{"codec_type":"video","codec_name":"h264","width":0,"height":10}]}`, `{"format":{"duration":"2"},"streams":[{"codec_type":"video","codec_name":"h264","width":10,"height":10,"side_data_list":[{"rotation":45}]}]}`} {
		_, err := parseProbe([]byte(raw))
		require.Error(t, err, raw)
	}
	m, err := parseProbe([]byte(`{"format":{"duration":"1"},"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080}]}`))
	require.NoError(t, err)
	require.Empty(t, m.AudioCodec)
}
