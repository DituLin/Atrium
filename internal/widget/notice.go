package widget

import (
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/config"
)

// CurrentNotice is the shared home and family overview projection. Invalid
// manually constructed configurations fail closed just like invalid file loads.
// The validity interval includes its start and excludes its end.
func CurrentNotice(n config.Notice, now time.Time) *Notice {
	if !n.Enabled || strings.TrimSpace(n.Text) == "" || n.Validate() != nil {
		return nil
	}
	from, _ := config.NoticeTime(n.ValidFrom)
	until, _ := config.NoticeTime(n.ValidUntil)
	if (from != nil && now.Before(*from)) || (until != nil && !now.Before(*until)) {
		return nil
	}
	return &Notice{Text: n.Text, UpdatedAt: noticeValue(n.UpdatedAt), ValidFrom: noticeValue(n.ValidFrom), ValidUntil: noticeValue(n.ValidUntil)}
}

func noticeValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
