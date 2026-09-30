package hard

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Lesson indices of the fixture.
const (
	lecture  int32 = iota // Иванов, stream G1+G2, lecture room, item 1
	seminar               // Петров, G1, seminar room, item 2
	lab1                  // Петров, G1 english/1, lab, biweekly, item 3
	lab2                  // Иванов, G1 english/2, lab, item 4
	seminar2              // Петров, G2, seminar room, item 2 (sibling of seminar by item)
	peLesson              // Сидоров, G1 pe/1, gym, item 5
)

// Room indices.
const (
	hall     int32 = iota // lecture, 90 seats
	semRoom               // seminar, 30 seats
	labRoom               // lab, 15 seats, closed Friday 1
	smallSem              // seminar, 10 seats
	gym                   // gym, 40 seats
)

func fixture(t *testing.T) *engine.Problem {
	t.Helper()
	in := engine.Input{
		Grid:      domain.Grid{Days: 6, PeriodsPerDay: 7},
		Buildings: []engine.Building{{ID: 1, Name: "A"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "A-100", Building: 1, Type: "lecture", Capacity: 90},
			{ID: 2, Name: "A-101", Building: 1, Type: "seminar", Capacity: 30},
			{ID: 3, Name: "A-102", Building: 1, Type: "lab", Capacity: 15,
				Avail: []engine.SlotAvailability{{Slot: slot(domain.Friday, 1), Status: engine.Unavailable}}},
			{ID: 4, Name: "A-103", Building: 1, Type: "seminar", Capacity: 10},
			{ID: 5, Name: "Спортзал", Building: 1, Type: "gym", Capacity: 40},
		},
		Teachers: []engine.TeacherInput{
			{ID: 1, Name: "Иванов И.И.", Avail: []engine.SlotAvailability{
				{Slot: slot(domain.Monday, 1), Status: engine.Unavailable},
				{Slot: slot(domain.Monday, 2), Status: engine.Undesired},
			}},
			{ID: 2, Name: "Петров П.П."},
			{ID: 3, Name: "Сидоров С.С."},
		},
		Groups: []engine.Group{{ID: 10, Name: "G1", Size: 25}, {ID: 20, Name: "G2", Size: 20}},
		Subgroups: []engine.SubgroupInput{
			{Group: 10, Division: "english", Part: 1, Size: 12},
			{Group: 10, Division: "english", Part: 2, Size: 13},
		},
		Disciplines: []engine.Discipline{{ID: 1, Name: "Математика"}, {ID: 2, Name: "Английский"}, {ID: 3, Name: "Физкультура"}},
		Lessons: []engine.LessonInput{
			{ID: 1, Item: 1, Discipline: 1, Kind: "lecture", Teacher: 1, RoomType: "lecture",
				Audience: domain.Audience{domain.WholeGroup(10), domain.WholeGroup(20)}},
			{ID: 2, Item: 2, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar",
				Audience: domain.Audience{domain.WholeGroup(10)}},
			{ID: 3, Item: 3, Discipline: 2, Kind: "lab", Teacher: 2, RoomType: "lab", Biweekly: true,
				Audience: domain.Audience{domain.Subgroup(10, "english", 1)}},
			{ID: 4, Item: 4, Discipline: 2, Kind: "lab", Teacher: 1, RoomType: "lab",
				Audience: domain.Audience{domain.Subgroup(10, "english", 2)}},
			{ID: 5, Item: 2, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar",
				Audience: domain.Audience{domain.WholeGroup(20)}},
			{ID: 6, Item: 5, Discipline: 3, Kind: "pe", Teacher: 3, RoomType: "gym",
				Audience: domain.Audience{domain.Subgroup(10, "pe", 1)}},
		},
	}
	p, err := engine.NewProblem(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func slot(d domain.Day, period uint8) domain.Slot { return domain.Slot{Day: d, Period: period} }

func odd(d domain.Day, period uint8) domain.Slot {
	return domain.Slot{Day: d, Period: period, Parity: domain.OddWeek}
}

// valid returns a schedule that satisfies every rule: all lessons on different days.
func valid(t *testing.T, p *engine.Problem) *engine.Schedule {
	t.Helper()
	s := engine.NewSchedule(p)
	place(t, s, lecture, slot(domain.Tuesday, 1), hall)
	place(t, s, seminar, slot(domain.Tuesday, 2), semRoom)
	place(t, s, lab1, odd(domain.Wednesday, 1), labRoom)
	place(t, s, lab2, slot(domain.Wednesday, 2), labRoom)
	place(t, s, seminar2, slot(domain.Thursday, 1), semRoom)
	place(t, s, peLesson, slot(domain.Friday, 3), gym)
	return s
}

func place(t *testing.T, s *engine.Schedule, l int32, sl domain.Slot, room int32) {
	t.Helper()
	if err := s.Place(l, sl, room); err != nil {
		t.Fatal(err)
	}
}

func rulesOf(vs []Violation) []Rule {
	out := make([]Rule, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func only(t *testing.T, vs []Violation, want Rule, lessons ...int32) Violation {
	t.Helper()
	if len(vs) != 1 || vs[0].Rule != want {
		t.Fatalf("violations = %v, want exactly one %v", describe(vs), want)
	}
	if !slices.Equal(vs[0].Lessons, lessons) {
		t.Fatalf("%v lessons = %v, want %v", want, vs[0].Lessons, lessons)
	}
	if vs[0].Message == "" {
		t.Fatalf("%v has empty message", want)
	}
	return vs[0]
}

func describe(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "%v %s; ", v.Rule, v.Message)
	}
	return b.String()
}

func TestValidScheduleHasNoViolations(t *testing.T) {
	s := valid(t, fixture(t))
	if vs := Check(s, nil); len(vs) != 0 {
		t.Fatalf("violations: %s", describe(vs))
	}
}

func TestH1TeacherBusy(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	// Иванов: lecture Tuesday 1 and lab2 moved there.
	place(t, s, lab2, slot(domain.Tuesday, 1), labRoom)
	vs := Check(s, nil)
	// lab2 (english/2) also collides with the stream lecture of G1 → H2 as well.
	h1 := only(t, filter(vs, H1TeacherBusy), H1TeacherBusy, lecture, lab2)
	if h1.Kind != TeacherResource || h1.Resource != 0 || h1.Slot != slot(domain.Tuesday, 1) {
		t.Fatalf("H1 = %+v", h1)
	}
	if !strings.Contains(h1.Message, "Иванов") || !strings.Contains(h1.Message, "вторник, 1-я пара") {
		t.Fatalf("message = %q", h1.Message)
	}

	// Odd and even weeks never meet: Петров with two biweekly-compatible lessons is fine.
	s = valid(t, p)
	place(t, s, lab1, odd(domain.Thursday, 1), labRoom) // Петров also has seminar2 Thursday 1 every week
	only(t, filter(Check(s, nil), H1TeacherBusy), H1TeacherBusy, lab1, seminar2)
}

func TestH2GroupBusy(t *testing.T) {
	p := fixture(t)

	t.Run("whole group vs stream", func(t *testing.T) {
		s := valid(t, p)
		place(t, s, seminar2, slot(domain.Tuesday, 1), semRoom) // G2 is in the Tuesday 1 stream lecture
		vs := Check(s, nil)
		v := only(t, filter(vs, H2GroupBusy), H2GroupBusy, lecture, seminar2)
		if v.Kind != GroupResource || p.Groups[v.Resource].Name != "G2" {
			t.Fatalf("H2 = %+v", v)
		}
	})

	t.Run("parallel subgroups are fine", func(t *testing.T) {
		s := valid(t, p)
		place(t, s, lab2, slot(domain.Wednesday, 1), smallSem) // english/2 parallel to english/1
		if vs := filter(Check(s, nil), H2GroupBusy); len(vs) != 0 {
			t.Fatalf("parallel subgroups flagged: %s", describe(vs))
		}
	})

	t.Run("cross divisions collide", func(t *testing.T) {
		s := valid(t, p)
		place(t, s, peLesson, slot(domain.Wednesday, 2), gym) // pe/1 vs english/2 lab
		only(t, filter(Check(s, nil), H2GroupBusy), H2GroupBusy, lab2, peLesson)
	})

	t.Run("subgroup vs whole group", func(t *testing.T) {
		s := valid(t, p)
		place(t, s, peLesson, slot(domain.Tuesday, 2), gym) // pe/1 vs G1 seminar
		only(t, filter(Check(s, nil), H2GroupBusy), H2GroupBusy, seminar, peLesson)
	})
}

func TestH3RoomBusy(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	place(t, s, seminar2, slot(domain.Tuesday, 2), semRoom) // same room as seminar
	vs := Check(s, nil)
	v := only(t, filter(vs, H3RoomBusy), H3RoomBusy, seminar, seminar2)
	if v.Kind != RoomResource || v.Resource != semRoom {
		t.Fatalf("H3 = %+v", v)
	}

	s = valid(t, p)
	place(t, s, seminar2, slot(domain.Tuesday, 2), smallSem) // different room: no H3
	if vs := filter(Check(s, nil), H3RoomBusy); len(vs) != 0 {
		t.Fatalf("different rooms flagged: %s", describe(vs))
	}
}

func TestH4Availability(t *testing.T) {
	p := fixture(t)

	s := valid(t, p)
	place(t, s, lecture, slot(domain.Monday, 1), hall)
	v := only(t, Check(s, nil), H4Availability, lecture)
	if v.Kind != TeacherResource {
		t.Fatalf("H4 = %+v", v)
	}

	s = valid(t, p)
	place(t, s, lab2, slot(domain.Friday, 1), labRoom)
	v = only(t, Check(s, nil), H4Availability, lab2)
	if v.Kind != RoomResource {
		t.Fatalf("H4 = %+v", v)
	}

	// "Undesired" is a preference, not a hard rule.
	s = valid(t, p)
	place(t, s, lecture, slot(domain.Monday, 2), hall)
	if vs := Check(s, nil); len(vs) != 0 {
		t.Fatalf("undesired slot flagged: %s", describe(vs))
	}
}

func TestH5RoomType(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	place(t, s, seminar, slot(domain.Tuesday, 2), hall) // seminar in the lecture hall
	v := only(t, Check(s, nil), H5RoomType, seminar)
	if !strings.Contains(v.Message, "lecture") || !strings.Contains(v.Message, "seminar") {
		t.Fatalf("message = %q", v.Message)
	}
}

func TestH6Capacity(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	place(t, s, seminar, slot(domain.Tuesday, 2), smallSem) // 25 students, 10 seats
	v := only(t, Check(s, nil), H6Capacity, seminar)
	if !strings.Contains(v.Message, "10") || !strings.Contains(v.Message, "25") {
		t.Fatalf("message = %q", v.Message)
	}
	// Subgroup size counts, not the group size: english/2 (13) fits a 15-seat lab.
	if vs := filter(Check(valid(t, p), nil), H6Capacity); len(vs) != 0 {
		t.Fatalf("subgroup lab flagged: %s", describe(vs))
	}
}

func TestH7Completeness(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	s.Unplace(seminar)
	v := only(t, Check(s, nil), H7Completeness, seminar)
	if v.Slot != (domain.Slot{}) {
		t.Fatalf("unplaced H7 slot = %v", v.Slot)
	}

	s = valid(t, p)
	place(t, s, seminar, slot(domain.Tuesday, 2), engine.None)
	v = only(t, Check(s, nil), H7Completeness, seminar)
	if !strings.Contains(v.Message, "аудитория") {
		t.Fatalf("message = %q", v.Message)
	}
}

func TestH8SameDay(t *testing.T) {
	p := fixture(t)

	s := valid(t, p)
	place(t, s, seminar2, slot(domain.Tuesday, 4), semRoom) // same item as seminar (Tuesday 2)
	v := only(t, Check(s, nil), H8SameDay, seminar, seminar2)
	if v.Slot.Day != domain.Tuesday {
		t.Fatalf("H8 = %+v", v)
	}

	// A different day is fine.
	s = valid(t, p)
	place(t, s, seminar2, slot(domain.Wednesday, 4), semRoom)
	if vs := Check(s, nil); len(vs) != 0 {
		t.Fatalf("different days flagged: %s", describe(vs))
	}
}

func TestH8MaxLessonsPerDay(t *testing.T) {
	p := fixture(t)
	p.MaxLessonsPerDay = 1
	s := valid(t, p)
	// Tuesday: G1 has lecture + seminar → 2 > 1. Wednesday: english/1 lab (odd) + english/2 lab
	// → 2 for G1 in the odd week. Петров: seminar Tuesday + lab1 Wednesday → 1 per day.
	vs := Check(s, nil)
	if got := rulesOf(vs); !slices.Equal(got, []Rule{H8SameDay, H8SameDay}) {
		t.Fatalf("violations = %s", describe(vs))
	}
	if vs[0].Kind != GroupResource || !strings.Contains(vs[0].Message, "G1") {
		t.Fatalf("first = %+v", vs[0])
	}

	p.MaxLessonsPerDay = 2
	if vs := Check(s, nil); len(vs) != 0 {
		t.Fatalf("limit 2 flagged: %s", describe(vs))
	}
}

func TestPolicy(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	place(t, s, seminar, slot(domain.Tuesday, 2), smallSem) // H6
	s.Unplace(peLesson)                                     // H7

	vs := Check(s, Policy{H6Capacity: HeavySoft, H7Completeness: Off})
	v := only(t, vs, H6Capacity, seminar)
	if v.Strictness != HeavySoft {
		t.Fatalf("strictness = %v", v.Strictness)
	}
	hardOnes, heavy := Split(vs)
	if len(hardOnes) != 0 || len(heavy) != 1 {
		t.Fatalf("split = %d hard, %d heavy", len(hardOnes), len(heavy))
	}

	hardOnes, heavy = Split(Check(s, nil))
	if len(hardOnes) != 2 || len(heavy) != 0 {
		t.Fatalf("default policy split = %d hard, %d heavy", len(hardOnes), len(heavy))
	}
}

func TestInvolving(t *testing.T) {
	p := fixture(t)
	s := valid(t, p)
	// Same teacher, room and item as seminar: H1, H3 and H8 for the pair.
	place(t, s, seminar2, slot(domain.Tuesday, 2), semRoom)
	s.Unplace(peLesson)
	vs := Check(s, nil)
	if got := rulesOf(Involving(vs, seminar2)); !slices.Equal(got, []Rule{H1TeacherBusy, H3RoomBusy, H8SameDay}) {
		t.Fatalf("involving seminar2 = %v (%s)", got, describe(vs))
	}
	if got := Involving(vs, peLesson, lecture); len(got) != 1 || got[0].Rule != H7Completeness {
		t.Fatalf("involving pe/lecture = %s", describe(got))
	}
	if got := Involving(vs, lab1); len(got) != 0 {
		t.Fatalf("involving lab1 = %s", describe(got))
	}
}

func TestStrings(t *testing.T) {
	if H1TeacherBusy.String() != "H1" || H8SameDay.String() != "H8" || Rule(42).String() != "Rule(42)" {
		t.Fatal("rule codes")
	}
	if len(Rules) != 8 {
		t.Fatal("rules list")
	}
	if describeSlot(domain.Slot{Day: domain.Monday, Period: 2, Parity: domain.EvenWeek}) != "понедельник, 2-я пара, знаменатель" {
		t.Fatal("describeSlot even")
	}
	if describeSlot(odd(domain.Friday, 1)) != "пятница, 1-я пара, числитель" {
		t.Fatal("describeSlot odd")
	}
	if dayName(domain.Day(9)) != "Day(9)" {
		t.Fatal("dayName fallback")
	}
}

func filter(vs []Violation, r Rule) []Violation {
	var out []Violation
	for _, v := range vs {
		if v.Rule == r {
			out = append(out, v)
		}
	}
	return out
}
