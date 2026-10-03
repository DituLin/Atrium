package archive

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func (p *parser) decodeText(data []byte) (string, error) {
	p.out.Coverage.Encoding = "utf-8"
	var decoded string
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		var order binary.ByteOrder = binary.LittleEndian
		p.out.Coverage.Encoding = "utf-16le"
		if data[0] == 0xfe {
			order = binary.BigEndian
			p.out.Coverage.Encoding = "utf-16be"
		}
		data = data[2:]
		if len(data)%2 != 0 {
			return "", invalid("invalid_utf16")
		}
		var b strings.Builder
		for i := 0; i < len(data); i += 2 {
			r := rune(order.Uint16(data[i:]))
			if r >= 0xd800 && r <= 0xdbff {
				if i+3 >= len(data) {
					return "", invalid("invalid_utf16")
				}
				lo := rune(order.Uint16(data[i+2:]))
				if lo < 0xdc00 || lo > 0xdfff {
					return "", invalid("invalid_utf16")
				}
				r = utf16.DecodeRune(r, lo)
				i += 2
			} else if r >= 0xdc00 && r <= 0xdfff {
				return "", invalid("invalid_utf16")
			}
			b.WriteRune(r)
			if int64(b.Len()) > p.limits.TextBytes {
				return "", limit("decoded_text_bytes")
			}
		}
		decoded = b.String()
	} else {
		data = bytes.TrimPrefix(data, []byte{239, 187, 191})
		if !utf8.Valid(data) {
			return "", invalid("invalid_utf8")
		}
		decoded = string(data)
	}
	if int64(len(decoded)) > p.limits.TextBytes {
		return "", limit("decoded_text_bytes")
	}
	if strings.ContainsRune(decoded, 0) {
		return "", reject(ParseUnsupported, "binary_text", "NUL-containing input is not supported plain text")
	}
	return decoded, nil
}
func (p *parser) parseCSV(decoded string) error {
	p.out.Coverage.OffsetUnit = "decoded_input_byte"
	p.out.Coverage.Notes = append(p.out.Coverage.Notes, "Comma-separated strict CSV; physical positions use encoding/csv.FieldPos in decoded input. CRLF becomes LF within fields; blank physical lines are ignored. Positions are not original byte offsets.")
	r := csv.NewReader(strings.NewReader(decoded))
	r.FieldsPerRecord = -1
	width := -1
	for record := 1; ; record++ {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		fields, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid("invalid_csv")
		}
		if width < 0 {
			width = len(fields)
		} else if width != len(fields) {
			p.partial("csv_ragged_record", "Logical record widths differ; no missing fields were synthesized", "")
		}
		for col, value := range fields {
			p.out.Counts.Cells++
			if p.out.Counts.Cells > p.limits.Cells {
				return limit("cells")
			}
			line, column := r.FieldPos(col)
			if err := p.add(Segment{Text: value, Locator: Locator{Kind: "csv", CSV: &CSVLocation{record, col + 1, line, column}}}); err != nil {
				return err
			}
		}
	}
}
