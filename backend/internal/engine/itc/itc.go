// Package itc reads ITC-2007 Track 3 (curriculum-based course timetabling) instances and
// solutions and maps them onto the engine model, so published feasible solutions can be used to
// cross-check the hard-constraint checker (arch §19).
//
// Instance format (.ctt): a header of "Key: value" lines (Name, Courses, Rooms, Days,
// Periods_per_day, Curricula, Constraints), then the sections COURSES ("course teacher lectures
// min_days students"), ROOMS ("room capacity"), CURRICULA ("curriculum n course...") and
// UNAVAILABILITY_CONSTRAINTS ("course day period"), terminated by "END.". Solution format: one
// line "course room day period" per lecture. Days and periods are 0-based.
//
// # Mapping to the engine
//
//   - Grid: Days × Periods_per_day, every lesson is weekly (no parity).
//   - Each lecture of a course is a lesson; Item = the course, so engine H8 (siblings on one
//     day) corresponds to the ITC soft constraint MinimumWorkingDays and must be switched off.
//   - Each curriculum is a group; a lesson's audience is the whole groups of every curriculum
//     containing its course, so ITC curriculum conflicts are engine H2. A course in no curriculum
//     gets a private group of its own.
//   - Each ITC teacher is a teacher (ITC teacher conflicts and two lectures of one course in the
//     same period are both engine H1); rooms keep their capacity in a single building and share
//     one room type, so engine H3 is ITC room occupancy and H5 never fires.
//   - Groups have size 0: ITC room capacity is a soft constraint (S1), so engine H6 never fires.
//   - ITC unavailability is per course, while the engine only knows teacher and room
//     availability; it is not mapped (a synthetic per-course teacher would break H1 between
//     courses of the same teacher) and must be checked against Instance.Unavailability directly.
package itc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Course is a course with its number of weekly lectures.
type Course struct {
	ID       string
	Teacher  string
	Lectures int
	MinDays  int
	Students int
}

// Room is a room with its capacity.
type Room struct {
	ID       string
	Capacity int
}

// Curriculum is a set of courses sharing students.
type Curriculum struct {
	ID      string
	Courses []string
}

// Period is one (day, period) position, both 0-based.
type Period struct {
	Day    int
	Period int
}

// Unavailability forbids lectures of a course in one period.
type Unavailability struct {
	Course string
	Period
}

// Instance is a parsed .ctt file.
type Instance struct {
	Name           string
	Days           int
	PeriodsPerDay  int
	Courses        []Course
	Rooms          []Room
	Curricula      []Curriculum
	Unavailability []Unavailability
}

// Assignment is one line of a solution: a lecture of the course in the room and period.
type Assignment struct {
	Course string
	Room   string
	Period
}

// ErrFormat wraps every parse error.
var ErrFormat = errors.New("itc: bad format")

func formatErr(line int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", ErrFormat, line, fmt.Sprintf(format, args...))
}

