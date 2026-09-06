package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/backup"
	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/diag"
	"github.com/DituLin/Atritum/internal/indexer"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/screen"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/widget"
)

// Deps are the collaborators the HTTP layer needs. Everything is an explicit
// field so tests can substitute fakes without touching package state.
type Deps struct {
	Config   *config.Config
	DB       *store.DB
	Logger   *slog.Logger
	Auth     *auth.Authenticator
	Pairing  *auth.PairingService
	Home     *clock.Home
	Snapshot *widget.Composer
	Now      func() time.Time
	// Insecure is true when tls.mode = off; it drops the Secure cookie flag.
	Insecure bool
	// StartedAt feeds uptime in diagnostics.
	StartedAt time.Time
	// UI serves the embedded web client; may be nil in tests.
	UI http.Handler

	// V0.2 collaborators. Each is optional so a test can wire only what the
	// route under test needs; handlers guard on nil.

	// Sources is the runtime source registry (health, identity, filesystems).
	Sources *source.Manager
	// Index is the scan scheduler behind the admin scan routes and the home
	// snapshot's index progress.
	Index *indexer.Scheduler
	// Queue enqueues preview rebuilds requested by the media route.
	Queue *jobs.Queue
	// Cache reads derived images from disk.
	Cache *media.Cache
	// Media owns access-time accounting and the cache janitor.
	Media *media.Pipeline
	// Bus publishes change notifications to the V0.3 hub.
	Bus *events.Bus

	// V0.3 and H1 collaborators.

	// Commands issues, validates and resolves screen commands.
	Commands *screen.Service
	// Sessions is the WebSocket hub: it owns presence and delivery.
	Sessions Sessions
	// WS serves the /screens/connect upgrade.
	WS http.Handler
	// Diagnostics aggregates the operator document of design §6.11.
	Diagnostics *diag.Aggregator
	// Backup writes and lists SQLite snapshots.
	Backup *backup.Service
	// Admin issues and rotates admin credentials.
	Admin *auth.AdminService
	// TokenFileDir is where `admin token rotate` expects the new token to be
	// written by the CLI; the server only reports it.
	TokenFileDir string
}

// Sessions is the presence and revocation surface the HTTP layer needs from
// the WebSocket hub. Keeping it an interface lets tests answer presence
// without opening sockets.
type Sessions interface {
	Online(screenID string) bool
	Revoke(screenID, reason string)
}

// API holds the routed handler and its dependencies.
type API struct {
	deps    Deps
	mux     *http.ServeMux
	handler http.Handler
	limits  rateLimits
}

// rateLimits holds the per-route limiters from design §6.8.
type rateLimits struct {
	pairStartIP     *auth.Limiter
	pairStartGlobal *auth.Limiter
	pairPoll        *auth.Limiter
}

// New builds the API with its middleware chain.
func New(deps Deps) *API {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Diagnostics == nil && deps.DB != nil {
		// A route that answers 500 because a collaborator is missing is worse
		// than one that answers with the sections it can build.
		deps.Diagnostics = diag.New(diag.Deps{
			DB: deps.DB, Presence: deps.Sessions, StartedAt: deps.StartedAt,
			Insecure: deps.Insecure, Now: deps.Now,
		})
	}
	a := &API{
		deps: deps,
		mux:  http.NewServeMux(),
		limits: rateLimits{
			pairStartIP:     auth.NewLimiter(auth.LimiterOptions{PerMinute: PairStartPerMinutePerIP, Now: deps.Now}),
			pairStartGlobal: auth.NewLimiter(auth.LimiterOptions{PerMinute: PairStartPerMinuteTotal, Now: deps.Now}),
			pairPoll:        auth.NewLimiter(auth.LimiterOptions{PerMinute: PairPollPerMinute, Now: deps.Now}),
		},
	}
	a.routes()
	a.handler = Chain(a.mux,
		WithRequestID(),
		WithLogger(deps.Logger),
		WithRecovery(),
		WithSecurityHeaders(),
		WithAccessLog(),
	)
	return a
}

// Handler returns the fully wrapped HTTP handler.
func (a *API) Handler() http.Handler { return a.handler }

// now reads the injected clock.
func (a *API) now() time.Time { return a.deps.Now() }

// requireScope authenticates a request and stores the identity on its context.
func (a *API) requireScope(scope auth.Scope, next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := a.deps.Auth.Authenticate(r.Context(), r, scope)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		ctx := auth.WithIdentity(r.Context(), id)
		next(w, r.WithContext(ctx))
	}
}

// Server wraps http.Server with the lifecycle the app expects.
type Server struct {
	http *http.Server
}

// NewServer builds an HTTP server with the timeouts from design §4.1.
func NewServer(addr string, handler http.Handler, log *slog.Logger) *Server {
	return &Server{http: &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}}
}

// HTTP exposes the underlying server for listener control.
func (s *Server) HTTP() *http.Server { return s.http }

// Shutdown stops the server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
