package soft

import (
	"math/rand/v2"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
)

// Fixture: one group G (30 students, english 1/2), teachers T1 (undesired Friday 7) and T2,
// rooms in building A (hall 100 seats, sem 30) and B (sem 30, lab 15).
const (
	hallA int32 = iota
	semA
	semB
	labB
)

func fixture(t *testing.T, lessons ...engine.LessonInput) *engine.Problem {
	t.Helper()
	in := engine.Input{
		Grid:      domain.Grid{Days: 6, PeriodsPerDay: 7},
		Buildings: []engine.Building{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "A-hall", Building: 1, Type: "lecture", Capacity: 100},
			{ID: 2, Name: "A-sem", Building: 1, Type: "seminar", Capacity: 30},
			{ID: 3, Name: "B-sem", Building: 2, Type: "seminar", Capacity: 30},
			{ID: 4, Name: "B-lab", Building: 2, Type: "lab", Capacity: 15,
				Avail: []engine.SlotAvailability{{Slot: slot(domain.Monday, 3), Status: engine.Undesired}}},
		},
		Teachers: []engine.TeacherInput{
			{ID: 1, Name: "T1", Avail: []engine.SlotAvailability{{Slot: slot(domain.Friday, 7), Status: engine.Undesired}}},
			{ID: 2, Name: "T2"},
		},
		Groups:      []engine.Group{{ID: 1, Name: "G", Size: 30}},
		Disciplines: []engine.Discipline{{ID: 1, Name: "D"}},
		Lessons:     lessons,
	}
	p, err := engine.NewProblem(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func lesson(id int64, item int64, teacher domain.TeacherID, roomType string, aud ...domain.Member) engine.LessonInput {
	if len(aud) == 0 {
		aud = domain.Audience{domain.WholeGroup(1)}
	}
	return engine.LessonInput{
		ID: domain.LessonID(id), Item: item, Discipline: 1, Kind: roomType, Teacher: teacher, RoomType: roomType,
		Audience: aud,
	}
}

func slot(d domain.Day, period uint8) domain.Slot { return domain.Slot{Day: d, Period: period} }

func place(t *testing.T, s *engine.Schedule, l int32, sl domain.Slot, room int32) {
	t.Helper()
	if err := s.Place(l, sl, room); err != nil {
		t.Fatal(err)
	}
}

// check compares the full breakdown with expected non-zero criteria.
func check(t *testing.T, s *engine.Schedule, want map[Criterion]int) {
	t.Helper()
	got := New(s.P).Full(s)
	for c := range NumCriteria {
		if got[c] != want[c] {
			t.Errorf("%s = %d, want %d", c.Key(), got[c], want[c])
		}
	}
}

func TestGapsAndCompactness(t *testing.T) {
	// Group G: periods 2 and 5 on Tuesday → 2 gaps for the group; T1 teaches both → 2 gaps;
	// both lessons isolated → 2 curriculum-compactness points (every week → counted per week).
	p := fixture(t, lesson(1, 1, 1, "seminar"), lesson(2, 2, 1, "seminar"))
	s := engine.NewSchedule(p)
	place(t, s, 0, slot(domain.Tuesday, 2), semA)
	place(t, s, 1, slot(domain.Tuesday, 5), semA)
	check(t, s, map[Criterion]int{
		Gaps:                  (2 + 2) * 2, // group + teacher, odd + even week
		CurriculumCompactness: 2 * 2,
		TeacherCompactness:    1 * 2, // one teaching day per week
		RoomCount:             1,
	})

	// Adjacent lessons: no gaps, not isolated.
	place(t, s, 1, slot(domain.Tuesday, 3), semA)
	check(t, s, map[Criterion]int{TeacherCompactness: 2, RoomCount: 1})
}

func TestBuildingTransitions(t *testing.T) {
	// G: A (1) → B (2) → A (3): two transitions for the group; T1 teaches 1 and 3 only:
	// A → A, no transition, but a gap.
	p := fixture(t, lesson(1, 1, 1, "seminar"), lesson(2, 2, 2, "seminar"), lesson(3, 3, 1, "seminar"))
	s := engine.NewSchedule(p)
	place(t, s, 0, slot(domain.Monday, 1), semA)
	place(t, s, 1, slot(domain.Monday, 2), semB)
	place(t, s, 2, slot(domain.Monday, 3), semA)
	check(t, s, map[Criterion]int{
		BuildingTransitions: 2 * 2,
		Gaps:                1 * 2, // T1 idle in period 2
		TeacherCompactness:  2 * 2, // T1 and T2 each teach one day per week
		EdgeSlots:           1,     // period 1
		RoomCount:           2,
	})
}

func TestParityWeeks(t *testing.T) {
	// A biweekly lesson (odd) and a weekly one on the same day: gaps only in the odd week.
	bi := lesson(2, 2, 1, "seminar")
	bi.Biweekly = true
	p := fixture(t, lesson(1, 1, 1, "seminar"), bi)
	s := engine.NewSchedule(p)
	place(t, s, 0, slot(domain.Tuesday, 2), semA)
	place(t, s, 1, domain.Slot{Day: domain.Tuesday, Period: 4, Parity: domain.OddWeek}, semA)
	check(t, s, map[Criterion]int{
		Gaps:                  1 + 1, // group + teacher, odd week only
		CurriculumCompactness: 2,     // both isolated in the odd week
		TeacherCompactness:    2,
		RoomCount:             1,
	})
}

func TestMinWorkingDaysAndRoomStability(t *testing.T) {
	// Item 1 has three lessons on two days in two rooms.
	p := fixture(t, lesson(1, 1, 1, "seminar"), lesson(2, 1, 1, "seminar"), lesson(3, 1, 1, "seminar"))
	s := engine.NewSchedule(p)
	place(t, s, 0, slot(domain.Monday, 2), semA)
	place(t, s, 1, slot(domain.Monday, 3), semB)
	place(t, s, 2, slot(domain.Wednesday, 2), semA)
	check(t, s, map[Criterion]int{
		MinWorkingDays:      1, // 3 lessons, 2 days
		RoomStability:       1, // 2 rooms
		BuildingTransitions: 2 * 2,
		TeacherCompactness:  2 * 2,
		RoomCount:           2,
	})
}

func TestLessonCriteria(t *testing.T) {
	// Lecture of 30 in a 100-seat hall: 70 empty → 7. Friday 7 is undesired for T1 and last.
	// Subgroup lab (english/1 has no size data → group size 30) in the undesired lab slot.
	p := fixture(t, lesson(1, 1, 1, "lecture"), lesson(2, 2, 2, "lab", domain.Subgroup(1, "english", 1)))
	s := engine.NewSchedule(p)
	place(t, s, 0, slot(domain.Friday, 7), hallA)
	place(t, s, 1, slot(domain.Monday, 3), labB)
	check(t, s, map[Criterion]int{
		CapacityWaste:      7,
		EdgeSlots:          1,
		Preferences:        2,
		TeacherCompactness: 2 * 2,
		RoomCount:          2,
	})

	// Unplaced lessons and lessons without a room contribute nothing lesson-level.
	s.Unplace(0)
	place(t, s, 1, slot(domain.Tuesday, 3), engine.None)
	check(t, s, map[Criterion]int{TeacherCompactness: 2})
}

func TestBreakdownArithmetic(t *testing.T) {
	var a, b Breakdown
	a[Gaps], a[EdgeSlots] = 3, 1
	b[Gaps] = 1
	if got := a.Add(b)[Gaps]; got != 4 {
		t.Fatalf("Add = %d", got)
	}
	if got := a.Sub(b)[Gaps]; got != 2 {
		t.Fatalf("Sub = %d", got)
	}
	w := DefaultWeights()
	if got := a.Total(w); got != 3*5+1*2 {
		t.Fatalf("Total = %v", got)
	}
	weighted := a.Weighted(w)
	var sum float64
	for _, v := range weighted {
		sum += v
	}
	if sum != a.Total(w) {
		t.Fatalf("weighted sum %v != total %v", sum, a.Total(w))
	}
}

func TestCriterionKeys(t *testing.T) {
	for c := range NumCriteria {
		got, ok := ParseCriterion(c.Key())
		if !ok || got != c {
			t.Fatalf("round trip %v", c)
		}
	}
	if _, ok := ParseCriterion("nope"); ok {
		t.Fatal("unknown key parsed")
	}
	if NumCriteria.Key() != "criterion_10" {
		t.Fatal("fallback key")
	}
}

// The core property (issue #27): measuring only the scopes a move touches gives exactly the
// change of the full penalty.
func TestIncrementalMatchesFull(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	p, err := engine.NewProblem(enginetest.RandomInput(r, 150))
	if err != nil {
		t.Fatal(err)
	}
	e := New(p)
	s := enginetest.RandomSchedule(r, p)
	full := e.Full(s)

	for i := range 10000 {
		// Move one, two (swap-like) or three lessons at once.
		n := 1 + r.IntN(3)
		type change struct {
			l  int32
			to engine.Assignment
		}
		changes := make([]change, n)
		var ss ScopeSet
		for j := range changes {
			l := int32(r.IntN(len(p.Lessons)))
			to := engine.Assignment{Room: engine.None}
			if r.IntN(10) != 0 {
				to = engine.Assignment{
					Placed: true,
					Slot:   enginetest.RandomSlot(r, p.Lessons[l].Biweekly),
					Room:   enginetest.RandomRoom(r, p, l),
				}
			}
			changes[j] = change{l, to}
			e.AddLesson(&ss, l, s.Assignment(l))
			e.AddLesson(&ss, l, to)
		}

		before := e.Measure(s, &ss)
		for _, c := range changes {
			if !c.to.Placed {
				s.Unplace(c.l)
			} else if err := s.Place(c.l, c.to.Slot, c.to.Room); err != nil {
				t.Fatal(err)
			}
		}
		after := e.Measure(s, &ss)

		newFull := e.Full(s)
		if predicted := full.Add(after.Sub(before)); predicted != newFull {
			t.Fatalf("move %d: incremental %v != full %v", i, predicted, newFull)
		}
		full = newFull
	}
}
