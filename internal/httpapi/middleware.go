package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
)

type loggerKey struct{}
type requestIDKey struct{}

// RequestIDHeader is echoed on every response so logs and clients agree.
const RequestIDHeader = "X-Request-Id"

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in order; the first entry is outermost.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// RequestIDFrom returns the request identifier attached to a context.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithRequestID assigns an identifier to each request and echoes it back.
func WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithLogger attaches a request-scoped logger carrying the request ID.
func WithLogger(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := base.With("component", "http", "request_id", RequestIDFrom(r.Context()))
			ctx := context.WithValue(r.Context(), loggerKey{}, log)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithRecovery converts a panic into a 500 and keeps the server alive.
func WithRecovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if log := LoggerFrom(r.Context()); log != nil {
					log.Error("panic in handler",
						"event", "panic", "path", r.URL.Path,
						"panic", rec, "stack", string(debug.Stack()))
				}
				WriteError(w, r, domain.Errorf(domain.CodeInternal, "internal error"))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter records the response status and size for the access log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
		s.ResponseWriter.WriteHeader(code)
	}
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// WithAccessLog logs one line per request. It never records credentials, and
// query strings are dropped because they may carry identifiers a screen owns.
func WithAccessLog() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			log := LoggerFrom(r.Context())
			if log == nil {
				return
			}
			attrs := []any{
				"event", "http_request",
				"method", r.Method,
				"path", redactPath(r.URL.Path),
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", auth.ClientIP(r),
			}
			if id := auth.FromContext(r.Context()); id != nil {
				attrs = append(attrs, "scope", string(id.Scope))
				if id.Screen != nil {
					attrs = append(attrs, "screen_id", id.Screen.ID)
				}
			}
			log.Info("request", attrs...)
		})
	}
}

// redactPath keeps the route shape but drops opaque identifiers that would
// otherwise let a log reader enumerate photos.
func redactPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if len(p) == 26 && isULID(p) {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

func isULID(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}

// WithSecurityHeaders sets the headers appropriate for a same-origin SPA that
// loads no third-party resources.
func WithSecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
					"connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			next.ServeHTTP(w, r)
		})
	}
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(buf)
}
