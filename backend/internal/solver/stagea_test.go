package solver

import (
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/edit"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

func TestBuildFeasibleSmall(t *testing.T) {
	p := mustProblem(t, smallInput())
	res := BuildFeasible(NewGraph(p), engine.NewSchedule(p), StageAOptions{Seed: 1})
	if !res.Feasible || len(res.Violations) != 0 || len(res.Forced) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

// overConstrained: one teacher with three weekly lessons on a grid with two slots, and a
// lecture for 50 students with only a 20-seat room.
func overConstrained() engine.Input {
	return engine.Input{
		Grid:      domain.Grid{Days: 1, PeriodsPerDay: 2},
		Buildings: []engine.Building{{ID: 1, Name: "A"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "R1", Building: 1, Type: "sem", Capacity: 30},
			{ID: 2, Name: "R2", Building: 1, Type: "sem", Capacity: 30},
			{ID: 3, Name: "R3", Building: 1, Type: "sem", Capacity: 30},
			{ID: 4, Name: "Small", Building: 1, Type: "lecture", Capacity: 20},
		},
		Teachers:    []engine.TeacherInput{{ID: 1, Name: "T"}, {ID: 2, Name: "U"}},
		Groups:      []engine.Group{{ID: 1, Name: "G1", Size: 20}, {ID: 2, Name: "G2", Size: 20}, {ID: 3, Name: "G3", Size: 20}, {ID: 4, Name: "Big", Size: 50}},
		Disciplines: []engine.Discipline{{ID: 1, Name: "D"}},
		Lessons: []engine.LessonInput{
			{ID: 1, Item: 1, Discipline: 1, Teacher: 1, RoomType: "sem", Audience: domain.Audience{domain.WholeGroup(1)}},
			{ID: 2, Item: 2, Discipline: 1, Teacher: 1, RoomType: "sem", Audience: domain.Audience{domain.WholeGroup(2)}},
			{ID: 3, Item: 3, Discipline: 1, Teacher: 1, RoomType: "sem", Audience: domain.Audience{domain.WholeGroup(3)}},
			{ID: 4, Item: 4, Discipline: 1, Teacher: 2, RoomType: "lecture", Audience: domain.Audience{domain.WholeGroup(4)}},
		},
	}
}

func TestBuildFeasibleFallback(t *testing.T) {
	p := mustProblem(t, overConstrained())
	s := engine.NewSchedule(p)
	res := BuildFeasible(NewGraph(p), s, StageAOptions{Seed: 1, RepairTime: 100_000_000})
	if res.Feasible {
		t.Fatal("over-constrained instance reported feasible")
	}
	if len(res.Forced) != 2 || len(res.Unplaced) != 0 {
		t.Fatalf("forced = %v, unplaced = %v", res.Forced, res.Unplaced)
	}
	// The schedule is complete: every lesson has a slot and a room.
	for l, a := range s.Assignments() {
		if !a.Placed || a.Room == engine.None {
			t.Fatalf("lesson %d not fully placed: %+v", l, a)
		}
	}
	rules := map[hard.Rule]hard.Strictness{}
	for _, v := range res.Violations {
		rules[v.Rule] = v.Strictness
	}
	if rules[hard.H1TeacherBusy] != hard.HeavySoft || rules[hard.H6Capacity] != hard.HeavySoft {
		t.Fatalf("violations = %+v", res.Violations)
	}
	if _, ok := rules[hard.H7Completeness]; ok {
		t.Fatal("H7 reported although every lesson is placed")
	}

	// The report converts to the EvaluateMove format.
	conv := edit.ConvertViolations(p, res.Violations)
	if len(conv) != len(res.Violations) || conv[0].Strictness != "heavy_soft" || len(conv[0].Lessons) == 0 {
		t.Fatalf("converted = %+v", conv)
	}
	if !slices.ContainsFunc(conv, func(v edit.Violation) bool {
		return v.Rule == "H1" && v.Resource != nil && v.Resource.Kind == "teacher" && v.Resource.ID == 1
	}) {
		t.Fatalf("no H1 for teacher 1 in %+v", conv)
	}
}

func TestBuildFeasibleRespectsOffPolicy(t *testing.T) {
	p := mustProblem(t, overConstrained())
	res := BuildFeasible(NewGraph(p), engine.NewSchedule(p), StageAOptions{
		Seed: 2, RepairTime: 100_000_000, Policy: hard.Policy{hard.H6Capacity: hard.Off},
	})
	for _, v := range res.Violations {
		if v.Rule == hard.H6Capacity {
			t.Fatal("H6 reported although switched off")
		}
	}
}

func TestBuildFeasibleSeedDataset(t *testing.T) {
	p := seedProblem(t)
	res := BuildFeasible(NewGraph(p), engine.NewSchedule(p), StageAOptions{Seed: 1})
	t.Logf("seed dataset: feasible=%v dsatur-unplaced=%d forced=%d in %v", res.Feasible, res.DSaturUnplaced, len(res.Forced), res.Duration)
	if !res.Feasible {
		for _, v := range res.Violations {
			t.Log(v.Rule, v.Message)
		}
		t.Fatal("seed dataset is feasible by construction")
	}
}
