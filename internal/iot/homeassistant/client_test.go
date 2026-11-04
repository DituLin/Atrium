package homeassistant

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type sink chan iot.Update

func (s sink) Publish(ctx context.Context, u iot.Update) error {
	select {
	case s <- u:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func fixture(t *testing.T) config.HomeAssistant {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if e := os.WriteFile(p, []byte("synthetic-secret"), 0600); e != nil {
		t.Fatal(e)
	}
	return config.HomeAssistant{ID: "home", URL: "https://example.invalid", TokenFile: p, Devices: []config.IoTDevice{{ID: "lamp", Name: "Lamp", RoomID: "room", RoomName: "Room", Capabilities: []config.IoTCapability{{ID: "power", Kind: iot.Power, EntityID: "switch.lamp"}, {ID: "battery", Kind: iot.Battery, EntityID: "switch.lamp", Attribute: "battery", Unit: "%"}}}}}
}
func server(t *testing.T, f func(context.Context, *websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/websocket" {
			t.Error("wrong endpoint")
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		f(r.Context(), c)
	}))
}
func write(ctx context.Context, c *websocket.Conn, v any) { _ = wsjson.Write(ctx, c, v) }
func read(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	var v map[string]any
	if e := wsjson.Read(ctx, c, &v); e != nil {
		t.Error(e)
	}
	return v
}
func handshake(t *testing.T, ctx context.Context, c *websocket.Conn) {
	write(ctx, c, map[string]any{"type": "auth_required"})
	v := read(t, ctx, c)
	if v["type"] != "auth" || v["access_token"] != "synthetic-secret" {
		t.Error("invalid auth")
	}
	write(ctx, c, map[string]any{"type": "auth_ok"})
	v = read(t, ctx, c)
	if v["type"] != "subscribe_events" || v["event_type"] != "state_changed" {
		t.Error("subscription missing")
	}
	write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true})
}
func state(value, ts string) map[string]any {
	return map[string]any{"entity_id": "switch.lamp", "state": value, "attributes": map[string]any{"battery": 87}, "last_updated": ts}
}
func event(s any, ts string) map[string]any {
	return map[string]any{"id": 1, "type": "event", "event": map[string]any{"event_type": "state_changed", "time_fired": ts, "data": map[string]any{"entity_id": "switch.lamp", "new_state": s}}}
}
func await(t *testing.T, s sink, p func(iot.Update) bool) iot.Update {
	t.Helper()
	for {
		select {
		case u := <-s:
			if p(u) {
				return u
			}
		case <-time.After(3 * time.Second):
			t.Fatal("update timeout")
		}
	}
}
func TestSnapshotRaceAndRemoval(t *testing.T) {
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		handshake(t, ctx, c)
		v := read(t, ctx, c)
		if v["type"] != "get_states" {
			t.Error("expected snapshot")
		}
		write(ctx, c, event(state("on", "2026-01-01T00:00:02Z"), "2026-01-01T00:00:02Z"))
		write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("off", "2026-01-01T00:00:01Z")}})
		write(ctx, c, event(nil, "2026-01-01T00:00:03Z"))
		<-ctx.Done()
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	cl, e := New(cfg, WithHTTPClient(srv.Client()))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := make(sink, 20)
	done := make(chan error, 1)
	go func() { done <- cl.Run(ctx, s) }()
	u := await(t, s, func(u iot.Update) bool { return u.Replace && u.Status == iot.Connected })
	if u.Observations[0].Value != true {
		t.Fatalf("snapshot overwrote event: %+v", u)
	}
	u = await(t, s, func(u iot.Update) bool { return !u.Replace && len(u.Observations) > 0 })
	for _, o := range u.Observations {
		if o.Value != nil || o.Reason != iot.ReasonMissing {
			t.Fatalf("removal retained data: %+v", o)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
}
func TestAuthRejectedIsSanitized(t *testing.T) {
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		write(ctx, c, map[string]any{"type": "auth_required"})
		read(t, ctx, c)
		write(ctx, c, map[string]any{"type": "auth_invalid", "message": "synthetic-secret private details"})
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	cl, e := New(cfg, WithHTTPClient(srv.Client()))
	if e != nil {
		t.Fatal(e)
	}
	s := make(sink, 20)
	if e = cl.Run(context.Background(), s); !errors.Is(e, ErrUnauthorized) {
		t.Fatalf("unexpected error %v", e)
	}
	b, _ := json.Marshal(<-s)
	if strings.Contains(string(b), "synthetic-secret") {
		t.Fatal("secret leak")
	}
	u := await(t, s, func(u iot.Update) bool { return u.Replace })
	if u.Status != iot.Disconnected {
		t.Fatal("cache not cleared")
	}
}
func TestTokenPermissions(t *testing.T) {
	cfg := fixture(t)
	_ = os.Chmod(cfg.TokenFile, 0644)
	if _, e := New(cfg); e == nil {
		t.Fatal("accepted world-readable token")
	}
}

func TestUnavailableAttributesAndInvalidValues(t *testing.T) {
	cfg := fixture(t)
	c, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		value     any
		attribute any
		reason    iot.Reason
	}{{"unavailable", 87, iot.ReasonUnavailable}, {"unknown", 87, iot.ReasonUnknown}, {"on", 999, iot.ReasonInvalid}, {"on", nil, iot.ReasonMissing}} {
		obs := c.observations(map[string]record{"switch.lamp": {state: &entity{State: tc.value, Attributes: map[string]any{"battery": tc.attribute}, Updated: "bad-time"}}}, 1)
		if obs[1].Reason != tc.reason || obs[1].Value != nil {
			t.Fatalf("invalid attribute accepted: %+v", obs[1])
		}
		if obs[1].SourceUpdatedAt != nil || obs[1].SampledAt != nil {
			t.Fatal("fabricated timestamp")
		}
	}
}
func TestHeartbeatReconcileReconnect(t *testing.T) {
	var sessions atomic.Int32
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		n := sessions.Add(1)
		handshake(t, ctx, c)
		snapshots := 0
		for {
			var v map[string]any
			if wsjson.Read(ctx, c, &v) != nil {
				return
			}
			switch v["type"] {
			case "get_states":
				snapshots++
				write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("on", "2026-01-01T00:00:01Z")}})
				if n == 1 && snapshots == 2 {
					return
				}
			case "ping":
				write(ctx, c, map[string]any{"id": v["id"], "type": "pong"})
			default:
				t.Errorf("forbidden command %v", v["type"])
				return
			}
		}
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	c, e := New(cfg, WithHTTPClient(srv.Client()), WithTimings(Timings{Heartbeat: 10 * time.Millisecond, Timeout: time.Second, Reconcile: 40 * time.Millisecond, RetryMin: time.Millisecond, RetryMax: time.Millisecond}))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := make(sink, 100)
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, s) }()
	u := await(t, s, func(u iot.Update) bool { return u.Replace && u.Generation == 2 && u.Status == iot.Connected })
	if u.Observations[0].Generation != 2 {
		t.Fatal("old session generation")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
}
func TestReconcileDoesNotRegressNewerLiveState(t *testing.T) {
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		handshake(t, ctx, c)
		v := read(t, ctx, c)
		write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("on", "2026-01-01T00:00:03Z")}})
		v = read(t, ctx, c)
		write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("off", "2026-01-01T00:00:01Z")}})
		<-ctx.Done()
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	c, e := New(cfg, WithHTTPClient(srv.Client()), WithTimings(Timings{Reconcile: 20 * time.Millisecond}))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := make(sink, 20)
	go c.Run(ctx, s)
	await(t, s, func(u iot.Update) bool { return u.Replace })
	u := await(t, s, func(u iot.Update) bool { return u.Replace })
	if u.Observations[0].Value != true {
		t.Fatal("reconciliation regressed known newer state")
	}
}

