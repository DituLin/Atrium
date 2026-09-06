//go:build !unix

package media

// diskFree has no portable implementation; reporting zero free space would
// pause the pipeline permanently, so an unknown volume is reported as large.
func diskFree(string) (int64, int64, error) { return 1 << 62, 1 << 62, nil }
