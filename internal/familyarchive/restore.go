package familyarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
)

// RestoreOptions selects the trusted archive, new output database and member mapping.
type RestoreOptions struct {
	Root        string
	Identity    string
	Destination string
	Members     []domain.FamilyMember
	AllowLocal  bool
}

// Restore publishes a new family-only database after validating the complete
// archive. Existing destinations are never overwritten; source files are read
// only. Screen credentials, original IM idempotency keys and other Core data
// are not present in the archive and cannot be reconstructed from it.
func Restore(ctx context.Context, o RestoreOptions) (count int, err error) {
	if !filepath.IsAbs(o.Root) || !filepath.IsAbs(o.Destination) || strings.TrimSpace(o.Identity) == "" || len(o.Identity) > 128 || len(o.Members) == 0 {
		return 0, errors.New("absolute archive/output paths, identity and members are required")
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if _, err = os.Lstat(o.Destination); !errors.Is(err, os.ErrNotExist) {
		return 0, errors.New("recovery output must not already exist")
	}
	archivePath, err := filepath.EvalSymlinks(o.Root)
	if err != nil {
		return 0, err
	}
	outputParent, err := filepath.EvalSymlinks(filepath.Dir(o.Destination))
	if err != nil {
		return 0, err
	}
	relative, err := filepath.Rel(archivePath, outputParent)
	if err != nil || relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return 0, errors.New("recovery output must be outside the archive")
	}
	rootInfo, err := os.Lstat(o.Root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return 0, ErrUnavailable
	}
	probe, err := source.NewOSFS(source.OSFSOptions{Root: o.Root, IOTimeout: 20 * time.Second, MaxInflight: 1})
	if err != nil {
		return 0, err
	}
	stat, err := probe.Statfs(ctx)
	if err != nil || (!o.AllowLocal && !source.IsNetworkFilesystem(stat.FSType)) {
		return 0, ErrUnavailable
	}
	destProbe, err := source.NewOSFS(source.OSFSOptions{Root: filepath.Dir(o.Destination), IOTimeout: 20 * time.Second, MaxInflight: 1})
	if err != nil {
		return 0, err
	}
	destStat, err := destProbe.Statfs(ctx)
	if err != nil || source.IsNetworkFilesystem(destStat.FSType) {
		return 0, errors.New("recovery database must be on a local filesystem")
	}
	root, err := os.OpenRoot(o.Root)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }() // Read-only directory descriptor cleanup.
	marker, err := readBounded(root, MarkerFile, 256)
	if err != nil || strings.TrimSpace(string(marker)) != o.Identity {
		return 0, ErrUnavailable
	}
	temp, err := os.CreateTemp(filepath.Dir(o.Destination), ".family-recovery-*.db")
	if err != nil {
		return 0, err
	}
	tempPath := temp.Name()
	if err = temp.Close(); err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(tempPath); _ = os.Remove(tempPath + "-wal"); _ = os.Remove(tempPath + "-shm") }()
	db, err := store.Open(ctx, tempPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }() // Recovery cleanup; publication requires the checked Close below.
	if _, err = db.Migrate(ctx); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	members := map[string]bool{}
	for _, member := range o.Members {
		if members[member.ID] {
			return 0, errors.New("duplicate member mapping")
		}
		members[member.ID] = true
		if err = db.FamilyRecords().PutMember(ctx, member, now); err != nil {
			return 0, err
		}
	}
	digest := sha256.Sum256([]byte(o.Identity))
	identity := hex.EncodeToString(digest[:])
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if path == MarkerFile || !strings.HasSuffix(path, ".json") {
			return nil
		}
		if count >= 100000 {
			return errors.New("archive exceeds 100000 records")
		}
		raw, err := readBounded(root, path, MaxDocumentBytes)
		if err != nil {
			return err
		}
		var doc Document
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&doc); err != nil {
			return fmt.Errorf("invalid archive document %s: %w", path, err)
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			return fmt.Errorf("trailing JSON in %s", path)
		}
		r := doc.Record
		expected := filepath.ToSlash(filepath.Join(r.MemberID, r.OccurredAt.UTC().Format("2006/01"), r.ID+".json"))
		validVersion := (doc.SchemaVersion == "1" && r.SupersedesID == "") || (doc.SchemaVersion == "2" && r.SupersedesID != "")
		if !validVersion || r.ExportedAt != nil || r.SupersededBy != "" || path != expected || !members[r.MemberID] {
			return fmt.Errorf("invalid archive identity or member mapping: %s", path)
		}
		if err = db.RestoreFamilyRecord(ctx, r, identity, now); err != nil {
			return fmt.Errorf("invalid archive record %s: %w", path, err)
		}
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err = db.RestoreFamilyRevisionLinks(ctx); err != nil {
		return 0, err
	}
	if err = db.Checkpoint(ctx); err != nil {
		return 0, err
	}
	if err = db.Close(); err != nil {
		return 0, err
	}
	// Recheck the marker and cancellation before publication. A no-replace link
	// makes concurrent creation of the requested destination fail safely.
	marker, err = readBounded(root, MarkerFile, 256)
	if err != nil || strings.TrimSpace(string(marker)) != o.Identity {
		return 0, ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(tempPath, os.O_RDWR, 0600) // #nosec G304 -- tempPath is generated by CreateTemp in the validated local output parent; the file is synced and closed before no-replace publication.
	if err != nil {
		return 0, err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if err = os.Link(tempPath, o.Destination); err != nil {
		return 0, err
	}
	return count, nil
}
