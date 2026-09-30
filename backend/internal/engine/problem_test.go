package engine

import (
	"errors"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
)

func TestNewProblemIndexes(t *testing.T) {
	p := mustProblem(t, smallInput())

	if len(p.Lessons) != 4 || len(p.Teachers) != 2 || len(p.Rooms) != 3 || len(p.Groups) != 2 {
		t.Fatalf("sizes: %d lessons, %d teachers, %d rooms, %d groups",
			len(p.Lessons), len(p.Teachers), len(p.Rooms), len(p.Groups))
	}
	// Units: G1, G2, G1/english/1, G1/english/2 in first-use order.
	if len(p.Units) != 4 || p.Divisions != 1 {
		t.Fatalf("units = %+v, divisions = %d", p.Units, p.Divisions)
	}
	if p.DivisionGroup(0) != 0 {
		t.Fatalf("division 0 belongs to group %d", p.DivisionGroup(0))
	}

	lecture := p.Lessons[0]
	if lecture.Size != 55 || !slices.Equal(lecture.Rooms, []int32{0}) || len(lecture.Units) != 2 {
		t.Fatalf("lecture = %+v", lecture)
	}
	lab := p.Lessons[2]
	if lab.Size != 12 || !lab.Biweekly || p.Units[lab.Units[0]].Division != 0 || p.Units[lab.Units[0]].Part != 1 {
		t.Fatalf("lab = %+v unit = %+v", lab, p.Units[lab.Units[0]])
	}
	if i, ok := p.LessonIndex(3); !ok || i != 2 {
		t.Fatalf("LessonIndex(3) = %d, %v", i, ok)
	}
	if _, ok := p.LessonIndex(99); ok {
		t.Fatal("LessonIndex(99) found")
	}
	if i, ok := p.RoomIndex(102); !ok || i != 2 {
		t.Fatalf("RoomIndex(102) = %d, %v", i, ok)
	}
	if p.Rooms[2].Building != 1 {
		t.Fatalf("room B-102 building index = %d", p.Rooms[2].Building)
	}
}

func TestNewProblemSubgroupSizeDefaultsToGroup(t *testing.T) {
	in := smallInput()
	in.Subgroups = nil
	p := mustProblem(t, in)
	if got := p.Lessons[2].Size; got != 25 {
		t.Fatalf("size without subgroup data = %d, want group size 25", got)
	}
}

func TestAvailabilityExpansion(t *testing.T) {
	p := mustProblem(t, smallInput())
	g := p.Grid
	mon1 := g.Cells(slot(domain.Monday, 1))

	// "every" unavailable + "odd" preferred on the same position: most restrictive wins.
	for _, c := range mon1 {
		if got := p.TeacherAvail(0, c); got != Unavailable {
			t.Fatalf("Ivanov cell %d = %v, want Unavailable", c, got)
		}
	}
	if got := p.TeacherAvail(0, g.Cells(slot(domain.Monday, 2))[0]); got != Available {
		t.Fatalf("Ivanov Monday 2 = %v", got)
	}
	if got := p.TeacherAvail(1, mon1[0]); got != Available {
		t.Fatalf("Petrov without exceptions = %v", got)
	}
	if got := p.RoomAvail(2, g.Cells(slot(domain.Friday, 1))[1]); got != Unavailable {
		t.Fatalf("lab Friday 1 even = %v", got)
	}
	if got := p.RoomAvail(0, mon1[0]); got != Available {
		t.Fatalf("hall = %v", got)
	}
}

func TestNewProblemValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{"bad grid", func(in *Input) { in.Grid.Days = 0 }},
		{"duplicate building", func(in *Input) { in.Buildings = append(in.Buildings, in.Buildings[0]) }},
		{"duplicate room", func(in *Input) { in.Rooms = append(in.Rooms, in.Rooms[0]) }},
		{"room unknown building", func(in *Input) { in.Rooms[0].Building = 99 }},
		{"room zero capacity", func(in *Input) { in.Rooms[0].Capacity = 0 }},
		{"room availability outside grid", func(in *Input) {
			in.Rooms[0].Avail = []SlotAvailability{{Slot: slot(domain.Sunday, 1), Status: Unavailable}}
		}},
		{"duplicate teacher", func(in *Input) { in.Teachers = append(in.Teachers, in.Teachers[0]) }},
		{"teacher availability outside grid", func(in *Input) {
			in.Teachers[1].Avail = []SlotAvailability{{Slot: slot(domain.Monday, 9), Status: Unavailable}}
		}},
		{"duplicate group", func(in *Input) { in.Groups = append(in.Groups, in.Groups[0]) }},
		{"negative group size", func(in *Input) { in.Groups[0].Size = -1 }},
		{"subgroup unknown group", func(in *Input) { in.Subgroups[0].Group = 99 }},
		{"subgroup without division", func(in *Input) { in.Subgroups[0].Division = "" }},
		{"duplicate discipline", func(in *Input) { in.Disciplines = append(in.Disciplines, in.Disciplines[0]) }},
		{"duplicate lesson", func(in *Input) { in.Lessons = append(in.Lessons, in.Lessons[0]) }},
		{"lesson unknown teacher", func(in *Input) { in.Lessons[0].Teacher = 99 }},
		{"lesson unknown discipline", func(in *Input) { in.Lessons[0].Discipline = 99 }},
		{"lesson empty audience", func(in *Input) { in.Lessons[0].Audience = nil }},
		{"lesson unknown group", func(in *Input) { in.Lessons[0].Audience = domain.Audience{domain.WholeGroup(99)} }},
		{"lesson subgroup part zero", func(in *Input) {
			in.Lessons[2].Audience = domain.Audience{domain.Subgroup(10, "english", 0)}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := smallInput()
			tt.mutate(&in)
			if _, err := NewProblem(in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}
