//go:build unix

package media

import (
	"fmt"
	"syscall"
)

func diskFree(path string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, fmt.Errorf("media: statfs %s: %w", path, err)
	}
	bs := int64(st.Bsize)                                    //nolint:unconvert // Bsize is uint32 on darwin and int64 on linux
	return int64(st.Bavail) * bs, int64(st.Blocks) * bs, nil //nolint:gosec // block counts fit in int64
}
