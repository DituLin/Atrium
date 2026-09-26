package homemcp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"gopkg.in/yaml.v3"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixture(t *testing.T, h http.HandlerFunc) Config {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	d := t.TempDir()
	ca := filepath.Join(d, "ca.pem")
	tok := filepath.Join(d, "token")
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600)
	_ = os.WriteFile(tok, []byte("service-secret"), 0600)
	return Config{CoreURL: s.URL, CAFile: ca, TokenFile: tok, RequestTimeoutMS: 500}
}
func session(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	s, e := NewServer(cfg)
	if e != nil {
		t.Fatal(e)
	}
	a, b := mcp.NewInMemoryTransports()
	ss, e := s.Connect(context.Background(), b, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), a, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func TestDiscoveryAndValidation(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached core") })
	cs := session(t, cfg)
	ls, e := cs.ListTools(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(ls.Tools) != 11 {
		t.Fatalf("tools=%d", len(ls.Tools))
	}
	for _, tool := range ls.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		_ = json.Unmarshal(b, &schema)
		if schema["additionalProperties"] != false || tool.OutputSchema == nil || tool.Annotations == nil {
			t.Fatalf("incomplete tool %s", tool.Name)
		}
	}
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{"token": "x"}})
	if e != nil {
		t.Fatal(e)
	}
	if !res.IsError {
		t.Fatal("extra argument accepted")
	}
}
func TestReadRedactsUnexpectedBackendFields(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-secret" {
			t.Error("missing service auth")
		}
		_, _ = w.Write([]byte(`{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"online","data":{"core":"online","timezone":"Asia/Singapore","day":"2026-09-06","capabilities":[],"token":"service-secret","path":"/private/nas"}}`))
	})
	cs := session(t, cfg)
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var x map[string]any
	if json.Unmarshal(b, &x) != nil {
		t.Fatal("invalid json")
	}
	if res.IsError {
		t.Fatalf("error: %s", b)
	}
	if string(b) != res.Content[0].(*mcp.TextContent).Text {
		t.Fatal("text differs")
	}
	if string(b) == "" {
		t.Fatal("empty")
	}
	data := x["data"].(map[string]any)
	if data["token"] != nil || data["path"] != nil {
		t.Fatal("secret leaked")
	}
}

func TestControlStatesAndPolling(t *testing.T) {
	for _, state := range []string{"accepted", "applied", "failed", "expired", "unknown"} {
		t.Run(state, func(t *testing.T) {
			var posts atomic.Int32
			cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts.Add(1)
				}
				status := state
				if r.Method == "POST" && state == "applied" {
					status = "accepted"
				}
				if state == "failed" {
					w.WriteHeader(409)
				}
				fmt.Fprintf(w, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":"cmd1","status":%q}}`, status)
			})
			cs := session(t, cfg)
			start := time.Now()
			res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "wait_ms": 200}})
			if e != nil {
				t.Fatal(e)
			}
			if res.IsError != (state == "failed" || state == "expired" || state == "unknown") {
				t.Fatalf("state=%s error=%v", state, res.IsError)
			}
			if posts.Load() != 1 {
				t.Fatal("repeated POST")
			}
			if time.Since(start) > time.Second {
				t.Fatal("poll budget exceeded")
			}
			if res.StructuredContent.(map[string]any)["data"].(map[string]any)["status"] != state {
				t.Fatal("incorrect state")
			}
		})
	}
}
func TestLostPOSTResponseIsUnknown(t *testing.T) {
	var posts atomic.Int32
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		h := w.(http.Hijacker)
		conn, _, _ := h.Hijack()
		_ = conn.Close()
	})
	cs := session(t, cfg)
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV"}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "outcome_unknown") || !strings.Contains(string(b), "01ARZ3NDEKTSV4RRFFQ69G5FAV") || posts.Load() != 1 {
		t.Fatalf("wrong unknown handling: %s", b)
	}
}
func TestTLSAndRedirectAndOversize(t *testing.T) {
	for _, mode := range []string{"ca", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect leaked request") }))
			defer target.Close()
			cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, http.StatusFound)
				} else {
					_, _ = w.Write([]byte(strings.Repeat("x", maxOutput+1)))
				}
			})
			if mode == "ca" {
				other := httptest.NewTLSServer(http.NotFoundHandler())
				defer other.Close()
				_ = os.WriteFile(cfg.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: other.Certificate().Raw}), 0600)
				cfg.CoreURL = strings.Replace(cfg.CoreURL, "127.0.0.1", "127.0.0.2", 1)
			}
			cs := session(t, cfg)
			res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
			if e != nil {
				t.Fatal(e)
			}
			if !res.IsError {
				t.Fatal("unsafe response accepted")
			}
			b, _ := json.Marshal(res)
			if strings.Contains(string(b), "service-secret") || strings.Contains(string(b), cfg.CoreURL) {
				t.Fatal("sensitive error")
			}
		})
	}
}
func TestConfigRejectsUnsafeSettings(t *testing.T) {
	cfg := fixture(t, http.NotFound)
	for _, u := range []string{"http://localhost", "https://user:pass@localhost", "https://localhost/api", "https://localhost?token=x", "https://localhost/#fragment"} {
		bad := cfg
		bad.CoreURL = u
		if _, e := NewServer(bad); e == nil {
			t.Fatalf("accepted %s", u)
		}
	}
	_ = os.Chmod(cfg.TokenFile, 0644)
	if _, e := NewServer(cfg); e == nil {
		t.Fatal("accepted world-readable token")
	}
	_ = os.Chmod(cfg.TokenFile, 0600)
	link := cfg.TokenFile + "-link"
	_ = os.Symlink(cfg.TokenFile, link)
	cfg.TokenFile = link
	if _, e := NewServer(cfg); e == nil {
		t.Fatal("accepted symlink")
	}
	p := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(p, []byte("core_url: https://localhost\nadmin_token: secret\n"), 0600)
	if _, e := LoadConfig(p); e == nil {
		t.Fatal("accepted extra field")
	}
}
func TestConcurrencyBounds(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		fmt.Fprint(w, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"core":"online"}}`)
	})
	cs := session(t, cfg)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
		}()
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
	close(release)
	wg.Wait()
	if e != nil || !res.IsError {
		t.Fatal("concurrency cap not enforced")
	}
}

func TestPerScreenMutationBusy(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		fmt.Fprint(w, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":"cmd1","status":"accepted"}}`)
	})
	cs := session(t, cfg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "wait_ms": 0}})
	}()
	<-entered
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAW", "wait_ms": 0}})
	close(release)
	<-done
	if e != nil || !res.IsError {
		t.Fatal("screen mutation not serialized")
	}
}

