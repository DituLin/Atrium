// Package familyarchive writes an explicit family archive, separately from the
// read-only photo source filesystem. SQLite stays on the local system disk.
package familyarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarkerFile is the archive-root identity marker filename.
const MarkerFile = ".atrium-family-archive"

// MaxDocumentBytes bounds one immutable family archive document.
const MaxDocumentBytes = 128 * 1024

// ErrUnavailable reports an archive that could not be safely accessed or published.
var ErrUnavailable = errors.New("family archive unavailable")

// ErrConflict reports content that conflicts with an immutable archive document.
var ErrConflict = errors.New("family archive content conflict")

// Document is the versioned immutable family record stored in an archive.
type Document struct {
	SchemaVersion string              `json:"schema_version"`
	Record        domain.FamilyRecord `json:"record"`
}

// Options configures the family exporter and its trusted archive root.
type Options struct {
	DB         *store.DB
	Root       string
	Identity   string
	AllowLocal bool
	Now        func() time.Time
	Timeout    time.Duration
}

// Exporter publishes pending family records with bounded filesystem concurrency.
type Exporter struct {
	opts Options
	fs   *source.OSFS
	slot chan struct{}
}

// New validates exporter configuration and creates its filesystem probe.
func New(o Options) (*Exporter, error) {
	if o.DB == nil || !filepath.IsAbs(o.Root) || strings.TrimSpace(o.Identity) == "" || len(o.Identity) > 128 {
		return nil, errors.New("invalid family archive configuration")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Timeout <= 0 {
		o.Timeout = 20 * time.Second
	}
	fs, err := source.NewOSFS(source.OSFSOptions{Root: o.Root, IOTimeout: o.Timeout, MaxInflight: 1})
	if err != nil {
		return nil, err
	}
	return &Exporter{opts: o, fs: fs, slot: make(chan struct{}, 1)}, nil
}

// Export bounds waiting for SMB I/O. A stuck syscall retains the sole slot:
// subsequent passes fail promptly instead of accumulating blocked goroutines.
// Late publication is harmless: only a completed DB acknowledgement marks sync.
func (e *Exporter) Export(ctx context.Context) (int, error) {
	select {
	case e.slot <- struct{}{}:
	default:
		return 0, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, e.opts.Timeout)
	defer cancel()
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { defer func() { <-e.slot }(); n, err := e.export(ctx); done <- result{n, err} }()
	select {
	case r := <-done:
		return r.n, r.err
	case <-ctx.Done():
		return 0, ErrUnavailable
	}
}
func (e *Exporter) export(ctx context.Context) (int, error) {
	pending, err := e.opts.DB.FamilyRecords().PendingExports(ctx, 50)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	stat, err := e.fs.Statfs(ctx)
	if err != nil || (!e.opts.AllowLocal && !source.IsNetworkFilesystem(stat.FSType)) {
		return 0, ErrUnavailable
	}
	info, err := os.Lstat(e.opts.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, ErrUnavailable
	}
	root, err := os.OpenRoot(e.opts.Root)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer func() { _ = root.Close() }() // Read-only directory descriptor cleanup.
	marker, err := readBounded(root, MarkerFile, 256)
	if err != nil || strings.TrimSpace(string(marker)) != e.opts.Identity {
		return 0, ErrUnavailable
	}
	n := 0
	for _, r := range pending {
		if err = ctx.Err(); err != nil {
			return n, err
		}
		r.ExportedAt = nil // The immutable document does not depend on acknowledgement.
		r.SupersededBy = ""
		version := "1"
		if r.SupersedesID != "" {
			version = "2"
		}
		raw, err := json.MarshalIndent(Document{SchemaVersion: version, Record: r}, "", "  ")
		if err != nil {
			return n, err
		}
		raw = append(raw, '\n')
		parts := []string{r.MemberID, r.OccurredAt.UTC().Format("2006"), r.OccurredAt.UTC().Format("01")}
		rel := ""
		for _, part := range parts {
			rel = filepath.Join(rel, part)
			if err = root.Mkdir(rel, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return n, ErrUnavailable
			}
			i, err := root.Lstat(rel)
			if err != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
				return n, ErrUnavailable
			}
		}
		path := filepath.Join(rel, r.ID+".json")
		existing, err := readBounded(root, path, MaxDocumentBytes)
		if err == nil {
			if !bytes.Equal(existing, raw) {
				return n, ErrConflict
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err = publish(root, path, raw); err != nil {
				return n, err
			}
		} else {
			return n, ErrUnavailable
		}
		if err = e.opts.DB.FamilyRecords().MarkExported(ctx, r.ID, e.opts.Now()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
func readBounded(root *os.Root, path string, limit int64) ([]byte, error) {
	info, err := root.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrConflict
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // Read-only source cleanup; read errors are handled separately.
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, ErrConflict
	}
	return raw, nil
}
func publish(root *os.Root, path string, raw []byte) error {
	temp := path + "." + domain.NewID() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = root.Remove(temp) }() // Best-effort unpublished staging cleanup; Rename removes this name on success.
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrUnavailable
	}
	if err = root.Rename(temp, path); err != nil {
		return ErrUnavailable
	}
	actual, err := readBounded(root, path, MaxDocumentBytes)
	if err != nil || !bytes.Equal(raw, actual) {
		return fmt.Errorf("%w: verification failed", ErrUnavailable)
	}
	return nil
}
