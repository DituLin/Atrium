//go:build darwin || linux

package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func newTestBlobs(t *testing.T, limit int64) (*BlobStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "archives")
	s, err := NewBlobStore(root, limit)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s, root
}

func blobPath(root string, ref domain.ArchiveBlobRef) string {
	return filepath.Join(root, ref.HouseholdID, "blobs", ref.SHA256[:2], ref.SHA256)
}

func testRef(home string, content []byte) domain.ArchiveBlobRef {
	return domain.ArchiveBlobRef{HouseholdID: home, SHA256: fmt.Sprintf("%x", sha256.Sum256(content)), SizeBytes: int64(len(content)), MIME: http.DetectContentType(content), StorageVersion: 1}
}

func assertOriginal(t *testing.T, s *BlobStore, ref domain.ArchiveBlobRef, content []byte) {
	t.Helper()
	f, err := s.OpenVerified(context.Background(), ref)
	require.NoError(t, err)
	defer f.Close()
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, content, got)
}

func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), ".staging-"), e.Name())
	}
}

func TestBlobPutPreservesBytesAndSniffsMIME(t *testing.T) {
	s, root := newTestBlobs(t, 0)
	for _, content := range [][]byte{[]byte("喝奶 70ml"), []byte("%PDF-1.7\noriginal"), {0, 1, 2, 3, 0xff}, []byte("PK\x03\x04office container"), bytes.Repeat([]byte("x"), 600)} {
		ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, testRef("home-1", content), ref)
		assertOriginal(t, s, ref, content)
		info, err := os.Stat(blobPath(root, ref))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	assertNoStaging(t, root)
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestBlobPutBoundedAndValidatesExpectedMetadata(t *testing.T) {
	for _, n := range []int{0, 1, 8, 9, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, root := newTestBlobs(t, 8)
			r := bytes.NewReader(bytes.Repeat([]byte("x"), n))
			ref, err := s.Put(context.Background(), "home-1", r, PutOptions{})
			if n == 0 || n > 8 {
				require.Error(t, err)
				require.Zero(t, ref)
				require.GreaterOrEqual(t, r.Len(), n-9, "read at most max+1 bytes")
				require.NoDirExists(t, filepath.Join(root, "home-1"))
			} else {
				require.NoError(t, err)
				assertOriginal(t, s, ref, bytes.Repeat([]byte("x"), n))
			}
			assertNoStaging(t, root)
		})
	}
	content := []byte("original")
	for _, opts := range []PutOptions{{ExpectedSize: ptrInt64(7)}, {ExpectedSize: ptrInt64(9)}, {ExpectedSize: ptrInt64(0)}, {ExpectedSize: ptrInt64(-1)}, {ExpectedSHA256: strings.Repeat("0", 64)}, {ExpectedSHA256: strings.Repeat("A", 64)}, {ExpectedSHA256: "../bad"}} {
		s, root := newTestBlobs(t, 8)
		ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), opts)
		require.Error(t, err)
		require.Zero(t, ref)
		assertNoStaging(t, root)
		require.NoDirExists(t, filepath.Join(root, "home-1"))
	}
	s, _ := newTestBlobs(t, 8)
	ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{ExpectedSize: ptrInt64(8), ExpectedSHA256: testRef("home-1", content).SHA256})
	require.NoError(t, err)
	assertOriginal(t, s, ref, content)
}
func ptrInt64(v int64) *int64 { return &v }

