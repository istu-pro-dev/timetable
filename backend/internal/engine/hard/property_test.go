package hard_test

// Property-based invariants of the hard-constraint checker (arch §19): on random data the
// checker agrees with the occupancy counters and with a brute-force reading of the rules,
// schedules valid by construction never report violations, and a single injected conflict is
// always caught whatever surrounds it.

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/move"
)

const maxLessons = 30

// IDs of entities the tests add to a generated input; generated IDs are far below.
const (
	freshID    = 1000
	freshItem  = 1 << 40
	freshType  = "fresh"
	otherType  = "other"
	freshSeats = 10000
)

// TestPropCheckerMatchesCounters checks that on arbitrary schedules — full of conflicts,
// unplaced lessons and wrong rooms — each lesson takes part in a violation of a rule exactly
// when the occupancy counters or a brute-force reading of the rule say so.
func TestPropCheckerMatchesCounters(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		_, p := enginetest.DrawProblem(t, maxLessons)
		s := enginetest.DrawSchedule(t, p)
		assertConsistent(t, s, hard.Check(s, hard.Policy{}))
	})
}

// TestPropValidScheduleHasNoViolations checks that a schedule built to satisfy every rule is
// never reported, and that H7 fires exactly for the lessons left unplaced.
func TestPropValidScheduleHasNoViolations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		_, p := enginetest.DrawProblem(t, maxLessons)
		s := enginetest.DrawValidSchedule(t, p)
		if vs := hard.Check(s, hard.Policy{hard.H7Completeness: hard.Off}); len(vs) != 0 {
			t.Fatalf("valid schedule reported: %+v", vs)
		}
		vs := hard.Check(s, hard.Policy{})
		for _, v := range vs {
			if v.Rule != hard.H7Completeness || len(v.Lessons) != 1 || s.Assignment(v.Lessons[0]).Placed {
				t.Fatalf("unexpected violation %+v", v)
			}
		}
		unplaced := 0
		for l := range p.Lessons {
			if !s.Assignment(int32(l)).Placed {
				unplaced++
			}
		}
		if len(vs) != unplaced {
			t.Fatalf("%d H7 violations for %d unplaced lessons", len(vs), unplaced)
		}
		assertConsistent(t, s, vs)
	})
}

type conflictKind int

const (
	teacherConflict conflictKind = iota
	groupConflict
	roomConflict
)

var conflictRule = map[conflictKind]hard.Rule{
	teacherConflict: hard.H1TeacherBusy, groupConflict: hard.H2GroupBusy, roomConflict: hard.H3RoomBusy,
}

// TestPropInjectedConflictIsCaught adds a lesson X that shares exactly one resource (teacher,
// students or room) with a placed lesson Y of a valid schedule and puts X at Y's day and
// period with a random parity. The shared resource's rule must fire for X and Y exactly when
// the parities overlap; nothing that does not involve X may be reported. The same edit made as
// a dry-run move must report the same and leave the schedule untouched.
func TestPropInjectedConflictIsCaught(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in, s, y := drawValidWithAnchor(t)
		kind := conflictKind(rapid.IntRange(0, 2).Draw(t, "kind"))
		ya := s.Assignment(y)
		yl := s.P.Lessons[y]

		in.MaxLessonsPerDay = 0 // X adds to daily loads; H8 is not what is tested here
		in.Teachers = append(in.Teachers, engine.TeacherInput{ID: freshID, Name: "fresh"})
		in.Groups = append(in.Groups, engine.Group{ID: freshID, Name: "fresh", Size: 2})
		in.Rooms = append(in.Rooms, engine.RoomInput{ID: freshID, Name: "fresh", Building: in.Buildings[0].ID, Type: freshType, Capacity: freshSeats})
		x := engine.LessonInput{
			ID: freshID, Item: freshItem, Discipline: in.Disciplines[0].ID, Kind: "x",
			Teacher: freshID, Audience: domain.Audience{domain.WholeGroup(freshID)}, RoomType: freshType,
			Biweekly: rapid.Bool().Draw(t, "xBiweekly"),
		}
		switch kind {
		case teacherConflict:
			x.Teacher = s.P.Teachers[yl.Teacher].ID
		case groupConflict:
			x.Audience = domain.Audience{overlappingMember(t, in.Lessons[y].Audience)}
		case roomConflict:
			x.RoomType = yl.RoomType
		}
		in.Lessons = append(in.Lessons, x)
		p2 := mustProblem(t, in)
		base := replay(t, p2, s)
		xi := int32(len(p2.Lessons) - 1)
		room := int32(len(p2.Rooms) - 1)
		if kind == roomConflict {
			room = ya.Room
		}
		slot := domain.Slot{Day: ya.Slot.Day, Period: ya.Slot.Period}
		if x.Biweekly {
			slot.Parity = rapid.SampledFrom([]domain.Parity{domain.OddWeek, domain.EvenWeek}).Draw(t, "xParity")
		}

		pol := hard.Policy{hard.H7Completeness: hard.Off}
		ev := move.NewEvaluator(p2, pol)
		m := move.Relocate(base, xi, slot, room)
		before := base.Assignments()
		res, err := ev.Evaluate(base, m)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(before, base.Assignments()) {
			t.Fatal("dry run changed the schedule")
		}
		if vs := hard.Check(base, pol); len(vs) != 0 {
			t.Fatalf("dry run left violations behind: %+v", vs)
		}

		after := base.Clone()
		if _, err := move.Apply(after, m); err != nil {
			t.Fatal(err)
		}
		vs := hard.Check(after, pol)
		assertConsistent(t, after, hard.Check(after, hard.Policy{}))

		overlap := slot.Overlaps(ya.Slot)
		rule := conflictRule[kind]
		caught := slices.ContainsFunc(vs, func(v hard.Violation) bool {
			return v.Rule == rule && v.Involves(xi) && v.Involves(y)
		})
		if caught != overlap {
			t.Fatalf("%v between X %v and Y %v: caught=%v, parities overlap=%v; violations %+v", rule, slot, ya.Slot, caught, overlap, vs)
		}
		for _, v := range vs {
			if !v.Involves(xi) {
				t.Fatalf("violation not involving the injected lesson: %+v", v)
			}
			if !overlap && v.Involves(y) {
				t.Fatalf("Y reported although parities do not overlap: %+v", v)
			}
		}
		if cellsSubset(p2.Grid, slot, ya.Slot) && (len(vs) != 1 || vs[0].Rule != rule) {
			t.Fatalf("want exactly one %v violation, got %+v", rule, vs)
		}

		if !sameViolations(res.Violations, hard.Involving(vs, xi)) || !sameViolations(res.Introduced, res.Violations) || len(res.Resolved) != 0 {
			t.Fatalf("dry run disagrees with the applied move:\nevaluate %+v\nintroduced %+v\nresolved %+v\ncheck %+v",
				res.Violations, res.Introduced, res.Resolved, vs)
		}
	})
}

type lessonCorruption int

const (
	teacherUnavailable lessonCorruption = iota
	roomUnavailable
	wrongRoomType
	roomTooSmall
)

// TestPropLessonCorruptionIsCaught breaks one placed lesson of a valid schedule — its teacher or
// room becomes unavailable in one of its cells, or it moves to a free room of the wrong type or
// one seat too small — and expects exactly the matching violation for that lesson alone.
func TestPropLessonCorruptionIsCaught(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in, s, x := drawValidWithAnchor(t)
		a := s.Assignment(x)
		les := s.P.Lessons[x]
		cells := []domain.Slot{a.Slot}
		if a.Slot.Parity == domain.EveryWeek {
			cells = []domain.Slot{a.Slot, withParity(a.Slot, domain.OddWeek), withParity(a.Slot, domain.EvenWeek)}
		}
		blocked := engine.SlotAvailability{Slot: rapid.SampledFrom(cells).Draw(t, "blockedCell"), Status: engine.Unavailable}

		kind := lessonCorruption(rapid.IntRange(0, 3).Draw(t, "corruption"))
		want := hard.Violation{Rule: hard.H4Availability, Lessons: []int32{x}, Slot: a.Slot}
		room := a.Room
		switch kind {
		case teacherUnavailable:
			ti := s.P.Teachers[les.Teacher].ID
			i := slices.IndexFunc(in.Teachers, func(t engine.TeacherInput) bool { return t.ID == ti })
			in.Teachers[i].Avail = append(slices.Clone(in.Teachers[i].Avail), blocked)
			want.Kind, want.Resource = hard.TeacherResource, les.Teacher
		case roomUnavailable:
			in.Rooms[a.Room].Avail = append(slices.Clone(in.Rooms[a.Room].Avail), blocked)
			want.Kind, want.Resource = hard.RoomResource, a.Room
		case wrongRoomType, roomTooSmall:
			typ, seats := otherType, freshSeats
			want.Rule = hard.H5RoomType
			if kind == roomTooSmall {
				typ, seats = les.RoomType, les.Size-1
				want.Rule = hard.H6Capacity
			}
			in.Rooms = append(in.Rooms, engine.RoomInput{ID: freshID, Name: "fresh", Building: in.Buildings[0].ID, Type: typ, Capacity: seats})
			room = int32(len(in.Rooms) - 1)
			want.Kind, want.Resource = hard.RoomResource, room
		}
		p2 := mustProblem(t, in)
		s2 := replay(t, p2, s)
		if err := s2.Place(x, a.Slot, room); err != nil {
			t.Fatal(err)
		}

		vs := hard.Check(s2, hard.Policy{hard.H7Completeness: hard.Off})
		if len(vs) != 1 {
			t.Fatalf("want one %v violation, got %+v", want.Rule, vs)
		}
		got := vs[0]
		if got.Rule != want.Rule || !slices.Equal(got.Lessons, want.Lessons) || got.Kind != want.Kind ||
			got.Resource != want.Resource || got.Slot != want.Slot || got.Strictness != hard.Hard || got.Message == "" {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		assertConsistent(t, s2, hard.Check(s2, hard.Policy{}))
	})
}

// TestPropParity checks the parity algebra conflicts rely on: odd and even never meet, every
// week meets everything, and slots overlap only at the same day and period.
func TestPropParity(t *testing.T) {
	parity := rapid.SampledFrom([]domain.Parity{domain.EveryWeek, domain.OddWeek, domain.EvenWeek})
	rapid.Check(t, func(t *rapid.T) {
		p, q := parity.Draw(t, "p"), parity.Draw(t, "q")
		want := p == domain.EveryWeek || q == domain.EveryWeek || p == q
		if p.Overlaps(q) != want || q.Overlaps(p) != want {
			t.Fatalf("%v overlaps %v = %v, want %v", p, q, p.Overlaps(q), want)
		}
		g := domain.Grid{Days: 6, PeriodsPerDay: 8}
		a, b := enginetest.SlotGen(g, false).Draw(t, "a"), enginetest.SlotGen(g, false).Draw(t, "b")
		a.Parity, b.Parity = p, q
		same := a.Day == b.Day && a.Period == b.Period
		if a.Overlaps(b) != (same && want) {
			t.Fatalf("%v overlaps %v = %v", a, b, a.Overlaps(b))
		}
		shared := slices.ContainsFunc(g.Cells(a), func(c int) bool { return slices.Contains(g.Cells(b), c) })
		if shared != a.Overlaps(b) {
			t.Fatalf("%v and %v share a cell = %v, overlap = %v", a, b, shared, a.Overlaps(b))
		}
	})
}

