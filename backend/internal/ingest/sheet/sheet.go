// Package sheet reads spreadsheet files (.xls BIFF8 and .xlsx) into a plain cell grid with
// merged ranges. It keeps only what the import adapters need: cell text and merges.
package sheet

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Pos is a zero-based cell position.
type Pos struct{ Row, Col int }

// Range is an inclusive rectangle of merged cells; From is the cell that holds the value.
type Range struct{ From, To Pos }

// Contains reports whether p lies inside the range.
func (r Range) Contains(p Pos) bool {
	return p.Row >= r.From.Row && p.Row <= r.To.Row && p.Col >= r.From.Col && p.Col <= r.To.Col
}

// Sheet is one worksheet.
type Sheet struct {
	Name   string
	Cells  map[Pos]string
	Merges []Range
	MaxRow int
	MaxCol int

	owner map[Pos]Pos
}

// Workbook is an ordered list of worksheets.
type Workbook struct {
	Sheets []*Sheet
}

func newSheet(name string) *Sheet {
	return &Sheet{Name: name, Cells: map[Pos]string{}, MaxRow: -1, MaxCol: -1}
}

func (s *Sheet) set(p Pos, v string) {
	if strings.TrimSpace(v) == "" {
		return
	}
	s.Cells[p] = v
	s.MaxRow = max(s.MaxRow, p.Row)
	s.MaxCol = max(s.MaxCol, p.Col)
}

// maxMergedCells caps the cells one sheet may cover by merges, so a hostile file cannot make
// index building explode.
const maxMergedCells = 4_000_000

func (s *Sheet) index() error {
	s.owner = map[Pos]Pos{}
	total := 0
	for _, m := range s.Merges {
		if m.To.Row < m.From.Row || m.To.Col < m.From.Col {
			continue
		}
		total += (m.To.Row - m.From.Row + 1) * (m.To.Col - m.From.Col + 1)
		if total > maxMergedCells {
			return fmt.Errorf("sheet %q: merged ranges cover too many cells", s.Name)
		}
		for r := m.From.Row; r <= m.To.Row; r++ {
			for c := m.From.Col; c <= m.To.Col; c++ {
				s.owner[Pos{r, c}] = m.From
			}
		}
		s.MaxRow = max(s.MaxRow, m.To.Row)
		s.MaxCol = max(s.MaxCol, m.To.Col)
	}
	return nil
}

// Owner returns the top-left cell of the merge covering p, or p itself.
func (s *Sheet) Owner(p Pos) Pos {
	if o, ok := s.owner[p]; ok {
		return o
	}
	return p
}

// IsOwner reports whether p is not hidden by a merge started elsewhere.
func (s *Sheet) IsOwner(p Pos) bool { return s.Owner(p) == p }

// Raw returns the text stored in exactly this cell.
func (s *Sheet) Raw(p Pos) string { return s.Cells[p] }

// At returns the visible text of p: the value of the merge covering it, trimmed.
func (s *Sheet) At(p Pos) string { return strings.TrimSpace(s.Cells[s.Owner(p)]) }

// ColName returns the spreadsheet column letters for a zero-based index (0 → "A").
func ColName(c int) string {
	var b []byte
	for c++; c > 0; c = (c - 1) / 26 {
		b = append([]byte{byte('A' + (c-1)%26)}, b...)
	}
	return string(b)
}

// ErrUnknownFormat is returned when the data is neither .xls nor .xlsx.
var ErrUnknownFormat = errors.New("not an .xls or .xlsx file")

var (
	cfbMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	zipMagic = []byte("PK\x03\x04")
)

// IsSpreadsheet reports whether data looks like an .xls or .xlsx file.
func IsSpreadsheet(data []byte) bool {
	return bytes.HasPrefix(data, cfbMagic) || bytes.HasPrefix(data, zipMagic)
}

// Read detects the format by its signature and parses the workbook.
func Read(data []byte) (*Workbook, error) {
	var (
		wb  *Workbook
		err error
	)
	switch {
	case bytes.HasPrefix(data, cfbMagic):
		wb, err = readXLS(data)
	case bytes.HasPrefix(data, zipMagic):
		wb, err = readXLSX(data)
	default:
		return nil, ErrUnknownFormat
	}
	if err != nil {
		return nil, err
	}
	for _, s := range wb.Sheets {
		if err := s.index(); err != nil {
			return nil, err
		}
	}
	return wb, nil
}
