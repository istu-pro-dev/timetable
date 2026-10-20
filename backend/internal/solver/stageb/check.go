package stageb

import (
	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

// checker counts hard-rule conflicts of single lessons from the schedule's occupancy
// counters, so a move is judged in O(lesson size) instead of re-running hard.Check on the
// whole schedule. Only rules with Hard strictness are counted: heavy-soft and disabled rules
// may be broken.
//
// local(l) counts, over l's cells, every other lesson it collides with (teacher, students,
// room), plus one per broken per-lesson rule (availability, room type, capacity, missing
// room) and per same-day rule (H8). It is zero exactly when hard.Check reports no Hard
// violation involving l.
type checker struct {
	p    *engine.Problem
	pol  hard.Policy
	on   [hard.H8SameDay + 1]bool
	sibs [][]int32 // lessons of the same curriculum item
	// Lessons of every teacher and group, for the daily load limit (H8).
	byTeacher [][]int32
	byGroup   [][]int32
	groupsOf  [][]int32 // distinct groups of every lesson
}

func newChecker(p *engine.Problem, pol hard.Policy, siblings [][]int32) *checker {
	c := &checker{
		p: p, pol: pol, sibs: siblings,
		byTeacher: make([][]int32, len(p.Teachers)), byGroup: make([][]int32, len(p.Groups)),
		groupsOf: make([][]int32, len(p.Lessons)),
	}
	for _, r := range hard.Rules {
		c.on[r] = pol.Of(r) == hard.Hard
	}
	for li := range p.Lessons {
		l := int32(li)
		les := &p.Lessons[l]
		c.byTeacher[les.Teacher] = append(c.byTeacher[les.Teacher], l)
		for _, u := range les.Units {
			g := p.Units[u].Group
			if !contains(c.groupsOf[l], g) {
				c.groupsOf[l] = append(c.groupsOf[l], g)
				c.byGroup[g] = append(c.byGroup[g], l)
			}
		}
	}
	return c
}

func contains(xs []int32, x int32) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// local returns the conflict count of lesson l in its current placement.
func (c *checker) local(s *engine.Schedule, l int32) int {
	a := s.Assignment(l)
	if !a.Placed {
		if c.on[hard.H7Completeness] {
			return 1
		}
		return 0
	}
	p := c.p
	les := &p.Lessons[l]
	n := 0
	for _, cell := range p.Grid.Cells(a.Slot) {
		if c.on[hard.H1TeacherBusy] {
			n += s.TeacherLoad(les.Teacher, cell) - 1
		}
		if c.on[hard.H2GroupBusy] {
			n += c.studentClashes(s, l, cell)
		}
		if a.Room != engine.None && c.on[hard.H3RoomBusy] {
			n += s.RoomLoad(a.Room, cell) - 1
		}
		if c.on[hard.H4Availability] {
			if p.TeacherAvail(les.Teacher, cell) == engine.Unavailable {
				n++
			}
			if a.Room != engine.None && p.RoomAvail(a.Room, cell) == engine.Unavailable {
				n++
			}
		}
	}
	if a.Room == engine.None {
		if c.on[hard.H7Completeness] {
			n++
		}
	} else {
		room := &p.Rooms[a.Room]
		if c.on[hard.H5RoomType] && room.Type != les.RoomType {
			n++
		}
		if c.on[hard.H6Capacity] && room.Capacity < les.Size {
			n++
		}
	}
	if c.on[hard.H8SameDay] {
		n += c.sameDay(s, l, a.Slot)
	}
	return n
}

// studentClashes returns how many other lessons share students with l in the cell. A lesson
// may list several units of one group (two subgroups of a stream lab); the maximum over its
// units counts each colliding lesson once in the common case.
func (c *checker) studentClashes(s *engine.Schedule, l int32, cell int) int {
	worst := 0
	for _, u := range c.p.Lessons[l].Units {
		worst = max(worst, s.UnitLoad(u, cell)-c.ownUnitLoad(l, u))
	}
	return worst
}

// ownUnitLoad is what lesson l itself contributes to UnitLoad(u) in each of its cells.
func (c *checker) ownUnitLoad(l, u int32) int {
	p := c.p
	unit := &p.Units[u]
	n := 0
	for _, v := range p.Lessons[l].Units {
		w := &p.Units[v]
		if w.Group != unit.Group {
			continue
		}
		switch {
		case unit.Division == engine.None:
			n++ // whole-group load counts every unit of the group
		case w.Division == engine.None:
			n++
		case w.Division != unit.Division:
			n++
		case v == u:
			n++
		}
	}
	return n
}

// sameDay counts H8 conflicts of l: siblings on the same day in an overlapping week, and
// days where l's teacher or groups exceed the daily limit.
func (c *checker) sameDay(s *engine.Schedule, l int32, slot domain.Slot) int {
	n := 0
	for _, v := range c.sibs[l] {
		if b := s.Assignment(v); b.Placed && b.Slot.Day == slot.Day && b.Slot.Parity.Overlaps(slot.Parity) {
			n++
		}
	}
	if limit := c.p.MaxLessonsPerDay; limit > 0 {
		les := &c.p.Lessons[l]
		for _, week := range weeks(slot.Parity) {
			if c.dayLoad(s, c.byTeacher[les.Teacher], slot.Day, week) > limit {
				n++
			}
			for _, g := range c.groupsOf[l] {
				if c.dayLoad(s, c.byGroup[g], slot.Day, week) > limit {
					n++
				}
			}
		}
	}
	return n
}

func (c *checker) dayLoad(s *engine.Schedule, lessons []int32, day domain.Day, week int) int {
	n := 0
	for _, l := range lessons {
		if a := s.Assignment(l); a.Placed && a.Slot.Day == day && covers(a.Slot.Parity, week) {
			n++
		}
	}
	return n
}

func weeks(p domain.Parity) []int {
	switch p {
	case domain.OddWeek:
		return []int{0}
	case domain.EvenWeek:
		return []int{1}
	default:
		return []int{0, 1}
	}
}

func covers(p domain.Parity, week int) bool {
	return p == domain.EveryWeek || (p == domain.OddWeek) == (week == 0)
}
