package domain

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode is a stable API error identifier (technical design §8).
type ErrorCode string

// Stable error codes. Keep in sync with docs/api/openapi.yaml.
const (
	CodeUnauthorized       ErrorCode = "unauthorized"
	CodeForbidden          ErrorCode = "forbidden"
	CodeNotFound           ErrorCode = "not_found"
	CodeInvalidRequest     ErrorCode = "invalid_request"
	CodeInvalidCommand     ErrorCode = "invalid_command"
	CodeScreenOffline      ErrorCode = "screen_offline"
	CodeScreenRevoked      ErrorCode = "screen_revoked"
	CodePairingExpired     ErrorCode = "pairing_expired"
	CodePairingClaimed     ErrorCode = "pairing_claimed"
	CodeRateLimited        ErrorCode = "rate_limited"
	CodePreviewProcessing  ErrorCode = "preview_processing"
	CodePreviewUnavailable ErrorCode = "preview_unavailable"
	CodeSourceOffline      ErrorCode = "source_offline"
	CodeIdentityMismatch   ErrorCode = "identity_mismatch"
	CodeConflict           ErrorCode = "conflict"
	CodeInternal           ErrorCode = "internal"
)

// AllErrorCodes lists every stable code, used by tests and documentation.
func AllErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeUnauthorized, CodeForbidden, CodeNotFound, CodeInvalidRequest,
		CodeInvalidCommand, CodeScreenOffline, CodeScreenRevoked, CodePairingExpired,
		CodePairingClaimed, CodeRateLimited, CodePreviewProcessing, CodePreviewUnavailable,
		CodeSourceOffline, CodeIdentityMismatch, CodeConflict, CodeInternal,
	}
}

// HTTPStatus maps an error code to its canonical HTTP status.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeInvalidRequest, CodeInvalidCommand:
		return http.StatusBadRequest
	case CodeScreenOffline, CodeConflict, CodeIdentityMismatch:
		return http.StatusConflict
	case CodeScreenRevoked, CodePairingExpired, CodePairingClaimed:
		return http.StatusGone
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodePreviewProcessing:
		return http.StatusAccepted
	case CodePreviewUnavailable, CodeSourceOffline:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Error is an API-facing error carrying a stable code and optional details.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	// Status overrides the canonical status for the code when non-zero.
	Status int   `json:"-"`
	Cause  error `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.Cause }

// HTTPStatus returns the status this error should be rendered with.
func (e *Error) HTTPStatus() int {
	if e.Status != 0 {
		return e.Status
	}
	return e.Code.HTTPStatus()
}

// Errorf builds an Error with a formatted message.
func Errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WrapErr builds an Error wrapping cause.
func WrapErr(code ErrorCode, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// WithDetails attaches details and returns the same error for chaining.
func (e *Error) WithDetails(d map[string]any) *Error {
	e.Details = d
	return e
}

// WithStatus overrides the HTTP status.
func (e *Error) WithStatus(s int) *Error {
	e.Status = s
	return e
}

// AsError extracts a *Error from an error chain.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Sentinel errors shared across packages.
var (
	// ErrNotFound signals a missing row; repositories return it directly.
	ErrNotFound = errors.New("not found")
	// ErrConflict signals a uniqueness or state conflict.
	ErrConflict = errors.New("conflict")
)
