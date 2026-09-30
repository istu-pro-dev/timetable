package itc_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/itc"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/move"
)

// policy switches off H8: lectures of one course on the same day are allowed in ITC-2007
// (MinimumWorkingDays is soft) while the engine treats them as siblings of one item.
var policy = hard.Policy{hard.H8SameDay: hard.Off}

type solutionFile struct {
	instance string // e.g. comp01
	source   string // subdirectory of testdata/solutions
	path     string
}

func solutionFiles(t *testing.T) []solutionFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "solutions", "*", "*.sol"))
	if err != nil {
		t.Fatal(err)
	}
	var out []solutionFile
	for _, p := range paths {
		out = append(out, solutionFile{
			instance: strings.TrimSuffix(filepath.Base(p), ".sol"),
			source:   filepath.Base(filepath.Dir(p)),
			path:     p,
		})
	}
	if len(out) < 21 {
		t.Fatalf("found %d solutions, want at least one for each of comp01–comp21", len(out))
	}
	return out
}

func load(t *testing.T, f solutionFile) (*itc.Instance, []itc.Assignment) {
	t.Helper()
	inst := loadInstance(t, f.instance)
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	sol, err := itc.ParseSolution(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return inst, sol
}

func loadInstance(t *testing.T, name string) *itc.Instance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "instances", name+".ctt"))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := itc.ParseInstance(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return inst
}

// build maps the instance and places the solution.
func build(t *testing.T, inst *itc.Instance, sol []itc.Assignment) (*engine.Schedule, []int32) {
	t.Helper()
	m := itc.NewModel(inst)
	p, err := engine.NewProblem(m.Input)
	if err != nil {
		t.Fatal(err)
	}
	s, lessons, err := m.Schedule(p, sol)
	if err != nil {
		t.Fatal(err)
	}
	return s, lessons
}

// TestITCFeasibleSolutions validates every published solution twice: with the from-scratch
// reference checker below (so a broken test file is not blamed on the engine) and with the
// engine, which must report no hard violation at all.
func TestITCFeasibleSolutions(t *testing.T) {
	for _, f := range solutionFiles(t) {
		t.Run(f.source+"/"+f.instance, func(t *testing.T) {
			inst, sol := load(t, f)
			if errs := referenceCheck(inst, sol); len(errs) != 0 {
				t.Fatalf("reference checker rejects the published solution: %v", errs)
			}
			s, _ := build(t, inst, sol)
			if vs := hard.Check(s, policy); len(vs) != 0 {
				t.Fatalf("engine reports %d violations on a feasible solution, first: %+v", len(vs), vs[0])
			}
		})
	}
}

// TestITCCorruptionCrossCheck moves single lectures of every published solution onto the
// period (and sometimes the room) of another lecture and checks that the engine's H1–H3
// agree lecture by lecture with the reference checker's clashes, and that the dry-run move
// evaluator reports the same.
func TestITCCorruptionCrossCheck(t *testing.T) {
	moves := 25
	if testing.Short() {
		moves = 8
	}
	for fi, f := range solutionFiles(t) {
		t.Run(f.source+"/"+f.instance, func(t *testing.T) {
			inst, sol := load(t, f)
			s, lessons := build(t, inst, sol)
			ev := move.NewEvaluator(s.P, policy)
			rooms := map[string]int32{}
			for i, r := range inst.Rooms {
				rooms[r.ID] = int32(i)
			}
			r := rand.New(rand.NewPCG(uint64(fi), 29))
			caught := 0
			for range moves {
				x, y := r.IntN(len(sol)), r.IntN(len(sol))
				bad := slices.Clone(sol)
				bad[x].Period = sol[y].Period
				switch r.IntN(3) {
				case 0:
					bad[x].Room = sol[y].Room
				case 1:
					bad[x].Room = inst.Rooms[r.IntN(len(inst.Rooms))].ID
				}

				m := move.Relocate(s, lessons[x], itc.Slot(bad[x].Period), rooms[bad[x].Room])
				res, err := ev.Evaluate(s, m)
				if err != nil {
					t.Fatal(err)
				}
				after := s.Clone()
				if _, err := move.Apply(after, m); err != nil {
					t.Fatal(err)
				}
				vs := hard.Check(after, policy)

				engineClash := map[int32]bool{}
				for _, v := range vs {
					if v.Rule > hard.H3RoomBusy {
						t.Fatalf("unexpected %v after moving a lecture: %+v", v.Rule, v)
					}
					for _, l := range v.Lessons {
						engineClash[l] = true
					}
				}
				refClash := map[int32]bool{}
				for _, c := range clashes(inst, bad) {
					refClash[lessons[c.a]], refClash[lessons[c.b]] = true, true
					if !slices.ContainsFunc(vs, func(v hard.Violation) bool {
						return v.Rule == c.rule && v.Involves(lessons[c.a]) && v.Involves(lessons[c.b])
					}) {
						t.Fatalf("move %v → %v: engine misses %v between %v and %v; engine: %+v", sol[x], bad[x], c.rule, bad[c.a], bad[c.b], vs)
					}
				}
				if !mapsEqual(engineClash, refClash) {
					t.Fatalf("move %v → %v: engine clashes %v, reference %v", sol[x], bad[x], keys(engineClash), keys(refClash))
				}
				if got := hard.Involving(vs, lessons[x]); len(got) != len(res.Violations) {
					t.Fatalf("dry run reports %d violations, applied move %d", len(res.Violations), len(got))
				}
				if len(vs) > 0 {
					caught++
				}
			}
			if caught == 0 {
				t.Fatal("no move produced a clash; the cross-check tested nothing")
			}
			if vs := hard.Check(s, policy); len(vs) != 0 {
				t.Fatalf("dry runs changed the schedule: %+v", vs[0])
			}
		})
	}
}

