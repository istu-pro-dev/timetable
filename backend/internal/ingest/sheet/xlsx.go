package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

var errXLSX = errors.New("corrupt .xlsx workbook")

// maxXLSXPart limits the decompressed size of one zip member (zip bombs).
const maxXLSXPart = 256 << 20

func readXLSX(data []byte) (*Workbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errXLSX, err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("%w: missing %s", errXLSX, name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		b, err := io.ReadAll(io.LimitReader(rc, maxXLSXPart+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxXLSXPart {
			return nil, fmt.Errorf("%w: %s is too large", errXLSX, name)
		}
		return b, nil
	}

	var shared []string
	if _, ok := files["xl/sharedStrings.xml"]; ok {
		b, err := read("xl/sharedStrings.xml")
		if err != nil {
			return nil, err
		}
		var sst struct {
			SI []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if err := xml.Unmarshal(b, &sst); err != nil {
			return nil, fmt.Errorf("%w: shared strings: %w", errXLSX, err)
		}
		for _, si := range sst.SI {
			v := si.T
			for _, r := range si.R {
				v += r.T
			}
			shared = append(shared, v)
		}
	}

	wbXML, err := read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(wbXML, &book); err != nil {
		return nil, fmt.Errorf("%w: workbook: %w", errXLSX, err)
	}
	relsXML, err := read("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsXML, &rels); err != nil {
		return nil, fmt.Errorf("%w: relationships: %w", errXLSX, err)
	}
	targets := map[string]string{}
	for _, r := range rels.Rel {
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join("xl", t)
		}
		targets[r.ID] = t
	}

	wb := &Workbook{}
	for _, sh := range book.Sheets {
		b, err := read(targets[sh.RID])
		if err != nil {
			return nil, err
		}
		s, err := parseXLSXSheet(sh.Name, b, shared)
		if err != nil {
			return nil, err
		}
		wb.Sheets = append(wb.Sheets, s)
	}
	return wb, nil
}

func parseXLSXSheet(name string, data []byte, shared []string) (*Sheet, error) {
	var ws struct {
		Rows []struct {
			C []struct {
				R  string `xml:"r,attr"`
				T  string `xml:"t,attr"`
				V  string `xml:"v"`
				IS struct {
					T string `xml:"t"`
					R []struct {
						T string `xml:"t"`
					} `xml:"r"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
		Merges []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCells>mergeCell"`
	}
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("%w: sheet %q: %w", errXLSX, name, err)
	}
	s := newSheet(name)
	for _, row := range ws.Rows {
		for _, c := range row.C {
			p, ok := parseRef(c.R)
			if !ok {
				continue
			}
			v := c.V
			switch c.T {
			case "s":
				var i int
				if _, err := fmt.Sscan(c.V, &i); err != nil || i < 0 || i >= len(shared) {
					continue
				}
				v = shared[i]
			case "inlineStr":
				v = c.IS.T
				for _, r := range c.IS.R {
					v += r.T
				}
			}
			s.set(p, v)
		}
	}
	for _, m := range ws.Merges {
		a, b, ok := strings.Cut(m.Ref, ":")
		if !ok {
			continue
		}
		from, ok1 := parseRef(a)
		to, ok2 := parseRef(b)
		if ok1 && ok2 {
			s.Merges = append(s.Merges, Range{From: from, To: to})
		}
	}
	return s, nil
}

// parseRef parses an A1-style reference.
func parseRef(ref string) (Pos, bool) {
	col, i := 0, 0
	for ; i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z'; i++ {
		col = col*26 + int(ref[i]-'A'+1)
		if col > 1<<14 {
			return Pos{}, false
		}
	}
	row := 0
	for j := i; j < len(ref); j++ {
		if ref[j] < '0' || ref[j] > '9' {
			return Pos{}, false
		}
		row = row*10 + int(ref[j]-'0')
		if row > 1<<20 {
			return Pos{}, false
		}
	}
	if i == 0 || i == len(ref) || row == 0 {
		return Pos{}, false
	}
	return Pos{Row: row - 1, Col: col - 1}, true
}
