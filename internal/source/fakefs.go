package source

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// FakeFS is an in-memory FS with fault injection, used by tests to reproduce
// the NAS failure modes that are impossible to trigger reliably against a real
// mount: a hang until the caller's deadline, EACCES on part of the tree, the
// whole share disappearing, and slow-but-working I/O.
type FakeFS struct {
	mu sync.Mutex

	files map[string]*FakeFile
	stats VolumeStats

	// Hang blocks every call until the caller's context is done.
	Hang bool
	// Missing makes the root and everything under it report os.ErrNotExist.
	Missing bool
	// Denied lists relative paths (and their subtrees) that return EACCES.
	Denied map[string]bool
	// Delay is added to every call before it returns.
	Delay time.Duration
	// StatfsErr, when set, is returned by Statfs.
	StatfsErr error

	calls int
}

// FakeFile is one in-memory entry.
type FakeFile struct {
	Data    []byte
	Dir     bool
	Symlink bool
	Mode    fs.FileMode
	ModTime time.Time
}

// NewFakeFS creates an empty fake with a plausible network volume.
func NewFakeFS() *FakeFS {
	return &FakeFS{
		files:  map[string]*FakeFile{"": {Dir: true, ModTime: time.Unix(0, 0)}},
		Denied: map[string]bool{},
		stats: VolumeStats{
			TotalBytes: 1 << 40, FreeBytes: 1 << 39,
			FSType: "smbfs", MountFrom: "//guest@nas/photos", IsMountPoint: true,
		},
	}
}

// SetVolume overrides the reported volume statistics.
func (f *FakeFS) SetVolume(v VolumeStats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = v
}

// AddFile inserts a regular file, creating parent directories.
func (f *FakeFS) AddFile(rel string, data []byte, mod time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mkdirAll(path.Dir(rel))
	f.files[rel] = &FakeFile{Data: data, ModTime: mod}
}

// AddSymlink inserts an entry that must never be followed.
func (f *FakeFS) AddSymlink(rel string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mkdirAll(path.Dir(rel))
	f.files[rel] = &FakeFile{Symlink: true, Mode: fs.ModeSymlink, ModTime: time.Unix(0, 0)}
}

// Remove deletes an entry.
func (f *FakeFS) Remove(rel string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, rel)
}

// Calls returns how many operations were issued.
func (f *FakeFS) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *FakeFS) mkdirAll(dir string) {
	if dir == "." || dir == "" || dir == "/" {
		return
	}
	if _, ok := f.files[dir]; ok {
		return
	}
	f.mkdirAll(path.Dir(dir))
	f.files[dir] = &FakeFile{Dir: true, ModTime: time.Unix(0, 0)}
}

// gate applies the injected faults common to every operation.
func (f *FakeFS) gate(ctx context.Context, rel string) error {
	f.mu.Lock()
	f.calls++
	hang, missing, delay := f.Hang, f.Missing, f.Delay
	denied := f.deniedLocked(rel)
	f.mu.Unlock()

	if hang {
		<-ctx.Done()
		return ErrStuck
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ErrStuck
		}
	}
	if missing {
		return &fs.PathError{Op: "stat", Path: rel, Err: os.ErrNotExist}
	}
	if denied {
		return &fs.PathError{Op: "open", Path: rel, Err: os.ErrPermission}
	}
	return ctx.Err()
}

func (f *FakeFS) deniedLocked(rel string) bool {
	for p := range f.Denied {
		if rel == p || (p != "" && strings.HasPrefix(rel, p+"/")) {
			return true
		}
	}
	return false
}

// Stat implements FS.
func (f *FakeFS) Stat(ctx context.Context, rel string) (fs.FileInfo, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	if err := f.gate(ctx, clean); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.files[clean]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: clean, Err: os.ErrNotExist}
	}
	if entry.Symlink {
		return nil, ErrSymlink
	}
	return fakeInfo{name: path.Base(clean), entry: entry}, nil
}

// ReadDir implements FS.
func (f *FakeFS) ReadDir(ctx context.Context, rel string) ([]fs.DirEntry, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	if err := f.gate(ctx, clean); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry, ok := f.files[clean]; !ok || !entry.Dir {
		return nil, &fs.PathError{Op: "readdir", Path: clean, Err: os.ErrNotExist}
	}
	var out []fs.DirEntry
	for name, entry := range f.files {
		if name == clean || path.Dir(name) != dirKey(clean) {
			continue
		}
		out = append(out, fakeInfo{name: path.Base(name), entry: entry})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Open implements FS.
func (f *FakeFS) Open(ctx context.Context, rel string) (io.ReadSeekCloser, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	if err := f.gate(ctx, clean); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.files[clean]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: clean, Err: os.ErrNotExist}
	}
	if entry.Symlink || entry.Dir {
		return nil, ErrSymlink
	}
	return nopSeekCloser{bytes.NewReader(entry.Data)}, nil
}

// Statfs implements FS.
func (f *FakeFS) Statfs(ctx context.Context) (VolumeStats, error) {
	if err := f.gate(ctx, ""); err != nil {
		return VolumeStats{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatfsErr != nil {
		return VolumeStats{}, f.StatfsErr
	}
	return f.stats, nil
}

// dirKey normalises the root so path.Dir comparisons work for top-level files.
func dirKey(clean string) string {
	if clean == "" {
		return "."
	}
	return clean
}

// fakeInfo satisfies both fs.FileInfo and fs.DirEntry.
type fakeInfo struct {
	name  string
	entry *FakeFile
}

func (i fakeInfo) Name() string { return i.name }
func (i fakeInfo) Size() int64  { return int64(len(i.entry.Data)) }
func (i fakeInfo) Mode() fs.FileMode {
	switch {
	case i.entry.Symlink:
		return fs.ModeSymlink | 0o777
	case i.entry.Dir:
		return fs.ModeDir | 0o755
	default:
		return 0o644
	}
}
func (i fakeInfo) ModTime() time.Time         { return i.entry.ModTime }
func (i fakeInfo) IsDir() bool                { return i.entry.Dir }
func (i fakeInfo) Sys() any                   { return nil }
func (i fakeInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i fakeInfo) Info() (fs.FileInfo, error) { return i, nil }

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }
