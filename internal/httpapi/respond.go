// Package httpapi owns the HTTP surface: routing, middleware, DTOs and the
// mapping from domain errors to responses.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/DituLin/Atritum/internal/domain"
)

// errorBody is the wire shape of every error response (design §8).
type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    domain.ErrorCode `json:"code"`
	Message string           `json:"message"`
	Details map[string]any   `json:"details,omitempty"`
}

// WriteJSON renders v with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError renders an error using its stable code. Unknown errors become
// `internal` so no implementation detail reaches the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr, ok := domain.AsError(err)
	if !ok {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			apiErr = domain.Errorf(domain.CodeNotFound, "not found")
		case errors.Is(err, domain.ErrConflict):
			apiErr = domain.Errorf(domain.CodeConflict, "conflict")
		default:
			apiErr = domain.Errorf(domain.CodeInternal, "internal error")
		}
	}
	status := apiErr.HTTPStatus()
	if status == http.StatusTooManyRequests {
		if retry, ok := apiErr.Details["retry_after_seconds"].(int); ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
		}
	}
	if status >= http.StatusInternalServerError {
		if log := LoggerFrom(r.Context()); log != nil {
			log.Error("request failed", "code", string(apiErr.Code), "error", apiErr.Error())
		}
	}
	WriteJSON(w, status, errorBody{Error: errorPayload{
		Code: apiErr.Code, Message: apiErr.Message, Details: apiErr.Details,
	}})
}

// DecodeJSON reads a JSON request body with a size limit and rejects unknown
// fields so a typo in a client is a visible error.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBody = 1 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.WrapErr(domain.CodeInvalidRequest, err, "request body is not valid JSON")
	}
	return nil
}

// LoggerFrom returns the request-scoped logger, or nil.
func LoggerFrom(ctx interface{ Value(any) any }) *slog.Logger {
	log, _ := ctx.Value(loggerKey{}).(*slog.Logger)
	return log
}

// WriteJSONError renders an error with extra top-level fields. It is used
// where the design says the failure carries a body of its own, such as the
// 409 that returns the command it just recorded as failed (design §6.5).
func WriteJSONError(w http.ResponseWriter, status int, apiErr *domain.Error, extra map[string]any) {
	body := map[string]any{
		"error": errorPayload{Code: apiErr.Code, Message: apiErr.Message, Details: apiErr.Details},
	}
	for k, v := range extra {
		body[k] = v
	}
	WriteJSON(w, status, body)
}
