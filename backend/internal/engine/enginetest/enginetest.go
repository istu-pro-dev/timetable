// Package enginetest generates random problems and schedules for property-based tests.
package enginetest

import (
	"fmt"
	"math/rand/v2"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Grid is the default week shape: six days, seven pairs.
var Grid = domain.Grid{Days: 6, PeriodsPerDay: 7}

// RandomInput generates a university-like instance with the given number of lessons: three
// buildings, lecture/seminar/lab rooms, groups with two language subgroups, streams,
// biweekly lessons, and random availability exceptions.
func RandomInput(r *rand.Rand, lessons int) engine.Input {
	in := engine.Input{
		Grid:      Grid,
		Buildings: []engine.Building{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}},
	}
	types := []string{"lecture", "seminar", "lab"}
	statuses := []engine.Availability{engine.Unavailable, engine.Undesired, engine.Preferred}
	randomAvail := func() []engine.SlotAvailability {
		var out []engine.SlotAvailability
		for range r.IntN(3) {
			out = append(out, engine.SlotAvailability{Slot: RandomSlot(r, false), Status: statuses[r.IntN(3)]})
		}
		return out
	}

	nRooms := max(lessons/20, 3)
	for i := range nRooms {
		in.Rooms = append(in.Rooms, engine.RoomInput{
			ID: domain.RoomID(i + 1), Name: fmt.Sprint("R", i), Building: domain.BuildingID(i%3 + 1),
			Type: types[i%3], Capacity: 10 + r.IntN(100), Avail: randomAvail(),
		})
	}
	nTeachers := max(lessons/12, 2)
	for i := range nTeachers {
		in.Teachers = append(in.Teachers, engine.TeacherInput{
			ID: domain.TeacherID(i + 1), Name: fmt.Sprint("T", i), Avail: randomAvail(),
		})
	}
	nGroups := max(lessons/20, 2)
	for i := range nGroups {
		id := domain.GroupID(i + 1)
		in.Groups = append(in.Groups, engine.Group{ID: id, Name: fmt.Sprint("G", i), Size: 15 + r.IntN(20)})
		in.Subgroups = append(in.Subgroups,
			engine.SubgroupInput{Group: id, Division: "lang", Part: 1, Size: 10},
			engine.SubgroupInput{Group: id, Division: "lang", Part: 2, Size: 10})
	}
	in.Disciplines = []engine.Discipline{{ID: 1, Name: "D"}}
	for i := range lessons {
		g := domain.GroupID(r.IntN(nGroups) + 1)
		aud := domain.Audience{domain.WholeGroup(g)}
		switch r.IntN(6) {
		case 0:
			if other := domain.GroupID(r.IntN(nGroups) + 1); other != g {
				aud = append(aud, domain.WholeGroup(other))
			}
		case 1:
			aud = domain.Audience{domain.Subgroup(g, "lang", uint8(r.IntN(2)+1))}
		case 2:
			aud = domain.Audience{domain.Subgroup(g, "pe", 1)}
		}
		in.Lessons = append(in.Lessons, engine.LessonInput{
			ID: domain.LessonID(i + 1), Item: int64(i/3 + 1), Discipline: 1, Kind: types[i%3],
			Teacher: domain.TeacherID(r.IntN(nTeachers) + 1), RoomType: types[i%3],
			Audience: aud, Biweekly: r.IntN(6) == 0,
		})
	}
	return in
}

// RandomSlot returns a random slot of Grid. Biweekly slots are odd or even at random.
func RandomSlot(r *rand.Rand, biweekly bool) domain.Slot {
	s := domain.Slot{
		Day:    domain.Day(r.IntN(int(Grid.Days))),
		Period: uint8(r.IntN(int(Grid.PeriodsPerDay)) + 1),
	}
	if biweekly {
		s.Parity = domain.OddWeek + domain.Parity(r.IntN(2))
	}
	return s
}

// RandomRoom returns a random suitable room for lesson l, occasionally any room or none.
func RandomRoom(r *rand.Rand, p *engine.Problem, l int32) int32 {
	switch r.IntN(10) {
	case 0:
		return engine.None
	case 1:
		return int32(r.IntN(len(p.Rooms)))
	}
	rooms := p.Lessons[l].Rooms
	if len(rooms) == 0 {
		return engine.None
	}
	return rooms[r.IntN(len(rooms))]
}

// RandomSchedule places every lesson at a random slot and room, leaving about 5% unplaced.
func RandomSchedule(r *rand.Rand, p *engine.Problem) *engine.Schedule {
	s := engine.NewSchedule(p)
	for l := range p.Lessons {
		if r.IntN(20) == 0 {
			continue
		}
		RandomPlace(r, s, int32(l))
	}
	return s
}

// RandomPlace moves lesson l to a random slot and room.
func RandomPlace(r *rand.Rand, s *engine.Schedule, l int32) {
	if err := s.Place(l, RandomSlot(r, s.P.Lessons[l].Biweekly), RandomRoom(r, s.P, l)); err != nil {
		panic(err)
	}
}
