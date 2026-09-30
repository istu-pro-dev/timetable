package solver

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
)

// Lessons of the hand-made instance.
const (
	lecA  int32 = iota // T1, stream G1+G2, lecture
	semB               // T1, G2, seminar
	semC               // T2, G1, seminar
	labD               // T2, G1 english/1, lab, biweekly
	labE               // T3, G1 english/2, lab
	peF                // T3, G1 pe/1, lab
	semC2              // T2, G1, seminar, same item as semC
	nSmall
)

func smallInput() engine.Input {
	aud := func(ms ...domain.Member) domain.Audience { return ms }
	return engine.Input{
		Grid:      domain.Grid{Days: 5, PeriodsPerDay: 4},
		Buildings: []engine.Building{{ID: 1, Name: "A"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "hall", Building: 1, Type: "lecture", Capacity: 60},
			{ID: 2, Name: "sem1", Building: 1, Type: "seminar", Capacity: 30},
			{ID: 3, Name: "sem2", Building: 1, Type: "seminar", Capacity: 40},
			{ID: 4, Name: "lab1", Building: 1, Type: "lab", Capacity: 15,
				Avail: []engine.SlotAvailability{{Slot: domain.Slot{Day: domain.Friday, Period: 1}, Status: engine.Unavailable}}},
			{ID: 5, Name: "lab2", Building: 1, Type: "lab", Capacity: 15,
				Avail: []engine.SlotAvailability{{Slot: domain.Slot{Day: domain.Friday, Period: 1}, Status: engine.Unavailable}}},
		},
		Teachers: []engine.TeacherInput{
			{ID: 1, Name: "T1", Avail: []engine.SlotAvailability{{Slot: domain.Slot{Day: domain.Monday, Period: 1}, Status: engine.Unavailable}}},
			{ID: 2, Name: "T2"},
			{ID: 3, Name: "T3"},
		},
		Groups: []engine.Group{{ID: 1, Name: "G1", Size: 25}, {ID: 2, Name: "G2", Size: 25}},
		Subgroups: []engine.SubgroupInput{
			{Group: 1, Division: "english", Part: 1, Size: 12}, {Group: 1, Division: "english", Part: 2, Size: 13},
			{Group: 1, Division: "pe", Part: 1, Size: 12},
		},
		Disciplines: []engine.Discipline{{ID: 1, Name: "D"}},
		Lessons: []engine.LessonInput{
			{ID: 1, Item: 1, Discipline: 1, Kind: "lecture", Teacher: 1, RoomType: "lecture", Audience: aud(domain.WholeGroup(1), domain.WholeGroup(2))},
			{ID: 2, Item: 2, Discipline: 1, Kind: "seminar", Teacher: 1, RoomType: "seminar", Audience: aud(domain.WholeGroup(2))},
			{ID: 3, Item: 3, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar", Audience: aud(domain.WholeGroup(1))},
			{ID: 4, Item: 4, Discipline: 1, Kind: "lab", Teacher: 2, RoomType: "lab", Biweekly: true, Audience: aud(domain.Subgroup(1, "english", 1))},
			{ID: 5, Item: 5, Discipline: 1, Kind: "lab", Teacher: 3, RoomType: "lab", Audience: aud(domain.Subgroup(1, "english", 2))},
			{ID: 6, Item: 6, Discipline: 1, Kind: "lab", Teacher: 3, RoomType: "lab", Audience: aud(domain.Subgroup(1, "pe", 1))},
			{ID: 7, Item: 3, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar", Audience: aud(domain.WholeGroup(1))},
		},
	}
}

func mustProblem(t testing.TB, in engine.Input) *engine.Problem {
	t.Helper()
	p, err := engine.NewProblem(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGraphEdges(t *testing.T) {
	g := NewGraph(mustProblem(t, smallInput()))
	want := map[int32][]int32{
		lecA:  {semB, semC, labD, labE, peF, semC2}, // T1 + stream covers G1 and G2
		semB:  {lecA},                               // T1; G2 only
		semC:  {lecA, labD, labE, peF, semC2},       // whole G1 + T2
		labD:  {lecA, semC, peF, semC2},             // english/1: not english/2, but pe crosses
		labE:  {lecA, semC, peF, semC2},             // english/2 + T3
		peF:   {lecA, semC, labD, labE, semC2},      // pe crosses english
		semC2: {lecA, semC, labD, labE, peF},
	}
	for l := range nSmall {
		if !slices.Equal(g.Adj[l], want[l]) {
			t.Errorf("Adj[%d] = %v, want %v", l, g.Adj[l], want[l])
		}
	}
	if g.Edges() != 15 {
		t.Errorf("Edges = %d, want 15", g.Edges())
	}
	if !slices.Equal(g.Siblings[semC], []int32{semC2}) || len(g.Siblings[lecA]) != 0 {
		t.Errorf("siblings: %v / %v", g.Siblings[semC], g.Siblings[lecA])
	}
}

func TestGraphDomains(t *testing.T) {
	p := mustProblem(t, smallInput())
	g := NewGraph(p)
	positions := p.Grid.TimeCount() // 20

	// T1 unavailable Monday 1.
	if len(g.Domain[lecA]) != positions-1 || slices.Contains(g.Domain[lecA], domain.Slot{Day: domain.Monday, Period: 1}) {
		t.Errorf("lecture domain = %d slots", len(g.Domain[lecA]))
	}
	// Biweekly lab: odd and even per position, minus Friday 1 (both labs closed).
	if got := len(g.Domain[labD]); got != 2*(positions-1) {
		t.Errorf("biweekly domain = %d, want %d", got, 2*(positions-1))
	}
	for _, s := range g.Domain[labD] {
		if s.Parity == domain.EveryWeek {
			t.Fatalf("biweekly lesson got weekly slot %v", s)
		}
	}
	// The seminar for G1 (25) fits sem1 (30) and sem2 (40): full domain.
	if len(g.Domain[semC]) != positions {
		t.Errorf("seminar domain = %d", len(g.Domain[semC]))
	}
}

func TestGraphEmptyDomainWhenNoRoomFits(t *testing.T) {
	in := smallInput()
	in.Groups[1].Size = 100 // stream G1+G2 = 125 > hall 60
	g := NewGraph(mustProblem(t, in))
	if len(g.Domain[lecA]) != 0 {
		t.Fatalf("domain = %v, want empty", g.Domain[lecA])
	}
}

func TestGraphBuildTime(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test")
	}
	p := mustProblem(t, enginetest.RandomInput(rand.New(rand.NewPCG(1, 1)), 5000))
	start := time.Now()
	g := NewGraph(p)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("build took %v, want < 200ms", d)
	}
	if g.Edges() == 0 {
		t.Fatal("no edges")
	}
}

func BenchmarkGraph5k(b *testing.B) {
	p := mustProblem(b, enginetest.RandomInput(rand.New(rand.NewPCG(1, 1)), 5000))
	for b.Loop() {
		NewGraph(p)
	}
}
