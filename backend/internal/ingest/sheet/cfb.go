package sheet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Compound File Binary format ([MS-CFB]): the container of legacy Office files. Only reading
// a named stream is supported.

const (
	cfbEndOfChain = 0xFFFFFFFE
	cfbFree       = 0xFFFFFFFF
	cfbHeaderSize = 512
	cfbDirEntry   = 128
)

var errCFB = errors.New("corrupt compound file")

type cfb struct {
	data       []byte
	secSize    int
	miniSize   int
	miniCutoff uint32
	fat        []uint32
	miniFAT    []uint32
	dir        []cfbEntry
	ministream []byte
}

type cfbEntry struct {
	name  string
	typ   byte // 1 storage, 2 stream, 5 root
	start uint32
	size  uint64
}

func openCFB(data []byte) (*cfb, error) {
	if len(data) < cfbHeaderSize {
		return nil, errCFB
	}
	le := binary.LittleEndian
	secShift := le.Uint16(data[0x1E:])
	miniShift := le.Uint16(data[0x20:])
	if secShift != 9 && secShift != 12 || miniShift != 6 {
		return nil, fmt.Errorf("%w: sector shift %d/%d", errCFB, secShift, miniShift)
	}
	f := &cfb{data: data, secSize: 1 << secShift, miniSize: 1 << miniShift, miniCutoff: le.Uint32(data[0x38:])}

	// DIFAT: 109 entries in the header, then a chain of DIFAT sectors.
	var difat []uint32
	for i := range 109 {
		difat = append(difat, le.Uint32(data[0x4C+4*i:]))
	}
	next, count := le.Uint32(data[0x44:]), int(le.Uint32(data[0x48:]))
	for i := 0; i < count && next != cfbEndOfChain && next != cfbFree; i++ {
		sec, err := f.sector(next)
		if err != nil {
			return nil, err
		}
		per := f.secSize/4 - 1
		for j := range per {
			difat = append(difat, le.Uint32(sec[4*j:]))
		}
		next = le.Uint32(sec[4*per:])
	}
	nFAT := int(le.Uint32(data[0x2C:]))
	if nFAT > len(difat) {
		return nil, fmt.Errorf("%w: FAT sector count", errCFB)
	}
	for _, s := range difat[:nFAT] {
		sec, err := f.sector(s)
		if err != nil {
			return nil, err
		}
		for j := 0; j < f.secSize; j += 4 {
			f.fat = append(f.fat, le.Uint32(sec[j:]))
		}
	}

	dirData, err := f.chain(le.Uint32(data[0x30:]), -1)
	if err != nil {
		return nil, err
	}
	for off := 0; off+cfbDirEntry <= len(dirData); off += cfbDirEntry {
		e := dirData[off : off+cfbDirEntry]
		nameLen := min(int(le.Uint16(e[0x40:])), 64)
		u := make([]uint16, 0, 32)
		for i := 0; i+1 < nameLen; i += 2 {
			if c := le.Uint16(e[i:]); c != 0 {
				u = append(u, c)
			}
		}
		f.dir = append(f.dir, cfbEntry{
			name: string(utf16.Decode(u)), typ: e[0x42], start: le.Uint32(e[0x74:]), size: le.Uint64(e[0x78:]) & 0xFFFFFFFF,
		})
	}
	if len(f.dir) == 0 || f.dir[0].typ != 5 {
		return nil, fmt.Errorf("%w: no root entry", errCFB)
	}

	if mf := le.Uint32(data[0x3C:]); mf != cfbEndOfChain && mf != cfbFree {
		raw, err := f.chain(mf, -1)
		if err != nil {
			return nil, err
		}
		for j := 0; j+4 <= len(raw); j += 4 {
			f.miniFAT = append(f.miniFAT, le.Uint32(raw[j:]))
		}
		if f.ministream, err = f.chain(f.dir[0].start, int(f.dir[0].size)); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *cfb) sector(n uint32) ([]byte, error) {
	off := (int64(n) + 1) * int64(f.secSize)
	if n >= cfbEndOfChain-3 || off+int64(f.secSize) > int64(len(f.data)) {
		return nil, fmt.Errorf("%w: sector %d out of range", errCFB, n)
	}
	return f.data[off : off+int64(f.secSize)], nil
}

// chain concatenates a FAT chain; size < 0 reads the whole chain.
func (f *cfb) chain(start uint32, size int) ([]byte, error) {
	var out []byte
	for s, steps := start, 0; s != cfbEndOfChain && s != cfbFree; steps++ {
		if steps > len(f.fat) || int(s) >= len(f.fat) {
			return nil, fmt.Errorf("%w: broken FAT chain", errCFB)
		}
		sec, err := f.sector(s)
		if err != nil {
			return nil, err
		}
		out = append(out, sec...)
		if size >= 0 && len(out) >= size {
			break
		}
		s = f.fat[s]
	}
	if size >= 0 {
		if len(out) < size {
			return nil, fmt.Errorf("%w: stream shorter than declared", errCFB)
		}
		out = out[:size]
	}
	return out, nil
}

func (f *cfb) miniChain(start uint32, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	for s, steps := start, 0; len(out) < size; steps++ {
		if s == cfbEndOfChain || steps > len(f.miniFAT) || int(s) >= len(f.miniFAT) {
			return nil, fmt.Errorf("%w: broken mini FAT chain", errCFB)
		}
		off := int(s) * f.miniSize
		if off+f.miniSize > len(f.ministream) {
			return nil, fmt.Errorf("%w: mini sector out of range", errCFB)
		}
		out = append(out, f.ministream[off:off+f.miniSize]...)
		s = f.miniFAT[s]
	}
	return out[:size], nil
}

// stream returns the contents of the first stream with one of the names.
func (f *cfb) stream(names ...string) ([]byte, error) {
	for _, want := range names {
		for _, e := range f.dir[1:] {
			if e.typ != 2 || e.name != want {
				continue
			}
			if e.size < uint64(f.miniCutoff) {
				return f.miniChain(e.start, int(e.size))
			}
			return f.chain(e.start, int(e.size))
		}
	}
	return nil, fmt.Errorf("%w: stream %v not found", errCFB, names)
}
