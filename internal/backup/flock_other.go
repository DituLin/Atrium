//go:build !unix

package backup

import "os"

// lockFile is a no-op off unix: there is no portable advisory lock, so
// `atrium restore` there relies on the operator having stopped the service,
// which the runbook requires anyway. Atrium targets macOS, where the unix
// implementation applies.
func lockFile(*os.File) error { return nil }

// unlockFile is a no-op off unix.
func unlockFile(*os.File) error { return nil }
