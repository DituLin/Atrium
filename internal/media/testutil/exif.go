// Package testutil generates the sample images the media tests need. Nothing
// is committed to the repository: every JPEG, PNG and malformed file is built
// in code, so the test suite carries no real photographs and needs no external
// tool (dev plan §0.1, B-206).
package testutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// EXIF tag identifiers used by the writer.
const (
	tagOrientation        = 0x0112
	tagExifIFDPointer     = 0x8769
	tagDateTimeOriginal   = 0x9003
	tagOffsetTimeOriginal = 0x9011

	typeASCII = 2
	typeShort = 3
	typeLong  = 4
)

// ExifOptions describes the EXIF block to embed.
type ExifOptions struct {
	// DateTimeOriginal is written as "YYYY:MM:DD HH:MM:SS" when non-zero.
	DateTimeOriginal time.Time
	// OffsetTimeOriginal is the EXIF 2.31 offset tag, e.g. "+08:00". Empty
	// omits the tag, which is what makes captured_at "inferred".
	OffsetTimeOriginal string
	// Orientation is the 1..8 EXIF value; zero omits the tag.
	Orientation uint16
}

// Empty reports whether the options would produce no tags at all.
func (o ExifOptions) Empty() bool {
	return o.DateTimeOriginal.IsZero() && o.OffsetTimeOriginal == "" && o.Orientation == 0
}

// entry is one IFD record before serialisation.
type entry struct {
	tag, typ uint16
	count    uint32
	inline   uint32
	data     []byte
}

func asciiEntry(tag uint16, s string) entry {
	b := append([]byte(s), 0)
	return entry{tag: tag, typ: typeASCII, count: uint32(len(b)), data: b}
}

// buildTIFF lays out a little-endian TIFF block: header, IFD0, the Exif
// sub-IFD and a trailing data area for values wider than four bytes.
func buildTIFF(opts ExifOptions) []byte {
	var ifd0, exif []entry
	if opts.Orientation != 0 {
		ifd0 = append(ifd0, entry{tag: tagOrientation, typ: typeShort, count: 1, inline: uint32(opts.Orientation)})
	}
	if !opts.DateTimeOriginal.IsZero() {
		exif = append(exif, asciiEntry(tagDateTimeOriginal, opts.DateTimeOriginal.Format("2006:01:02 15:04:05")))
	}
	if opts.OffsetTimeOriginal != "" {
		exif = append(exif, asciiEntry(tagOffsetTimeOriginal, opts.OffsetTimeOriginal))
	}
	// The Exif pointer must come last in IFD0 so its offset is known.
	ifd0 = append(ifd0, entry{tag: tagExifIFDPointer, typ: typeLong, count: 1})

	const headerSize = 8
	ifd0Size := uint32(2 + 12*len(ifd0) + 4)
	exifSize := uint32(2 + 12*len(exif) + 4)
	exifOffset := headerSize + ifd0Size
	ifd0[len(ifd0)-1].inline = exifOffset
	dataOffset := exifOffset + exifSize

	var body, data bytes.Buffer
	body.Write([]byte{'I', 'I'})
	_ = binary.Write(&body, binary.LittleEndian, uint16(42))
	_ = binary.Write(&body, binary.LittleEndian, uint32(headerSize))
	writeIFD(&body, &data, ifd0, &dataOffset)
	writeIFD(&body, &data, exif, &dataOffset)
	body.Write(data.Bytes())
	return body.Bytes()
}

// writeIFD appends one directory, spilling oversized values into data.
func writeIFD(body, data *bytes.Buffer, entries []entry, dataOffset *uint32) {
	_ = binary.Write(body, binary.LittleEndian, uint16(len(entries)))
	for _, e := range entries {
		_ = binary.Write(body, binary.LittleEndian, e.tag)
		_ = binary.Write(body, binary.LittleEndian, e.typ)
		_ = binary.Write(body, binary.LittleEndian, e.count)
		if len(e.data) > 4 {
			_ = binary.Write(body, binary.LittleEndian, *dataOffset)
			data.Write(e.data)
			*dataOffset += uint32(len(e.data))
			continue
		}
		var raw [4]byte
		if len(e.data) > 0 {
			copy(raw[:], e.data)
		} else {
			switch e.typ {
			case typeShort:
				binary.LittleEndian.PutUint16(raw[:2], uint16(e.inline))
			default:
				binary.LittleEndian.PutUint32(raw[:], e.inline)
			}
		}
		body.Write(raw[:])
	}
	_ = binary.Write(body, binary.LittleEndian, uint32(0)) // no next IFD
}

// app1Segment wraps a TIFF block in a JPEG APP1 marker segment.
func app1Segment(tiff []byte) ([]byte, error) {
	payload := append([]byte("Exif\x00\x00"), tiff...)
	size := len(payload) + 2
	if size > 0xFFFF {
		return nil, fmt.Errorf("testutil: EXIF segment of %d bytes is too large", size)
	}
	seg := []byte{0xFF, 0xE1, byte(size >> 8), byte(size)}
	return append(seg, payload...), nil
}
