// Package ingest turns uploaded files into ttx batches. A file is either ttx itself (JSON
// Lines, any producer) or a spreadsheet that one of the adapters recognises.
package ingest

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/istu-pro-dev/timetable/backend/internal/ingest/sheet"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/tandem"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
)

// ErrUnsupported is returned when no adapter accepts the file.
var ErrUnsupported = errors.New("unsupported file: expected ttx (JSON Lines), or a Тандем .xls/.xlsx timetable")

// Load detects the file type and converts it. name is used for the source description only.
func Load(name string, data []byte) (*ttx.Batch, error) {
	if trimmed := bytes.TrimLeft(data, " \t\r\n\xef\xbb\xbf"); len(trimmed) > 0 && trimmed[0] == '{' {
		return ttx.Read(bytes.NewReader(trimmed))
	}
	if !sheet.IsSpreadsheet(data) {
		return nil, ErrUnsupported
	}
	wb, err := sheet.Read(data)
	if err != nil {
		return nil, fmt.Errorf("read spreadsheet: %w", err)
	}
	if tandem.Detect(wb) {
		return tandem.Convert(wb, name, data)
	}
	return nil, ErrUnsupported
}