// TestITCUnavailabilityIsNotModelled documents the mapping gap: ITC forbids periods per
// course, the engine only per teacher or room, so a lecture moved into a forbidden period is
// caught by the reference checker only.
func TestITCUnavailabilityIsNotModelled(t *testing.T) {
	inst := loadInstance(t, "comp01")
	f := solutionFile{instance: "comp01", path: filepath.Join("testdata", "solutions", "erm", "comp01.sol")}
	_, sol := load(t, f)
	u := inst.Unavailability[0]
	i := slices.IndexFunc(sol, func(a itc.Assignment) bool { return a.Course == u.Course })
	bad := slices.Clone(sol)
	bad[i].Period = u.Period
	errs := referenceCheck(inst, bad)
	if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, "unavailable") }) {
		t.Fatalf("reference checker misses the unavailability: %v", errs)
	}
	s, _ := build(t, inst, bad)
	for _, v := range hard.Check(s, policy) {
		if v.Rule == hard.H4Availability {
			t.Fatalf("course unavailability unexpectedly mapped: %+v", v)
		}
	}
}

func TestParseInstance(t *testing.T) {
	inst := loadInstance(t, "comp01")
	if inst.Name != "Fis0506-1" || inst.Days != 5 || inst.PeriodsPerDay != 6 ||
		len(inst.Courses) != 30 || len(inst.Rooms) != 6 || len(inst.Curricula) != 14 || len(inst.Unavailability) != 53 {
		t.Fatalf("comp01 header: %+v", inst)
	}
	if c := inst.Courses[0]; c != (itc.Course{ID: "c0001", Teacher: "t000", Lectures: 6, MinDays: 4, Students: 130}) {
		t.Fatalf("first course %+v", c)
	}
	if r := inst.Rooms[0]; r != (itc.Room{ID: "B", Capacity: 200}) {
		t.Fatalf("first room %+v", r)
	}
	if q := inst.Curricula[0]; q.ID != "q000" || !slices.Equal(q.Courses, []string{"c0001", "c0002", "c0004", "c0005"}) {
		t.Fatalf("first curriculum %+v", q)
	}
	if u := inst.Unavailability[0]; u != (itc.Unavailability{Course: "c0001", Period: itc.Period{Day: 4, Period: 0}}) {
		t.Fatalf("first unavailability %+v", u)
	}
}

const tiny = `Name: tiny
Courses: 2
Rooms: 1
Days: 1
Periods_per_day: 2
Curricula: 1
Constraints: 1

COURSES:
c1 t1 1 1 10
c2 t1 1 1 10

ROOMS:
r1 20

CURRICULA:
q1 1 c1

UNAVAILABILITY_CONSTRAINTS:
c1 0 1

END.
`