// drawValidWithAnchor draws an input with a valid schedule and returns a placed lesson of it.
// If the greedy construction placed nothing, a lesson with its own teacher, group and room is
// added and placed.
func drawValidWithAnchor(t *rapid.T) (engine.Input, *engine.Schedule, int32) {
	in, p := enginetest.DrawProblem(t, maxLessons)
	s := enginetest.DrawValidSchedule(t, p)
	var placed []int32
	for l := range p.Lessons {
		if s.Assignment(int32(l)).Placed {
			placed = append(placed, int32(l))
		}
	}
	if len(placed) > 0 {
		return cloneInput(in), s, rapid.SampledFrom(placed).Draw(t, "anchor")
	}
	in = cloneInput(in)
	in.Teachers = append(in.Teachers, engine.TeacherInput{ID: freshID + 1, Name: "anchor"})
	in.Groups = append(in.Groups, engine.Group{ID: freshID + 1, Name: "anchor", Size: 2})
	in.Rooms = append(in.Rooms, engine.RoomInput{ID: freshID + 1, Name: "anchor", Building: in.Buildings[0].ID, Type: "anchor", Capacity: 30})
	in.Lessons = append(in.Lessons, engine.LessonInput{
		ID: freshID + 1, Item: freshItem + 1, Discipline: in.Disciplines[0].ID, Kind: "anchor", Teacher: freshID + 1,
		Audience: domain.Audience{domain.WholeGroup(freshID + 1)}, RoomType: "anchor", Biweekly: rapid.Bool().Draw(t, "anchorBiweekly"),
	})
	p = mustProblem(t, in)
	s = replay(t, p, s)
	y := int32(len(p.Lessons) - 1)
	if err := s.Place(y, enginetest.SlotGen(p.Grid, p.Lessons[y].Biweekly).Draw(t, "anchorSlot"), int32(len(p.Rooms)-1)); err != nil {
		t.Fatal(err)
	}
	return in, s, y
}

// overlappingMember returns an audience member that shares students with the audience.
func overlappingMember(t *rapid.T, aud domain.Audience) domain.Member {
	m := rapid.SampledFrom(aud).Draw(t, "sharedMember")
	divs := []string{"lang", "pe"}
	var out domain.Member
	switch k := rapid.IntRange(0, 2).Draw(t, "overlapWith"); {
	case k == 0:
		out = domain.WholeGroup(m.Group)
	case m.IsWhole():
		out = domain.Subgroup(m.Group, rapid.SampledFrom(divs).Draw(t, "division"), uint8(rapid.IntRange(1, 2).Draw(t, "part")))
	case k == 1:
		out = m
	default:
		other := divs[0]
		if m.Division == other {
			other = divs[1]
		}
		out = domain.Subgroup(m.Group, other, uint8(rapid.IntRange(1, 2).Draw(t, "part")))
	}
	if !aud.Overlaps(domain.Audience{out}) {
		t.Fatalf("%v does not overlap %v", out, aud)
	}
	return out
}

func withParity(s domain.Slot, p domain.Parity) domain.Slot {
	s.Parity = p
	return s
}

func cellsSubset(g domain.Grid, a, b domain.Slot) bool {
	cb := g.Cells(b)
	return a.Day == b.Day && a.Period == b.Period &&
		!slices.ContainsFunc(g.Cells(a), func(c int) bool { return !slices.Contains(cb, c) })
}

func mustProblem(t *rapid.T, in engine.Input) *engine.Problem {
	p, err := engine.NewProblem(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// replay copies the placements of s onto p, a problem built from the same input with entities
// appended at the end, so indices of existing lessons and rooms carry over.
func replay(t *rapid.T, p *engine.Problem, s *engine.Schedule) *engine.Schedule {
	out := engine.NewSchedule(p)
	for l, a := range s.Assignments() {
		if !a.Placed {
			continue
		}
		if err := out.Place(int32(l), a.Slot, a.Room); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func cloneInput(in engine.Input) engine.Input {
	in.Buildings = slices.Clone(in.Buildings)
	in.Rooms = slices.Clone(in.Rooms)
	in.Teachers = slices.Clone(in.Teachers)
	in.Groups = slices.Clone(in.Groups)
	in.Subgroups = slices.Clone(in.Subgroups)
	in.Disciplines = slices.Clone(in.Disciplines)
	in.Lessons = slices.Clone(in.Lessons)
	return in
}

func sameViolations(a, b []hard.Violation) bool {
	return slices.EqualFunc(a, b, func(x, y hard.Violation) bool {
		return x.Rule == y.Rule && x.Kind == y.Kind && x.Resource == y.Resource && x.Slot == y.Slot &&
			x.Strictness == y.Strictness && x.Message == y.Message && slices.Equal(x.Lessons, y.Lessons)
	})
}
