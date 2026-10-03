//go:build darwin || linux

// Package archive stores immutable originals for trusted internal services.
package archive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/DituLin/Atrium/internal/domain"
	"golang.org/x/sys/unix"
)

const DefaultMaxBlobBytes int64 = 32 << 20

var householdPathEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var (
	ErrEmpty    = errors.New("archive: empty original")
	ErrTooLarge = errors.New("archive: original exceeds byte limit")
	ErrMismatch = errors.New("archive: original does not match expected metadata")
	ErrCorrupt  = errors.New("archive: stored original does not match its reference")
)

// BlobStore pins a trusted host-local archive root. Put/OpenVerified may run
// concurrently. Close waits for them and is idempotent; returned read handles
// remain usable after Close. There is no GC or deletion of published originals.
type BlobStore struct {
	mu       sync.RWMutex
	root     *os.File
	maxBytes int64
	fs       blobIO
}

// Only filesystem failure boundaries are substitutable, per store instance.
// Production always uses ordinary writes, fsync and atomic no-overwrite links.
type blobIO struct {
	write func(*os.File, []byte) (int, error)
	sync  func(*os.File) error
	link  func(int, string, int, string, int) error
}

type PutOptions struct {
	ExpectedSize   *int64 // nil means unspecified; explicit zero is rejected.
	ExpectedSHA256 string // empty means unspecified; otherwise canonical lowercase.
}

// NewBlobStore creates only the final root component (0700). Its parent must
// already exist and be trusted. OS aliases in ancestors, e.g. /tmp, are allowed;
// the final root and every descendant reject symlinks. Zero selects 32 MiB.
func NewBlobStore(root string, maxBytes int64) (*BlobStore, error) {
	return newBlobStore(root, maxBytes, blobIO{write: (*os.File).Write, sync: (*os.File).Sync, link: unix.Linkat})
}

func newBlobStore(root string, maxBytes int64, fs blobIO) (*BlobStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return nil, fmt.Errorf("archive: root must be an absolute directory path")
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxBlobBytes
	}
	if maxBytes < 1 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("archive: invalid byte limit")
	}
	fd, err := unix.Open(filepath.Dir(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("archive: open trusted parent: %w", err)
	}
	parent := os.NewFile(uintptr(fd), "archive-parent")
	defer parent.Close()
	s := &BlobStore{maxBytes: maxBytes, fs: fs}
	s.root, err = s.directory(parent, filepath.Base(root), true)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *BlobStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil
	}
	err := s.root.Close()
	s.root = nil
	return err
}

// directory uses only a pinned parent descriptor and a validated/generated
// single component. Sync even an existing parent entry to let a retry complete
// a prior interrupted creation/publication whose sync failed.
func (s *BlobStore) directory(parent *os.File, name string, create bool) (*os.File, error) {
	if create {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, fmt.Errorf("archive: create directory: %w", err)
		}
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("archive: open directory: %w", err)
	}
	dir := os.NewFile(uintptr(fd), name)
	if create {
		if err := s.fs.sync(parent); err != nil {
			dir.Close()
			return nil, fmt.Errorf("archive: sync parent directory: %w", err)
		}
	}
	return dir, nil
}

// Storage version 1 encodes the exact validated household ID bytes as lowercase
// unpadded base32. Distinct IDs remain distinct on case-insensitive filesystems;
// the maximum 128-byte ID becomes 205 bytes, within a 255-byte name component.
// Domain identities and authorization remain case-sensitive and unchanged.
func householdDirectoryName(household string) string {
	return householdPathEncoding.EncodeToString([]byte(household))
}

func (s *BlobStore) blobDirectory(ref domain.ArchiveBlobRef, create bool) (*os.File, error) {
	parent := s.root
	for _, component := range []string{householdDirectoryName(ref.HouseholdID), "blobs", ref.SHA256[:2]} {
		next, err := s.directory(parent, component, create)
		if parent != s.root {
			parent.Close()
		}
		if err != nil {
			return nil, err
		}
		parent = next
	}
	return parent, nil
}

