//go:build darwin

package source

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// statfs reads the volume that carries root and decides whether root is a
// mount point by comparing its device number with its parent's (design §6.1).
func statfs(root string) (VolumeStats, error) {
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(root, &fsStat); err != nil {
		return VolumeStats{}, fmt.Errorf("source: statfs: %w", err)
	}
	out := VolumeStats{
		TotalBytes: int64(fsStat.Blocks) * int64(fsStat.Bsize), //nolint:gosec // block counts fit in int64
		FreeBytes:  int64(fsStat.Bavail) * int64(fsStat.Bsize), //nolint:gosec // block counts fit in int64
		FSType:     cString(fsStat.Fstypename[:]),
		MountFrom:  cString(fsStat.Mntfromname[:]),
	}
	out.IsMountPoint = isMountPoint(root)
	return out, nil
}

// isMountPoint compares the device of root with the device of its parent.
func isMountPoint(root string) bool {
	self, err := os.Lstat(root)
	if err != nil {
		return false
	}
	parent := filepath.Dir(root)
	if parent == root {
		return true
	}
	up, err := os.Lstat(parent)
	if err != nil {
		return false
	}
	selfStat, ok := self.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	upStat, ok := up.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return selfStat.Dev != upStat.Dev
}

// cString converts a NUL-terminated C array to a Go string.
func cString(b []int8) string {
	buf := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c)) //nolint:gosec // C string bytes are ASCII
	}
	return string(buf)
}
