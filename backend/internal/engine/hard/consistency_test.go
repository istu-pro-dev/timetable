package hard_test

import (
	"slices"

	"pgregory.net/rapid"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

// assertConsistent checks the violations reported for s (under the all-hard policy) against an
// independent reading of every rule, lesson by lesson:
//
//   - H1: some cell of the lesson's slot has TeacherLoad > 1;
//   - H2: some cell has UnitLoad > 1 for one of the lesson's units, and — brute force — another
//     placed lesson overlaps it in time and shares students;
//   - H3: the lesson has a room and some cell has RoomLoad > 1;
//   - H4: the teacher or the room is unavailable in one of its cells;
//   - H5/H6: the room has the wrong type / fewer seats than students;
//   - H7: the lesson is unplaced or has no room;
//   - H8: a sibling of the same curriculum item meets on the same day in a common week, or the
//     daily limit is exceeded for its teacher or one of its groups.
//
// It also checks structural invariants of every violation.
func assertConsistent(t *rapid.T, s *engine.Schedule, vs []hard.Violation) {
	p := s.P
	in := map[hard.Rule][]bool{}
	for _, r := range hard.Rules {
		in[r] = make([]bool, len(p.Lessons))
	}
	for _, v := range vs {
		checkViolationShape(t, s, v)
		for _, l := range v.Lessons {
			in[v.Rule][l] = true
		}
	}

	for li := range p.Lessons {
		l := int32(li)
		les := &p.Lessons[l]
		a := s.Assignment(l)
		want := map[hard.Rule]bool{hard.H7Completeness: !a.Placed || a.Room == engine.None}
		if a.Placed {
			cells := p.Grid.Cells(a.Slot)
			anyCell := func(f func(c int) bool) bool { return slices.ContainsFunc(cells, f) }
			want[hard.H1TeacherBusy] = anyCell(func(c int) bool { return s.TeacherLoad(les.Teacher, c) > 1 })
			want[hard.H2GroupBusy] = anyCell(func(c int) bool {
				return slices.ContainsFunc(les.Units, func(u int32) bool { return s.UnitLoad(u, c) > 1 })
			})
			if brute := sharesStudentsWithOther(s, l); brute != want[hard.H2GroupBusy] {
				t.Fatalf("lesson %d: UnitLoad says H2=%v, brute force says %v", l, want[hard.H2GroupBusy], brute)
			}
			want[hard.H4Availability] = anyCell(func(c int) bool { return p.TeacherAvail(les.Teacher, c) == engine.Unavailable })
			if a.Room != engine.None {
				room := &p.Rooms[a.Room]
				want[hard.H3RoomBusy] = anyCell(func(c int) bool { return s.RoomLoad(a.Room, c) > 1 })
				want[hard.H4Availability] = want[hard.H4Availability] ||
					anyCell(func(c int) bool { return p.RoomAvail(a.Room, c) == engine.Unavailable })
				want[hard.H5RoomType] = room.Type != les.RoomType
				want[hard.H6Capacity] = room.Capacity < les.Size
			}
			want[hard.H8SameDay] = sameDayViolated(s, l)
		}
		for _, r := range hard.Rules {
			if in[r][l] != want[r] {
				t.Fatalf("lesson %d (%+v): in %v violation = %v, want %v; violations %+v", l, a, r, in[r][l], want[r], vs)
			}
		}
	}
}

// checkViolationShape checks invariants every violation must satisfy.
func checkViolationShape(t *rapid.T, s *engine.Schedule, v hard.Violation) {
	p := s.P
	if !slices.IsSorted(v.Lessons) || len(slices.Compact(slices.Clone(v.Lessons))) != len(v.Lessons) || len(v.Lessons) == 0 {
		t.Fatalf("lessons not sorted and distinct: %+v", v)
	}
	if v.Strictness != hard.Hard || v.Message == "" {
		t.Fatalf("bad strictness or message: %+v", v)
	}
	switch v.Rule {
	case hard.H1TeacherBusy, hard.H2GroupBusy, hard.H3RoomBusy:
		if len(v.Lessons) < 2 {
			t.Fatalf("conflict with a single lesson: %+v", v)
		}
		for _, l := range v.Lessons {
			a := s.Assignment(l)
			les := &p.Lessons[l]
			sameTime := a.Placed && a.Slot.Day == v.Slot.Day && a.Slot.Period == v.Slot.Period
			var uses bool
			switch v.Rule {
			case hard.H1TeacherBusy:
				uses = v.Kind == hard.TeacherResource && les.Teacher == v.Resource
			case hard.H2GroupBusy:
				uses = v.Kind == hard.GroupResource &&
					slices.ContainsFunc(les.Units, func(u int32) bool { return p.Units[u].Group == v.Resource })
			default:
				uses = v.Kind == hard.RoomResource && a.Room == v.Resource
			}
			if !sameTime || !uses {
				t.Fatalf("lesson %d does not belong to %+v", l, v)
			}
			// Every lesson of a conflict meets at least one other one in a common week.
			if !slices.ContainsFunc(v.Lessons, func(o int32) bool { return o != l && s.Assignment(o).Slot.Overlaps(a.Slot) }) {
				t.Fatalf("lesson %d overlaps no other lesson of %+v", l, v)
			}
		}
	case hard.H4Availability, hard.H5RoomType, hard.H6Capacity, hard.H7Completeness:
		if len(v.Lessons) != 1 {
			t.Fatalf("per-lesson violation with %d lessons: %+v", len(v.Lessons), v)
		}
	}
}

// sharesStudentsWithOther reports, by brute force over all pairs, whether another placed lesson
// meets lesson l in a common week and shares at least one student with it.
func sharesStudentsWithOther(s *engine.Schedule, l int32) bool {
	p := s.P
	a := s.Assignment(l)
	for oi := range p.Lessons {
		o := int32(oi)
		b := s.Assignment(o)
		if o == l || !b.Placed || !a.Slot.Overlaps(b.Slot) {
			continue
		}
		for _, u := range p.Lessons[l].Units {
			if slices.ContainsFunc(p.Lessons[o].Units, func(w int32) bool { return unitsShareStudents(p.Units[u], p.Units[w]) }) {
				return true
			}
		}
	}
	return false
}

func unitsShareStudents(x, y engine.Unit) bool {
	if x.Group != y.Group {
		return false
	}
	if x.Division == engine.None || y.Division == engine.None || x.Division != y.Division {
		return true
	}
	return x.Part == y.Part
}

// sameDayViolated reports whether H8 applies to placed lesson l: a sibling of the same item
// meets on the same day in a common week, or its teacher or one of its groups has more than
// MaxLessonsPerDay lessons on that day in one of its weeks.
func sameDayViolated(s *engine.Schedule, l int32) bool {
	p := s.P
	a := s.Assignment(l)
	les := &p.Lessons[l]
	groups := lessonGroups(p, l)
	for oi := range p.Lessons {
		o := int32(oi)
		b := s.Assignment(o)
		if o != l && b.Placed && p.Lessons[o].Item == les.Item && b.Slot.Day == a.Slot.Day && b.Slot.Parity.Overlaps(a.Slot.Parity) {
			return true
		}
	}
	if p.MaxLessonsPerDay <= 0 {
		return false
	}
	for _, week := range []domain.Parity{domain.OddWeek, domain.EvenWeek} {
		if !a.Slot.Parity.Overlaps(week) {
			continue
		}
		teacher := 0
		group := map[int32]int{}
		for oi := range p.Lessons {
			b := s.Assignment(int32(oi))
			if !b.Placed || b.Slot.Day != a.Slot.Day || !b.Slot.Parity.Overlaps(week) {
				continue
			}
			if p.Lessons[oi].Teacher == les.Teacher {
				teacher++
			}
			for _, g := range lessonGroups(p, int32(oi)) {
				group[g]++
			}
		}
		if teacher > p.MaxLessonsPerDay || slices.ContainsFunc(groups, func(g int32) bool { return group[g] > p.MaxLessonsPerDay }) {
			return true
		}
	}
	return false
}

func lessonGroups(p *engine.Problem, l int32) []int32 {
	var out []int32
	for _, u := range p.Lessons[l].Units {
		if g := p.Units[u].Group; !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}
