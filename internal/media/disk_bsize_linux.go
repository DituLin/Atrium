//go:build linux

package media

import "syscall"

// statfsBlockSize returns the Bsize field, which is already int64 on linux.
func statfsBlockSize(st *syscall.Statfs_t) int64 { return st.Bsize }
