package main

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/webui"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/DituLin/Atrium/internal/ws"
)

func serve(ctx context.Context, d *store.DB, blobs *archive.BlobStore, dir string, s state) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	cfg.Archives.NativeHelperPath = os.Getenv("ATRIUM_NATIVE_HELPER")
	home, e := clock.NewHome("Asia/Singapore", nil)
	must(e)
	logger := logging.Discard()
	bus := events.NewBus()
	server := httptest.NewUnstartedServer(nil)
	addressPath := filepath.Join(dir, "listen-address")
	address, addressErr := os.ReadFile(addressPath)
	if addressErr == nil {
		host, _, err := net.SplitHostPort(string(address))
		must(err)
		if host != "127.0.0.1" {
			panic("synthetic listener must be loopback")
		}
		must(server.Listener.Close())
		server.Listener, e = net.Listen("tcp", string(address))
		must(e)
	} else if errors.Is(addressErr, os.ErrNotExist) {
		must(os.WriteFile(addressPath, []byte(server.Listener.Addr().String()), 0600))
	} else {
		must(addressErr)
	}
	origin := "https://" + server.Listener.Addr().String()
	authenticator := auth.NewAuthenticator(d, auth.NewOriginPolicy([]string{origin}), nil)
	hub := ws.NewHub(ws.Options{Auth: authenticator, Bus: bus, Logger: logger})
	svc := screen.NewService(screen.Options{DB: d, Hub: hub, Logger: logger})
	hub.SetHandler(svc)
	go hub.Run(ctx)
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Home: home, Auth: authenticator, Logger: logger, ArchiveBlobs: blobs, ArchivePreview: archive.NativeParser{HelperPath: cfg.Archives.NativeHelperPath}, Archives: archive.NewIntake(blobs, d.ArchiveTasks()), Pairing: auth.NewPairingService(d, nil), Admin: auth.NewAdminService(d), Snapshot: widget.NewComposer(cfg, d, home, nil), Commands: svc, Sessions: hub, WS: hub, Bus: bus, UI: webui.New(webui.Options{FS: os.DirFS("web/dist")})})
	server.Config.Handler = api.Handler()
	server.StartTLS()
	defer server.Close()
	must(os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	screenToken, e := os.ReadFile(filepath.Join(dir, "screen-token"))
	must(e)
	adminPath := filepath.Join(dir, "admin-token")
	adminBytes, err := os.ReadFile(adminPath)
	if errors.Is(err, os.ErrNotExist) {
		admin, _, issueErr := auth.NewAdminService(d).EnsureInitial(ctx, "T15 synthetic", time.Now())
		must(issueErr)
		if admin == "" {
			panic("existing synthetic admin has no retained token; explicit recovery required")
		}
		must(os.WriteFile(adminPath, []byte(admin), 0600))
		adminBytes = []byte(admin)
	} else {
		must(err)
	}
	admin := string(adminBytes)
	if admin == "" {
		panic("empty synthetic admin credential")
	}
	writeJSON(filepath.Join(dir, "browser-state.json"), map[string]string{"origin": server.URL, "screen_token": string(screenToken), "integration_token": s.Token, "admin_token": admin})
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(4 * time.Hour)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
				return
			}
			_, e := d.ArchiveTasks().IndexArchives(ctx, s.Home, 32)
			if errors.Is(e, domain.ErrConflict) {
				continue
			}
			if e != nil {
				panic(fmt.Errorf("synthetic index: %w", e))
			}
		}
	}
}
