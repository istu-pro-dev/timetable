package enginetest

import (
	"fmt"
	"slices"

	"pgregory.net/rapid"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Room types used by the generators.
var roomTypes = []string{"lecture", "seminar", "lab"}

// Divisions every generated group is split by, each into two parts.
var divisions = []string{"lang", "pe"}

// InputGen generates small, densely packed instances for property-based tests: a grid of a
// few days and periods, a handful of teachers, rooms and groups, and up to maxLessons lessons
// so that random placements collide often.
//
// Audiences never contain two overlapping members (e.g. a whole group together with one of its
// subgroups), so a lesson never conflicts with itself and every occupancy counter it touches
// grows by exactly one. There is a room of every type, and groups and subgroups have at least
// two students, so a room one seat too small for any lesson can always be built.
func InputGen(maxLessons int) *rapid.Generator[engine.Input] {
	return rapid.Custom(func(t *rapid.T) engine.Input {
		in := engine.Input{
			Grid: domain.Grid{
				Days:          uint8(rapid.IntRange(1, 6).Draw(t, "days")),
				PeriodsPerDay: uint8(rapid.IntRange(1, 8).Draw(t, "periods")),
			},
			MaxLessonsPerDay: rapid.IntRange(0, 4).Draw(t, "maxPerDay"),
		}
		nBuildings := rapid.IntRange(1, 2).Draw(t, "buildings")
		for i := range nBuildings {
			in.Buildings = append(in.Buildings, engine.Building{ID: domain.BuildingID(i + 1), Name: fmt.Sprint("B", i)})
		}

		// One room of every type, then a few more of random types.
		for i := range len(roomTypes) + rapid.IntRange(0, 3).Draw(t, "extraRooms") {
			rt := roomTypes[i%len(roomTypes)]
			if i >= len(roomTypes) {
				rt = rapid.SampledFrom(roomTypes).Draw(t, "roomType")
			}
			in.Rooms = append(in.Rooms, engine.RoomInput{
				ID: domain.RoomID(i + 1), Name: fmt.Sprint("R", i),
				Building: domain.BuildingID(i%nBuildings + 1),
				Type:     rt,
				Capacity: rapid.IntRange(5, 120).Draw(t, "capacity"),
				Avail:    availGen(in.Grid).Draw(t, "roomAvail"),
			})
		}

		nTeachers := rapid.IntRange(1, 5).Draw(t, "teachers")
		for i := range nTeachers {
			in.Teachers = append(in.Teachers, engine.TeacherInput{
				ID: domain.TeacherID(i + 1), Name: fmt.Sprint("T", i), Avail: availGen(in.Grid).Draw(t, "teacherAvail"),
			})
		}

		nGroups := rapid.IntRange(1, 4).Draw(t, "groups")
		for i := range nGroups {
			id := domain.GroupID(i + 1)
			in.Groups = append(in.Groups, engine.Group{ID: id, Name: fmt.Sprint("G", i), Size: rapid.IntRange(2, 40).Draw(t, "groupSize")})
			for _, d := range divisions {
				for part := uint8(1); part <= 2; part++ {
					in.Subgroups = append(in.Subgroups, engine.SubgroupInput{
						Group: id, Division: d, Part: part, Size: rapid.IntRange(2, 25).Draw(t, "subgroupSize"),
					})
				}
			}
		}

		in.Disciplines = []engine.Discipline{{ID: 1, Name: "D1"}, {ID: 2, Name: "D2"}}
		for i := range rapid.IntRange(0, maxLessons).Draw(t, "lessons") {
			rt := rapid.SampledFrom(roomTypes).Draw(t, "lessonRoomType")
			in.Lessons = append(in.Lessons, engine.LessonInput{
				ID:         domain.LessonID(i + 1),
				Item:       int64(rapid.IntRange(1, max(maxLessons/2, 1)).Draw(t, "item")),
				Discipline: domain.DisciplineID(rapid.IntRange(1, 2).Draw(t, "discipline")),
				Kind:       rt,
				Teacher:    domain.TeacherID(rapid.IntRange(1, nTeachers).Draw(t, "teacher")),
				Audience:   audienceGen(nGroups).Draw(t, "audience"),
				Biweekly:   rapid.IntRange(0, 3).Draw(t, "biweekly") == 0,
				RoomType:   rt,
			})
		}
		return in
	})
}

// availGen generates a few availability exceptions inside the grid.
func availGen(g domain.Grid) *rapid.Generator[[]engine.SlotAvailability] {
	return rapid.Custom(func(t *rapid.T) []engine.SlotAvailability {
		var out []engine.SlotAvailability
		for range rapid.IntRange(0, 3).Draw(t, "exceptions") {
			out = append(out, engine.SlotAvailability{
				Slot: SlotGen(g, rapid.Bool().Draw(t, "biweekly")).Draw(t, "slot"),
				Status: rapid.SampledFrom([]engine.Availability{
					engine.Unavailable, engine.Undesired, engine.Preferred,
				}).Draw(t, "status"),
			})
		}
		return out
	})
}

// audienceGen generates an audience without internally overlapping members: a whole group, a
// subgroup, both disjoint parts of one division of a group, or a stream of members of distinct
// groups.
func audienceGen(nGroups int) *rapid.Generator[domain.Audience] {
	return rapid.Custom(func(t *rapid.T) domain.Audience {
		switch rapid.IntRange(0, 3).Draw(t, "audienceKind") {
		case 0:
			return domain.Audience{memberGen(domain.GroupID(rapid.IntRange(1, nGroups).Draw(t, "group"))).Draw(t, "member")}
		case 1:
			g := domain.GroupID(rapid.IntRange(1, nGroups).Draw(t, "group"))
			d := rapid.SampledFrom(divisions).Draw(t, "division")
			return domain.Audience{domain.Subgroup(g, d, 1), domain.Subgroup(g, d, 2)}
		default:
			groups := rapid.SliceOfNDistinct(rapid.IntRange(1, nGroups), 1, 3, rapid.ID[int]).Draw(t, "stream")
			aud := make(domain.Audience, 0, len(groups))
			for _, g := range groups {
				aud = append(aud, memberGen(domain.GroupID(g)).Draw(t, "member"))
			}
			return aud
		}
	})
}

// memberGen generates the whole group g or one of its subgroups.
func memberGen(g domain.GroupID) *rapid.Generator[domain.Member] {
	return rapid.Custom(func(t *rapid.T) domain.Member {
		if rapid.Bool().Draw(t, "whole") {
			return domain.WholeGroup(g)
		}
		return domain.Subgroup(g, rapid.SampledFrom(divisions).Draw(t, "division"), uint8(rapid.IntRange(1, 2).Draw(t, "part")))
	})
}

// SlotGen generates a slot of the grid; biweekly slots are odd or even.
func SlotGen(g domain.Grid, biweekly bool) *rapid.Generator[domain.Slot] {
	return rapid.Custom(func(t *rapid.T) domain.Slot {
		s := domain.Slot{
			Day:    domain.Day(rapid.IntRange(0, int(g.Days)-1).Draw(t, "day")),
			Period: uint8(rapid.IntRange(1, int(g.PeriodsPerDay)).Draw(t, "period")),
		}
		if biweekly {
			s.Parity = rapid.SampledFrom([]domain.Parity{domain.OddWeek, domain.EvenWeek}).Draw(t, "parity")
		}
		return s
	})
}

// DrawProblem draws an input from InputGen and builds the problem; generated inputs are always
// valid.
func DrawProblem(t *rapid.T, maxLessons int) (engine.Input, *engine.Problem) {
	in := InputGen(maxLessons).Draw(t, "input")
	p, err := engine.NewProblem(in)
	if err != nil {
		t.Fatalf("generated input rejected: %v", err)
	}
	return in, p
}

// DrawSchedule places lessons at random slots: most are placed, some without a room or in a
// room of the wrong type, some left unplaced. Conflicts of every kind are frequent.
func DrawSchedule(t *rapid.T, p *engine.Problem) *engine.Schedule {
	s := engine.NewSchedule(p)
	for l := range p.Lessons {
		li := int32(l)
		if rapid.IntRange(0, 9).Draw(t, "unplaced") == 0 {
			continue
		}
		slot := SlotGen(p.Grid, p.Lessons[l].Biweekly).Draw(t, "slot")
		room := engine.None
		switch k := rapid.IntRange(0, 9).Draw(t, "roomKind"); {
		case k == 0:
		case k == 1 || len(p.Lessons[l].Rooms) == 0:
			room = int32(rapid.IntRange(0, len(p.Rooms)-1).Draw(t, "anyRoom"))
		default:
			room = rapid.SampledFrom(p.Lessons[l].Rooms).Draw(t, "room")
		}
		if err := s.Place(li, slot, room); err != nil {
			t.Fatalf("place: %v", err)
		}
	}
	return s
}

// DrawValidSchedule builds a schedule that satisfies H1–H6 and H8 by construction: lessons are
// taken in a random order and each goes to the first slot and room — scanned from a random
// offset — that keeps every constraint, judged by the occupancy counters and the raw input
// data rather than by the hard-constraint checker. Lessons that fit nowhere stay unplaced, so
// only H7 may be violated.
func DrawValidSchedule(t *rapid.T, p *engine.Problem) *engine.Schedule {
	g := &greedy{s: engine.NewSchedule(p), dayLoad: map[dayKey]int{}, itemDays: map[itemDay]bool{}}
	slots := func(biweekly bool) []domain.Slot {
		if biweekly {
			return p.Grid.Slots(domain.OddWeek, domain.EvenWeek)
		}
		return p.Grid.Slots(domain.EveryWeek)
	}
	order := rapid.Permutation(lessonIndices(p)).Draw(t, "order")
	for _, l := range order {
		les := &p.Lessons[l]
		cand := slots(les.Biweekly)
		start := rapid.IntRange(0, len(cand)-1).Draw(t, "slotOffset")
		roomStart := 0
		if len(les.Rooms) > 0 {
			roomStart = rapid.IntRange(0, len(les.Rooms)-1).Draw(t, "roomOffset")
		}
	search:
		for i := range cand {
			slot := cand[(start+i)%len(cand)]
			if !g.timeFree(l, slot) {
				continue
			}
			for j := range les.Rooms {
				room := les.Rooms[(roomStart+j)%len(les.Rooms)]
				if g.roomFits(l, slot, room) {
					g.place(l, slot, room)
					break search
				}
			}
		}
	}
	return g.s
}

func lessonIndices(p *engine.Problem) []int32 {
	out := make([]int32, len(p.Lessons))
	for i := range out {
		out[i] = int32(i)
	}
	return out
}

type dayKey struct {
	teacher bool
	res     int32
	day     domain.Day
	week    int
}

type itemDay struct {
	item int64
	day  domain.Day
	week int
}

// greedy tracks what DrawValidSchedule needs beyond the schedule's own counters.
type greedy struct {
	s        *engine.Schedule
	dayLoad  map[dayKey]int
	itemDays map[itemDay]bool
}

// weeksOf returns the weeks (0 odd, 1 even) a slot occupies.
func weeksOf(p domain.Parity) []int {
	switch p {
	case domain.OddWeek:
		return []int{0}
	case domain.EvenWeek:
		return []int{1}
	default:
		return []int{0, 1}
	}
}

func (g *greedy) dayKeys(l int32, slot domain.Slot, week int) []dayKey {
	les := &g.s.P.Lessons[l]
	keys := []dayKey{{teacher: true, res: les.Teacher, day: slot.Day, week: week}}
	for _, u := range les.Units {
		k := dayKey{res: g.s.P.Units[u].Group, day: slot.Day, week: week}
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return keys
}

// timeFree reports whether the lesson's teacher and students are free and available in the
// slot and H8 allows another lesson that day.
func (g *greedy) timeFree(l int32, slot domain.Slot) bool {
	p := g.s.P
	les := &p.Lessons[l]
	for _, c := range p.Grid.Cells(slot) {
		if g.s.TeacherLoad(les.Teacher, c) > 0 || p.TeacherAvail(les.Teacher, c) == engine.Unavailable {
			return false
		}
		for _, u := range les.Units {
			if g.s.UnitLoad(u, c) > 0 {
				return false
			}
		}
	}
	for _, w := range weeksOf(slot.Parity) {
		if g.itemDays[itemDay{les.Item, slot.Day, w}] {
			return false
		}
		if p.MaxLessonsPerDay <= 0 {
			continue
		}
		for _, k := range g.dayKeys(l, slot, w) {
			if g.dayLoad[k] >= p.MaxLessonsPerDay {
				return false
			}
		}
	}
	return true
}

// roomFits reports whether the room is free, available and large enough.
func (g *greedy) roomFits(l int32, slot domain.Slot, room int32) bool {
	p := g.s.P
	if p.Rooms[room].Capacity < p.Lessons[l].Size || p.Rooms[room].Type != p.Lessons[l].RoomType {
		return false
	}
	for _, c := range p.Grid.Cells(slot) {
		if g.s.RoomLoad(room, c) > 0 || p.RoomAvail(room, c) == engine.Unavailable {
			return false
		}
	}
	return true
}

func (g *greedy) place(l int32, slot domain.Slot, room int32) {
	if err := g.s.Place(l, slot, room); err != nil {
		panic(err)
	}
	for _, w := range weeksOf(slot.Parity) {
		g.itemDays[itemDay{g.s.P.Lessons[l].Item, slot.Day, w}] = true
		for _, k := range g.dayKeys(l, slot, w) {
			g.dayLoad[k]++
		}
	}
}
