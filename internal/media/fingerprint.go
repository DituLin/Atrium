package media

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

// FingerprintChunk is how much is read from each end of a file.
const FingerprintChunk = 64 << 10

// Fingerprint identifies a file version cheaply: size, mtime and the first and
// last 64 KiB (design §6.3 step 5). It is not a content hash — reading whole
// photos over SMB on every scan is exactly what the design avoids — but it
// changes whenever an editor rewrites a file, which is what preview
// invalidation and the media ETag need.
func Fingerprint(r io.ReadSeeker, size, mtimeUnix int64) (string, error) {
	h := sha256.New()
	var header [16]byte
	// The values are hashed, not interpreted, so the two's-complement
	// reinterpretation of a negative mtime is harmless and deterministic.
	binary.BigEndian.PutUint64(header[0:8], uint64(size))       //nolint:gosec // hashed, not interpreted
	binary.BigEndian.PutUint64(header[8:16], uint64(mtimeUnix)) //nolint:gosec // hashed, not interpreted
	h.Write(header[:])

	head := make([]byte, minInt64(size, FingerprintChunk))
	if len(head) > 0 {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("media: seek head: %w", err)
		}
		if _, err := io.ReadFull(r, head); err != nil {
			return "", fmt.Errorf("media: read head: %w", err)
		}
		h.Write(head)
	}

	if size > FingerprintChunk {
		tailLen := minInt64(size-FingerprintChunk, FingerprintChunk)
		if _, err := r.Seek(size-tailLen, io.SeekStart); err != nil {
			return "", fmt.Errorf("media: seek tail: %w", err)
		}
		tail := make([]byte, tailLen)
		if _, err := io.ReadFull(r, tail); err != nil {
			return "", fmt.Errorf("media: read tail: %w", err)
		}
		h.Write(tail)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
