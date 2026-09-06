//go:build !unix

package source

// isNotConnected has no portable equivalent outside unix.
func isNotConnected(error) bool { return false }
