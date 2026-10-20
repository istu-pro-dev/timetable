package sheet

import (
	"errors"
	"os"
	"testing"
)

func TestColName(t *testing.T) {
	for c, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := ColName(c); got != want {
			t.Errorf("ColName(%d) = %q, want %q", c, got, want)
		}
	}
}

func TestParseRef(t *testing.T) {
	cases := map[string]Pos{"A1": {0, 0}, "C18": {17, 2}, "AA10": {9, 26}}
	for ref, want := range cases {
		if got, ok := parseRef(ref); !ok || got != want {
			t.Errorf("parseRef(%q) = %v, %v; want %v", ref, got, ok, want)
		}
	}
	for _, bad := range []string{"", "A", "1", "A0", "a1", "A1B"} {
		if _, ok := parseRef(bad); ok {
			t.Errorf("parseRef(%q) accepted", bad)
		}
	}
}

func TestDecodeRK(t *testing.T) {
	cases := []struct {
		rk   uint32
		want float64
	}{
		{rk: 100<<2 | 2, want: 100},      // integer
		{rk: 12345<<2 | 3, want: 123.45}, // integer / 100
		{rk: 0x3FF00000, want: 1},        // IEEE high bits of 1.0
	}
	for _, c := range cases {
		if got := decodeRK(c.rk); got != c.want {
			t.Errorf("decodeRK(%#x) = %v, want %v", c.rk, got, c.want)
		}
	}
}

// TestParseSSTContinue splits a UTF-16 string across a CONTINUE record whose option byte
// switches to compressed characters.
func TestParseSSTContinue(t *testing.T) {
	sst := []byte{
		2, 0, 0, 0, 2, 0, 0, 0, // total, unique
		3, 0, 0, 'a', 'b', 'c', // "abc", compressed
		4, 0, 1, 0x1F, 0x04, 0x40, 0x04, // "Пр" in UTF-16, then the record ends
	}
	cont := []byte{0, 'x', 'y'} // option byte: compressed; "xy"
	got, err := parseSST([][]byte{sst, cont})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "abc" || got[1] != "Прxy" {
		t.Fatalf("parseSST = %q", got)
	}
}

func TestParseSSTTruncated(t *testing.T) {
	if _, err := parseSST([][]byte{{1, 0, 0, 0, 1, 0, 0, 0, 5, 0, 0, 'a'}}); err == nil {
		t.Fatal("truncated string accepted")
	}
}

func TestReadRejectsGarbage(t *testing.T) {
	if _, err := Read([]byte("hello")); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("err = %v, want ErrUnknownFormat", err)
	}
	broken := append(append([]byte{}, cfbMagic...), make([]byte, 600)...)
	if _, err := Read(broken); err == nil {
		t.Fatal("broken compound file accepted")
	}
	if _, err := Read(append([]byte("PK\x03\x04"), make([]byte, 100)...)); err == nil {
		t.Fatal("broken zip accepted")
	}
}

// TestXLSMatchesXLSX reads the same workbook saved as BIFF8 and as OOXML.
func TestXLSMatchesXLSX(t *testing.T) {
	read := func(name string) *Workbook {
		data, err := os.ReadFile("../tandem/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		wb, err := Read(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return wb
	}
	a, b := read("t1.xls"), read("t1.xlsx")
	if len(a.Sheets) != 1 || len(b.Sheets) != 1 {
		t.Fatalf("sheets: %d and %d", len(a.Sheets), len(b.Sheets))
	}
	x, y := a.Sheets[0], b.Sheets[0]
	if x.Name != "32" || x.Name != y.Name {
		t.Errorf("names %q, %q", x.Name, y.Name)
	}
	if len(x.Cells) != len(y.Cells) {
		t.Errorf("cells: %d vs %d", len(x.Cells), len(y.Cells))
	}
	for p, v := range y.Cells {
		if x.Cells[p] != v {
			t.Errorf("%s%d: %q vs %q", ColName(p.Col), p.Row+1, x.Cells[p], v)
		}
	}
	merges := map[Range]bool{}
	for _, m := range y.Merges {
		merges[m] = true
	}
	for _, m := range x.Merges {
		if !merges[m] {
			t.Errorf("merge %v only in .xls", m)
		}
	}
	if len(x.Merges) != len(y.Merges) {
		t.Errorf("merges: %d vs %d", len(x.Merges), len(y.Merges))
	}
	// The day cell is merged over the whole Monday block.
	if got := x.At(Pos{Row: 10, Col: 1}); got != "пн" {
		t.Errorf("At(B11) = %q, want пн", got)
	}
}
