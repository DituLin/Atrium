package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
)

func integrationTraceFields(r *http.Request) map[string]string {
	fields := map[string]string{"request_id": RequestIDFrom(r.Context())}
	if value := r.Header.Get(integration.TurnRefHeader); integration.ValidTraceRef(value) {
		fields["turn_ref"] = value
	}
	if value := r.Header.Get(integration.CallRefHeader); integration.ValidTraceRef(value) {
		fields["call_ref"] = value
	}
	return fields
}

func beginIntegrationTrace(w http.ResponseWriter, r *http.Request, principal, permission string) (http.ResponseWriter, *http.Request, func()) {
	log := LoggerFrom(r.Context())
	if log == nil {
		return w, r, func() {}
	}
	log = log.With("principal_id", principal, "permission", permission)
	fields := integrationTraceFields(r)
	for _, key := range []string{"turn_ref", "call_ref"} {
		if value := fields[key]; value != "" {
			log = log.With(key, value)
		}
	}
	r = r.WithContext(context.WithValue(r.Context(), loggerKey{}, log))
	observed := &statusWriter{ResponseWriter: w}
	started := time.Now()
	return observed, r, func() {
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		log.InfoContext(r.Context(), "Integration request completed", "event", "integration.request", "http_status", status, "duration_ms", time.Since(started).Milliseconds())
	}
}
