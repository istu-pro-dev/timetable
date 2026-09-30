package move

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
)

// Fixture: teacher T teaches lessons 0 (G1) and 1 (G2); lesson 2 is G1 with teacher U.
// Rooms: 0 and 1 (seminar, 30 seats).
func fixture(t *testing.T) (*engine.Problem, *engine.Schedule) {
	t.Helper()
	p, err := engine.NewProblem(engine.Input{
		Grid:      domain.Grid{Days: 6, PeriodsPerDay: 7},
		Buildings: []engine.Building{{ID: 1, Name: "A"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "101", Building: 1, Type: "seminar", Capacity: 30},
			{ID: 2, Name: "102", Building: 1, Type: "seminar", Capacity: 30},
		},
		Teachers:    []engine.TeacherInput{{ID: 1, Name: "T"}, {ID: 2, Name: "U"}},
		Groups:      []engine.Group{{ID: 1, Name: "G1", Size: 20}, {ID: 2, Name: "G2", Size: 20}},
		Disciplines: []engine.Discipline{{ID: 1, Name: "D"}},
		Lessons: []engine.LessonInput{
			{ID: 10, Item: 1, Discipline: 1, Kind: "seminar", Teacher: 1, RoomType: "seminar", Audience: domain.Audience{domain.WholeGroup(1)}},
			{ID: 11, Item: 2, Discipline: 1, Kind: "seminar", Teacher: 1, RoomType: "seminar", Audience: domain.Audience{domain.WholeGroup(2)}},
			{ID: 12, Item: 3, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar", Audience: domain.Audience{domain.WholeGroup(1)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := engine.NewSchedule(p)
	for l, sl := range []domain.Slot{slot(domain.Monday, 2), slot(domain.Monday, 3), slot(domain.Tuesday, 2)} {
		if err := s.Place(int32(l), sl, int32(l%2)); err != nil {
			t.Fatal(err)
		}
	}
	return p, s
}

func slot(d domain.Day, period uint8) domain.Slot { return domain.Slot{Day: d, Period: period} }

func TestEvaluateIsDryRun(t *testing.T) {
	p, s := fixture(t)
	before := s.Assignments()
	e := NewEvaluator(p, nil)

	res, err := e.Evaluate(s, Relocate(s, 1, slot(domain.Monday, 2), 1)) // T busy with lesson 0
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Assignments(), before) {
		t.Fatal("Evaluate changed the schedule")
	}
	if res.HardCount() != 1 || res.Violations[0].Rule != hard.H1TeacherBusy {
		t.Fatalf("violations = %+v", res.Violations)
	}
	if len(res.Introduced) != 1 || len(res.Resolved) != 0 {
		t.Fatalf("introduced %d, resolved %d", len(res.Introduced), len(res.Resolved))
	}
	// Check that occupancy was restored too, not only the assignments.
	if vs := hard.Check(s, nil); len(vs) != 0 {
		t.Fatalf("schedule dirty after dry run: %+v", vs)
	}
}

func TestEvaluateResolves(t *testing.T) {
	p, s := fixture(t)
	e := NewEvaluator(p, nil)
	mustApply(t, s, Relocate(s, 1, slot(domain.Monday, 2), 1)) // create the conflict
	res, err := e.Evaluate(s, Relocate(s, 1, slot(domain.Monday, 4), 1))
	if err != nil {
		t.Fatal(err)
	}
	if res.HardCount() != 0 || len(res.Resolved) != 1 || len(res.Introduced) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSoftDeltaMatchesFull(t *testing.T) {
	p, s := fixture(t)
	e := NewEvaluator(p, nil)
	m := Relocate(s, 1, slot(domain.Monday, 6), 1) // opens a gap for T on Monday
	res, err := e.Evaluate(s, m)
	if err != nil {
		t.Fatal(err)
	}
	full0 := e.Soft.Full(s)
	mustApply(t, s, m)
	full1 := e.Soft.Full(s)
	if got, want := res.SoftDelta(), full1.Sub(full0); got != want {
		t.Fatalf("delta %v, want %v", got, want)
	}
	if res.SoftDelta()[soft.Gaps] <= 0 {
		t.Fatalf("expected more gaps, delta = %v", res.SoftDelta())
	}
}

func TestSwapIsAtomic(t *testing.T) {
	p, s := fixture(t)
	e := NewEvaluator(p, nil)
	// Lessons 0 and 1 share teacher T: moving either alone onto the other's slot conflicts,
	// swapping them as one move does not.
	res, err := e.Evaluate(s, Swap(s, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 0 {
		t.Fatalf("swap violations = %+v", res.Violations)
	}
	mustApply(t, s, Swap(s, 0, 1))
	if s.Assignment(0).Slot != slot(domain.Monday, 3) || s.Assignment(1).Slot != slot(domain.Monday, 2) {
		t.Fatal("swap not applied")
	}
	if s.Assignment(0).Room != 1 || s.Assignment(1).Room != 0 {
		t.Fatal("rooms not swapped")
	}
}

func TestSwapWithUnplaced(t *testing.T) {
	_, s := fixture(t)
	mustApply(t, s, Unplace(s, 2))
	mustApply(t, s, Swap(s, 0, 2))
	if s.Assignment(0).Placed || !s.Assignment(2).Placed || s.Assignment(2).Slot != slot(domain.Monday, 2) {
		t.Fatalf("assignments = %+v", s.Assignments())
	}
}

func TestChangeRoomPinAndThen(t *testing.T) {
	_, s := fixture(t)
	mustApply(t, s, ChangeRoom(s, 0, 1).Then(Pin(s, 0, true)))
	// Then: the later change (pin, built from the old assignment with room 0) wins.
	a := s.Assignment(0)
	if !a.Pinned || a.Room != 0 {
		t.Fatalf("assignment = %+v", a)
	}
	mustApply(t, s, ChangeRoom(s, 0, 1))
	if a := s.Assignment(0); a.Room != 1 || !a.Pinned || a.Slot != slot(domain.Monday, 2) {
		t.Fatalf("after room change = %+v", a)
	}
	mustApply(t, s, Relocate(s, 0, slot(domain.Friday, 1), engine.None))
	if !s.Assignment(0).Pinned {
		t.Fatal("relocate dropped pinned flag")
	}
	if got := ChangeRoom(s, 0, 1).Then(Pin(s, 1, true)).Lessons(); !slices.Equal(got, []int32{0, 1}) {
		t.Fatalf("Lessons() = %v", got)
	}
}

func TestValidation(t *testing.T) {
	p, s := fixture(t)
	e := NewEvaluator(p, nil)
	before := s.Assignments()
	tests := []struct {
		name string
		m    Move
		want error
	}{
		{"empty", Move{}, ErrEmptyMove},
		{"unknown lesson", Move{Changes: []Change{{Lesson: 9}}}, ErrUnknownLesson},
		{"negative lesson", Move{Changes: []Change{{Lesson: -1}}}, ErrUnknownLesson},
		{"outside grid", Relocate(s, 0, slot(domain.Sunday, 1), 0), engine.ErrSlotOutOfGrid},
		// A valid first change must not be applied when a later one is invalid.
		{"partially invalid batch", Relocate(s, 0, slot(domain.Friday, 1), 0).Then(Relocate(s, 1, slot(domain.Monday, 9), 0)), engine.ErrSlotOutOfGrid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.Evaluate(s, tt.m); !errors.Is(err, tt.want) {
				t.Fatalf("Evaluate err = %v, want %v", err, tt.want)
			}
			if _, err := Apply(s, tt.m); !errors.Is(err, tt.want) {
				t.Fatalf("Apply err = %v, want %v", err, tt.want)
			}
			if !slices.Equal(s.Assignments(), before) {
				t.Fatal("invalid move changed the schedule")
			}
		})
	}
}

func TestHeavySoftNotCountedAsHard(t *testing.T) {
	p, s := fixture(t)
	e := NewEvaluator(p, hard.Policy{hard.H1TeacherBusy: hard.HeavySoft})
	res, err := e.Evaluate(s, Relocate(s, 1, slot(domain.Monday, 2), 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 1 || res.HardCount() != 0 {
		t.Fatalf("violations = %+v", res.Violations)
	}
}

// Property: applying a random move and then its undo restores the schedule exactly, and
// Evaluate never changes it.
func TestApplyUndoRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 8))
	p, err := engine.NewProblem(enginetest.RandomInput(r, 200))
	if err != nil {
		t.Fatal(err)
	}
	s := enginetest.RandomSchedule(r, p)
	e := NewEvaluator(p, nil)
	for range 300 {
		var m Move
		for range 1 + r.IntN(3) {
			l := int32(r.IntN(len(p.Lessons)))
			switch r.IntN(4) {
			case 0:
				m = m.Then(Swap(s, l, int32(r.IntN(len(p.Lessons)))))
			case 1:
				m = m.Then(Unplace(s, l))
			default:
				m = m.Then(Relocate(s, l, enginetest.RandomSlot(r, p.Lessons[l].Biweekly), enginetest.RandomRoom(r, p, l)))
			}
		}
		snapshot := s.Clone()
		if _, err := e.Evaluate(s, m); err != nil {
			// e.g. swapping a weekly and a biweekly lesson: parities do not fit.
			if !errors.Is(err, engine.ErrParityMismatch) {
				t.Fatal(err)
			}
			if _, err := Apply(s, m); err == nil {
				t.Fatal("Apply accepted a move Evaluate rejected")
			}
			assertSame(t, s, snapshot)
			continue
		}
		assertSame(t, s, snapshot)
		undo := mustApply(t, s, m)
		mustApply(t, s, undo)
		assertSame(t, s, snapshot)
		mustApply(t, s, m) // keep evolving
	}
}

func assertSame(t *testing.T, a, b *engine.Schedule) {
	t.Helper()
	if !slices.Equal(a.Assignments(), b.Assignments()) {
		t.Fatal("assignments differ")
	}
	for tc := range a.P.Teachers {
		for c := range a.P.Grid.CellCount() {
			if a.TeacherLoad(int32(tc), c) != b.TeacherLoad(int32(tc), c) {
				t.Fatal("occupancy differs")
			}
		}
	}
}

func mustApply(t *testing.T, s *engine.Schedule, m Move) Move {
	t.Helper()
	undo, err := Apply(s, m)
	if err != nil {
		t.Fatal(err)
	}
	return undo
}