// Put streams at most max+1 bytes. Context is checked around each Reader call;
// callers own deadlines/closure for blocking readers (no background read goroutine).
// Only a completed publication returns metadata. A later DB failure must leave
// the published blob in place so a retry can verify and reuse it.
func (s *BlobStore) Put(ctx context.Context, household string, r io.Reader, opts PutOptions) (out domain.ArchiveBlobRef, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.root == nil {
		return out, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := domain.ValidateArchiveIdentifier(household); err != nil {
		return out, err
	}
	if r == nil {
		return out, fmt.Errorf("archive: missing reader")
	}
	if opts.ExpectedSize != nil && (*opts.ExpectedSize < 1 || *opts.ExpectedSize > s.maxBytes) {
		return out, ErrMismatch
	}
	if opts.ExpectedSHA256 != "" {
		if err := domain.ValidateArchiveSHA256(opts.ExpectedSHA256); err != nil {
			return out, err
		}
	}
	name := ".staging-" + rand.Text()
	fd, err := unix.Openat(int(s.root.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return out, fmt.Errorf("archive: create staging file: %w", err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() {
		closeErr := f.Close()
		unlinkErr := unix.Unlinkat(int(s.root.Fd()), name, 0)
		err = errors.Join(err, closeErr, unlinkErr)
		if err != nil {
			out = domain.ArchiveBlobRef{}
		}
	}()
	size, hash, mime, err := inspectStream(ctx, r, s.maxBytes, func(p []byte) error {
		n, err := s.fs.write(f, p)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		return err
	})
	if err != nil {
		return out, err
	}
	if size == 0 {
		return out, ErrEmpty
	}
	if opts.ExpectedSize != nil && *opts.ExpectedSize != size || opts.ExpectedSHA256 != "" && opts.ExpectedSHA256 != hash {
		return out, ErrMismatch
	}
	ref := domain.ArchiveBlobRef{HouseholdID: household, SHA256: hash, SizeBytes: size, MIME: mime, StorageVersion: domain.ArchiveStorageVersion}
	if err := s.fs.sync(f); err != nil {
		return out, fmt.Errorf("archive: sync original: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	dir, err := s.blobDirectory(ref, true)
	if err != nil {
		return out, err
	}
	defer dir.Close()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	err = s.fs.link(int(s.root.Fd()), name, int(dir.Fd()), hash, 0)
	if errors.Is(err, unix.EEXIST) {
		existing, verifyErr := openVerifiedAt(ctx, dir, ref)
		if verifyErr != nil {
			return out, verifyErr
		}
		if err := existing.Close(); err != nil {
			return out, err
		}
	} else if err != nil {
		return out, fmt.Errorf("archive: publish original: %w", err)
	}
	// Never unlink the final name, including on sync/cancellation/DB failures.
	if err := s.fs.sync(dir); err != nil {
		return out, fmt.Errorf("archive: sync blob directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return ref, nil
}

// OpenVerified requires a reference obtained from an authorized document. A
// raw hash is not download permission. It returns a read-only handle positioned
// at byte zero only after full size/hash/MIME verification; no pathname reopen.
func (s *BlobStore) OpenVerified(ctx context.Context, ref domain.ArchiveBlobRef) (*os.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.root == nil {
		return nil, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	dir, err := s.blobDirectory(ref, false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return openVerifiedAt(ctx, dir, ref)
}

func openVerifiedAt(ctx context.Context, dir *os.File, ref domain.ArchiveBlobRef) (*os.File, error) {
	// NONBLOCK prevents a forged FIFO from hanging before Fstat rejects it.
	fd, err := unix.Openat(int(dir.Fd()), ref.SHA256, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("archive: open original: %w", err)
	}
	f := os.NewFile(uintptr(fd), ref.SHA256)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		f.Close()
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != ref.SizeBytes || ref.SizeBytes == math.MaxInt64 {
		f.Close()
		return nil, ErrCorrupt
	}
	size, hash, mime, err := inspectStream(ctx, f, ref.SizeBytes, nil)
	if err != nil {
		f.Close()
		return nil, err
	}
	if size != ref.SizeBytes || hash != ref.SHA256 || mime != ref.MIME {
		f.Close()
		return nil, ErrCorrupt
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func inspectStream(ctx context.Context, r io.Reader, limit int64, write func([]byte) error) (int64, string, string, error) {
	h := sha256.New()
	sniff := make([]byte, 0, 512)
	buf := make([]byte, 32*1024)
	var total int64
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", "", err
		}
		p := buf[:min(int64(len(buf)), limit+1-total)]
		n, err := r.Read(p)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, "", "", ctxErr
		}
		if n < 0 || n > len(p) {
			return 0, "", "", fmt.Errorf("archive: invalid reader count")
		}
		if err != nil && err != io.EOF {
			return 0, "", "", err
		}
		if n > 0 {
			total += int64(n)
			if total > limit {
				return 0, "", "", ErrTooLarge
			}
			if write != nil {
				if err := write(p[:n]); err != nil {
					return 0, "", "", err
				}
			}
			h.Write(p[:n])
			sniff = append(sniff, p[:min(n, 512-len(sniff))]...)
			emptyReads = 0
		} else {
			emptyReads++
		}
		if err == io.EOF {
			break
		}
		if emptyReads >= 100 {
			return 0, "", "", io.ErrNoProgress
		}
	}
	return total, fmt.Sprintf("%x", h.Sum(nil)), http.DetectContentType(sniff), nil
}