func TestStdioNegotiationAndEOF(t *testing.T) {
	cfg := fixture(t, http.NotFound)
	dir := t.TempDir()
	bin := filepath.Join(dir, "home-mcp")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/home-mcp")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	conf := filepath.Join(dir, "config.yaml")
	b, _ := yaml.Marshal(cfg)
	_ = os.WriteFile(conf, b, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.Command(bin, "--config", conf)
	command.Stderr = &stderr
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if e != nil {
		t.Fatal(e)
	}
	ls, e := cs.ListTools(ctx, nil)
	if e != nil || len(ls.Tools) != 11 {
		t.Fatalf("stdio discovery %v", e)
	}
	if e = cs.Close(); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(stderr.String(), "service-secret") {
		t.Fatal("token logged")
	}
	if command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("EOF exit failed: %v %s", command.ProcessState, stderr.String())
	}
}

func TestCertificateFailureHasStableCode(t *testing.T) {
	cfg := fixture(t, http.NotFound)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, e := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(7), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(7), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(cfg.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	cs := session(t, cfg)
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "tls_error") {
		t.Fatalf("missing TLS classification: %s", b)
	}
}

func TestUnparseablePOSTRetainsOperation(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("broken")) })
	cs := session(t, cfg)
	res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV"}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "outcome_unknown") || !strings.Contains(string(b), "01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatalf("lost recoverable operation: %s", b)
	}
}
func TestInvalidArgumentsNeverReachCore(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid arguments reached Core") })
	cs := session(t, cfg)
	for _, tt := range []struct {
		name string
		args map[string]any
	}{{"home_get_screen", map[string]any{"screen_id": "../private"}}, {"home_refresh_screen", map[string]any{"screen_id": "tv", "operation_id": "invalid"}}, {"home_refresh_screen", map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "wait_ms": 5001}}, {"home_list_photos", map[string]any{"limit": 51}}, {"home_list_photos", map[string]any{"limit": 1.5}}, {"home_list_photos", map[string]any{"collection": "secrets"}}, {"home_navigate_screen", map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "route": "dashboard", "collection": "all"}}} {
		res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.name, Arguments: tt.args})
		if e != nil || !res.IsError {
			t.Fatalf("accepted invalid %s: %v", tt.name, e)
		}
	}
}

