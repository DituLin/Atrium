// Package archivespool opens a trusted native attachment below an allowed root.
package archivespool

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

var ErrSource = errors.New("archive_source")

// Open walks every component relative to its already-opened parent FD. Renaming
// an ancestor cannot redirect the traversal, unlike lstat + pathname opening.
func Open(root, relative string) (*os.File, error) {
	if !filepath.IsAbs(root) || relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return nil, ErrSource
	}
	for _, p := range strings.Split(relative, string(filepath.Separator)) {
		if p == "." || p == ".." || p == "" {
			return nil, ErrSource
		}
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrSource
	}
	parts := append(strings.Split(strings.TrimPrefix(filepath.Clean(root), string(filepath.Separator)), string(filepath.Separator)), strings.Split(relative, string(filepath.Separator))...)
	for i, p := range parts {
		if p == "" {
			continue
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, p, flags, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, ErrSource
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "trusted-archive-source")
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 32<<20 {
		f.Close()
		return nil, ErrSource
	}
	return f, nil
}
