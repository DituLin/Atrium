package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestIntegrationErrorTraceUsesOnlyStableCodes(t *testing.T) {
	for _, code := range []domain.ErrorCode{domain.CodeForbidden, domain.CodeInvalidRequest, domain.CodeIdempotencyConflict, domain.CodeInternal, domain.ErrorCode("private-unknown-code")} {
		t.Run(string(code), func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("principal_id", "principal", "request_id", "request", "turn_ref", "turn", "call_ref", "call")
			ctx := context.WithValue(context.Background(), loggerKey{}, logger)
			ctx = auth.WithIdentity(ctx, &auth.Identity{Scope: auth.ScopeIntegration, Integration: &domain.IntegrationPrincipal{ID: "principal"}})
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			WriteError(httptest.NewRecorder(), r, domain.WrapErr(code, errors.New("private-token /private/nas"), "private-message"))
			expected := code
			if code == domain.ErrorCode("private-unknown-code") {
				expected = domain.CodeInternal
			}
			require.Contains(t, logs.String(), `"error_code":"`+string(expected)+`"`)
			require.Contains(t, logs.String(), `"event":"integration.error"`)
			for _, field := range []string{"principal_id", "request_id", "turn_ref", "call_ref", "http_status"} {
				require.Contains(t, logs.String(), `"`+field+`":`)
			}
			require.NotContains(t, logs.String(), "private-")
			require.NotContains(t, logs.String(), "/private/nas")
		})
	}
}
