package engine

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
)

var testGrid = domain.Grid{Days: 6, PeriodsPerDay: 7}

// smallInput is a hand-made instance used across tests:
//
//	teachers: 1 Ivanov (unavailable Monday), 2 Petrov
//	groups: 10 G1 (25 students, english 1/2), 20 G2 (30 students)
//	rooms: 100 lecture hall A (90), 101 seminar A (30), 102 lab B (15)
//	lessons:
//	  1 lecture Ivanov → stream G1+G2
//	  2 seminar Petrov → G1
//	  3 lab Petrov → G1 english/1 (biweekly)
//	  4 lab Ivanov → G1 english/2
func smallInput() Input {
	return Input{
		Grid:      testGrid,
		Buildings: []Building{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		Rooms: []RoomInput{
			{ID: 100, Name: "A-100", Building: 1, Type: "lecture", Capacity: 90},
			{ID: 101, Name: "A-101", Building: 1, Type: "seminar", Capacity: 30},
			{ID: 102, Name: "B-102", Building: 2, Type: "lab", Capacity: 15,
				Avail: []SlotAvailability{{Slot: domain.Slot{Day: domain.Friday, Period: 1}, Status: Unavailable}}},
		},
		Teachers: []TeacherInput{
			{ID: 1, Name: "Иванов", Avail: []SlotAvailability{
				{Slot: domain.Slot{Day: domain.Monday, Period: 1}, Status: Unavailable},
				{Slot: domain.Slot{Day: domain.Monday, Period: 1, Parity: domain.OddWeek}, Status: Preferred},
			}},
			{ID: 2, Name: "Петров"},
		},
		Groups: []Group{{ID: 10, Name: "G1", Size: 25}, {ID: 20, Name: "G2", Size: 30}},
		Subgroups: []SubgroupInput{
			{Group: 10, Division: "english", Part: 1, Size: 12},
			{Group: 10, Division: "english", Part: 2, Size: 13},
		},
		Disciplines: []Discipline{{ID: 1, Name: "Математика"}, {ID: 2, Name: "Английский"}},
		Lessons: []LessonInput{
			{ID: 1, Item: 1, Discipline: 1, Kind: "lecture", Teacher: 1, RoomType: "lecture",
				Audience: domain.Audience{domain.WholeGroup(10), domain.WholeGroup(20)}},
			{ID: 2, Item: 2, Discipline: 1, Kind: "seminar", Teacher: 2, RoomType: "seminar",
				Audience: domain.Audience{domain.WholeGroup(10)}},
			{ID: 3, Item: 3, Discipline: 2, Kind: "lab", Teacher: 2, RoomType: "lab", Biweekly: true,
				Audience: domain.Audience{domain.Subgroup(10, "english", 1)}},
			{ID: 4, Item: 4, Discipline: 2, Kind: "lab", Teacher: 1, RoomType: "lab",
				Audience: domain.Audience{domain.Subgroup(10, "english", 2)}},
		},
	}
}

func mustProblem(t testing.TB, in Input) *Problem {
	t.Helper()
	p, err := NewProblem(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func slot(d domain.Day, period uint8) domain.Slot {
	return domain.Slot{Day: d, Period: period}
}

func slotP(d domain.Day, period uint8, parity domain.Parity) domain.Slot {
	return domain.Slot{Day: d, Period: period, Parity: parity}
}

// randomInput generates a university-like instance with the given number of lessons.
func randomInput(r *rand.Rand, lessons int) Input {
	in := Input{Grid: testGrid, Buildings: []Building{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}}}
	types := []string{"lecture", "seminar", "lab"}
	nRooms := max(lessons/20, 3)
	for i := range nRooms {
		in.Rooms = append(in.Rooms, RoomInput{
			ID: domain.RoomID(i + 1), Name: fmt.Sprint("R", i), Building: domain.BuildingID(i%3 + 1),
			Type: types[i%3], Capacity: 20 + r.IntN(100),
		})
	}
	nTeachers := max(lessons/12, 2)
	for i := range nTeachers {
		in.Teachers = append(in.Teachers, TeacherInput{ID: domain.TeacherID(i + 1), Name: fmt.Sprint("T", i)})
	}
	nGroups := max(lessons/20, 2)
	for i := range nGroups {
		id := domain.GroupID(i + 1)
		in.Groups = append(in.Groups, Group{ID: id, Name: fmt.Sprint("G", i), Size: 15 + r.IntN(20)})
		in.Subgroups = append(in.Subgroups,
			SubgroupInput{Group: id, Division: "lang", Part: 1, Size: 10},
			SubgroupInput{Group: id, Division: "lang", Part: 2, Size: 10})
	}
	in.Disciplines = []Discipline{{ID: 1, Name: "D"}}
	for i := range lessons {
		g := domain.GroupID(r.IntN(nGroups) + 1)
		aud := domain.Audience{domain.WholeGroup(g)}
		switch r.IntN(5) {
		case 0:
			aud = append(aud, domain.WholeGroup(domain.GroupID(r.IntN(nGroups)+1)))
		case 1:
			aud = domain.Audience{domain.Subgroup(g, "lang", uint8(r.IntN(2)+1))}
		}
		if len(aud) == 2 && aud[0] == aud[1] {
			aud = aud[:1]
		}
		in.Lessons = append(in.Lessons, LessonInput{
			ID: domain.LessonID(i + 1), Item: int64(i/2 + 1), Discipline: 1, Kind: types[i%3],
			Teacher: domain.TeacherID(r.IntN(nTeachers) + 1), RoomType: types[i%3],
			Audience: aud, Biweekly: r.IntN(6) == 0,
		})
	}
	return in
}

// randomSlot returns a random in-grid slot that matches the lesson's parity kind.
func randomSlot(r *rand.Rand, p *Problem, l int32) domain.Slot {
	s := domain.Slot{
		Day:    domain.Day(r.IntN(int(p.Grid.Days))),
		Period: uint8(r.IntN(int(p.Grid.PeriodsPerDay)) + 1),
	}
	if p.Lessons[l].Biweekly {
		s.Parity = domain.OddWeek + domain.Parity(r.IntN(2))
	}
	return s
}

// randomRoom returns a suitable room for the lesson or None.
func randomRoom(r *rand.Rand, p *Problem, l int32) int32 {
	rooms := p.Lessons[l].Rooms
	if len(rooms) == 0 || r.IntN(10) == 0 {
		return None
	}
	return rooms[r.IntN(len(rooms))]
}
