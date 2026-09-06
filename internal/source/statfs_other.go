//go:build !darwin

package source

import (
	"fmt"
	"os"
)

// statfs is the portable fallback: it confirms the root exists but reports no
// filesystem type, mount source or capacity. Identity checks that need those
// values treat an empty FSType as "unverifiable", so require_mount cannot be
// satisfied here without identity.allow_local.
func statfs(root string) (VolumeStats, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return VolumeStats{}, fmt.Errorf("source: statfs fallback: %w", err)
	}
	if !info.IsDir() {
		return VolumeStats{}, fmt.Errorf("source: %w", ErrNotSupported)
	}
	return VolumeStats{}, nil
}
