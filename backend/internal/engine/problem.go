package engine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
)

// Availability of a teacher or a room in one occupancy cell.
type Availability uint8

// Availability values. Available is the zero value: no restriction, no preference.
const (
	Available Availability = iota
	Preferred
	Undesired
	Unavailable
)

// None marks a missing index (e.g. a lesson without a room).
const None int32 = -1

// Problem is the immutable description of a timetabling instance: resources, lessons and
// their requirements. Entities are addressed by dense int32 indices; the *ID fields keep the
// database identifiers. A Problem is shared read-only by every Schedule and solver worker.
type Problem struct {
	Grid      domain.Grid
	Buildings []Building
	Rooms     []Room
	Teachers  []Teacher
	Groups    []Group
	// Units are the audience atoms: every whole group and every subgroup that appears
	// anywhere in the curriculum.
	Units []Unit
	// Divisions counts distinct (group, division) pairs; Unit.Division indexes them.
	Divisions   int
	Disciplines []Discipline
	Lessons     []Lesson

	// MaxLessonsPerDay limits lessons per day for any group or teacher (H8); 0 = unlimited.
	MaxLessonsPerDay int

	// divisionGroup maps a division index to its group index.
	divisionGroup []int32
	lessonIndex   map[domain.LessonID]int32
	roomIndex     map[domain.RoomID]int32
}

// LessonIndex returns the index of the lesson with the given database ID.
func (p *Problem) LessonIndex(id domain.LessonID) (int32, bool) {
	i, ok := p.lessonIndex[id]
	return i, ok
}

// RoomIndex returns the index of the room with the given database ID.
func (p *Problem) RoomIndex(id domain.RoomID) (int32, bool) {
	i, ok := p.roomIndex[id]
	return i, ok
}

// Building is a campus building (корпус).
type Building struct {
	ID   domain.BuildingID
	Name string
}

// Room is a teaching room.
type Room struct {
	ID       domain.RoomID
	Name     string
	Building int32
	Type     string
	Capacity int
	// Avail has one entry per grid cell.
	Avail []Availability
}

// Teacher is a teacher with per-cell availability.
type Teacher struct {
	ID    domain.TeacherID
	Name  string
	Avail []Availability
}

// Group is a student group.
type Group struct {
	ID   domain.GroupID
	Name string
	Size int
}

// Unit is a whole group (Division == None) or one subgroup of it.
type Unit struct {
	Group    int32
	Division int32
	Part     uint8
	Size     int
}

// Discipline is a subject.
type Discipline struct {
	ID   domain.DisciplineID
	Name string
}

// Lesson is one schedulable occurrence from the curriculum.
type Lesson struct {
	ID         domain.LessonID
	Item       int64 // curriculum item ID; lessons of one item are siblings
	Discipline int32
	Kind       string
	Teacher    int32
	Units      []int32
	Biweekly   bool
	RoomType   string
	// Size is the number of students attending.
	Size int
	// Rooms lists rooms of the required type, regardless of capacity.
	Rooms []int32
}

// DivisionGroup returns the group a division belongs to.
func (p *Problem) DivisionGroup(division int32) int32 {
	return p.divisionGroup[division]
}

// Input describes a problem in terms of database identifiers. NewProblem validates it and
// builds the indexed Problem.
type Input struct {
	Grid             domain.Grid
	MaxLessonsPerDay int
	Buildings        []Building
	Rooms            []RoomInput
	Teachers         []TeacherInput
	Groups           []Group
	Subgroups        []SubgroupInput
	Disciplines      []Discipline
	Lessons          []LessonInput
}

// SlotAvailability is an availability exception for one slot.
type SlotAvailability struct {
	Slot   domain.Slot
	Status Availability
}

// RoomInput describes a room.
type RoomInput struct {
	ID       domain.RoomID
	Name     string
	Building domain.BuildingID
	Type     string
	Capacity int
	Avail    []SlotAvailability
}

// TeacherInput describes a teacher.
type TeacherInput struct {
	ID    domain.TeacherID
	Name  string
	Avail []SlotAvailability
}

// SubgroupInput gives the size of a subgroup.
type SubgroupInput struct {
	Group    domain.GroupID
	Division string
	Part     uint8
	Size     int
}

// LessonInput describes a lesson.
type LessonInput struct {
	ID         domain.LessonID
	Item       int64
	Discipline domain.DisciplineID
	Kind       string
	Teacher    domain.TeacherID
	Audience   domain.Audience
	Biweekly   bool
	RoomType   string
}

