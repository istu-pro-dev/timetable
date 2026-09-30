// Package seed defines the demo dataset (one faculty) and loads it into the database through
// the store layer. The dataset is built deterministically: the same code always produces the
// same rows. See docs/dev/seed.md.
package seed

import (
	"fmt"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Division names used for subgroups.
const (
	DivisionEnglish = "english"
	DivisionLab     = "lab"
)

// Room type codes.
const (
	RoomLecture  = "lecture"
	RoomSeminar  = "seminar"
	RoomLab      = "lab"
	RoomComputer = "computer"
	RoomGym      = "gym"
)

// Dataset is the whole demo dataset. Entities refer to each other by slice index; the loader maps
// indices to database IDs.
type Dataset struct {
	Days          int
	PeriodsPerDay int
	Periods       []Period
	RoomTypes     []RoomType
	Buildings     []Building
	Rooms         []Room
	Groups        []Group
	Subgroups     []Subgroup
	Teachers      []Teacher
	Disciplines   []string
	TeacherAvail  []TeacherAvailability
	RoomAvail     []RoomAvailability
	Items         []Item
}

// Period is one pair: start and end as minutes since midnight.
type Period struct {
	Number     int
	Start, End int // minutes since midnight
}

// RoomType is a room type code and its display name.
type RoomType struct{ Code, Name string }

// Building is a campus building.
type Building struct{ Name, Address string }

// Room belongs to Buildings[Building].
type Room struct {
	Building int
	Name     string
	Type     string
	Capacity int
}

// Group is a student group.
type Group struct {
	Name   string
	Course int
	Size   int
}

// Subgroup is part Part of division Division of Groups[Group].
type Subgroup struct {
	Group    int
	Division string
	Part     int
	Size     int
}

// Teacher is a teacher. MaxLoad caps the weekly load (pairs, biweekly = 0.5) the generator gives
// them; Dept is only used to pick who teaches what.
type Teacher struct {
	FullName  string
	ShortName string
	Dept      string
	MaxLoad   float64
}

// TeacherAvailability is an exception for Teachers[Teacher] at (Day, Period, Parity).
type TeacherAvailability struct {
	Teacher int
	Day     int
	Period  int
	Parity  db.Parity
	Status  db.Availability
}

// RoomAvailability is an exception for Rooms[Room] at (Day, Period, Parity).
type RoomAvailability struct {
	Room   int
	Day    int
	Period int
	Parity db.Parity
	Status db.Availability
}

// Member is one audience member: a whole group (Division == "") or a subgroup.
type Member struct {
	Group    int
	Division string
	Part     int
}

// Item is a curriculum item: Weekly lessons every week plus Biweekly lessons every other week.
type Item struct {
	Discipline int
	Kind       db.LessonKind
	Teacher    int
	RoomType   string
	Weekly     int
	Biweekly   int
	Audience   []Member
}

// Load is the weekly load of an item in pairs, counting a biweekly lesson as half a pair.
func (it Item) Load() float64 { return float64(it.Weekly) + 0.5*float64(it.Biweekly) }

// Lessons is the number of lesson rows generated from the item.
func (it Item) Lessons() int { return it.Weekly + it.Biweekly }

// LessonCount is the total number of lessons generated from the curriculum.
func (d *Dataset) LessonCount() int {
	n := 0
	for _, it := range d.Items {
		n += it.Lessons()
	}
	return n
}

// MemberSize is the number of students in an audience member.
func (d *Dataset) MemberSize(m Member) int {
	if m.Division == "" {
		return d.Groups[m.Group].Size
	}
	for _, sg := range d.Subgroups {
		if sg.Group == m.Group && sg.Division == m.Division && sg.Part == m.Part {
			return sg.Size
		}
	}
	return -1
}

// AudienceSize is the number of students attending an item.
func (d *Dataset) AudienceSize(it Item) int {
	n := 0
	for _, m := range it.Audience {
		n += d.MemberSize(m)
	}
	return n
}

// Build returns the demo dataset. It is deterministic.
func Build() *Dataset {
	d := &Dataset{Days: 6, PeriodsPerDay: 7}
	b := builder{d: d, discIdx: map[string]int{}, teacherLoad: map[int]float64{}}
	b.timeGrid()
	b.rooms()
	b.groups()
	b.teachers()
	b.curriculum()
	b.availability()
	return d
}

type builder struct {
	d           *Dataset
	discIdx     map[string]int
	teacherLoad map[int]float64
	// groupsByCourse[course-1][program] lists group indices.
	groupsByCourse [4]map[string][]int
}

func (b *builder) timeGrid() {
	starts := []string{"08:00", "09:40", "11:20", "13:20", "15:00", "16:40", "18:20"}
	for i, s := range starts {
		var h, m int
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
			panic(err)
		}
		start := h*60 + m
		b.d.Periods = append(b.d.Periods, Period{Number: i + 1, Start: start, End: start + 90})
	}
}

