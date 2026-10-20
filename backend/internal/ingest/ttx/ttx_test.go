package ttx

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	two := 2
	b := &Batch{
		Source:   Source{ID: "s", Adapter: "test"},
		Periods:  []Period{{N: 1, Start: "08:30", End: "10:00"}},
		Groups:   []Group{{Key: "Б24-782-2", Level: "bachelor", Intake: 2024}},
		Teachers: []Teacher{{Key: "Соболева Н.В."}},
		Rooms:    []Room{{Key: "3-605", Building: "3", Type: "computer", Title: "выч кл. 3-605"}},
		Places:   []Place{{Key: "online", Title: "Онлайн", Virtual: true}},
		Lessons: []Lesson{{
			ID: 1, Refs: []Ref{{Sheet: "32", Row: 3, Col: "C"}}, Audience: []Member{{Group: "Б24-782-2", Subgroup: &two}},
			Day: "wed", Period: 1, Parity: "even", LessonType: "lab", Discipline: "Базы данных",
			Teachers: []string{"Соболева Н.В."}, Room: "3-605",
		}},
		Issues: []Issue{{Code: "typo_week", Message: "опечатка"}},
	}
	var buf bytes.Buffer
	if err := Write(&buf, b); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "\n"); n != 8 {
		t.Errorf("lines = %d, want 8", n)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	b.Source.Kind, b.Source.Format = "source", Format
	for i := range b.Periods {
		b.Periods[i].Kind = "period"
	}
	b.Groups[0].Kind, b.Teachers[0].Kind, b.Rooms[0].Kind, b.Places[0].Kind = "group", "teacher", "room", "place"
	b.Lessons[0].Kind, b.Issues[0].Kind = "lesson", "issue"
	if !reflect.DeepEqual(got, b) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, b)
	}
}

func TestReadLessonsOnly(t *testing.T) {
	in := `{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":2,"lesson_type":"lecture","discipline":"X","teachers":["T"],"room":"1-1"}
{"kind":"future_kind","x":1}
`
	b, err := Read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if b.Lessons[0].Parity != "every" {
		t.Errorf("parity = %q, want every", b.Lessons[0].Parity)
	}
	if len(b.Groups) != 1 || len(b.Teachers) != 1 || len(b.Rooms) != 1 {
		t.Errorf("implicit records: %+v %+v %+v", b.Groups, b.Teachers, b.Rooms)
	}
}

func TestReadErrors(t *testing.T) {
	cases := map[string]string{
		"bad json":       `{"kind":`,
		"unknown day":    `{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"xx","period":1}`,
		"no audience":    `{"kind":"lesson","id":1,"audience":[],"day":"mon","period":1}`,
		"room and place": `{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":1,"room":"r","place":"online"}`,
		"bad parity":     `{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":1,"parity":"weekly"}`,
		"bad subgroup":   `{"kind":"lesson","id":1,"audience":[{"group":"A","subgroup":0}],"day":"mon","period":1}`,
		"duplicate id": `{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":1}
{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":2}`,
		"unknown period": `{"kind":"period","n":1,"start":"08:30","end":"10:00"}
{"kind":"lesson","id":1,"audience":[{"group":"A"}],"day":"mon","period":3}`,
	}
	for name, in := range cases {
		if _, err := Read(strings.NewReader(in)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}
