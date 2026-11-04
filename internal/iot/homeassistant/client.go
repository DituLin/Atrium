// Package homeassistant implements the read-only Home Assistant WebSocket API.
package homeassistant

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/iot"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var ErrUnauthorized = errors.New("home assistant authentication rejected")
var errSession = errors.New("home assistant connection interrupted")

// Timings permits short deterministic integration tests; zero values use defaults.
type Timings struct{ Heartbeat, Timeout, Reconcile, RetryMin, RetryMax time.Duration }
type Option func(*Client)

func WithHTTPClient(h *http.Client) Option  { return func(c *Client) { c.http = h } }
func WithTimings(t Timings) Option          { return func(c *Client) { c.timings = t } }
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

type Client struct {
	cfg      config.HomeAssistant
	http     *http.Client
	token    string
	endpoint string
	timings  Timings
	now      func() time.Time
	bindings map[string][]binding
}
type binding struct {
	device string
	cap    config.IoTCapability
}
type entity struct {
	EntityID   string         `json:"entity_id"`
	State      any            `json:"state"`
	Attributes map[string]any `json:"attributes"`
	Updated    string         `json:"last_updated"`
}
type record struct {
	state   *entity
	updated time.Time
}
type message struct {
	Type    string          `json:"type"`
	ID      int             `json:"id"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Error   struct {
		Code string `json:"code"`
	} `json:"error"`
	Event struct {
		Type string `json:"event_type"`
		Time string `json:"time_fired"`
		Data struct {
			EntityID string  `json:"entity_id"`
			New      *entity `json:"new_state"`
		} `json:"data"`
	} `json:"event"`
}

// Equal timestamps cannot establish an ordering for divergent state payloads.
func conflicting(a, b record) bool {
	if !a.updated.Equal(b.updated) {
		return false
	}
	if a.state == nil || b.state == nil {
		return a.state != b.state
	}
	return !reflect.DeepEqual(a.state.State, b.state.State) || !reflect.DeepEqual(a.state.Attributes, b.state.Attributes)
}
func denied(m message) bool {
	return m.Type == "result" && !m.Success && m.Error.Code == "unauthorized"
}

func New(cfg config.HomeAssistant, options ...Option) (*Client, error) {
	if e := (config.IoT{Enabled: true, HomeAssistant: cfg}).Validate(); e != nil {
		return nil, errors.New("invalid home assistant configuration")
	}
	// Refuse symlinks and nonregular files; validate the opened descriptor too.
	st, e := os.Lstat(cfg.TokenFile)
	if e != nil || !st.Mode().IsRegular() {
		return nil, errors.New("invalid token file")
	}
	f, e := os.Open(cfg.TokenFile)
	if e != nil {
		return nil, errors.New("cannot read token file")
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(st, actual) || (actual.Mode().Perm() != 0600 && actual.Mode().Perm() != 0400) {
		return nil, errors.New("token file requires owner-only read/write permissions")
	}
	b, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil || len(b) > 16384 || strings.TrimSpace(string(b)) == "" {
		return nil, errors.New("invalid token file")
	}
	c := &Client{cfg: cfg, token: strings.TrimSpace(string(b)), now: time.Now, bindings: map[string][]binding{}}
	for _, o := range options {
		o(c)
	}
	if c.http == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.CAFile != "" {
			pem, e := os.ReadFile(cfg.CAFile)
			if e != nil {
				return nil, errors.New("cannot read CA file")
			}
			roots, e := x509.SystemCertPool()
			if e != nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid CA file")
			}
			tr.TLSClientConfig.RootCAs = roots
		}
		c.http = &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	// Copy the injected client so adapter policy cannot change its caller's client.
	hc := *c.http
	tr := hc.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	transport, ok := tr.(*http.Transport)
	if !ok || (transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify) {
		return nil, errors.New("HTTP client must verify TLS certificates")
	}
	hc.Transport = transport.Clone()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &hc
	defaults := Timings{20 * time.Second, 10 * time.Second, 5 * time.Minute, time.Second, 60 * time.Second}
	p := []*time.Duration{&c.timings.Heartbeat, &c.timings.Timeout, &c.timings.Reconcile, &c.timings.RetryMin, &c.timings.RetryMax}
	d := []time.Duration{defaults.Heartbeat, defaults.Timeout, defaults.Reconcile, defaults.RetryMin, defaults.RetryMax}
	for i, v := range p {
		if *v <= 0 {
			*v = d[i]
		}
	}
	u, _ := url.Parse(cfg.URL)
	u.Scheme = "wss"
	u.Path = "/api/websocket"
	c.endpoint = u.String()
	for _, dev := range cfg.Devices {
		for _, cap := range dev.Capabilities {
			c.bindings[cap.EntityID] = append(c.bindings[cap.EntityID], binding{dev.ID, cap})
		}
	}
	return c, nil
}
func (c *Client) observations(states map[string]record, g uint64) []iot.Observation {
	out := []iot.Observation{}
	now := c.now()
	for _, d := range c.cfg.Devices {
		for _, cap := range d.Capabilities {
			r := states[cap.EntityID]
			var raw any
			unit := cap.Unit
			var updated *time.Time
			if r.state != nil {
				s := r.state
				raw = s.State
				if cap.Attribute != "" && s.State != "unavailable" && s.State != "unknown" {
					raw = s.Attributes[cap.Attribute]
				} else if cap.Attribute == "" && unit == "" {
					unit, _ = s.Attributes["unit_of_measurement"].(string)
				}
				if t, e := time.Parse(time.RFC3339Nano, s.Updated); e == nil {
					updated = &t
				}
			}
			n := iot.Normalize(cap.Kind, raw, unit, cap.Values)
			out = append(out, iot.Observation{InstanceID: c.cfg.ID, DeviceID: d.ID, CapabilityID: cap.ID, Generation: g, Value: n.Value, Unit: n.Unit, Availability: n.Availability, Reason: n.Reason, SourceUpdatedAt: updated, ReceivedAt: now, ConfirmedAt: now})
		}
	}
	return out
}
func (c *Client) Run(ctx context.Context, sink iot.Sink) error {
	delay := c.timings.RetryMin
	for g := uint64(1); ; g++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e := sink.Publish(ctx, iot.Update{InstanceID: c.cfg.ID, Generation: g, Status: iot.Connecting}); e != nil {
			return e
		}
		started := c.now()
		e := c.session(ctx, sink, g)
		u := iot.Update{InstanceID: c.cfg.ID, Generation: g, Status: iot.Disconnected}
		if errors.Is(e, ErrUnauthorized) {
			u.Replace = true
			u.Observations = c.observations(nil, g)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if pe := sink.Publish(ctx, u); pe != nil {
			return pe
		}
		if errors.Is(e, ErrUnauthorized) {
			return e
		}
		if c.now().Sub(started) > c.timings.Heartbeat*2 {
			delay = c.timings.RetryMin
		}
		timer := time.NewTimer(time.Duration(float64(delay) * (0.8 + rand.Float64()*0.2)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay *= 2
		if delay > c.timings.RetryMax {
			delay = c.timings.RetryMax
		}
	}
}
func (c *Client) session(parent context.Context, sink iot.Sink, g uint64) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	dialCtx, dc := context.WithTimeout(ctx, c.timings.Timeout)
	conn, _, e := websocket.Dial(dialCtx, c.endpoint, &websocket.DialOptions{HTTPClient: c.http})
	dc()
	if e != nil {
		return errSession
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	incoming := make(chan message, 256)
	failure := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var m message
			if wsjson.Read(ctx, conn, &m) != nil {
				select {
				case failure <- struct{}{}:
				default:
				}
				return
			}
			select {
			case incoming <- m:
			case <-ctx.Done():
				return
			default:
				select {
				case failure <- struct{}{}:
				default:
				}
				return
			}
		}
	}()
	defer func() { cancel(); conn.CloseNow(); <-done }()
	send := func(v any) error {
		wctx, wc := context.WithTimeout(ctx, c.timings.Timeout)
		defer wc()
		if wsjson.Write(wctx, conn, v) != nil {
			return errSession
		}
		return nil
	}
	receive := func() (message, error) {
		timer := time.NewTimer(c.timings.Timeout)
		defer timer.Stop()
		select {
		case m := <-incoming:
			return m, nil
		case <-timer.C:
			return message{}, errSession
		case <-failure:
			return message{}, errSession
		case <-ctx.Done():
			return message{}, ctx.Err()
		}
	}
	m, e := receive()
	if e != nil || m.Type != "auth_required" {
		return errSession
	}
	if e = send(map[string]any{"type": "auth", "access_token": c.token}); e != nil {
		return e
	}
	m, e = receive()
	if e != nil {
		return e
	}
	if m.Type == "auth_invalid" {
		return ErrUnauthorized
	}
	if m.Type != "auth_ok" {
		return errSession
	}
	if e = send(map[string]any{"id": 1, "type": "subscribe_events", "event_type": "state_changed"}); e != nil {
		return e
	}
	m, e = receive()
	if e == nil && denied(m) {
		return ErrUnauthorized
	}
	if e != nil || m.Type != "result" || m.ID != 1 || !m.Success {
		return errSession
	}
	id := 2
	snapshotID := id
	pending := true
	buffer := map[string]record{}
	bufferCount := 0
	states := map[string]record{}
	deadline := time.NewTimer(c.timings.Timeout)
	defer deadline.Stop()
	if e = send(map[string]any{"id": id, "type": "get_states"}); e != nil {
		return e
	}
	heartbeat := time.NewTicker(c.timings.Heartbeat)
	defer heartbeat.Stop()
	reconcile := time.NewTicker(c.timings.Reconcile)
	defer reconcile.Stop()
	pong := time.NewTimer(time.Hour)
	pong.Stop()
	defer pong.Stop()
	pingID := 0
	publish := func(replace bool, changed string) error {
		obs := c.observations(states, g)
		if !replace {
			filtered := obs[:0]
			for _, o := range obs {
				for _, b := range c.bindings[changed] {
					if b.device == o.DeviceID && b.cap.ID == o.CapabilityID {
						filtered = append(filtered, o)
					}
				}
			}
			obs = filtered
		}
		return sink.Publish(ctx, iot.Update{InstanceID: c.cfg.ID, Generation: g, Status: iot.Connected, Replace: replace, Observations: obs})
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-failure:
			return errSession
		case <-deadline.C:
			return errSession
		case <-pong.C:
			return errSession
		case <-heartbeat.C:
			if pingID == 0 {
				id++
				pingID = id
				if e = send(map[string]any{"id": id, "type": "ping"}); e != nil {
					return e
				}
				pong.Reset(c.timings.Timeout)
			}
		case <-reconcile.C:
			if !pending {
				id++
				snapshotID = id
				pending = true
				buffer = map[string]record{}
				bufferCount = 0
				deadline.Reset(c.timings.Timeout)
				if e = send(map[string]any{"id": id, "type": "get_states"}); e != nil {
					return e
				}
			}
		case m := <-incoming:
			if denied(m) {
				return ErrUnauthorized
			}
			if m.Type == "pong" && m.ID == pingID {
				pingID = 0
				pong.Stop()
				continue
			}
			if m.Type == "result" && pending && m.ID == snapshotID {
				if !m.Success {
					return errSession
				}
				var list []entity
				if json.Unmarshal(m.Result, &list) != nil {
					return errSession
				}
				next := map[string]record{}
				for i := range list {
					s := list[i]
					if _, ok := c.bindings[s.EntityID]; ok {
						t, _ := time.Parse(time.RFC3339Nano, s.Updated)
						next[s.EntityID] = record{&s, t}
						if prior, ok := states[s.EntityID]; ok && !prior.updated.IsZero() {
							if t.IsZero() || conflicting(prior, next[s.EntityID]) {
								return errSession
							}
							if prior.updated.After(t) {
								next[s.EntityID] = prior
							}
						}
					}
				}
				for key, r := range buffer {
					old, ok := next[key]
					if ok && (old.updated.IsZero() || conflicting(old, r)) {
						return errSession
					}
					if !ok || !r.updated.Before(old.updated) || r.updated.IsZero() {
						next[key] = r
					}
				}
				states = next
				pending = false
				deadline.Stop()
				if e = publish(true, ""); e != nil {
					return e
				}
				continue
			}
			if m.Type == "event" && m.ID == 1 && m.Event.Type == "state_changed" {
				key := m.Event.Data.EntityID
				if _, ok := c.bindings[key]; !ok {
					continue
				}
				s := m.Event.Data.New
				t, _ := time.Parse(time.RFC3339Nano, m.Event.Time)
				if s != nil {
					t, _ = time.Parse(time.RFC3339Nano, s.Updated)
				}
				if t.IsZero() {
					return errSession
				}
				r := record{s, t}
				if pending {
					bufferCount++
					if bufferCount > 1024 {
						return errSession
					}
					old, ok := buffer[key]
					if ok && conflicting(old, r) {
						return errSession
					}
					if !ok || !t.Before(old.updated) || t.IsZero() {
						buffer[key] = r
					}
				} else {
					old, ok := states[key]
					if ok && conflicting(old, r) {
						return errSession
					}
					if !ok || !t.Before(old.updated) || t.IsZero() {
						states[key] = r
						if e = publish(false, key); e != nil {
							return e
						}
					}
				}
			}
		}
	}
}
