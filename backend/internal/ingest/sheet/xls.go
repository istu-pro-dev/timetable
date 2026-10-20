package sheet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
)

// BIFF8 record types ([MS-XLS] 2.3).
const (
	recFormula     = 0x0006
	recEOF         = 0x000A
	recContinue    = 0x003C
	recBoundSheet  = 0x0085
	recMulRK       = 0x00BD
	recRString     = 0x00D6
	recMergedCells = 0x00E5
	recSST         = 0x00FC
	recLabelSST    = 0x00FD
	recNumber      = 0x0203
	recLabel       = 0x0204
	recString      = 0x0207
	recRK          = 0x027E
	recBOF         = 0x0809
)

// ErrUnsupportedXLS is returned for pre-Excel-97 (BIFF5 and older) files.
var ErrUnsupportedXLS = errors.New("only Excel 97–2003 (BIFF8) .xls files are supported")

var errBIFF = errors.New("corrupt .xls workbook")

type record struct {
	typ  uint16
	data []byte
	pos  int // offset of the record header in the stream
}

type biffReader struct {
	buf []byte
	off int
}

func (r *biffReader) next() (record, bool) {
	if r.off+4 > len(r.buf) {
		return record{}, false
	}
	le := binary.LittleEndian
	typ, n := le.Uint16(r.buf[r.off:]), int(le.Uint16(r.buf[r.off+2:]))
	start := r.off + 4
	if start+n > len(r.buf) {
		return record{}, false
	}
	rec := record{typ: typ, data: r.buf[start : start+n], pos: r.off}
	r.off = start + n
	return rec, true
}

// peek returns the type of the next record without consuming it.
func (r *biffReader) peek() uint16 {
	if r.off+4 > len(r.buf) {
		return 0
	}
	return binary.LittleEndian.Uint16(r.buf[r.off:])
}

type boundSheet struct {
	name string
	pos  int
}

func readXLS(data []byte) (*Workbook, error) {
	f, err := openCFB(data)
	if err != nil {
		return nil, err
	}
	stream, err := f.stream("Workbook", "Book")
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian

	r := &biffReader{buf: stream}
	bof, ok := r.next()
	if !ok || bof.typ != recBOF || len(bof.data) < 2 {
		if ok && (bof.typ == 0x0409 || bof.typ == 0x0209) {
			return nil, ErrUnsupportedXLS
		}
		return nil, fmt.Errorf("%w: no BOF", errBIFF)
	}
	if le.Uint16(bof.data) != 0x0600 {
		return nil, ErrUnsupportedXLS
	}

	var (
		sheets []boundSheet
		sst    []string
	)
	for {
		rec, ok := r.next()
		if !ok || rec.typ == recEOF {
			break
		}
		switch rec.typ {
		case recBoundSheet:
			if len(rec.data) < 8 || rec.data[5] != 0 { // dt != 0: chart, macro sheet
				continue
			}
			s := &cursor{chunks: [][]byte{rec.data[6:]}}
			cch := int(s.u8())
			name := s.chars(cch, s.u8()&1 != 0)
			if s.err == nil {
				sheets = append(sheets, boundSheet{name: name, pos: int(le.Uint32(rec.data))})
			}
		case recSST:
			chunks := [][]byte{rec.data}
			for r.peek() == recContinue {
				c, _ := r.next()
				chunks = append(chunks, c.data)
			}
			if sst, err = parseSST(chunks); err != nil {
				return nil, err
			}
		}
	}

	wb := &Workbook{}
	for _, bs := range sheets {
		s, err := readXLSSheet(stream, bs, sst)
		if err != nil {
			return nil, err
		}
		wb.Sheets = append(wb.Sheets, s)
	}
	return wb, nil
}

