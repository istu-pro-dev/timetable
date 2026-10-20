package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/review"
)

func postFile(t *testing.T, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/review", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).ServeHTTP(rec, req)
	return rec
}

func TestReviewSpreadsheet(t *testing.T) {
	data, err := os.ReadFile("../ingest/tandem/testdata/t1.xls")
	if err != nil {
		t.Fatal(err)
	}
	rec := postFile(t, "t1.xls", data)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var rep review.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Stats.Lessons != 11 || len(rep.Violations) != 2 || rep.Source.File != "t1.xls" {
		t.Errorf("report: stats %+v, %d violations, source %+v", rep.Stats, len(rep.Violations), rep.Source)
	}
}

func TestReviewTTX(t *testing.T) {
	rec := postFile(t, "x.jsonl", []byte(`{"kind":"lesson","id":1,"audience":[{"group":"G"}],"day":"mon","period":1,"discipline":"A","teachers":["T"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestReviewRejects(t *testing.T) {
	if rec := postFile(t, "notes.txt", []byte("hello")); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("text file: status = %d", rec.Code)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/review", bytes.NewReader([]byte("x")))
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no multipart: status = %d", rec.Code)
	}
}
