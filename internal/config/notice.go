package config

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Go's time parser accepts some values outside RFC 3339 (such as +24:00).
var noticeTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// NoticeTime parses an optional RFC 3339 timestamp with an explicit offset.
func NoticeTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if !noticeTimestamp.MatchString(value) {
		return nil, fmt.Errorf("must be RFC 3339 with an explicit offset")
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("must be RFC 3339 with an explicit offset: %w", err)
	}
	return &t, nil
}

// Validate checks notice timestamps even when the notice is disabled.
func (n Notice) Validate() error {
	var errs []error
	for _, field := range []struct{ name, value string }{{"updated_at", n.UpdatedAt}, {"valid_from", n.ValidFrom}, {"valid_until", n.ValidUntil}} {
		if _, err := NoticeTime(field.value); err != nil {
			errs = append(errs, fmt.Errorf("widgets.notice.%s: %w", field.name, err))
		}
	}
	from, fromErr := NoticeTime(n.ValidFrom)
	until, untilErr := NoticeTime(n.ValidUntil)
	if fromErr == nil && untilErr == nil && from != nil && until != nil && !until.After(*from) {
		errs = append(errs, errors.New("widgets.notice.valid_until must be after valid_from"))
	}
	return errors.Join(errs...)
}