func readXLSSheet(stream []byte, bs boundSheet, sst []string) (*Sheet, error) {
	if bs.pos < 0 || bs.pos >= len(stream) {
		return nil, fmt.Errorf("%w: sheet %q offset", errBIFF, bs.name)
	}
	le := binary.LittleEndian
	s := newSheet(bs.name)
	r := &biffReader{buf: stream, off: bs.pos}
	if bof, ok := r.next(); !ok || bof.typ != recBOF {
		return nil, fmt.Errorf("%w: sheet %q has no BOF", errBIFF, bs.name)
	}
	cell := func(d []byte) Pos { return Pos{int(le.Uint16(d)), int(le.Uint16(d[2:]))} }
	depth := 0 // embedded substreams (charts) have their own BOF/EOF
	for {
		rec, ok := r.next()
		if !ok {
			break
		}
		d := rec.data
		switch rec.typ {
		case recBOF:
			depth++
		case recEOF:
			if depth == 0 {
				return s, nil
			}
			depth--
		case recLabelSST:
			if depth == 0 && len(d) >= 10 {
				if i := int(le.Uint32(d[6:])); i < len(sst) {
					s.set(cell(d), sst[i])
				}
			}
		case recLabel, recRString:
			if depth == 0 && len(d) >= 9 {
				c := &cursor{chunks: [][]byte{d[6:]}}
				cch := int(c.u16())
				if v := c.chars(cch, c.u8()&1 != 0); c.err == nil {
					s.set(cell(d), v)
				}
			}
		case recNumber:
			if depth == 0 && len(d) >= 14 {
				s.set(cell(d), formatNumber(math.Float64frombits(le.Uint64(d[6:]))))
			}
		case recRK:
			if depth == 0 && len(d) >= 10 {
				s.set(cell(d), formatNumber(decodeRK(le.Uint32(d[6:]))))
			}
		case recMulRK:
			if depth == 0 && len(d) >= 6 {
				p := cell(d)
				for i, off := 0, 4; off+6 <= len(d)-2; i, off = i+1, off+6 {
					s.set(Pos{p.Row, p.Col + i}, formatNumber(decodeRK(le.Uint32(d[off+2:]))))
				}
			}
		case recFormula:
			if depth == 0 && len(d) >= 14 {
				p := cell(d)
				if le.Uint16(d[12:]) != 0xFFFF { // cached numeric result
					s.set(p, formatNumber(math.Float64frombits(le.Uint64(d[6:]))))
				} else if d[6] == 0 && r.peek() == recString { // cached string result follows
					sr, _ := r.next()
					if len(sr.data) >= 3 {
						c := &cursor{chunks: [][]byte{sr.data}}
						cch := int(c.u16())
						if v := c.chars(cch, c.u8()&1 != 0); c.err == nil {
							s.set(p, v)
						}
					}
				}
			}
		case recMergedCells:
			if depth == 0 && len(d) >= 2 {
				n := int(le.Uint16(d))
				for i := 0; i < n && 2+8*i+8 <= len(d); i++ {
					m := d[2+8*i:]
					rg := Range{
						From: Pos{int(le.Uint16(m)), int(le.Uint16(m[4:]))},
						To:   Pos{int(le.Uint16(m[2:])), int(le.Uint16(m[6:]))},
					}
					if rg.From != rg.To { // Excel sometimes stores single-cell "merges"
						s.Merges = append(s.Merges, rg)
					}
				}
			}
		}
	}
	return s, nil
}

func decodeRK(rk uint32) float64 {
	var v float64
	if rk&2 != 0 {
		v = float64(int32(rk) >> 2)
	} else {
		v = math.Float64frombits(uint64(rk&0xFFFFFFFC) << 32)
	}
	if rk&1 != 0 {
		v /= 100
	}
	return v
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// parseSST decodes the shared string table. Strings may be split across CONTINUE records;
// when character data is split, the continuation starts with a fresh option byte.
func parseSST(chunks [][]byte) ([]string, error) {
	c := &cursor{chunks: chunks}
	c.u32() // total references
	unique := int(c.u32())
	if c.err != nil {
		return nil, fmt.Errorf("%w: SST header", errBIFF)
	}
	out := make([]string, 0, min(unique, 1<<16))
	for range unique {
		cch := int(c.u16())
		flags := c.u8()
		var runs, ext int
		if flags&0x08 != 0 {
			runs = int(c.u16())
		}
		if flags&0x04 != 0 {
			ext = int(c.u32())
		}
		v := c.chars(cch, flags&1 != 0)
		c.skip(4*runs + ext)
		if c.err != nil {
			return nil, fmt.Errorf("%w: SST string %d", errBIFF, len(out))
		}
		out = append(out, v)
	}
	return out, nil
}

// cursor reads little-endian values across a list of record payloads.
type cursor struct {
	chunks [][]byte
	ci     int
	off    int
	err    error
}

var errShort = errors.New("unexpected end of record")

func (c *cursor) avail() int {
	for c.ci < len(c.chunks) && c.off >= len(c.chunks[c.ci]) {
		c.ci++
		c.off = 0
	}
	if c.ci >= len(c.chunks) {
		return 0
	}
	return len(c.chunks[c.ci]) - c.off
}

func (c *cursor) u8() byte {
	if c.err != nil || c.avail() == 0 {
		c.err = errShort
		return 0
	}
	b := c.chunks[c.ci][c.off]
	c.off++
	return b
}

func (c *cursor) u16() uint16 { return uint16(c.u8()) | uint16(c.u8())<<8 }

func (c *cursor) u32() uint32 { return uint32(c.u16()) | uint32(c.u16())<<16 }

func (c *cursor) skip(n int) {
	for n > 0 && c.err == nil {
		a := c.avail()
		if a == 0 {
			c.err = errShort
			return
		}
		k := min(a, n)
		c.off += k
		n -= k
	}
}

// chars reads cch characters, compressed (Latin-1) or UTF-16LE.
func (c *cursor) chars(cch int, high bool) string {
	u := make([]uint16, 0, min(cch, 1<<12))
	for len(u) < cch && c.err == nil {
		if c.ci < len(c.chunks) && c.off >= len(c.chunks[c.ci]) {
			// Character data continues in the next record, which restates the encoding.
			c.ci++
			c.off = 0
			if c.ci >= len(c.chunks) {
				c.err = errShort
				break
			}
			high = c.u8()&1 != 0
			continue
		}
		if c.ci >= len(c.chunks) {
			c.err = errShort
			break
		}
		chunk := c.chunks[c.ci]
		if high {
			if c.off+2 > len(chunk) {
				c.err = errShort
				break
			}
			u = append(u, binary.LittleEndian.Uint16(chunk[c.off:]))
			c.off += 2
		} else {
			u = append(u, uint16(chunk[c.off]))
			c.off++
		}
	}
	return string(utf16.Decode(u))
}