// ErrInvalidInput wraps every validation error returned by NewProblem.
var ErrInvalidInput = errors.New("invalid problem input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidInput}, args...)...)
}

// NewProblem validates the input and builds an indexed Problem.
func NewProblem(in Input) (*Problem, error) {
	if err := in.Grid.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	b := &problemBuilder{
		p: &Problem{
			Grid: in.Grid, MaxLessonsPerDay: in.MaxLessonsPerDay,
			lessonIndex: map[domain.LessonID]int32{}, roomIndex: map[domain.RoomID]int32{},
		},
		buildings: map[domain.BuildingID]int32{},
		teachers:  map[domain.TeacherID]int32{},
		groups:    map[domain.GroupID]int32{},
		discs:     map[domain.DisciplineID]int32{},
		units:     map[domain.Member]int32{},
		divisions: map[divisionKey]int32{},
		subSizes:  map[domain.Member]int{},
	}
	steps := []func(Input) error{
		b.addBuildings, b.addRooms, b.addTeachers, b.addGroups, b.addSubgroups, b.addDisciplines, b.addLessons,
	}
	for _, step := range steps {
		if err := step(in); err != nil {
			return nil, err
		}
	}
	return b.p, nil
}

type divisionKey struct {
	group    int32
	division string
}

type problemBuilder struct {
	p         *Problem
	buildings map[domain.BuildingID]int32
	teachers  map[domain.TeacherID]int32
	groups    map[domain.GroupID]int32
	discs     map[domain.DisciplineID]int32
	units     map[domain.Member]int32
	divisions map[divisionKey]int32
	subSizes  map[domain.Member]int
}

func (b *problemBuilder) addBuildings(in Input) error {
	for _, x := range in.Buildings {
		if _, dup := b.buildings[x.ID]; dup {
			return invalid("duplicate building %d", x.ID)
		}
		b.buildings[x.ID] = int32(len(b.p.Buildings))
		b.p.Buildings = append(b.p.Buildings, x)
	}
	return nil
}

func (b *problemBuilder) addRooms(in Input) error {
	for _, x := range in.Rooms {
		if _, dup := b.p.roomIndex[x.ID]; dup {
			return invalid("duplicate room %d", x.ID)
		}
		bi, ok := b.buildings[x.Building]
		if !ok {
			return invalid("room %d: unknown building %d", x.ID, x.Building)
		}
		if x.Capacity <= 0 {
			return invalid("room %d: capacity must be positive", x.ID)
		}
		avail, err := b.cellAvailability(x.Avail)
		if err != nil {
			return invalid("room %d: %v", x.ID, err)
		}
		b.p.roomIndex[x.ID] = int32(len(b.p.Rooms))
		b.p.Rooms = append(b.p.Rooms, Room{
			ID: x.ID, Name: x.Name, Building: bi, Type: x.Type, Capacity: x.Capacity, Avail: avail,
		})
	}
	return nil
}

func (b *problemBuilder) addTeachers(in Input) error {
	for _, x := range in.Teachers {
		if _, dup := b.teachers[x.ID]; dup {
			return invalid("duplicate teacher %d", x.ID)
		}
		avail, err := b.cellAvailability(x.Avail)
		if err != nil {
			return invalid("teacher %d: %v", x.ID, err)
		}
		b.teachers[x.ID] = int32(len(b.p.Teachers))
		b.p.Teachers = append(b.p.Teachers, Teacher{ID: x.ID, Name: x.Name, Avail: avail})
	}
	return nil
}

func (b *problemBuilder) addGroups(in Input) error {
	for _, x := range in.Groups {
		if _, dup := b.groups[x.ID]; dup {
			return invalid("duplicate group %d", x.ID)
		}
		if x.Size < 0 {
			return invalid("group %d: negative size", x.ID)
		}
		b.groups[x.ID] = int32(len(b.p.Groups))
		b.p.Groups = append(b.p.Groups, x)
	}
	return nil
}

func (b *problemBuilder) addSubgroups(in Input) error {
	for _, x := range in.Subgroups {
		if _, ok := b.groups[x.Group]; !ok {
			return invalid("subgroup of unknown group %d", x.Group)
		}
		if x.Division == "" || x.Part == 0 || x.Size < 0 {
			return invalid("subgroup %d/%s/%d: invalid", x.Group, x.Division, x.Part)
		}
		b.subSizes[domain.Subgroup(x.Group, x.Division, x.Part)] = x.Size
	}
	return nil
}

