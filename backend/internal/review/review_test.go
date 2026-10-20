package review

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/ingest"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
)

func load(t *testing.T, ttxText string) *Report {
	t.Helper()
	b, err := ttx.Read(strings.NewReader(ttxText))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Analyze(b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func rules(r *Report) []string {
	var out []string
	for _, v := range r.Violations {
		out = append(out, v.Rule+" "+v.Resource.Name)
	}
	return out
}

func TestAnalyzeFixture(t *testing.T) {
	data, err := os.ReadFile("../ingest/tandem/testdata/t1.xls")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ingest.Load("t1.xls", data)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Analyze(b)
	if err != nil {
		t.Fatal(err)
	}
	// Соболева teaches two groups different labs in one room at once; the two physical
	// education lessons in the sports complex do not collide.
	if got := rules(r); !slices.Equal(got, []string{"H1 Соболева Н.В.", "H3 3-605"}) {
		t.Errorf("violations = %v", got)
	}
	if v := r.Violations[0]; v.Day != "mon" || v.Period != 3 || len(v.Lessons) != 2 {
		t.Errorf("H1 = %+v", v)
	}
	if r.Stats.Lessons != 11 || r.Stats.Groups != 2 || r.Stats.Hard != 2 {
		t.Errorf("stats = %+v", r.Stats)
	}
	for _, s := range r.Teachers {
		if strings.HasPrefix(s.Name, "(не указан") {
			t.Errorf("stand-in teacher %q in scores", s.Name)
		}
	}
	for _, c := range r.Soft {
		if c.Key == "room_capacity_waste" && c.Available {
			t.Error("capacity waste available without capacities")
		}
	}
}

func TestSubgroupsInParallelAreNotAConflict(t *testing.T) {
	r := load(t, `{"kind":"lesson","id":1,"audience":[{"group":"G","subgroup":1}],"day":"mon","period":1,"parity":"odd","lesson_type":"lab","discipline":"A","teachers":["T1"],"room":"1"}
{"kind":"lesson","id":2,"audience":[{"group":"G","subgroup":2}],"day":"mon","period":1,"parity":"odd","lesson_type":"lab","discipline":"B","teachers":["T2"],"room":"2"}
{"kind":"lesson","id":3,"audience":[{"group":"G"}],"day":"mon","period":1,"parity":"even","lesson_type":"lecture","discipline":"C","teachers":["T1"],"room":"1"}`)
	if len(r.Violations) != 0 {
		t.Errorf("violations = %+v", r.Violations)
	}
}

func TestGroupConflictAcrossParities(t *testing.T) {
	r := load(t, `{"kind":"lesson","id":1,"audience":[{"group":"G"}],"day":"tue","period":2,"parity":"every","lesson_type":"lecture","discipline":"A","teachers":["T1"],"room":"1"}
{"kind":"lesson","id":2,"audience":[{"group":"G","subgroup":1}],"day":"tue","period":2,"parity":"even","lesson_type":"lab","discipline":"B","teachers":["T2"],"place":"online"}`)
	if got := rules(r); !slices.Equal(got, []string{"H2 G"}) {
		t.Errorf("violations = %v", got)
	}
}

func TestCapacityCheckedWhenKnown(t *testing.T) {
	r := load(t, `{"kind":"group","key":"G","size":30}
{"kind":"room","key":"1","building":"1","type":"general","title":"1","capacity":20}
{"kind":"lesson","id":1,"audience":[{"group":"G"}],"day":"mon","period":1,"lesson_type":"lecture","discipline":"A","teachers":["T"],"room":"1"}`)
	if got := rules(r); !slices.Equal(got, []string{"H6 1"}) {
		t.Errorf("violations = %v", got)
	}
	for _, c := range r.Soft {
		if c.Key == "room_capacity_waste" && !c.Available {
			t.Error("capacity waste unavailable with full data")
		}
	}
}

func TestScores(t *testing.T) {
	r := load(t, `{"kind":"room","key":"1-1","building":"1","type":"general","title":"1-1","capacity":null}
{"kind":"room","key":"2-1","building":"2","type":"general","title":"2-1","capacity":null}
{"kind":"lesson","id":1,"audience":[{"group":"G"}],"day":"mon","period":1,"lesson_type":"lecture","discipline":"A","teachers":["T"],"room":"1-1"}
{"kind":"lesson","id":2,"audience":[{"group":"G"}],"day":"mon","period":3,"lesson_type":"lecture","discipline":"B","teachers":["T"],"room":"2-1"}`)
	g := r.Groups[0]
	// Every-week lessons count in both weeks: one gap and one transition per week.
	if g.Name != "G" || g.Lessons != 4 || g.Gaps != 2 || g.Transitions != 2 || g.MaxPerDay != 2 {
		t.Errorf("group score = %+v", g)
	}
}
