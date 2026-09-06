//go:build unix && !darwin && !linux

package media

import "syscall"

// statfsBlockSize widens the platform-specific Bsize field.
func statfsBlockSize(st *syscall.Statfs_t) int64 { return int64(st.Bsize) }