// ParseInstance reads a .ctt instance.
func ParseInstance(r io.Reader) (*Instance, error) {
	inst := &Instance{}
	header := map[string]int{}
	section := ""
	sc := bufio.NewScanner(r)
	n := 0
	ended := false
	for sc.Scan() {
		n++
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if ended {
			return nil, formatErr(n, "data after END.")
		}
		switch {
		case f[0] == "END.":
			ended = true
			continue
		case len(f) == 1 && strings.HasSuffix(f[0], ":"):
			section = strings.TrimSuffix(f[0], ":")
			continue
		case section == "" && strings.HasSuffix(f[0], ":") && len(f) == 2:
			key := strings.TrimSuffix(f[0], ":")
			if key == "Name" {
				inst.Name = f[1]
				continue
			}
			v, err := strconv.Atoi(f[1])
			if err != nil {
				return nil, formatErr(n, "header %s: %v", key, err)
			}
			header[key] = v
			continue
		}
		if err := inst.parseLine(section, f); err != nil {
			return nil, formatErr(n, "%s: %v", section, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !ended {
		return nil, formatErr(n, "missing END.")
	}
	inst.Days, inst.PeriodsPerDay = header["Days"], header["Periods_per_day"]
	counts := []struct {
		key string
		got int
	}{
		{"Courses", len(inst.Courses)}, {"Rooms", len(inst.Rooms)}, {"Curricula", len(inst.Curricula)},
		{"Constraints", len(inst.Unavailability)},
	}
	for _, c := range counts {
		if header[c.key] != c.got {
			return nil, formatErr(n, "header %s = %d, found %d", c.key, header[c.key], c.got)
		}
	}
	return inst, inst.validate()
}

func (inst *Instance) parseLine(section string, f []string) error {
	switch section {
	case "COURSES":
		v, err := ints(f, 2, 5)
		if err != nil {
			return err
		}
		inst.Courses = append(inst.Courses, Course{ID: f[0], Teacher: f[1], Lectures: v[0], MinDays: v[1], Students: v[2]})
	case "ROOMS":
		v, err := ints(f, 1, 2)
		if err != nil {
			return err
		}
		inst.Rooms = append(inst.Rooms, Room{ID: f[0], Capacity: v[0]})
	case "CURRICULA":
		v, err := ints(f[:min(len(f), 2)], 1, 2)
		if err != nil {
			return err
		}
		if len(f) != 2+v[0] {
			return fmt.Errorf("curriculum %s: %d courses declared, %d listed", f[0], v[0], len(f)-2)
		}
		inst.Curricula = append(inst.Curricula, Curriculum{ID: f[0], Courses: slices.Clone(f[2:])})
	case "UNAVAILABILITY_CONSTRAINTS":
		v, err := ints(f, 1, 3)
		if err != nil {
			return err
		}
		inst.Unavailability = append(inst.Unavailability, Unavailability{Course: f[0], Period: Period{v[0], v[1]}})
	default:
		return fmt.Errorf("unexpected line %q", strings.Join(f, " "))
	}
	return nil
}

// ints parses f[from:] as integers and checks that the line has exactly n fields.
func ints(f []string, from, n int) ([]int, error) {
	if len(f) != n {
		return nil, fmt.Errorf("want %d fields, got %d", n, len(f))
	}
	out := make([]int, 0, n-from)
	for _, s := range f[from:] {
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// validate checks cross references.
func (inst *Instance) validate() error {
	if inst.Days <= 0 || inst.PeriodsPerDay <= 0 {
		return fmt.Errorf("%w: grid %d×%d", ErrFormat, inst.Days, inst.PeriodsPerDay)
	}
	courses := map[string]bool{}
	for _, c := range inst.Courses {
		if courses[c.ID] {
			return fmt.Errorf("%w: duplicate course %s", ErrFormat, c.ID)
		}
		courses[c.ID] = true
	}
	for _, q := range inst.Curricula {
		for _, c := range q.Courses {
			if !courses[c] {
				return fmt.Errorf("%w: curriculum %s: unknown course %s", ErrFormat, q.ID, c)
			}
		}
	}
	for _, u := range inst.Unavailability {
		if !courses[u.Course] || !inst.contains(u.Period) {
			return fmt.Errorf("%w: bad unavailability %+v", ErrFormat, u)
		}
	}
	return nil
}

func (inst *Instance) contains(p Period) bool {
	return p.Day >= 0 && p.Day < inst.Days && p.Period >= 0 && p.Period < inst.PeriodsPerDay
}

// ParseSolution reads a solution: one "course room day period" line per lecture.
func ParseSolution(r io.Reader) ([]Assignment, error) {
	var out []Assignment
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		v, err := ints(f, 2, 4)
		if err != nil {
			return nil, formatErr(n, "%v", err)
		}
		out = append(out, Assignment{Course: f[0], Room: f[1], Period: Period{v[0], v[1]}})
	}
	return out, sc.Err()
}

// Model is an instance mapped onto the engine (see the package documentation).
type Model struct {
	Instance *Instance
	Input    engine.Input
	// Lectures lists the lesson IDs of each course, in lecture order.
	Lectures map[string][]domain.LessonID
	// RoomIDs maps ITC room names to engine room IDs.
	RoomIDs map[string]domain.RoomID
}

// RoomType is the single room type of mapped instances.
const RoomType = "any"

// NewModel maps the instance onto an engine input.
func NewModel(inst *Instance) *Model {
	m := &Model{Instance: inst, Lectures: map[string][]domain.LessonID{}, RoomIDs: map[string]domain.RoomID{}}
	in := engine.Input{
		Grid:      domain.Grid{Days: uint8(inst.Days), PeriodsPerDay: uint8(inst.PeriodsPerDay)},
		Buildings: []engine.Building{{ID: 1, Name: "ITC"}},
	}
	for i, r := range inst.Rooms {
		id := domain.RoomID(i + 1)
		m.RoomIDs[r.ID] = id
		in.Rooms = append(in.Rooms, engine.RoomInput{ID: id, Name: r.ID, Building: 1, Type: RoomType, Capacity: max(r.Capacity, 1)})
	}
	teachers := map[string]domain.TeacherID{}
	for _, c := range inst.Courses {
		if _, ok := teachers[c.Teacher]; !ok {
			teachers[c.Teacher] = domain.TeacherID(len(teachers) + 1)
			in.Teachers = append(in.Teachers, engine.TeacherInput{ID: teachers[c.Teacher], Name: c.Teacher})
		}
	}
	audience := map[string]domain.Audience{}
	for i, q := range inst.Curricula {
		g := domain.GroupID(i + 1)
		in.Groups = append(in.Groups, engine.Group{ID: g, Name: q.ID})
		for _, c := range q.Courses {
			if !slices.Contains(audience[c], domain.WholeGroup(g)) {
				audience[c] = append(audience[c], domain.WholeGroup(g))
			}
		}
	}
	for ci, c := range inst.Courses {
		if len(audience[c.ID]) == 0 {
			g := domain.GroupID(len(in.Groups) + 1)
			in.Groups = append(in.Groups, engine.Group{ID: g, Name: "course " + c.ID})
			audience[c.ID] = domain.Audience{domain.WholeGroup(g)}
		}
		d := domain.DisciplineID(ci + 1)
		in.Disciplines = append(in.Disciplines, engine.Discipline{ID: d, Name: c.ID})
		for range c.Lectures {
			id := domain.LessonID(len(in.Lessons) + 1)
			m.Lectures[c.ID] = append(m.Lectures[c.ID], id)
			in.Lessons = append(in.Lessons, engine.LessonInput{
				ID: id, Item: int64(ci + 1), Discipline: d, Kind: "lecture", Teacher: teachers[c.Teacher],
				Audience: audience[c.ID], RoomType: RoomType,
			})
		}
	}
	m.Input = in
	return m
}

// Slot converts an ITC period to an engine slot.
func Slot(p Period) domain.Slot {
	return domain.Slot{Day: domain.Day(p.Day), Period: uint8(p.Period + 1)}
}

// Schedule places a solution onto a schedule of problem p, built from m.Input. The i-th line of
// a course goes to its i-th lecture. It returns the lesson index of every solution line.
func (m *Model) Schedule(p *engine.Problem, sol []Assignment) (*engine.Schedule, []int32, error) {
	s := engine.NewSchedule(p)
	next := map[string]int{}
	lessons := make([]int32, len(sol))
	for i, a := range sol {
		ids := m.Lectures[a.Course]
		k := next[a.Course]
		if k >= len(ids) {
			return nil, nil, fmt.Errorf("itc: course %q: too many lectures in the solution", a.Course)
		}
		next[a.Course]++
		roomID, ok := m.RoomIDs[a.Room]
		if !ok {
			return nil, nil, fmt.Errorf("itc: unknown room %q", a.Room)
		}
		l, _ := p.LessonIndex(ids[k])
		room, _ := p.RoomIndex(roomID)
		if err := s.Place(l, Slot(a.Period), room); err != nil {
			return nil, nil, fmt.Errorf("itc: line %d: %w", i+1, err)
		}
		lessons[i] = l
	}
	return s, lessons, nil
}