func TestParseErrors(t *testing.T) {
	if _, err := itc.ParseInstance(strings.NewReader(tiny)); err != nil {
		t.Fatalf("tiny: %v", err)
	}
	cases := map[string]string{
		"missing end":       strings.Replace(tiny, "END.", "", 1),
		"data after end":    tiny + "c1 0 0\n",
		"bad header":        strings.Replace(tiny, "Days: 1", "Days: x", 1),
		"count mismatch":    strings.Replace(tiny, "Courses: 2", "Courses: 3", 1),
		"course fields":     strings.Replace(tiny, "c2 t1 1 1 10", "c2 t1 1 1", 1),
		"course number":     strings.Replace(tiny, "c2 t1 1 1 10", "c2 t1 1 x 10", 1),
		"room fields":       strings.Replace(tiny, "r1 20", "r1", 1),
		"curriculum count":  strings.Replace(tiny, "q1 1 c1", "q1 2 c1", 1),
		"curriculum number": strings.Replace(tiny, "q1 1 c1", "q1 x c1", 1),
		"unknown course":    strings.Replace(tiny, "q1 1 c1", "q1 1 c9", 1),
		"unavail fields":    strings.Replace(tiny, "c1 0 1\n", "c1 0\n", 1),
		"unavail range":     strings.Replace(tiny, "c1 0 1\n", "c1 0 2\n", 1),
		"duplicate course":  strings.Replace(tiny, "c2 t1", "c1 t1", 1),
		"empty grid":        strings.Replace(tiny, "Days: 1", "Days: 0", 1),
		"stray line":        strings.Replace(tiny, "COURSES:", "", 1),
	}
	for name, text := range cases {
		if _, err := itc.ParseInstance(strings.NewReader(text)); !errors.Is(err, itc.ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	if _, err := itc.ParseSolution(strings.NewReader("c1 r1 0\n")); !errors.Is(err, itc.ErrFormat) {
		t.Errorf("short solution line: %v", err)
	}
	if _, err := itc.ParseSolution(strings.NewReader("c1 r1 0 x\n")); !errors.Is(err, itc.ErrFormat) {
		t.Errorf("bad solution number: %v", err)
	}
}

func TestModel(t *testing.T) {
	inst, err := itc.ParseInstance(strings.NewReader(tiny))
	if err != nil {
		t.Fatal(err)
	}
	m := itc.NewModel(inst)
	p, err := engine.NewProblem(m.Input)
	if err != nil {
		t.Fatal(err)
	}
	// c2 is in no curriculum and gets a private group; both courses share teacher t1.
	if len(p.Groups) != 2 || len(p.Teachers) != 1 || len(p.Lessons) != 2 || p.Lessons[0].Size != 0 {
		t.Fatalf("model: %+v", p)
	}
	for name, sol := range map[string]string{
		"too many lectures": "c1 r1 0 0\nc1 r1 0 1\n",
		"unknown room":      "c1 r9 0 0\n",
		"outside grid":      "c1 r1 3 0\n",
	} {
		a, err := itc.ParseSolution(strings.NewReader(sol))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.Schedule(p, a); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	a, _ := itc.ParseSolution(strings.NewReader("c1 r1 0 0\nc2 r1 0 0\n"))
	s, _, err := m.Schedule(p, a)
	if err != nil {
		t.Fatal(err)
	}
	// Same teacher and room in one period: H1 and H3, no H2 (different curricula).
	vs := hard.Check(s, policy)
	if len(vs) != 2 || vs[0].Rule != hard.H1TeacherBusy || vs[1].Rule != hard.H3RoomBusy {
		t.Fatalf("violations %+v", vs)
	}
}

// --- Reference checker ------------------------------------------------------------------
//
// A deliberately naive implementation of the ITC-2007 Track 3 hard constraints, written from
// the competition rules without any engine code, to validate the test data and to compare the
// engine against.

type clash struct {
	rule hard.Rule // the engine rule expected to report it
	a, b int       // solution line indices, a < b
}

// clashes returns every pair of lectures that may not share their period: same teacher (which
// includes two lectures of one course), a common curriculum, or the same room.
func clashes(inst *itc.Instance, sol []itc.Assignment) []clash {
	teacher := map[string]string{}
	for _, c := range inst.Courses {
		teacher[c.ID] = c.Teacher
	}
	var out []clash
	for i := range sol {
		for j := i + 1; j < len(sol); j++ {
			a, b := sol[i], sol[j]
			if a.Period != b.Period {
				continue
			}
			if teacher[a.Course] == teacher[b.Course] {
				out = append(out, clash{hard.H1TeacherBusy, i, j})
			}
			if a.Course != b.Course && shareCurriculum(inst, a.Course, b.Course) {
				out = append(out, clash{hard.H2GroupBusy, i, j})
			}
			if a.Room == b.Room {
				out = append(out, clash{hard.H3RoomBusy, i, j})
			}
		}
	}
	return out
}

func shareCurriculum(inst *itc.Instance, a, b string) bool {
	return slices.ContainsFunc(inst.Curricula, func(q itc.Curriculum) bool {
		return slices.Contains(q.Courses, a) && slices.Contains(q.Courses, b)
	})
}

// referenceCheck returns a description of every hard-constraint violation: wrong number of
// lectures, unknown course or room, period outside the grid, clashes and unavailability.
func referenceCheck(inst *itc.Instance, sol []itc.Assignment) []string {
	var errs []string
	count := map[string]int{}
	rooms := map[string]bool{}
	for _, r := range inst.Rooms {
		rooms[r.ID] = true
	}
	for _, a := range sol {
		count[a.Course]++
		if !rooms[a.Room] {
			errs = append(errs, "unknown room "+a.Room)
		}
		if a.Day < 0 || a.Day >= inst.Days || a.Period.Period < 0 || a.Period.Period >= inst.PeriodsPerDay {
			errs = append(errs, fmt.Sprintf("outside grid %+v", a))
		}
		for _, u := range inst.Unavailability {
			if u.Course == a.Course && u.Period == a.Period {
				errs = append(errs, fmt.Sprintf("unavailable %+v", a))
			}
		}
	}
	for _, c := range inst.Courses {
		if count[c.ID] != c.Lectures {
			errs = append(errs, fmt.Sprintf("course %s: %d lectures scheduled, %d required", c.ID, count[c.ID], c.Lectures))
		}
		delete(count, c.ID)
	}
	for c := range count {
		errs = append(errs, "unknown course "+c)
	}
	for _, c := range clashes(inst, sol) {
		errs = append(errs, fmt.Sprintf("%v clash %+v / %+v", c.rule, sol[c.a], sol[c.b]))
	}
	return errs
}

func mapsEqual(a, b map[int32]bool) bool {
	return slices.Equal(keys(a), keys(b))
}

func keys(m map[int32]bool) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
