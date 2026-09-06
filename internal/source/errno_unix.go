//go:build unix

package source

import (
	"errors"
	"syscall"
)

// isNotConnected recognises the errors a vanished network mount produces.
func isNotConnected(err error) bool {
	return errors.Is(err, syscall.ENOTCONN) ||
		errors.Is(err, syscall.EHOSTDOWN) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ECONNRESET)
}