func TestPollUncertaintyRetainsIdentifiers(t *testing.T) {
	for _, body := range []string{"broken", `{"schema_version":"1","data":{"id":"cmd1","status":"failed"}}`, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":"cmd1","status":"surprise"}}`} {
		t.Run(body, func(t *testing.T) {
			cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					fmt.Fprint(w, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":"cmd1","status":"accepted"}}`)
				} else {
					fmt.Fprint(w, body)
				}
			})
			cs := session(t, cfg)
			res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV"}})
			if e != nil {
				t.Fatal(e)
			}
			b, _ := json.Marshal(res)
			for _, want := range []string{"outcome_unknown", "cmd1", "01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
				if !strings.Contains(string(b), want) {
					t.Fatalf("missing %s in %s", want, b)
				}
			}
		})
	}
}
func TestNavigatePhotosRequiresCollection(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("implicit collection reached Core") })
	cs := session(t, cfg)
	r, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_navigate_screen", Arguments: map[string]any{"screen_id": "tv", "route": "photos", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV"}})
	if e != nil || !r.IsError {
		t.Fatal("missing collection accepted")
	}
}
func TestCompleteResultSizeBound(t *testing.T) {
	r := result(map[string]any{"schema_version": "1", "observed_at": time.Now().UTC(), "availability": "available", "data": map[string]any{"name": strings.Repeat("x", 20000)}}, false)
	b, _ := json.Marshal(r)
	if len(b) > maxOutput {
		t.Fatalf("complete MCP result %d exceeds %d", len(b), maxOutput)
	}
	if !r.IsError {
		t.Fatal("oversize result not rejected")
	}
}

func TestCancellationReachesCoreHTTP(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(canceled) })
	cfg.RequestTimeoutMS = 10000
	cs := session(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "home_get_status", Arguments: map[string]any{}})
	}()
	<-entered
	cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("MCP cancellation did not reach Core request")
	}
	<-done
}
func TestStdioSignalAndClientCancellation(t *testing.T) {
	cfg := fixture(t, http.NotFound)
	dir := t.TempDir()
	bin := filepath.Join(dir, "home-mcp")
	if b, e := exec.Command("go", "build", "-o", bin, "../../cmd/home-mcp").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	conf := filepath.Join(dir, "config.yaml")
	b, _ := yaml.Marshal(cfg)
	_ = os.WriteFile(conf, b, 0600)
	t.Run("signal", func(t *testing.T) {
		command := exec.Command(bin, "--config", conf)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cs, e := mcp.NewClient(&mcp.Implementation{Name: "signal-test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
		if e != nil {
			t.Fatal(e)
		}
		if e = command.Process.Signal(syscall.SIGTERM); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- cs.Wait() }()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("child did not terminate after signal")
		}
		_ = cs.Close()
		if command.ProcessState == nil || !command.ProcessState.Success() {
			t.Fatalf("signal exit: %v %s", command.ProcessState, stderr.String())
		}
	})
	t.Run("client_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		command := exec.CommandContext(ctx, bin, "--config", conf)
		cs, e := mcp.NewClient(&mcp.Implementation{Name: "cancel-test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
		if e != nil {
			cancel()
			t.Fatal(e)
		}
		cancel()
		done := make(chan error, 1)
		go func() { done <- cs.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("canceled child remained live")
		}
		_ = cs.Close()
		if command.ProcessState == nil || !command.ProcessState.Exited() && command.ProcessState.String() == "" {
			t.Fatal("missing child termination evidence")
		}
	})
}

func TestControlRejectsUnrelatedCommandIDs(t *testing.T) {
	for _, tt := range []struct {
		name, postID, postState, pollID, pollState string
		wait                                       int
	}{{"wrong_applied", "cmd1", "accepted", "cmd2", "applied", 500}, {"path_accepted", "cmd1", "accepted", "../private", "accepted", 500}, {"invalid_post_wait_zero", "../private", "accepted", "", "", 0}, {"invalid_post_terminal", "../private", "applied", "", "", 500}, {"invalid_post_failed", "../private", "failed", "", "", 500}} {
		t.Run(tt.name, func(t *testing.T) {
			var gets atomic.Int32
			cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				id, state := tt.postID, tt.postState
				if r.Method == "GET" {
					gets.Add(1)
					if r.URL.Path != "/api/v1/integrations/commands/cmd1" {
						t.Errorf("unexpected poll path %s", r.URL.Path)
					}
					id, state = tt.pollID, tt.pollState
				}
				fmt.Fprintf(w, `{"schema_version":"1","observed_at":"2026-09-06T00:00:00Z","availability":"available","data":{"id":%q,"status":%q}}`, id, state)
			})
			cs := session(t, cfg)
			res, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_refresh_screen", Arguments: map[string]any{"screen_id": "tv", "operation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "wait_ms": tt.wait}})
			if e != nil {
				t.Fatal(e)
			}
			b, _ := json.Marshal(res)
			if !res.IsError || !strings.Contains(string(b), "outcome_unknown") || !strings.Contains(string(b), "01ARZ3NDEKTSV4RRFFQ69G5FAV") {
				t.Fatalf("unrelated command accepted: %s", b)
			}
			if tt.postID == "cmd1" && !strings.Contains(string(b), "cmd1") {
				t.Fatal("original command lost")
			}
			if gets.Load() > 1 {
				t.Fatal("continued polling untrusted command")
			}
		})
	}
}