func (b *problemBuilder) addDisciplines(in Input) error {
	for _, x := range in.Disciplines {
		if _, dup := b.discs[x.ID]; dup {
			return invalid("duplicate discipline %d", x.ID)
		}
		b.discs[x.ID] = int32(len(b.p.Disciplines))
		b.p.Disciplines = append(b.p.Disciplines, x)
	}
	return nil
}

func (b *problemBuilder) addLessons(in Input) error {
	roomsByType := map[string][]int32{}
	for i, r := range b.p.Rooms {
		roomsByType[r.Type] = append(roomsByType[r.Type], int32(i))
	}
	for _, x := range in.Lessons {
		if _, dup := b.p.lessonIndex[x.ID]; dup {
			return invalid("duplicate lesson %d", x.ID)
		}
		ti, ok := b.teachers[x.Teacher]
		if !ok {
			return invalid("lesson %d: unknown teacher %d", x.ID, x.Teacher)
		}
		di, ok := b.discs[x.Discipline]
		if !ok {
			return invalid("lesson %d: unknown discipline %d", x.ID, x.Discipline)
		}
		if len(x.Audience) == 0 {
			return invalid("lesson %d: empty audience", x.ID)
		}
		l := Lesson{
			ID: x.ID, Item: x.Item, Discipline: di, Kind: x.Kind, Teacher: ti,
			Biweekly: x.Biweekly, RoomType: x.RoomType, Rooms: roomsByType[x.RoomType],
		}
		for _, m := range x.Audience {
			u, err := b.unit(m)
			if err != nil {
				return invalid("lesson %d: %v", x.ID, err)
			}
			l.Units = append(l.Units, u)
			l.Size += b.p.Units[u].Size
		}
		b.p.lessonIndex[x.ID] = int32(len(b.p.Lessons))
		b.p.Lessons = append(b.p.Lessons, l)
	}
	return nil
}

// unit returns the index of the audience unit for m, creating it on first use.
func (b *problemBuilder) unit(m domain.Member) (int32, error) {
	if u, ok := b.units[m]; ok {
		return u, nil
	}
	gi, ok := b.groups[m.Group]
	if !ok {
		return 0, fmt.Errorf("unknown group %d", m.Group)
	}
	u := Unit{Group: gi, Division: None, Size: b.p.Groups[gi].Size}
	if !m.IsWhole() {
		if m.Part == 0 {
			return 0, fmt.Errorf("group %d division %q: part must be positive", m.Group, m.Division)
		}
		key := divisionKey{gi, m.Division}
		d, ok := b.divisions[key]
		if !ok {
			d = int32(b.p.Divisions)
			b.divisions[key] = d
			b.p.Divisions++
			b.p.divisionGroup = append(b.p.divisionGroup, gi)
		}
		u.Division = d
		u.Part = m.Part
		if size, ok := b.subSizes[m]; ok {
			u.Size = size
		}
	}
	idx := int32(len(b.p.Units))
	b.units[m] = idx
	b.p.Units = append(b.p.Units, u)
	return idx, nil
}

// cellAvailability expands slot exceptions into a per-cell array. When several entries cover
// one cell, the most restrictive wins.
func (b *problemBuilder) cellAvailability(list []SlotAvailability) ([]Availability, error) {
	if len(list) == 0 {
		return nil, nil
	}
	g := b.p.Grid
	out := make([]Availability, g.CellCount())
	sorted := append([]SlotAvailability(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Status < sorted[j].Status })
	for _, a := range sorted {
		if !g.Contains(a.Slot) {
			return nil, fmt.Errorf("availability slot %v outside grid", a.Slot)
		}
		for _, c := range g.Cells(a.Slot) {
			out[c] = max(out[c], a.Status)
		}
	}
	return out, nil
}

// TeacherAvail returns the teacher's availability in a cell.
func (p *Problem) TeacherAvail(t int32, cell int) Availability {
	if a := p.Teachers[t].Avail; a != nil {
		return a[cell]
	}
	return Available
}

// RoomAvail returns the room's availability in a cell.
func (p *Problem) RoomAvail(r int32, cell int) Availability {
	if a := p.Rooms[r].Avail; a != nil {
		return a[cell]
	}
	return Available
}
