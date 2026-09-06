package media

// DiskStats reports free space for the volume holding a path. It is an
// interface so the low-disk pause can be tested without filling a real disk.
type DiskStats interface {
	// Free returns the available and total bytes for the volume of path.
	Free(path string) (free int64, total int64, err error)
}

// OSDiskStats reads the real filesystem.
type OSDiskStats struct{}

// Free implements DiskStats.
func (OSDiskStats) Free(path string) (int64, int64, error) { return diskFree(path) }

// FakeDiskStats returns fixed values, used by tests.
type FakeDiskStats struct {
	FreeBytes  int64
	TotalBytes int64
	Err        error
}

// Free implements DiskStats.
func (f FakeDiskStats) Free(string) (int64, int64, error) {
	return f.FreeBytes, f.TotalBytes, f.Err
}
