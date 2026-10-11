package store_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/DituLin/Atrium/internal/webui"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/DituLin/Atrium/internal/ws"
	"github.com/stretchr/testify/require"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in synthetic live Core for browser and separately installed TV preview.
// It never opens the user's DB or sends provider messages. Its only credentials
// are written to an operator-selected private temporary directory.
func TestT14SyntheticTVServer(t *testing.T) {
	dir := os.Getenv("ATRIUM_T14_LIVE_DIR")
	if dir == "" {
		t.Skip("synthetic live opt in")
	}
	require.True(t, filepath.IsAbs(dir))
	require.NoError(t, os.MkdirAll(dir, 0700))
	d := testutil.NewDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	_, e := d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	for _, domainName := range []string{"health", "education", "finance", "property"} {
		_, f, x := publicationFixture(t, d, r, domainName, domain.ArchiveInputText)
		bundle := publicationBundle(t, "70")
		for i := range bundle.Understanding.Chunks {
			extracted := bundle.Understanding.Chunks[i].Result
			extracted.Domain = domainName
			raw, _ := json.Marshal(extracted)
			ch, err := archive.ValidateChunk(bundle.Plan, bundle.Plan.Chunks[i], raw)
			require.NoError(t, err)
			bundle.Understanding.Chunks[i] = ch
		}
		bundle.Understanding, e = archive.MergeChunks(bundle.Plan, bundle.Understanding.Chunks)
		require.NoError(t, e)
		f = saveBundle(t, r, f, bundle)
		_, e = r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	blobs, e := archive.NewBlobStore(filepath.Join(dir, "originals"), 32<<20)
	require.NoError(t, e)
	defer blobs.Close()
	image, e := os.Open("../../adapters/macos-archive-parser/evidence/report.png")
	require.NoError(t, e)
	ref, e := blobs.Put(ctx, "home-1", image, archive.PutOptions{})
	image.Close()
	require.NoError(t, e)
	imageReceipt, e := r.AcceptIntake(ctx, archiveActor(t, d), "home-1", 1, domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputFile, Channel: "synthetic", Account: "test", Conversation: "test", Sender: "test", ProviderMessageID: "preview-image"}, OriginalName: "合成报告.png"}, ref)
	require.NoError(t, e)
	pdf, e := os.Open("../../adapters/macos-archive-parser/evidence/geometry.pdf")
	require.NoError(t, e)
	pdfRef, e := blobs.Put(ctx, "home-1", pdf, archive.PutOptions{})
	pdf.Close()
	require.NoError(t, e)
	pdfReceipt, e := r.AcceptIntake(ctx, archiveActor(t, d), "home-1", 1, domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputFile, Channel: "synthetic", Account: "test", Conversation: "test", Sender: "test", ProviderMessageID: "preview-pdf"}, OriginalName: "合成四页旋转报告.pdf"}, pdfRef)
	require.NoError(t, e)
	_, e = indexAllArchiveSearch(ctx, r, "home-1")
	require.NoError(t, e)
	screenToken, e := auth.NewScreenToken()
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "preview-browser", Name: "合成浏览器", TokenHash: auth.HashToken(screenToken)}))
	_, token, e := auth.NewIntegrationService(d, nil).Issue(ctx, "synthetic-control", domain.IntegrationPolicy{Permissions: []string{"archive.read", "archive.analyze", "screens.control", "screens.read", "commands.read"}, HouseholdIDs: []string{"home-1"}, ScreenIDs: []string{"preview-browser"}}, time.Now().Add(time.Hour))
	require.NoError(t, e)
	admin, _, e := auth.NewAdminService(d).EnsureInitial(ctx, "synthetic", time.Now())
	require.NoError(t, e)
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	cfg.Archives.NativeHelperPath = os.Getenv("ATRIUM_T14_PREVIEW_HELPER")
	home, e := clock.NewHome("Asia/Singapore", nil)
	require.NoError(t, e)
	logger := logging.Discard()
	bus := events.NewBus()
	server := httptest.NewUnstartedServer(nil)
	if listen := os.Getenv("ATRIUM_T14_LISTEN"); listen != "" {
		server.Listener.Close()
		listener, err := net.Listen("tcp", listen)
		require.NoError(t, err)
		server.Listener = listener
	}
	origin := "https://" + server.Listener.Addr().String()
	authenticator := auth.NewAuthenticator(d, auth.NewOriginPolicy([]string{origin}), nil)
	hub := ws.NewHub(ws.Options{Auth: authenticator, Bus: bus, Logger: logger})
	svc := screen.NewService(screen.Options{DB: d, Hub: hub, Logger: logger})
	hub.SetHandler(svc)
	go hub.Run(ctx)
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Home: home, Auth: authenticator, Logger: logger, ArchiveBlobs: blobs, ArchivePreview: archive.NativeParser{HelperPath: cfg.Archives.NativeHelperPath}, Archives: archive.NewIntake(blobs, d.ArchiveTasks()), Pairing: auth.NewPairingService(d, nil), Admin: auth.NewAdminService(d), Snapshot: widget.NewComposer(cfg, d, home, nil), Commands: svc, Sessions: hub, WS: hub, Bus: bus, UI: webui.New(webui.Options{FS: os.DirFS("../../web/dist")})})
	server.Config.Handler = api.Handler()
	server.StartTLS()
	defer server.Close()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	data, _ := json.Marshal(map[string]string{"origin": server.URL, "screen_token": screenToken, "integration_token": token, "admin_token": admin, "image_document": imageReceipt.DocumentID, "pdf_document": pdfReceipt.DocumentID})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), data, 0600))
	t.Log("synthetic Core ready; private state file created")
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			return
		case <-tick.C:
			if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
				return
			}
		}
	}
}
