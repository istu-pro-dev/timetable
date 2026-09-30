// Package hard checks the hard constraints H1–H8 (research §2) on an engine schedule.
//
// This is the single source of truth for schedule correctness (arch §19): the solver, manual
// edits, teacher change requests and the AI agent all rely on it.
package hard

import (
	"fmt"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Rule identifies a hard constraint.
type Rule uint8

// Hard constraints from the research document, section 2.
const (
	H1TeacherBusy  Rule = iota + 1 // a teacher gives two lessons at once
	H2GroupBusy                    // a group or subgroup attends two lessons at once
	H3RoomBusy                     // a room hosts two lessons at once
	H4Availability                 // teacher or room unavailable in the slot
	H5RoomType                     // room type does not suit the lesson
	H6Capacity                     // room is too small for the audience
	H7Completeness                 // lesson is not placed or has no room
	H8SameDay                      // lessons of one curriculum item repeat within a day, or daily load exceeded
)

// Rules lists every rule in order.
var Rules = []Rule{H1TeacherBusy, H2GroupBusy, H3RoomBusy, H4Availability, H5RoomType, H6Capacity, H7Completeness, H8SameDay}

var ruleCodes = map[Rule]string{
	H1TeacherBusy: "H1", H2GroupBusy: "H2", H3RoomBusy: "H3", H4Availability: "H4",
	H5RoomType: "H5", H6Capacity: "H6", H7Completeness: "H7", H8SameDay: "H8",
}

// String returns the code "H1".."H8".
func (r Rule) String() string {
	if c, ok := ruleCodes[r]; ok {
		return c
	}
	return fmt.Sprintf("Rule(%d)", r)
}

// Strictness says how a rule is treated (arch §14.1, §5).
type Strictness uint8

// Strictness levels.
const (
	// Hard violations make a schedule invalid.
	Hard Strictness = iota
	// HeavySoft violations are allowed but penalised far above any soft criterion.
	HeavySoft
	// Off disables the rule.
	Off
)

// Policy sets the strictness of each rule. The zero value treats every rule as Hard.
type Policy map[Rule]Strictness

// Of returns the strictness of a rule.
func (p Policy) Of(r Rule) Strictness {
	return p[r]
}

// ResourceKind tells what a violation is about.
type ResourceKind uint8

// Resource kinds.
const (
	NoResource ResourceKind = iota
	TeacherResource
	GroupResource
	RoomResource
)

// Violation is one broken rule instance.
type Violation struct {
	Rule       Rule
	Strictness Strictness
	// Lessons involved, sorted by index.
	Lessons []int32
	// Resource the violation is about (teacher, group or room index), if any.
	Kind     ResourceKind
	Resource int32
	// Slot where it happens; zero for H7 on an unplaced lesson.
	Slot domain.Slot
	// Message is a human-readable explanation in Russian.
	Message string
}

// Involves reports whether the violation concerns lesson l.
func (v Violation) Involves(l int32) bool {
	_, found := slices.BinarySearch(v.Lessons, l)
	return found
}

// Check returns every violation of the schedule under the policy, in rule order.
func Check(s *engine.Schedule, pol Policy) []Violation {
	c := checker{s: s, p: s.P, pol: pol}
	c.conflicts()
	c.perLesson()
	c.sameDay()
	slices.SortStableFunc(c.out, func(a, b Violation) int { return int(a.Rule) - int(b.Rule) })
	return c.out
}

// Involving returns the violations that concern at least one of the lessons.
func Involving(vs []Violation, lessons ...int32) []Violation {
	var out []Violation
	for _, v := range vs {
		if slices.ContainsFunc(lessons, v.Involves) {
			out = append(out, v)
		}
	}
	return out
}

// Split separates hard violations from heavy-soft ones.
func Split(vs []Violation) (hardOnes, heavySoft []Violation) {
	for _, v := range vs {
		if v.Strictness == Hard {
			hardOnes = append(hardOnes, v)
		} else {
			heavySoft = append(heavySoft, v)
		}
	}
	return hardOnes, heavySoft
}

type checker struct {
	s   *engine.Schedule
	p   *engine.Problem
	pol Policy
	out []Violation
}

func (c *checker) add(v Violation) {
	st := c.pol.Of(v.Rule)
	if st == Off {
		return
	}
	v.Strictness = st
	slices.Sort(v.Lessons)
	c.out = append(c.out, v)
}

type bucketKey struct {
	kind     ResourceKind
	resource int32
	time     int // grid time index (day, period)
}

// conflicts detects H1–H3: two lessons that share a teacher, students or a room and meet in
// at least one week.
func (c *checker) conflicts() {
	buckets := map[bucketKey][]int32{}
	var keys []bucketKey
	push := func(k bucketKey, l int32) {
		if _, ok := buckets[k]; !ok {
			keys = append(keys, k)
		}
		buckets[k] = append(buckets[k], l)
	}
	for li := range c.p.Lessons {
		l := int32(li)
		a := c.s.Assignment(l)
		if !a.Placed {
			continue
		}
		les := &c.p.Lessons[l]
		t := c.p.Grid.TimeIndex(a.Slot)
		push(bucketKey{TeacherResource, les.Teacher, t}, l)
		if a.Room != engine.None {
			push(bucketKey{RoomResource, a.Room, t}, l)
		}
		for _, g := range c.lessonGroups(l) {
			push(bucketKey{GroupResource, g, t}, l)
		}
	}

	for _, k := range keys {
		ls := buckets[k]
		if len(ls) < 2 {
			continue
		}
		involved := map[int32]bool{}
		for i, a := range ls {
			for _, b := range ls[i+1:] {
				if !c.s.Assignment(a).Slot.Overlaps(c.s.Assignment(b).Slot) {
					continue
				}
				if k.kind == GroupResource && !c.audiencesOverlapIn(a, b, k.resource) {
					continue
				}
				involved[a], involved[b] = true, true
			}
		}
		if len(involved) == 0 {
			continue
		}
		lessons := make([]int32, 0, len(involved))
		for l := range involved {
			lessons = append(lessons, l)
		}
		slices.Sort(lessons)
		c.add(c.conflictViolation(k, lessons))
	}
}

func (c *checker) conflictViolation(k bucketKey, lessons []int32) Violation {
	slot := c.s.Assignment(lessons[0]).Slot
	slot.Parity = domain.EveryWeek
	v := Violation{Lessons: lessons, Kind: k.kind, Resource: k.resource, Slot: slot}
	when := describeSlot(slot)
	switch k.kind {
	case TeacherResource:
		v.Rule = H1TeacherBusy
		v.Message = fmt.Sprintf("Преподаватель %s ведёт %d занятия одновременно: %s", c.p.Teachers[k.resource].Name, len(lessons), when)
	case GroupResource:
		v.Rule = H2GroupBusy
		v.Message = fmt.Sprintf("Группа %s занята на %d занятиях одновременно: %s", c.p.Groups[k.resource].Name, len(lessons), when)
	default:
		v.Rule = H3RoomBusy
		v.Message = fmt.Sprintf("Аудитория %s занята %d занятиями одновременно: %s", c.p.Rooms[k.resource].Name, len(lessons), when)
	}
	return v
}

// lessonGroups returns the distinct groups attending lesson l.
func (c *checker) lessonGroups(l int32) []int32 {
	units := c.p.Lessons[l].Units
	out := make([]int32, 0, len(units))
	for _, u := range units {
		g := c.p.Units[u].Group
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// audiencesOverlapIn reports whether lessons a and b share students of group g.
func (c *checker) audiencesOverlapIn(a, b, g int32) bool {
	for _, ua := range c.p.Lessons[a].Units {
		x := c.p.Units[ua]
		if x.Group != g {
			continue
		}
		for _, ub := range c.p.Lessons[b].Units {
			y := c.p.Units[ub]
			if y.Group == g && unitsOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

func unitsOverlap(x, y engine.Unit) bool {
	if x.Division == engine.None || y.Division == engine.None || x.Division != y.Division {
		return true
	}
	return x.Part == y.Part
}

// perLesson checks H4–H7 lesson by lesson.
func (c *checker) perLesson() {
	for li := range c.p.Lessons {
		l := int32(li)
		les := &c.p.Lessons[l]
		a := c.s.Assignment(l)
		name := c.lessonName(l)
		if !a.Placed {
			c.add(Violation{Rule: H7Completeness, Lessons: []int32{l}, Message: "Занятие не поставлено в расписание: " + name})
			continue
		}
		when := describeSlot(a.Slot)
		cells := c.p.Grid.Cells(a.Slot)

		if slices.ContainsFunc(cells, func(cell int) bool { return c.p.TeacherAvail(les.Teacher, cell) == engine.Unavailable }) {
			c.add(Violation{
				Rule: H4Availability, Lessons: []int32{l}, Kind: TeacherResource, Resource: les.Teacher, Slot: a.Slot,
				Message: fmt.Sprintf("Преподаватель %s недоступен: %s (%s)", c.p.Teachers[les.Teacher].Name, when, name),
			})
		}

		if a.Room == engine.None {
			c.add(Violation{Rule: H7Completeness, Lessons: []int32{l}, Slot: a.Slot, Message: "Не назначена аудитория: " + name})
			continue
		}
		room := &c.p.Rooms[a.Room]
		if slices.ContainsFunc(cells, func(cell int) bool { return c.p.RoomAvail(a.Room, cell) == engine.Unavailable }) {
			c.add(Violation{
				Rule: H4Availability, Lessons: []int32{l}, Kind: RoomResource, Resource: a.Room, Slot: a.Slot,
				Message: fmt.Sprintf("Аудитория %s недоступна: %s (%s)", room.Name, when, name),
			})
		}
		if room.Type != les.RoomType {
			c.add(Violation{
				Rule: H5RoomType, Lessons: []int32{l}, Kind: RoomResource, Resource: a.Room, Slot: a.Slot,
				Message: fmt.Sprintf("Тип аудитории %s (%s) не подходит, нужен %s: %s", room.Name, room.Type, les.RoomType, name),
			})
		}
		if room.Capacity < les.Size {
			c.add(Violation{
				Rule: H6Capacity, Lessons: []int32{l}, Kind: RoomResource, Resource: a.Room, Slot: a.Slot,
				Message: fmt.Sprintf("Аудитория %s вмещает %d, а студентов %d: %s", room.Name, room.Capacity, les.Size, name),
			})
		}
	}
}

type dayKey struct {
	kind     ResourceKind
	resource int32
	day      domain.Day
	week     int // 0 odd, 1 even
}

// sameDay checks H8: lessons of one curriculum item on the same day and the daily load limit.
func (c *checker) sameDay() {
	type itemDay struct {
		item int64
		day  domain.Day
		week int
	}
	siblings := map[itemDay][]int32{}
	var siblingKeys []itemDay
	load := map[dayKey][]int32{}
	var loadKeys []dayKey
	for li := range c.p.Lessons {
		l := int32(li)
		a := c.s.Assignment(l)
		if !a.Placed {
			continue
		}
		les := &c.p.Lessons[l]
		for _, week := range weeks(a.Slot.Parity) {
			k := itemDay{les.Item, a.Slot.Day, week}
			if _, ok := siblings[k]; !ok {
				siblingKeys = append(siblingKeys, k)
			}
			siblings[k] = append(siblings[k], l)
			if c.p.MaxLessonsPerDay <= 0 {
				continue
			}
			keys := []dayKey{{TeacherResource, les.Teacher, a.Slot.Day, week}}
			for _, g := range c.lessonGroups(l) {
				keys = append(keys, dayKey{GroupResource, g, a.Slot.Day, week})
			}
			for _, dk := range keys {
				if _, ok := load[dk]; !ok {
					loadKeys = append(loadKeys, dk)
				}
				load[dk] = append(load[dk], l)
			}
		}
	}

	reported := map[string]bool{}
	for _, k := range siblingKeys {
		ls := siblings[k]
		if len(ls) < 2 {
			continue
		}
		v := Violation{
			Rule: H8SameDay, Lessons: slices.Clone(ls), Slot: domain.Slot{Day: k.day},
			Message: fmt.Sprintf("%d занятия «%s» в один день (%s)", len(ls), c.p.Disciplines[c.p.Lessons[ls[0]].Discipline].Name, dayName(k.day)),
		}
		c.addOnce(reported, v)
	}
	for _, k := range loadKeys {
		ls := load[k]
		if len(ls) <= c.p.MaxLessonsPerDay {
			continue
		}
		v := Violation{Rule: H8SameDay, Lessons: slices.Clone(ls), Kind: k.kind, Resource: k.resource, Slot: domain.Slot{Day: k.day}}
		who := c.p.Teachers[k.resource].Name
		if k.kind == GroupResource {
			who = "группы " + c.p.Groups[k.resource].Name
		}
		v.Message = fmt.Sprintf("%d занятий в день у %s (%s), максимум %d", len(ls), who, dayName(k.day), c.p.MaxLessonsPerDay)
		c.addOnce(reported, v)
	}
}

// addOnce adds a violation unless an identical one (same lessons and message) was already
// reported, e.g. for the odd and the even week of every-week lessons.
func (c *checker) addOnce(seen map[string]bool, v Violation) {
	slices.Sort(v.Lessons)
	key := fmt.Sprint(v.Kind, v.Resource, v.Lessons, v.Message)
	if seen[key] {
		return
	}
	seen[key] = true
	c.add(v)
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

func (c *checker) lessonName(l int32) string {
	les := &c.p.Lessons[l]
	return fmt.Sprintf("%s (%s, %s)", c.p.Disciplines[les.Discipline].Name, les.Kind, c.p.Teachers[les.Teacher].Name)
}

var dayNames = [...]string{"понедельник", "вторник", "среда", "четверг", "пятница", "суббота", "воскресенье"}

func dayName(d domain.Day) string {
	if int(d) < len(dayNames) {
		return dayNames[d]
	}
	return d.String()
}

func describeSlot(s domain.Slot) string {
	out := fmt.Sprintf("%s, %d-я пара", dayName(s.Day), s.Period)
	switch s.Parity {
	case domain.OddWeek:
		out += ", числитель"
	case domain.EvenWeek:
		out += ", знаменатель"
	}
	return out
}
