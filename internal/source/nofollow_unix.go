//go:build unix

package source

import "syscall"

// openNoFollow refuses to open a symbolic link at the final path component.
const openNoFollow = syscall.O_NOFOLLOW