func (b *builder) rooms() {
	d := b.d
	d.RoomTypes = []RoomType{
		{RoomLecture, "Лекционная аудитория"},
		{RoomSeminar, "Аудитория для практических занятий"},
		{RoomLab, "Лаборатория"},
		{RoomComputer, "Компьютерный класс"},
		{RoomGym, "Спортивный зал"},
	}
	d.Buildings = []Building{
		{"Корпус А", "ул. Университетская, 1"},
		{"Корпус Б", "ул. Университетская, 3"},
		{"Корпус В", "ул. Спортивная, 7"},
	}
	add := func(building int, name, typ string, capacity int) {
		d.Rooms = append(d.Rooms, Room{Building: building, Name: name, Type: typ, Capacity: capacity})
	}
	add(0, "А-101", RoomLecture, 150)
	add(0, "А-201", RoomLecture, 120)
	add(0, "А-301", RoomLecture, 100)
	add(0, "А-401", RoomLecture, 80)
	add(0, "А-203", RoomSeminar, 32)
	add(0, "А-204", RoomSeminar, 32)
	add(0, "А-205", RoomSeminar, 30)
	add(0, "А-206", RoomSeminar, 30)
	add(0, "А-302", RoomSeminar, 30)
	add(0, "А-303", RoomSeminar, 28)
	add(0, "А-304", RoomSeminar, 26)
	add(0, "А-402", RoomSeminar, 24)
	add(0, "А-403", RoomSeminar, 16)
	add(1, "Б-101", RoomComputer, 30)
	add(1, "Б-102", RoomComputer, 30)
	add(1, "Б-103", RoomComputer, 25)
	add(1, "Б-104", RoomComputer, 25)
	add(1, "Б-105", RoomComputer, 16)
	add(1, "Б-106", RoomComputer, 16)
	add(1, "Б-201", RoomLab, 25)
	add(1, "Б-202", RoomLab, 16)
	add(1, "Б-203", RoomLab, 14)
	add(2, "В-110", RoomLab, 25)
	add(2, "В-112", RoomLab, 16)
	add(2, "Спортзал", RoomGym, 60)
}

// Programs of the faculty. ИВТ groups are large and split for labs; ПИ and ИСТ groups do labs
// as a whole group.
const (
	progIVT = "ИВТб"
	progPI  = "ПИб"
	progIST = "ИСТб"
)

var programs = []string{progIVT, progPI, progIST}

// groupSizes[course-1] lists sizes of ИВТ-1, ИВТ-2, ПИ-1, ПИ-2, ИСТ-1.
var groupSizes = [4][5]int{
	{30, 28, 25, 24, 22},
	{28, 27, 24, 22, 20},
	{27, 26, 23, 21, 19},
	{26, 24, 22, 20, 18},
}

func (b *builder) groups() {
	d := b.d
	for c := 1; c <= 4; c++ {
		year := 27 - c // admission year of course c in the 2026/27 academic year
		byProg := map[string][]int{}
		layout := []struct {
			prog string
			n    int
		}{{progIVT, 1}, {progIVT, 2}, {progPI, 1}, {progPI, 2}, {progIST, 1}}
		for i, l := range layout {
			size := groupSizes[c-1][i]
			g := len(d.Groups)
			d.Groups = append(d.Groups, Group{
				Name:   fmt.Sprintf("%s-%d-%d", l.prog, year, l.n),
				Course: c,
				Size:   size,
			})
			byProg[l.prog] = append(byProg[l.prog], g)
			b.split(g, DivisionEnglish)
			if l.prog == progIVT {
				b.split(g, DivisionLab)
			}
		}
		b.groupsByCourse[c-1] = byProg
	}
}

