//go:build darwin

package media

import "syscall"

// statfsBlockSize widens the platform-specific Bsize field (uint32 on darwin).
func statfsBlockSize(st *syscall.Statfs_t) int64 { return int64(st.Bsize) }
