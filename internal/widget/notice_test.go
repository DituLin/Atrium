package widget_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/stretchr/testify/require"
)

func TestNoticeLegacyUnknownTimeAndWhitespace(t *testing.T) {
	cfg := config.Defaults()
	cfg.Home.Timezone = "Asia/Singapore"
	cfg.Widgets.Notice = config.Notice{Enabled: true, Text: "Family reminder"}
	fake := clock.NewFake(time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC))
	home, err := clock.NewHome(cfg.Home.Timezone, fake)
	require.NoError(t, err)
	composer := widget.NewComposer(cfg, testutil.NewDB(t), home, nil)
	snap, err := composer.Compose(context.Background(), false)
	require.NoError(t, err)
	raw, err := json.Marshal(snap.Notice)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Contains(t, payload, "updated_at")
	require.Nil(t, payload["updated_at"], "request time is not content update time")
	require.Contains(t, payload, "valid_from")
	require.Nil(t, payload["valid_from"])
	require.Contains(t, payload, "valid_until")
	require.Nil(t, payload["valid_until"])
	cfg.Widgets.Notice.Text = " \n\t "
	snap, err = composer.Compose(context.Background(), false)
	require.NoError(t, err)
	require.Nil(t, snap.Notice)
}

func TestNoticeValiditySharedWithHome(t *testing.T) {
	from := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		at      time.Time
		present bool
	}{
		{"before", from.Add(-time.Nanosecond), false},
		{"start", from, true},
		{"inside", from.Add(30 * time.Minute), true},
		{"last instant", from.Add(time.Hour - time.Nanosecond), true},
		{"end", from.Add(time.Hour), false},
		{"after", from.Add(2 * time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Home.Timezone = "Asia/Singapore"
			cfg.Widgets.Notice = config.Notice{Enabled: true, Text: "Reminder", UpdatedAt: "2026-09-11T08:00:00.123+08:00", ValidFrom: "2026-09-12T18:00:00+08:00", ValidUntil: "2026-09-12T11:00:00Z"}
			projected := widget.CurrentNotice(cfg.Widgets.Notice, tc.at)
			home, err := clock.NewHome(cfg.Home.Timezone, clock.NewFake(tc.at))
			require.NoError(t, err)
			snap, err := widget.NewComposer(cfg, testutil.NewDB(t), home, nil).Compose(context.Background(), false)
			require.NoError(t, err)
			require.Equal(t, projected, snap.Notice)
			if !tc.present {
				require.Nil(t, projected)
				return
			}
			require.NotNil(t, projected)
			require.Equal(t, cfg.Widgets.Notice.UpdatedAt, *projected.UpdatedAt)
			require.Equal(t, cfg.Widgets.Notice.ValidFrom, *projected.ValidFrom)
			require.Equal(t, cfg.Widgets.Notice.ValidUntil, *projected.ValidUntil)
			raw, err := json.Marshal(snap)
			require.NoError(t, err)
			var decoded widget.Snapshot
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.Equal(t, snap.Notice, decoded.Notice)
		})
	}
}

func TestNoticeAbsentAndOpenEnded(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		n       config.Notice
		present bool
	}{
		{"disabled", config.Notice{Text: "Reminder"}, false},
		{"empty", config.Notice{Enabled: true}, false},
		{"whitespace", config.Notice{Enabled: true, Text: " \n "}, false},
		{"legacy", config.Notice{Enabled: true, Text: "Reminder"}, true},
		{"start only", config.Notice{Enabled: true, Text: "Reminder", ValidFrom: "2026-09-12T10:00:00Z"}, true},
		{"end only", config.Notice{Enabled: true, Text: "Reminder", ValidUntil: "2026-09-12T11:00:00Z"}, true},
		{"invalid manual", config.Notice{Enabled: true, Text: "Reminder", UpdatedAt: "yesterday"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) { got := widget.CurrentNotice(tc.n, now); require.Equal(t, tc.present, got != nil) })
	}
}