func (b *builder) split(g int, division string) {
	size := b.d.Groups[g].Size
	first := (size + 1) / 2
	b.d.Subgroups = append(b.d.Subgroups,
		Subgroup{Group: g, Division: division, Part: 1, Size: first},
		Subgroup{Group: g, Division: division, Part: 2, Size: size - first},
	)
}

// Departments.
const (
	deptMath = "math"
	deptPhys = "phys"
	deptCS   = "cs"
	deptHW   = "hw"
	deptHum  = "hum"
	deptEng  = "eng"
	deptPE   = "pe"
)

// Part-time teachers get a lower load cap and are available two days a week.
const partTimeLoad = 6

var teacherList = []struct {
	name, dept string
	partTime   bool
}{
	{"Иванов Иван Петрович", deptMath, false},
	{"Смирнова Ольга Викторовна", deptMath, false},
	{"Кузнецов Алексей Николаевич", deptMath, false},
	{"Попова Марина Сергеевна", deptMath, false},
	{"Васильев Дмитрий Андреевич", deptMath, false},
	{"Петрова Елена Александровна", deptMath, false},
	{"Соколов Михаил Юрьевич", deptMath, false},
	{"Михайлов Сергей Владимирович", deptPhys, false},
	{"Новикова Татьяна Игоревна", deptPhys, false},
	{"Фёдоров Павел Олегович", deptPhys, false},
	{"Морозов Андрей Васильевич", deptCS, false},
	{"Волкова Анна Дмитриевна", deptCS, false},
	{"Алексеев Николай Сергеевич", deptCS, false},
	{"Лебедева Ирина Павловна", deptCS, false},
	{"Семёнов Виктор Алексеевич", deptCS, false},
	{"Егорова Наталья Михайловна", deptCS, false},
	{"Павлов Артём Игоревич", deptCS, false},
	{"Козлова Светлана Николаевна", deptCS, false},
	{"Степанов Роман Евгеньевич", deptCS, false},
	{"Николаева Юлия Андреевна", deptCS, false},
	{"Орлов Константин Викторович", deptCS, false},
	{"Андреев Евгений Павлович", deptCS, true},
	{"Макаров Георгий Борисович", deptHW, false},
	{"Никитина Людмила Васильевна", deptHW, false},
	{"Захаров Олег Станиславович", deptHW, false},
	{"Зайцева Вера Геннадьевна", deptHW, false},
	{"Соловьёв Игорь Анатольевич", deptHum, false},
	{"Борисова Галина Петровна", deptHum, false},
	{"Яковлев Владимир Ильич", deptHum, false},
	{"Григорьева Надежда Олеговна", deptHum, false},
	{"Романов Кирилл Максимович", deptHum, false},
	{"Воробьёва Лариса Юрьевна", deptHum, true},
	{"Сергеева Екатерина Витальевна", deptEng, false},
	{"Кузьмина Дарья Александровна", deptEng, false},
	{"Фролова Ксения Сергеевна", deptEng, false},
	{"Александрова Маргарита Ивановна", deptEng, false},
	{"Дмитриева Полина Андреевна", deptEng, true},
	{"Королёв Станислав Игоревич", deptPE, false},
	{"Гусев Антон Валерьевич", deptPE, false},
	{"Киселёва Алина Романовна", deptPE, false},
}

// ShortName turns "Иванов Иван Петрович" into "Иванов И.П.".
func ShortName(full string) string {
	parts := strings.Fields(full)
	var sb strings.Builder
	sb.WriteString(parts[0])
	if len(parts) > 1 {
		sb.WriteByte(' ')
	}
	for _, p := range parts[1:] {
		r := []rune(p)
		sb.WriteRune(r[0])
		sb.WriteByte('.')
	}
	return sb.String()
}

func (b *builder) teachers() {
	for _, t := range teacherList {
		maxLoad := 18.0
		if t.partTime {
			maxLoad = partTimeLoad
		}
		b.d.Teachers = append(b.d.Teachers, Teacher{
			FullName:  t.name,
			ShortName: ShortName(t.name),
			Dept:      t.dept,
			MaxLoad:   maxLoad,
		})
	}
}