func TestBlobPutShortReadsErrorsAndCancellation(t *testing.T) {
	s, root := newTestBlobs(t, 16)
	content := []byte("original")
	i := 0
	ref, err := s.Put(context.Background(), "home-1", readerFunc(func(p []byte) (int, error) {
		if i == len(content) {
			return 0, io.EOF
		}
		p[0] = content[i]
		i++
		return 1, nil
	}), PutOptions{})
	require.NoError(t, err)
	assertOriginal(t, s, ref, content)
	readErr := errors.New("reader interrupted")
	_, err = s.Put(context.Background(), "other", readerFunc(func(p []byte) (int, error) { copy(p, "partial"); return 7, readErr }), PutOptions{})
	require.ErrorIs(t, err, readErr)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = s.Put(ctx, "other", readerFunc(func(p []byte) (int, error) { cancel(); p[0] = 'a'; return 1, io.EOF }), PutOptions{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = s.Put(ctx, "other", readerFunc(func([]byte) (int, error) { t.Fatal("cancelled input must not be read"); return 0, nil }), PutOptions{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = s.Put(context.Background(), "other", readerFunc(func([]byte) (int, error) { return 0, nil }), PutOptions{})
	require.ErrorIs(t, err, io.ErrNoProgress)
	assertNoStaging(t, root)
	require.NoDirExists(t, filepath.Join(root, "other"))
}

func TestBlobConcurrentDuplicatesAndHouseholdIsolation(t *testing.T) {
	s, root := newTestBlobs(t, 0)
	content := bytes.Repeat([]byte("original "), 1000)
	type result struct {
		ref domain.ArchiveBlobRef
		err error
	}
	results := make(chan result, 12)
	for i := 0; i < 12; i++ {
		go func() {
			ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
			results <- result{ref, err}
		}()
	}
	ref := testRef("home-1", content)
	for i := 0; i < 12; i++ {
		out := <-results
		require.NoError(t, out.err)
		require.Equal(t, ref, out.ref)
	}
	other, err := s.Put(context.Background(), "home-2", bytes.NewReader(content), PutOptions{})
	require.NoError(t, err)
	a, err := os.Stat(blobPath(root, ref))
	require.NoError(t, err)
	b, err := os.Stat(blobPath(root, other))
	require.NoError(t, err)
	require.False(t, os.SameFile(a, b), "cross-household files must be separate objects")
	assertOriginal(t, s, ref, content)
	assertOriginal(t, s, other, content)
	assertNoStaging(t, root)
}

func TestBlobRejectsCorruptionAndMetadataDriftWithoutOverwrite(t *testing.T) {
	s, root := newTestBlobs(t, 0)
	content := []byte("original")
	ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
	require.NoError(t, err)
	for _, change := range []func(*domain.ArchiveBlobRef){func(r *domain.ArchiveBlobRef) { r.SizeBytes++ }, func(r *domain.ArchiveBlobRef) { r.MIME = "image/png" }, func(r *domain.ArchiveBlobRef) { r.StorageVersion++ }, func(r *domain.ArchiveBlobRef) { r.SHA256 = strings.ToUpper(r.SHA256) }, func(r *domain.ArchiveBlobRef) { r.HouseholdID = "../home-1" }} {
		bad := ref
		change(&bad)
		f, err := s.OpenVerified(context.Background(), bad)
		require.Error(t, err)
		require.Nil(t, f)
	}
	for _, corrupt := range [][]byte{[]byte("tampered"), []byte("longer corrupt original")} {
		require.NoError(t, os.WriteFile(blobPath(root, ref), corrupt, 0o600))
		f, err := s.OpenVerified(context.Background(), ref)
		require.Error(t, err)
		require.Nil(t, f)
		got, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
		require.Error(t, err)
		require.Zero(t, got)
		data, err := os.ReadFile(blobPath(root, ref))
		require.NoError(t, err)
		require.Equal(t, corrupt, data)
	}
	assertNoStaging(t, root)
}

func TestBlobRejectsUnsafeComponentsAndConstructor(t *testing.T) {
	s, root := newTestBlobs(t, 0)
	for _, home := range []string{"", ".", "..", "../escape", "/absolute", "a/b", "a\\b", "a\x00b", strings.Repeat("a", 129)} {
		_, err := s.Put(context.Background(), home, strings.NewReader("bytes"), PutOptions{})
		require.Error(t, err, home)
	}
	_, err := NewBlobStore(filepath.Join(root, "bad"), -1)
	require.Error(t, err)
	_, err = NewBlobStore("relative", 0)
	require.Error(t, err)
	_, err = NewBlobStore("/", 0)
	require.Error(t, err)
	assertNoStaging(t, root)
}

func TestBlobRejectsSymlinkAtEachLevelAndSpecialFiles(t *testing.T) {
	content := []byte("original")
	ref := testRef("home-1", content)
	for _, level := range []string{"root", "household", "blobs", "prefix", "leaf"} {
		t.Run(level, func(t *testing.T) {
			s, root := newTestBlobs(t, 0)
			path := root
			switch level {
			case "household":
				path = filepath.Join(root, "home-1")
			case "blobs":
				path = filepath.Join(root, "home-1", "blobs")
			case "prefix":
				path = filepath.Dir(blobPath(root, ref))
			case "leaf":
				path = blobPath(root, ref)
			}
			if level == "root" {
				require.NoError(t, os.Rename(root, root+"-real"))
				require.NoError(t, os.Symlink(root+"-real", root))
				bad, err := NewBlobStore(root, 0)
				require.Error(t, err)
				require.Nil(t, bad)
				// The already-open store remains pinned even when its pathname changes.
				got, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
				require.NoError(t, err)
				assertOriginal(t, s, got, content)
				return
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			target := filepath.Join(root, "target")
			if level == "leaf" {
				require.NoError(t, os.WriteFile(target, content, 0o600))
			} else {
				require.NoError(t, os.Mkdir(target, 0o700))
			}
			require.NoError(t, os.Symlink(target, path))
			_, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
			require.Error(t, err)
			f, err := s.OpenVerified(context.Background(), ref)
			require.Error(t, err)
			require.Nil(t, f)
			assertNoStaging(t, root)
		})
	}
	for _, kind := range []string{"fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			s, root := newTestBlobs(t, 0)
			path := blobPath(root, ref)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			if kind == "fifo" {
				require.NoError(t, unix.Mkfifo(path, 0o600))
			} else {
				require.NoError(t, os.Mkdir(path, 0o700))
			}
			_, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
			require.Error(t, err)
			f, err := s.OpenVerified(context.Background(), ref)
			require.Error(t, err)
			require.Nil(t, f)
			assertNoStaging(t, root)
		})
	}
}

func TestBlobAllowsTrustedAncestorAlias(t *testing.T) {
	parent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "trusted")
	require.NoError(t, os.Symlink(parent, alias))
	s, err := NewBlobStore(filepath.Join(alias, "archives"), 0)
	require.NoError(t, err)
	defer s.Close()
	ref, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.NoError(t, err)
	assertOriginal(t, s, ref, []byte("original"))
}

func TestBlobFailureBoundariesNeverReportSuccessAndRetry(t *testing.T) {
	for _, boundary := range []string{"write", "short-write", "file-sync", "parent-sync", "link", "link-cross-device", "directory-sync"} {
		t.Run(boundary, func(t *testing.T) {
			s, root := newTestBlobs(t, 0)
			originalIO := s.fs
			failed := errors.New("injected " + boundary)
			switch boundary {
			case "write":
				s.fs.write = func(*os.File, []byte) (int, error) { return 0, failed }
			case "short-write":
				s.fs.write = func(*os.File, []byte) (int, error) { return 0, nil }
			case "file-sync", "parent-sync", "directory-sync":
				s.fs.sync = func(f *os.File) error {
					info, err := f.Stat()
					if err != nil {
						return err
					}
					if boundary == "file-sync" && info.Mode().IsRegular() || boundary == "parent-sync" && f == s.root || boundary == "directory-sync" && f.Name() == testRef("home-1", []byte("original")).SHA256[:2] {
						return failed
					}
					return originalIO.sync(f)
				}
			case "link", "link-cross-device":
				s.fs.link = func(int, string, int, string, int) error {
					if boundary == "link-cross-device" {
						return unix.EXDEV
					}
					return failed
				}
			}
			out, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
			require.Error(t, err)
			require.Zero(t, out)
			assertNoStaging(t, root)
			ref := testRef("home-1", []byte("original"))
			if boundary == "directory-sync" {
				require.FileExists(t, blobPath(root, ref))
			} else {
				require.NoFileExists(t, blobPath(root, ref))
			}
			s.fs = originalIO
			out, err = s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
			require.NoError(t, err)
			require.Equal(t, ref, out)
			assertOriginal(t, s, out, []byte("original"))
		})
	}
}

func TestBlobRetryFailurePreservesSuccessfulOriginal(t *testing.T) {
	s, root := newTestBlobs(t, 0)
	ref, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.NoError(t, err)
	before, err := os.Stat(blobPath(root, ref))
	require.NoError(t, err)
	syncFile := s.fs.sync
	s.fs.sync = func(f *os.File) error {
		if f.Name() == ref.SHA256[:2] {
			return errors.New("directory sync failed")
		}
		return syncFile(f)
	}
	out, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.Error(t, err)
	require.Zero(t, out)
	after, err := os.Stat(blobPath(root, ref))
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	assertOriginal(t, s, ref, []byte("original"))
	assertNoStaging(t, root)
}

func TestBlobRootSyncFailureAndRetry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archives")
	fs := blobIO{write: (*os.File).Write, sync: func(*os.File) error { return errors.New("parent sync failed") }, link: unix.Linkat}
	s, err := newBlobStore(root, 0, fs)
	require.Error(t, err)
	require.Nil(t, s)
	s, err = NewBlobStore(root, 0)
	require.NoError(t, err)
	defer s.Close()
	ref, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.NoError(t, err)
	assertOriginal(t, s, ref, []byte("original"))
}

func TestBlobCloseWaitsForPutAndLeavesReadHandlesUsable(t *testing.T) {
	s, _ := newTestBlobs(t, 0)
	ref, err := s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.NoError(t, err)
	f, err := s.OpenVerified(context.Background(), ref)
	require.NoError(t, err)
	defer f.Close()
	reading := make(chan struct{})
	release := make(chan struct{})
	putDone := make(chan error, 1)
	go func() {
		_, err := s.Put(context.Background(), "home-2", readerFunc(func(p []byte) (int, error) { close(reading); <-release; return copy(p, "original"), io.EOF }), PutOptions{})
		putDone <- err
	}()
	<-reading
	closeStarted := make(chan struct{})
	closed := make(chan error, 1)
	go func() { close(closeStarted); closed <- s.Close() }()
	<-closeStarted
	select {
	case <-closed:
		t.Fatal("Close finished while a Put still owns the root")
	default:
	}
	close(release)
	require.NoError(t, <-putDone)
	require.NoError(t, <-closed)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	_, err = s.Put(context.Background(), "home-1", strings.NewReader("original"), PutOptions{})
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = s.OpenVerified(context.Background(), ref)
	require.ErrorIs(t, err, os.ErrClosed)
	require.NoError(t, s.Close())
}

func TestBlobDefaultLimitAndReaderReturningDataWithEOF(t *testing.T) {
	s, _ := newTestBlobs(t, 0)
	content := bytes.Repeat([]byte("x"), int(DefaultMaxBlobBytes))
	ref, err := s.Put(context.Background(), "home-1", bytes.NewReader(content), PutOptions{})
	require.NoError(t, err)
	assertOriginal(t, s, ref, content)
	_, err = s.Put(context.Background(), "home-1", io.MultiReader(bytes.NewReader(content), strings.NewReader("x")), PutOptions{})
	require.ErrorIs(t, err, ErrTooLarge)
	ref, err = s.Put(context.Background(), "home-1", readerFunc(func(p []byte) (int, error) { return copy(p, "last bytes"), io.EOF }), PutOptions{})
	require.NoError(t, err)
	assertOriginal(t, s, ref, []byte("last bytes"))
}
