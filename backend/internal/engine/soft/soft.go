// Package soft computes soft-constraint penalties (research §3) with a per-criterion breakdown.
//
// The penalty is a sum over small independent scopes — one group's day, one teacher's day or
// week, one curriculum item, one lesson, one room. A move only changes the scopes its lessons
// touch before and after the move, so its delta is measured on those scopes alone instead of
// recomputing the whole schedule.
package soft

import (
	"fmt"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Criterion identifies a soft criterion.
type Criterion uint8

// Soft criteria. The raw value of each is a count; see the comments for the unit.
const (
	Gaps                  Criterion = iota // empty periods between lessons of a group or teacher in a day
	BuildingTransitions                    // building changes between consecutive lessons in a day
	TeacherCompactness                     // teaching days per teacher and week (fewer = more free days)
	MinWorkingDays                         // missing distinct days for lessons of one curriculum item
	CurriculumCompactness                  // isolated lessons of a group in a day with other lessons
	RoomStability                          // extra rooms used by lessons of one curriculum item
	CapacityWaste                          // empty seats in assigned rooms, in tens of seats
	EdgeSlots                              // lessons in the first or last period of the day
	RoomCount                              // rooms used at least once
	Preferences                            // lessons in undesired slots of a teacher or room
	NumCriteria
)

var criterionKeys = [NumCriteria]string{
	"gaps", "corpus_transitions", "teacher_compactness", "min_working_days", "curriculum_compactness",
	"room_stability", "room_capacity_waste", "edge_slots", "room_count", "teacher_preferences",
}

// Key returns the stable identifier used in JSON configuration (arch §14.2).
func (c Criterion) Key() string {
	if c < NumCriteria {
		return criterionKeys[c]
	}
	return fmt.Sprintf("criterion_%d", c)
}

// ParseCriterion is the inverse of Key.
func ParseCriterion(key string) (Criterion, bool) {
	i := slices.Index(criterionKeys[:], key)
	return Criterion(i), i >= 0
}

// Weights holds one multiplier per criterion.
type Weights [NumCriteria]float64

// DefaultWeights is a reasonable starting point: the "important" tier (5) for gaps and building
// transitions, "desirable" (2) for most others and "not important" (0) for the global room count.
func DefaultWeights() Weights {
	var w Weights
	w[Gaps] = 5
	w[BuildingTransitions] = 5
	w[TeacherCompactness] = 1
	w[MinWorkingDays] = 2
	w[CurriculumCompactness] = 2
	w[RoomStability] = 1
	w[CapacityWaste] = 1
	w[EdgeSlots] = 2
	w[RoomCount] = 0
	w[Preferences] = 5
	return w
}

// Breakdown holds raw criterion values.
type Breakdown [NumCriteria]int

// Add returns b + o.
func (b Breakdown) Add(o Breakdown) Breakdown {
	for i := range b {
		b[i] += o[i]
	}
	return b
}

// Sub returns b - o.
func (b Breakdown) Sub(o Breakdown) Breakdown {
	for i := range b {
		b[i] -= o[i]
	}
	return b
}

// Total returns the weighted sum.
func (b Breakdown) Total(w Weights) float64 {
	var t float64
	for i, v := range b {
		t += w[i] * float64(v)
	}
	return t
}

// Weighted returns the weighted value of every criterion.
func (b Breakdown) Weighted(w Weights) [NumCriteria]float64 {
	var out [NumCriteria]float64
	for i, v := range b {
		out[i] = w[i] * float64(v)
	}
	return out
}

// Evaluator measures soft penalties of schedules of one problem. It is immutable and safe for
// concurrent use; the schedule passed to its methods is only read.
type Evaluator struct {
	p         *engine.Problem
	byGroup   [][]int32
	byTeacher [][]int32
	byItem    map[int64][]int32
	items     []int64
	groupsOf  [][]int32 // distinct groups per lesson
}

// New precomputes the lesson indexes the evaluator needs.
func New(p *engine.Problem) *Evaluator {
	e := &Evaluator{
		p:         p,
		byGroup:   make([][]int32, len(p.Groups)),
		byTeacher: make([][]int32, len(p.Teachers)),
		byItem:    map[int64][]int32{},
		groupsOf:  make([][]int32, len(p.Lessons)),
	}
	for li, l := range p.Lessons {
		idx := int32(li)
		e.byTeacher[l.Teacher] = append(e.byTeacher[l.Teacher], idx)
		if _, ok := e.byItem[l.Item]; !ok {
			e.items = append(e.items, l.Item)
		}
		e.byItem[l.Item] = append(e.byItem[l.Item], idx)
		for _, u := range l.Units {
			g := p.Units[u].Group
			if !slices.Contains(e.groupsOf[li], g) {
				e.groupsOf[li] = append(e.groupsOf[li], g)
				e.byGroup[g] = append(e.byGroup[g], idx)
			}
		}
	}
	return e
}

// ScopeKind distinguishes scope types.
type ScopeKind uint8

// Scope kinds.
const (
	GroupDay ScopeKind = iota
	TeacherDay
	TeacherWeek
	Item
	Lesson
	Room
)

// Scope is one independent part of the penalty.
type Scope struct {
	Kind ScopeKind
	ID   int64 // group, teacher, lesson or room index, or curriculum item ID
	Day  domain.Day
	Week uint8 // 0 odd, 1 even
}

// ScopeSet collects distinct scopes.
type ScopeSet struct {
	list []Scope
	seen map[Scope]struct{}
}

func (ss *ScopeSet) add(s Scope) {
	if ss.seen == nil {
		ss.seen = map[Scope]struct{}{}
	}
	if _, ok := ss.seen[s]; ok {
		return
	}
	ss.seen[s] = struct{}{}
	ss.list = append(ss.list, s)
}

// Len returns the number of scopes.
func (ss *ScopeSet) Len() int { return len(ss.list) }

// AddLesson adds the scopes lesson l affects when it has assignment a. Call it with both the
// old and the new assignment of every lesson a move changes.
func (e *Evaluator) AddLesson(ss *ScopeSet, l int32, a engine.Assignment) {
	les := &e.p.Lessons[l]
	ss.add(Scope{Kind: Item, ID: les.Item})
	ss.add(Scope{Kind: Lesson, ID: int64(l)})
	if !a.Placed {
		return
	}
	for _, w := range weeksOf(a.Slot.Parity) {
		for _, g := range e.groupsOf[l] {
			ss.add(Scope{Kind: GroupDay, ID: int64(g), Day: a.Slot.Day, Week: w})
		}
		ss.add(Scope{Kind: TeacherDay, ID: int64(les.Teacher), Day: a.Slot.Day, Week: w})
		ss.add(Scope{Kind: TeacherWeek, ID: int64(les.Teacher), Week: w})
	}
	if a.Room != engine.None {
		ss.add(Scope{Kind: Room, ID: int64(a.Room)})
	}
}

// Measure returns the penalty of the given scopes in schedule s.
func (e *Evaluator) Measure(s *engine.Schedule, ss *ScopeSet) Breakdown {
	var b Breakdown
	for _, sc := range ss.list {
		e.measure(s, sc, &b)
	}
	return b
}

// Full returns the penalty of the whole schedule.
func (e *Evaluator) Full(s *engine.Schedule) Breakdown {
	var b Breakdown
	for w := uint8(0); w < 2; w++ {
		for d := range e.p.Grid.Days {
			for g := range e.p.Groups {
				e.measure(s, Scope{Kind: GroupDay, ID: int64(g), Day: domain.Day(d), Week: w}, &b)
			}
			for t := range e.p.Teachers {
				e.measure(s, Scope{Kind: TeacherDay, ID: int64(t), Day: domain.Day(d), Week: w}, &b)
			}
		}
		for t := range e.p.Teachers {
			e.measure(s, Scope{Kind: TeacherWeek, ID: int64(t), Week: w}, &b)
		}
	}
	for _, it := range e.items {
		e.measure(s, Scope{Kind: Item, ID: it}, &b)
	}
	for l := range e.p.Lessons {
		e.measure(s, Scope{Kind: Lesson, ID: int64(l)}, &b)
	}
	for r := range e.p.Rooms {
		e.measure(s, Scope{Kind: Room, ID: int64(r)}, &b)
	}
	return b
}

func (e *Evaluator) measure(s *engine.Schedule, sc Scope, b *Breakdown) {
	switch sc.Kind {
	case GroupDay:
		e.measureDay(s, e.byGroup[sc.ID], sc.Day, sc.Week, true, b)
	case TeacherDay:
		e.measureDay(s, e.byTeacher[sc.ID], sc.Day, sc.Week, false, b)
	case TeacherWeek:
		b[TeacherCompactness] += e.teachingDays(s, e.byTeacher[sc.ID], sc.Week)
	case Item:
		e.measureItem(s, e.byItem[sc.ID], b)
	case Lesson:
		e.measureLesson(s, int32(sc.ID), b)
	case Room:
		if e.roomUsed(s, int32(sc.ID)) {
			b[RoomCount]++
		}
	}
}

// maxPeriods bounds PeriodsPerDay (see migrations: 1..16).
const maxPeriods = 16

// measureDay adds gaps, building transitions and (for groups) isolated lessons of one day.
func (e *Evaluator) measureDay(s *engine.Schedule, lessons []int32, day domain.Day, week uint8, group bool, b *Breakdown) {
	var busy [maxPeriods + 2]bool
	var building [maxPeriods + 2]int32
	for i := range building {
		building[i] = engine.None
	}
	count := 0
	for _, l := range lessons {
		a := s.Assignment(l)
		if !a.Placed || a.Slot.Day != day || !coversWeek(a.Slot.Parity, week) {
			continue
		}
		p := a.Slot.Period
		if !busy[p] {
			busy[p] = true
			count++
		}
		if a.Room != engine.None {
			bi := e.p.Rooms[a.Room].Building
			if building[p] == engine.None || bi < building[p] {
				building[p] = bi
			}
		}
	}
	if count == 0 {
		return
	}
	first, last := -1, -1
	prevBuilding := engine.None
	for p := 1; p <= int(e.p.Grid.PeriodsPerDay); p++ {
		if !busy[p] {
			continue
		}
		if first < 0 {
			first = p
		}
		last = p
		if building[p] != engine.None {
			if prevBuilding != engine.None && building[p] != prevBuilding {
				b[BuildingTransitions]++
			}
			prevBuilding = building[p]
		}
		if group && count > 1 && !busy[p-1] && !busy[p+1] {
			b[CurriculumCompactness]++
		}
	}
	b[Gaps] += last - first + 1 - count
}

func (e *Evaluator) teachingDays(s *engine.Schedule, lessons []int32, week uint8) int {
	var days uint8 // bitmask
	for _, l := range lessons {
		a := s.Assignment(l)
		if a.Placed && coversWeek(a.Slot.Parity, week) {
			days |= 1 << a.Slot.Day
		}
	}
	n := 0
	for ; days != 0; days &= days - 1 {
		n++
	}
	return n
}

func (e *Evaluator) measureItem(s *engine.Schedule, lessons []int32, b *Breakdown) {
	var days uint8
	var rooms []int32
	placed := 0
	for _, l := range lessons {
		a := s.Assignment(l)
		if !a.Placed {
			continue
		}
		placed++
		days |= 1 << a.Slot.Day
		if a.Room != engine.None && !slices.Contains(rooms, a.Room) {
			rooms = append(rooms, a.Room)
		}
	}
	distinct := 0
	for ; days != 0; days &= days - 1 {
		distinct++
	}
	b[MinWorkingDays] += min(placed, int(e.p.Grid.Days)) - distinct
	if len(rooms) > 1 {
		b[RoomStability] += len(rooms) - 1
	}
}

func (e *Evaluator) measureLesson(s *engine.Schedule, l int32, b *Breakdown) {
	a := s.Assignment(l)
	if !a.Placed {
		return
	}
	les := &e.p.Lessons[l]
	if a.Slot.Period == 1 || a.Slot.Period == e.p.Grid.PeriodsPerDay {
		b[EdgeSlots]++
	}
	undesired := false
	for _, c := range e.p.Grid.Cells(a.Slot) {
		if e.p.TeacherAvail(les.Teacher, c) == engine.Undesired {
			undesired = true
		}
		if a.Room != engine.None && e.p.RoomAvail(a.Room, c) == engine.Undesired {
			undesired = true
		}
	}
	if undesired {
		b[Preferences]++
	}
	if a.Room != engine.None {
		if waste := e.p.Rooms[a.Room].Capacity - les.Size; waste > 0 {
			b[CapacityWaste] += waste / 10
		}
	}
}

func (e *Evaluator) roomUsed(s *engine.Schedule, r int32) bool {
	for c := range e.p.Grid.CellCount() {
		if s.RoomLoad(r, c) > 0 {
			return true
		}
	}
	return false
}

func weeksOf(p domain.Parity) []uint8 {
	switch p {
	case domain.OddWeek:
		return []uint8{0}
	case domain.EvenWeek:
		return []uint8{1}
	default:
		return []uint8{0, 1}
	}
}

func coversWeek(p domain.Parity, week uint8) bool {
	return p == domain.EveryWeek || (p == domain.OddWeek) == (week == 0)
}
