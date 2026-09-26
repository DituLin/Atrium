package homemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTraceSDKMetadataAndRedaction(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "poisoned"}[valid], func(t *testing.T) {
			var logs bytes.Buffer
			var turn, call string
			cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				turn = r.Header.Get(integration.TurnRefHeader)
				call = r.Header.Get(integration.CallRefHeader)
				if valid {
					w.Header().Set("X-Request-Id", "req_0123456789abcdef")
				} else {
					w.Header().Set("X-Request-Id", "secret-poison")
				}
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":{"code":"secret-poison","message":"service-secret /private/nas"}}`))
			})
			srv, err := NewServerWithLogger(cfg, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			a, b := mcp.NewInMemoryTransports()
			ss, err := srv.Connect(context.Background(), b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "trace-test", Version: "1"}, nil).Connect(context.Background(), a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			ref := "01K4D6H0000000000000000001"
			meta := mcp.Meta{integration.TurnRefMeta: ref, integration.CallRefMeta: ref}
			if !valid {
				meta = mcp.Meta{integration.TurnRefMeta: "secret-poison", integration.CallRefMeta: map[string]any{"secret": "secret-poison"}}
			}
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}, Meta: meta})
			if err != nil || !res.IsError {
				t.Fatalf("result %v %v", res, err)
			}
			if !integration.ValidTraceRef(call) || valid && turn != ref || !valid && turn != "" {
				t.Fatalf("trace headers %q %q", turn, call)
			}
			text := logs.String()
			for _, secret := range []string{"secret-poison", "service-secret", "/private/nas", cfg.CoreURL, "/api/v1/"} {
				if strings.Contains(text, secret) {
					t.Fatalf("leaked %s: %s", secret, text)
				}
			}
			lines := strings.Split(strings.TrimSpace(text), "\n")
			if len(lines) != 2 {
				t.Fatalf("logs %s", text)
			}
			for _, line := range lines {
				var item map[string]any
				if json.Unmarshal([]byte(line), &item) != nil {
					t.Fatal(line)
				}
				if item["call_ref"] != call || item["duration_ms"] == nil {
					t.Fatal(item)
				}
			}
			if valid && !strings.Contains(text, "req_0123456789abcdef") {
				t.Fatal(text)
			}
		})
	}
}

func TestTracePollingAndCancellation(t *testing.T) {
	var logs bytes.Buffer
	var mu sync.Mutex
	var headers []http.Header
	pollStarted := make(chan struct{})
	ref := "01K4D6H0000000000000000001"
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("X-Request-Id", "req_0123456789abcdef")
		if r.Method == "GET" {
			close(pollStarted)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":"01K4D6H0000000000000000002","screen_id":"tv","sequence":1,"kind":"refresh","payload":{},"issued_at":"2026-09-06T00:00:00Z","expires_at":"2026-09-06T00:00:10Z","status":"accepted"}}`))
	})
	client, token, err := cfg.client()
	if err != nil {
		t.Fatal(err)
	}
	a := &adapter{client: client, token: token, origin: cfg.CoreURL, logger: slog.New(slog.NewJSONHandler(&logs, nil)), slots: make(chan struct{}, 4), busy: map[string]bool{}}
	ctx, cancel := context.WithTimeout(withCallTrace(context.Background(), nil), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-pollStarted:
			cancel()
		case <-ctx.Done():
		}
	}()
	var out *mcp.CallToolResult
	started := time.Now()
	in := args{OperationID: ref, ScreenID: "tv"}
	out = a.call(ctx, toolDef{name: "home_refresh_screen", mutation: true}, in)
	a.logTool(ctx, "home_refresh_screen", in, out, started)
	if out.IsError {
		t.Fatalf("accepted action changed: %+v", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 2 {
		t.Fatalf("headers %d", len(headers))
	}
	call := headers[0].Get(integration.CallRefHeader)
	if !integration.ValidTraceRef(call) || headers[1].Get(integration.CallRefHeader) != call {
		t.Fatal(headers)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			t.Fatal(line)
		}
		records = append(records, record)
		if record["call_ref"] != call {
			t.Fatal(record)
		}
	}
	if len(records) != 3 || records[1]["http_status"] != float64(0) || records[2]["operation_id"] != ref || records[2]["command_id"] != "01K4D6H0000000000000000002" {
		t.Fatal(records)
	}
	for _, secret := range []string{cfg.CoreURL, "service-secret", "/api/v1/"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal(logs.String())
		}
	}
}

func TestTraceRejectsUntrustedCommandAndError(t *testing.T) {
	var logs bytes.Buffer
	a := &adapter{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	out := result(map[string]any{"data": map[string]any{"id": "secret-command", "error_code": "secret-error"}}, true)
	a.logTool(withCallTrace(context.Background(), nil), "home_get_command", args{OperationID: "secret-operation"}, out, time.Now())
	if strings.Contains(logs.String(), "secret-") || !strings.Contains(logs.String(), "tool_error") {
		t.Fatal(logs.String())
	}
}

func TestTracePreservesProjectedCoreCommandErrorCodes(t *testing.T) {
	for _, code := range []string{"screen_offline", "delivery_failed", "expired", "ack_timeout", "server_restart", "session_replaced", "photo_unavailable", "render_failed", "invalid_route", "superseded", "preview_unavailable", "preview_processing", "source_offline", "screen_revoked", "invalid_command"} {
		t.Run(code, func(t *testing.T) {
			var logs bytes.Buffer
			a := &adapter{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			out := result(map[string]any{"data": map[string]any{"id": "01K4D6H0000000000000000002", "error_code": code}}, true)
			a.logTool(withCallTrace(context.Background(), nil), "home_get_command", args{}, out, time.Now())
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["error_code"] != code {
				t.Fatalf("error_code=%v want %s", record["error_code"], code)
			}
		})
	}
	for _, code := range []any{"unexpected-secret", "/private/nas", map[string]any{"code": "screen_offline"}, nil} {
		if got := safeTraceError(code); got != "tool_error" {
			t.Fatalf("unknown code %v accepted as %s", code, got)
		}
	}
}
