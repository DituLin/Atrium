package video

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/DituLin/Atrium/internal/media"
	"github.com/oklog/ulid/v2"
)

// Cache errors distinguish resource pressure from corrupt input.
var (
	ErrCacheFull = errors.New("video: cover cache full")
	ErrLowDisk   = errors.New("video: low disk space")
)

// CoverCache is a dedicated, bounded partition of the application's total
// cache budget. Runtime wiring must reserve this budget from the photo cache.
type CoverCache struct {
	mu              sync.Mutex
	files           *media.Cache
	budget, minFree int64
	disk            media.DiskStats
	sharedBudget    int64
	otherBytes      func() (int64, error)
}

// NewCoverCache creates a local cache; no source directory is accepted here.
func NewCoverCache(root string, budget, minFree int64, disk media.DiskStats) *CoverCache {
	if disk == nil {
		disk = media.OSDiskStats{}
	}
	return &CoverCache{files: media.NewCache(root), budget: budget, minFree: minFree, disk: disk}
}

func coverName(token string) (string, error) {
	if _, err := ulid.ParseStrict(token); err != nil {
		return "", errors.New("video: invalid cover token")
	}
	return token + ".jpg", nil
}

// Read retrieves a generated cover using its internal claim token.
func (c *CoverCache) Read(token string) ([]byte, error) {
	name, err := coverName(token)
	if err != nil {
		return nil, err
	}
	return c.files.Read(name)
}

// Remove discards a rejected or obsolete result without following source paths.
func (c *CoverCache) Remove(token string) error {
	name, err := coverName(token)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files.Remove(name)
}

// Write atomically stores a result only if both its partition and disk allow it.
func (c *CoverCache) Write(token string, data []byte) error {
	name, err := coverName(token)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 2*1024*1024 {
		return ErrOutputLimit
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.files.Root(), 0700); err != nil {
		return err
	}
	used, err := c.bytes()
	if err != nil {
		return err
	}
	if c.budget <= 0 || int64(len(data)) > c.budget-used {
		return ErrCacheFull
	}
	if c.otherBytes != nil {
		other, err := c.otherBytes()
		if err != nil {
			return err
		}
		if int64(len(data)) > c.sharedBudget-other-used {
			return ErrCacheFull
		}
	}
	free, _, err := c.disk.Free(c.files.Root())
	if err != nil {
		return err
	}
	if free-int64(len(data)) < c.minFree {
		return ErrLowDisk
	}
	return c.files.Write(name, data)
}

func (c *CoverCache) bytes() (int64, error) {
	entries, err := os.ReadDir(c.files.Root())
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var used int64
	for _, e := range entries {
		if e.Type().IsRegular() {
			info, err := e.Info()
			if err != nil {
				return 0, err
			}
			used += info.Size()
		}
	}
	return used, nil
}

// Bytes includes pending and orphan files, not just published database records.
func (c *CoverCache) Bytes() (int64, error) { c.mu.Lock(); defer c.mu.Unlock(); return c.bytes() }

// Sweep removes only application-owned cover names absent from the retained
// set. Active claims must be retained so a concurrent publication is not lost.
func (c *CoverCache) Sweep(loadRetained func() (map[string]bool, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	retained, err := loadRetained()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(c.files.Root())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		// Writes hold this same mutex, so these are leftovers from an earlier
		// interrupted atomic write, never an active writer's temporary file.
		if e.Type().IsRegular() && strings.HasPrefix(e.Name(), ".tmp-") {
			if err := c.files.Remove(e.Name()); err != nil {
				return err
			}
			continue
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jpg") {
			continue
		}
		token := strings.TrimSuffix(e.Name(), ".jpg")
		if _, err := coverName(token); err != nil || retained[token] {
			continue
		}
		if err := c.files.Remove(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// IfMissing holds off cache writes while invalidating a missing reference.
// This prevents a newly written publication being cleared by an older check.
func (c *CoverCache) IfMissing(token string, forget func() error) error {
	name, err := coverName(token)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	info, err := os.Lstat(filepath.Join(c.files.Root(), name))
	if os.IsNotExist(err) {
		return forget()
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 2*1024*1024 {
		return forget()
	}
	return nil
}

// SetSharedBudget guards against preexisting usage above another partition's
// new budget while its janitor converges. Configure before starting the worker.
func (c *CoverCache) SetSharedBudget(total int64, otherBytes func() (int64, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sharedBudget, c.otherBytes = total, otherBytes
}

// TrimToBudget reconciles a reduced budget at process startup, before any
// claims run. Only generated regular cover files are evicted, oldest first.
// Missing references are repaired by the worker's normal reconciliation.
func (c *CoverCache) TrimToBudget() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	used, err := c.bytes()
	if err != nil || used <= c.budget {
		return err
	}
	entries, err := os.ReadDir(c.files.Root())
	if err != nil {
		return err
	}
	var candidates []os.FileInfo
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if _, err := coverName(strings.TrimSuffix(entry.Name(), ".jpg")); err != nil || !strings.HasSuffix(entry.Name(), ".jpg") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		candidates = append(candidates, info)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ModTime().Before(candidates[j].ModTime()) })
	for _, info := range candidates {
		if used <= c.budget {
			return nil
		}
		if err := c.files.Remove(info.Name()); err != nil {
			return err
		}
		used -= info.Size()
	}
	if used > c.budget {
		return ErrCacheFull
	}
	return nil
}
