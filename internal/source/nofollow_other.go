//go:build !unix

package source

// openNoFollow is unavailable on this platform; the explicit Lstat check in
// OSFS.Open remains the only guard.
const openNoFollow = 0
