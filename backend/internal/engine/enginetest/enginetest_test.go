package enginetest

import (
	"math/rand/v2"
	"testing"

	"pgregory.net/rapid"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

func TestRandomInputIsValid(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		p, err := engine.NewProblem(RandomInput(r, 100))
		if err != nil {
			t.Fatal(err)
		}
		s := RandomSchedule(r, p)
		for l := range p.Lessons {
			if a := s.Assignment(int32(l)); a.Placed && !p.Grid.Contains(a.Slot) {
				t.Fatalf("lesson %d outside the grid: %v", l, a.Slot)
			}
		}
	}
}

// TestPropGenerators checks what the property tests rely on: generated inputs are accepted,
// audiences never overlap themselves, and in a valid schedule no teacher, room or audience
// unit of a placed lesson is booked twice.
func TestPropGenerators(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in, p := DrawProblem(t, 20)
		for _, l := range in.Lessons {
			for i, m := range l.Audience {
				for _, o := range l.Audience[i+1:] {
					if m.Overlaps(o) {
						t.Fatalf("lesson %d: members %v and %v overlap", l.ID, m, o)
					}
				}
			}
		}
		DrawSchedule(t, p)

		s := DrawValidSchedule(t, p)
		for c := range p.Grid.CellCount() {
			for ti := range p.Teachers {
				if s.TeacherLoad(int32(ti), c) > 1 {
					t.Fatalf("teacher %d double-booked in cell %d", ti, c)
				}
			}
			for ri := range p.Rooms {
				if s.RoomLoad(int32(ri), c) > 1 {
					t.Fatalf("room %d double-booked in cell %d", ri, c)
				}
			}
		}
		// UnitLoad counts a lesson once per member, so only the units of the lessons placed in a
		// cell are meaningful there (a lesson for both halves of a division counts twice towards
		// the whole group).
		for l := range p.Lessons {
			a := s.Assignment(int32(l))
			if !a.Placed {
				continue
			}
			for _, c := range p.Grid.Cells(a.Slot) {
				for _, u := range p.Lessons[l].Units {
					if s.UnitLoad(u, c) != 1 {
						t.Fatalf("lesson %d: unit %d has load %d in cell %d", l, u, s.UnitLoad(u, c), c)
					}
				}
			}
		}
	})
}
