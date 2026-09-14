package domain_test

import (
	"encoding/json"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVideoRoutesReportedButNotCommandNavigable(t *testing.T) {
	for _, name := range []domain.RouteName{"videos", "video"} {
		require.True(t, name.Valid())
		require.False(t, name.NavigableRoute())
	}
	var route domain.RouteState
	require.NoError(t, json.Unmarshal([]byte(`{"name":"video","video_id":"v01"}`), &route))
	body, err := json.Marshal(route)
	require.NoError(t, err)
	require.Contains(t, string(body), `"video_id":"v01"`)
}
