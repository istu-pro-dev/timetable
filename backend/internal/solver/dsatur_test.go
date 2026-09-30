package solver

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

// placedValid asserts that every placed lesson satisfies all hard rules; unplaced lessons
// may only produce H7.
func placedValid(t *testing.T, s *engine.Schedule) {
	t.Helper()
	for _, v := range hard.Check(s, nil) {
		if v.Rule != hard.H7Completeness {
			t.Fatalf("%v: %s", v.Rule, v.Message)
		}
		if s.Assignment(v.Lessons[0]).Placed {
			t.Fatalf("H7 on a placed lesson: %s", v.Message)
		}
	}
}

func TestDSaturSmall(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := engine.NewSchedule(p)
	unplaced := DSatur(NewGraph(p), s, 1)
	if len(unplaced) != 0 {
		t.Fatalf("unplaced = %v", unplaced)
	}
	if vs := hard.Check(s, nil); len(vs) != 0 {
		t.Fatalf("violations: %+v", vs)
	}
}

func TestDSaturKeepsPinned(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := engine.NewSchedule(p)
	fixed := domain.Slot{Day: domain.Wednesday, Period: 2}
	if err := s.Place(semC, fixed, 1); err != nil {
		t.Fatal(err)
	}
	s.SetPinned(semC, true)
	if unplaced := DSatur(NewGraph(p), s, 3); len(unplaced) != 0 {
		t.Fatalf("unplaced = %v", unplaced)
	}
	if a := s.Assignment(semC); a.Slot != fixed || a.Room != 1 || !a.Pinned {
		t.Fatalf("pinned lesson moved: %+v", a)
	}
	placedValid(t, s)
}

func TestDSaturReportsImpossible(t *testing.T) {
	in := smallInput()
	in.Groups[1].Size = 100 // G2 fits neither the hall (stream) nor a seminar room
	p := mustProblem(t, in)
	s := engine.NewSchedule(p)
	if got := DSatur(NewGraph(p), s, 1); !slices.Equal(got, []int32{lecA, semB}) {
		t.Fatalf("unplaced = %v, want [lecA semB]", got)
	}
	placedValid(t, s)
}

func TestDSaturRandomInstances(t *testing.T) {
	for seed := range uint64(5) {
		r := rand.New(rand.NewPCG(seed, 99))
		p := mustProblem(t, enginetest.RandomInput(r, 400))
		s := engine.NewSchedule(p)
		unplaced := DSatur(NewGraph(p), s, seed)
		placedValid(t, s)
		if len(unplaced) > len(p.Lessons)/10 {
			t.Errorf("seed %d: %d of %d lessons unplaced", seed, len(unplaced), len(p.Lessons))
		}
	}
}

func TestDSaturDeterministicAndDiverse(t *testing.T) {
	p := mustProblem(t, enginetest.RandomInput(rand.New(rand.NewPCG(4, 4)), 300))
	g := NewGraph(p)
	run := func(seed uint64) []engine.Assignment {
		s := engine.NewSchedule(p)
		DSatur(g, s, seed)
		return s.Assignments()
	}
	a1, a2, b := run(7), run(7), run(8)
	if !slices.Equal(a1, a2) {
		t.Fatal("same seed gave different schedules")
	}
	differ := 0
	for i := range a1 {
		if a1[i].Slot != b[i].Slot {
			differ++
		}
	}
	if differ < len(a1)/2 {
		t.Fatalf("different seeds differ in only %d of %d lessons", differ, len(a1))
	}
}

// Saturation counters return to zero when every lesson is removed again.
func TestColouringCountersRoundTrip(t *testing.T) {
	p := mustProblem(t, enginetest.RandomInput(rand.New(rand.NewPCG(2, 3)), 200))
	g := NewGraph(p)
	s := engine.NewSchedule(p)
	c := newColouring(g, s, rand.New(rand.NewPCG(1, 1)))
	c.dsatur()
	for l := range p.Lessons {
		c.unplace(int32(l))
	}
	c.unplace(0) // no-op on an unplaced lesson
	for l := range p.Lessons {
		if c.sat[l] != 0 || slices.ContainsFunc(c.blocked[l], func(b uint16) bool { return b != 0 }) {
			t.Fatalf("lesson %d: sat %d blocked %v", l, c.sat[l], c.blocked[l])
		}
	}
}

func TestDSaturSpeed(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test")
	}
	p := mustProblem(t, enginetest.RandomInput(rand.New(rand.NewPCG(1, 2)), 2000))
	start := time.Now()
	DSatur(NewGraph(p), engine.NewSchedule(p), 1)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("2000 lessons took %v", d)
	}
}

func BenchmarkDSatur2k(b *testing.B) {
	p := mustProblem(b, enginetest.RandomInput(rand.New(rand.NewPCG(1, 2)), 2000))
	g := NewGraph(p)
	for b.Loop() {
		DSatur(g, engine.NewSchedule(p), 1)
	}
}
