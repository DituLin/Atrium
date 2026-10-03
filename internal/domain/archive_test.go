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