// availability adds teacher and room exceptions (edge cases, see docs/dev/seed.md).
func (b *builder) availability() {
	d := b.d
	teacher := func(name string) int {
		for i, t := range d.Teachers {
			if t.FullName == name {
				return i
			}
		}
		panic("unknown teacher " + name)
	}
	room := func(name string) int {
		for i, r := range d.Rooms {
			if r.Name == name {
				return i
			}
		}
		panic("unknown room " + name)
	}
	addT := func(t, day, period int, parity db.Parity, status db.Availability) {
		d.TeacherAvail = append(d.TeacherAvail, TeacherAvailability{t, day, period, parity, status})
	}

	// Part-time teachers: available on two days only.
	for _, pt := range PartTimeDays {
		t := teacher(pt.Teacher)
		for day := 0; day < d.Days; day++ {
			if day == pt.Days[0] || day == pt.Days[1] {
				continue
			}
			for p := 1; p <= d.PeriodsPerDay; p++ {
				addT(t, day, p, db.ParityEvery, db.AvailabilityUnavailable)
			}
		}
	}
	// Early birds dislike the first pair.
	for _, name := range []string{"Иванов Иван Петрович", "Волкова Анна Дмитриевна"} {
		t := teacher(name)
		for day := 0; day < d.Days; day++ {
			addT(t, day, 1, db.ParityEvery, db.AvailabilityUndesired)
		}
	}
	// No Saturdays, please.
	t := teacher("Соловьёв Игорь Анатольевич")
	for p := 1; p <= d.PeriodsPerDay; p++ {
		addT(t, 5, p, db.ParityEvery, db.AvailabilityUndesired)
	}
	// Prefers Tuesday and Thursday mornings.
	t = teacher("Морозов Андрей Васильевич")
	for _, day := range []int{1, 3} {
		for p := 2; p <= 3; p++ {
			addT(t, day, p, db.ParityEvery, db.AvailabilityPreferred)
		}
	}
	// Busy on odd Friday afternoons (parity-specific exception).
	t = teacher("Лебедева Ирина Павловна")
	for p := 5; p <= 7; p++ {
		addT(t, 4, p, db.ParityOdd, db.AvailabilityUnavailable)
	}

	// Room А-206 is closed on Wednesdays; evening pairs in the gym are undesired.
	r := room("А-206")
	for p := 1; p <= d.PeriodsPerDay; p++ {
		d.RoomAvail = append(d.RoomAvail, RoomAvailability{r, 2, p, db.ParityEvery, db.AvailabilityUnavailable})
	}
	r = room("Спортзал")
	for p := 6; p <= 7; p++ {
		d.RoomAvail = append(d.RoomAvail, RoomAvailability{r, 5, p, db.ParityEvery, db.AvailabilityUndesired})
	}
}

// PartTimeDays lists part-time teachers and the two days (Monday = 0) they are available.
var PartTimeDays = []struct {
	Teacher string
	Days    [2]int
}{
	{"Андреев Евгений Павлович", [2]int{0, 3}},
	{"Воробьёва Лариса Юрьевна", [2]int{1, 4}},
	{"Дмитриева Полина Андреевна", [2]int{2, 5}},
}

// pickTeacher returns the least loaded teacher of dept that can take load more pairs.
// Ties go to the lower index, so the result is deterministic.
func (b *builder) pickTeacher(dept string, load float64) int {
	best := -1
	for i, t := range b.d.Teachers {
		if t.Dept != dept || b.teacherLoad[i]+load > t.MaxLoad {
			continue
		}
		if best < 0 || b.teacherLoad[i] < b.teacherLoad[best] {
			best = i
		}
	}
	if best < 0 {
		panic(fmt.Sprintf("seed: department %s has no teacher for %.1f more pairs", dept, load))
	}
	b.teacherLoad[best] += load
	return best
}

func (b *builder) discipline(name string) int {
	if i, ok := b.discIdx[name]; ok {
		return i
	}
	b.d.Disciplines = append(b.d.Disciplines, name)
	b.discIdx[name] = len(b.d.Disciplines) - 1
	return b.discIdx[name]
}

func (b *builder) addItem(disc int, kind db.LessonKind, teacher int, roomType string, n count, aud []Member) {
	b.d.Items = append(b.d.Items, Item{
		Discipline: disc,
		Kind:       kind,
		Teacher:    teacher,
		RoomType:   roomType,
		Weekly:     n.w,
		Biweekly:   n.bw,
		Audience:   aud,
	})
}
