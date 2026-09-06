package media

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/DituLin/Atritum/internal/domain"
)

// CacheRelPath returns the storage path of a derived image relative to the
// cache root: <variant>/<id[0:2]>/<id[2:4]>/<id>.jpg. The two fan-out levels
// keep any single directory small enough for a 100k-photo library.
func CacheRelPath(variant domain.Variant, id string) (string, error) {
	if !variant.Valid() {
		return "", fmt.Errorf("media: unknown variant %q", variant)
	}
	if len(id) < 4 {
		return "", fmt.Errorf("media: photo id %q is too short", id)
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		default:
			return "", fmt.Errorf("media: photo id contains an unusable character")
		}
	}
	return path.Join(string(variant), id[0:2], id[2:4], id+".jpg"), nil
}

// Cache stores derived images under the data directory. Writes go through a
// temporary file and a rename so a crashed worker never leaves a half-written
// JPEG that a screen would try to display.
type Cache struct{ root string }

// NewCache builds a cache rooted at dir.
func NewCache(dir string) *Cache { return &Cache{root: dir} }

// Root returns the cache directory.
func (c *Cache) Root() string { return c.root }

// Abs resolves a cache-relative path.
func (c *Cache) Abs(rel string) string { return filepath.Join(c.root, filepath.FromSlash(rel)) }

// Write stores data atomically and returns the number of bytes written.
func (c *Cache) Write(rel string, data []byte) error {
	full := c.Abs(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return fmt.Errorf("media: create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return fmt.Errorf("media: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("media: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("media: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("media: close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("media: set cache file permissions: %w", err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("media: publish cache file: %w", err)
	}
	return nil
}

// Read returns the bytes of a cached file.
func (c *Cache) Read(rel string) ([]byte, error) {
	data, err := os.ReadFile(c.Abs(rel))
	if err != nil {
		return nil, fmt.Errorf("media: read cache file: %w", err)
	}
	return data, nil
}

// Exists reports whether a cached file is present.
func (c *Cache) Exists(rel string) bool {
	_, err := os.Stat(c.Abs(rel))
	return err == nil
}

// Remove deletes a cached file; a missing file is not an error.
func (c *Cache) Remove(rel string) error {
	if err := os.Remove(c.Abs(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("media: remove cache file: %w", err)
	}
	return nil
}
