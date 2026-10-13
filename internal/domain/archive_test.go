package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestArchiveObjectCanonicalization(t *testing.T) {
	got, err := domain.NormalizeArchiveObject(json.RawMessage(` {"z":[{"b":2,"a":1}],"a":9007199254740993} `))
	require.NoError(t, err)
	require.Equal(t, `{"a":9007199254740993,"z":[{"a":1,"b":2}]}`, string(got))
	integer, err := domain.NormalizeArchiveObject(json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	decimal, err := domain.NormalizeArchiveObject(json.RawMessage(`{"n":1.0}`))
	require.NoError(t, err)
	require.NotEqual(t, integer, decimal, "numeric spellings are intentionally part of request identity")
	for _, raw := range []string{"", "null", "[]", "1", `{} {}`, `{"x":1,"x":2}`, `{"nested":{"x":1,"x":2}}`, `{"x":"` + strings.Repeat("a", domain.MaxArchiveJSONBytes) + `"}`, `{"x":` + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40) + "}", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		t.Run(raw[:min(30, len(raw))], func(t *testing.T) {
			_, err := domain.NormalizeArchiveObject(json.RawMessage(raw))
			require.Error(t, err)
			e, ok := domain.AsError(err)
			require.True(t, ok)
			require.Equal(t, domain.CodeInvalidRequest, e.Code)
		})
	}
}

func TestArchiveDocumentDatePreservesPrecision(t *testing.T) {
	for _, d := range []domain.ArchiveDocumentDate{
		{Precision: "unknown"}, {Precision: "year", Value: "2026"}, {Precision: "month", Value: "2026-10"},
		{Precision: "date", Value: "2024-02-29"}, {Precision: "datetime", Value: "2026-10-03T09:30:00+08:00"},
	} {
		require.NoError(t, d.Validate(), "%+v", d)
	}
	for _, d := range []domain.ArchiveDocumentDate{
		{}, {Precision: "unknown", Value: "2026"}, {Precision: "year", Value: "2026-01-01"},
		{Precision: "year", Value: "0000"}, {Precision: "month", Value: "2026-13"}, {Precision: "date", Value: "2025-02-29"},
		{Precision: "datetime", Value: "2026-10-03"}, {Precision: "datetime", Value: "2026-10-03T09:30:00"},
	} {
		require.Error(t, d.Validate(), "%+v", d)
	}
}

func TestArchiveTaskJSONHidesLeaseToken(t *testing.T) {
	b, err := json.Marshal(domain.ArchiveTask{LeaseToken: "secret-lease-token"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "secret-lease-token")
	require.NotContains(t, string(b), "lease_token")
}

func TestArchiveDocumentDateStrictDatetime(t *testing.T) {
	for _, value := range []string{
		"2026-10-03T09:30:00Z",
		"2026-10-03T09:30:00+08:00",
		"2026-10-03T09:30:00-05:30",
		"2026-10-03T09:30:00.1Z",
		"2026-10-03T09:30:00.123456789+08:00",
		"2026-10-03T09:30:00.120000000Z",
	} {
		t.Run(value, func(t *testing.T) {
			date := domain.ArchiveDocumentDate{Precision: "datetime", Value: value}
			require.NoError(t, date.Validate())
			require.Equal(t, value, date.Value, "validation must preserve the source's exact fractional precision")
		})
	}
	for _, tc := range []struct{ name, value string }{
		{"offset_hour_24", "2026-10-03T09:30:00+24:00"},
		{"negative_offset_hour_24", "2026-10-03T09:30:00-24:00"},
		{"offset_minute_60", "2026-10-03T09:30:00+00:60"},
		{"single_digit_hour", "2026-10-03T9:30:00Z"},
		{"comma_fraction", "2026-10-03T09:30:00,1Z"},
		{"precision_beyond_nanoseconds", "2026-10-03T09:30:00.1234567890Z"},
		{"oversized_fraction", "2026-10-03T09:30:00." + strings.Repeat("1", 65537) + "Z"},
		{"invalid_calendar", "2026-02-30T09:30:00Z"},
		{"invalid_hour", "2026-10-03T24:30:00Z"},
		{"invalid_minute", "2026-10-03T09:60:00Z"},
		{"invalid_second", "2026-10-03T09:30:60Z"},
		{"empty_fraction", "2026-10-03T09:30:00.Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (domain.ArchiveDocumentDate{Precision: "datetime", Value: tc.value}).Validate()
			require.Error(t, err)
			e, ok := domain.AsError(err)
			require.True(t, ok)
			require.Equal(t, domain.CodeInvalidRequest, e.Code)
		})
	}
}