func TestHeartbeatTimeoutReconnects(t *testing.T) {
	var sessions atomic.Int32
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		sessions.Add(1)
		handshake(t, ctx, c)
		v := read(t, ctx, c)
		write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("on", "2026-01-01T00:00:01Z")}})
		for {
			var v map[string]any
			if wsjson.Read(ctx, c, &v) != nil {
				return
			}
			if v["type"] != "ping" {
				t.Error("unexpected command")
			}
		}
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	c, e := New(cfg, WithHTTPClient(srv.Client()), WithTimings(Timings{Heartbeat: 20 * time.Millisecond, Timeout: 50 * time.Millisecond, RetryMin: time.Millisecond}))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := make(sink, 20)
	go c.Run(ctx, s)
	await(t, s, func(u iot.Update) bool { return u.Generation == 2 && u.Status == iot.Connected })
	if sessions.Load() < 2 {
		t.Fatal("missing reconnect")
	}
}
func TestTokenSymlinkRefused(t *testing.T) {
	cfg := fixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if e := os.Symlink(cfg.TokenFile, link); e != nil {
		t.Fatal(e)
	}
	cfg.TokenFile = link
	if _, e := New(cfg); e == nil {
		t.Fatal("accepted symlink token")
	}
}
func TestUntrustedTLSRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS reached handler") }))
	defer srv.Close()
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	cfg := fixture(t)
	cfg.URL = srv.URL
	c, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = c.session(ctx, make(sink, 20), 1); !errors.Is(e, errSession) {
		t.Fatal("untrusted TLS accepted")
	}
}

func TestInjectedClientCannotDisableTLS(t *testing.T) {
	cfg := fixture(t)
	_, e := New(cfg, WithHTTPClient(&http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}))
	if e == nil {
		t.Fatal("accepted TLS verification bypass")
	}
}

func TestUnknownEventOrderResynchronizes(t *testing.T) {
	srv := server(t, func(ctx context.Context, c *websocket.Conn) {
		handshake(t, ctx, c)
		v := read(t, ctx, c)
		write(ctx, c, map[string]any{"id": v["id"], "type": "result", "success": true, "result": []any{state("on", "2026-01-01T00:00:03Z")}})
		write(ctx, c, event(state("off", "not-a-time"), "not-a-time"))
		<-ctx.Done()
	})
	defer srv.Close()
	cfg := fixture(t)
	cfg.URL = srv.URL
	c, e := New(cfg, WithHTTPClient(srv.Client()))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := make(sink, 20)
	if e = c.session(ctx, s, 1); !errors.Is(e, errSession) {
		t.Fatalf("unknown event order did not request resynchronization: %v", e)
	}
}
