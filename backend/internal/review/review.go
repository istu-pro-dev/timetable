// Package review evaluates an observed (already published) timetable: it builds an engine
// problem from a ttx batch, places every lesson as pinned and runs the same hard and soft
// checks the solver uses. Nothing is stored; the result is a report.
package review

import (
	"cmp"
	"fmt"
	"math/bits"
	"slices"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
)

// Report is the result of a review.
type Report struct {
	Source     ttx.Source    `json:"source"`
	Periods    []ttx.Period  `json:"periods"`
	Stats      Stats         `json:"stats"`
	Lessons    []ttx.Lesson  `json:"lessons"`
	Rooms      []ttx.Room    `json:"rooms"`
	Violations []Violation   `json:"violations"`
	Soft       []Criterion   `json:"soft"`
	SoftTotal  float64       `json:"soft_total"`
	Groups     []EntityScore `json:"groups"`
	Teachers   []EntityScore `json:"teachers"`
	Issues     []ttx.Issue   `json:"issues"`
	Notes      []string      `json:"notes"`
}

// Stats counts what the batch contains.
type Stats struct {
	Lessons     int `json:"lessons"`
	Groups      int `json:"groups"`
	Teachers    int `json:"teachers"`
	Rooms       int `json:"rooms"`
	Disciplines int `json:"disciplines"`
	Hard        int `json:"hard"`
}

// Violation is a broken hard rule with lessons given by their ttx IDs.
type Violation struct {
	Rule     string `json:"rule"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Lessons  []int  `json:"lesson_ids"`
	Resource *Named `json:"resource,omitempty"`
	Day      string `json:"day,omitempty"`
	Period   int    `json:"period,omitempty"`
}

// Named identifies a teacher, group or room by its key.
type Named struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Criterion is one soft criterion of the whole timetable.
type Criterion struct {
	Key       string  `json:"key"`
	Title     string  `json:"title"`
	Value     int     `json:"value"`
	Weight    float64 `json:"weight"`
	Weighted  float64 `json:"weighted"`
	Available bool    `json:"available"`
}

// EntityScore summarises one group or teacher.
type EntityScore struct {
	Name        string `json:"name"`
	Lessons     int    `json:"lessons"` // per two-week cycle
	Gaps        int    `json:"gaps"`
	Transitions int    `json:"transitions"`
	MaxPerDay   int    `json:"max_per_day"`
	Conflicts   int    `json:"conflicts"`
}

var ruleTitles = map[hard.Rule]string{
	hard.H1TeacherBusy: "Накладка преподавателя",
	hard.H2GroupBusy:   "Накладка группы",
	hard.H3RoomBusy:    "Накладка аудитории",
	hard.H6Capacity:    "Не хватает мест",
}

var criterionTitles = map[soft.Criterion]string{
	soft.Gaps:                  "Окна",
	soft.BuildingTransitions:   "Переходы между корпусами",
	soft.TeacherCompactness:    "Учебные дни преподавателей",
	soft.MinWorkingDays:        "Повторы дисциплины в один день",
	soft.CurriculumCompactness: "Одиночные пары в дне",
	soft.RoomStability:         "Смена аудиторий у дисциплины",
	soft.CapacityWaste:         "Пустые места (десятки)",
	soft.EdgeSlots:             "Первая и последняя пара",
	soft.RoomCount:             "Задействовано аудиторий",
	soft.Preferences:           "Нежелательные слоты",
}

// subgroupDivision is the division name for subgroups numbered by position in the source.
const subgroupDivision = "source"

type model struct {
	b          *ttx.Batch
	in         engine.Input
	lessonRoom map[int]domain.RoomID // ttx lesson ID → room, 0 for none
	roomName   map[domain.RoomID]string
	groupID    map[string]domain.GroupID
	teacherID  map[string]domain.TeacherID
	stand      map[domain.TeacherID]bool // stand-in teachers for lessons without one
	capacityOK bool
}

// Analyze evaluates the batch.
func Analyze(b *ttx.Batch) (*Report, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	m, err := build(b)
	if err != nil {
		return nil, err
	}
	p, err := engine.NewProblem(m.in)
	if err != nil {
		return nil, err
	}
	s := engine.NewSchedule(p)
	for _, l := range b.Lessons {
		li, _ := p.LessonIndex(domain.LessonID(l.ID))
		room := engine.None
		if rid := m.lessonRoom[l.ID]; rid != 0 {
			room, _ = p.RoomIndex(rid)
		}
		if err := s.Place(li, slotOf(l), room); err != nil {
			return nil, fmt.Errorf("lesson %d: %w", l.ID, err)
		}
		s.SetPinned(li, true)
	}

	pol := hard.Policy{
		hard.H4Availability: hard.Off, hard.H5RoomType: hard.Off, hard.H7Completeness: hard.Off, hard.H8SameDay: hard.Off,
	}
	if !m.capacityOK {
		pol[hard.H6Capacity] = hard.Off
	}
	vs := hard.Check(s, pol)
	r := &Report{
		Source: b.Source, Periods: b.Periods, Lessons: b.Lessons, Rooms: b.Rooms, Issues: b.Issues,
		Stats: Stats{
			Lessons: len(b.Lessons), Groups: len(b.Groups), Teachers: len(b.Teachers), Rooms: len(b.Rooms),
			Disciplines: len(m.in.Disciplines), Hard: len(vs),
		},
	}
	r.Violations = m.violations(p, vs)
	r.softCriteria(m, p, s)
	r.scores(m, p, s, vs)
	r.Notes = m.notes()
	if r.Issues == nil {
		r.Issues = []ttx.Issue{}
	}
	return r, nil
}

func slotOf(l ttx.Lesson) domain.Slot {
	par := map[string]domain.Parity{"every": domain.EveryWeek, "odd": domain.OddWeek, "even": domain.EvenWeek}[l.Parity]
	return domain.Slot{Day: domain.Day(slices.Index(ttx.Days, l.Day)), Period: uint8(l.Period), Parity: par}
}

func build(b *ttx.Batch) (*model, error) {
	m := &model{
		b: b, lessonRoom: map[int]domain.RoomID{}, roomName: map[domain.RoomID]string{},
		groupID: map[string]domain.GroupID{}, teacherID: map[string]domain.TeacherID{},
		stand: map[domain.TeacherID]bool{}, capacityOK: len(b.Rooms) > 0,
	}
	days, periods := 6, len(b.Periods)
	for _, l := range b.Lessons {
		days = max(days, slices.Index(ttx.Days, l.Day)+1)
		periods = max(periods, l.Period)
	}
	if periods > 16 {
		return nil, fmt.Errorf("%w: more than 16 periods per day", ttx.ErrFormat)
	}
	m.in.Grid = domain.Grid{Days: uint8(days), PeriodsPerDay: uint8(periods)}

	for i, g := range b.Groups {
		size := 0
		if g.Size != nil {
			size = max(*g.Size, 0)
		} else {
			m.capacityOK = false
		}
		id := domain.GroupID(i + 1)
		m.groupID[g.Key] = id
		m.in.Groups = append(m.in.Groups, engine.Group{ID: id, Name: g.Key, Size: size})
	}
	for _, t := range b.Teachers {
		m.teacher(t.Key)
	}

	buildings := map[string]domain.BuildingID{}
	building := func(name string) domain.BuildingID {
		if name == "" {
			name = "?"
		}
		if id, ok := buildings[name]; ok {
			return id
		}
		id := domain.BuildingID(len(buildings) + 1)
		buildings[name] = id
		m.in.Buildings = append(m.in.Buildings, engine.Building{ID: id, Name: name})
		return id
	}
	rooms := map[string]ttx.Room{}
	roomID := map[string]domain.RoomID{}
	addRoom := func(r ttx.Room, name string) domain.RoomID {
		capacity := 1 // unknown capacity: H6 is off then, and 1 seat keeps CapacityWaste at zero
		if r.Capacity != nil && m.capacityOK {
			capacity = max(*r.Capacity, 1)
		}
		id := domain.RoomID(len(m.in.Rooms) + 1)
		m.roomName[id] = name
		m.in.Rooms = append(m.in.Rooms, engine.RoomInput{
			ID: id, Name: name, Building: building(r.Building), Type: r.Type, Capacity: capacity,
		})
		return id
	}
	for _, r := range b.Rooms {
		rooms[r.Key] = r
		if r.Capacity == nil {
			m.capacityOK = false
		}
	}
	for _, r := range b.Rooms {
		if !shared(r) {
			roomID[r.Key] = addRoom(r, r.Key)
		}
	}

	discs := map[string]domain.DisciplineID{}
	items := map[string]int64{}
	for _, l := range b.Lessons {
		d, ok := discs[l.Discipline]
		if !ok {
			d = domain.DisciplineID(len(discs) + 1)
			discs[l.Discipline] = d
			m.in.Disciplines = append(m.in.Disciplines, engine.Discipline{ID: d, Name: l.Discipline})
		}
		var aud domain.Audience
		var audKey []string
		for _, mem := range l.Audience {
			g := m.groupID[mem.Group]
			if mem.Subgroup == nil {
				aud = append(aud, domain.WholeGroup(g))
				audKey = append(audKey, mem.Group)
			} else {
				aud = append(aud, domain.Subgroup(g, subgroupDivision, uint8(*mem.Subgroup)))
				audKey = append(audKey, fmt.Sprintf("%s/%d", mem.Group, *mem.Subgroup))
			}
		}
		itemKey := strings.Join([]string{l.Discipline, l.LessonType, strings.Join(audKey, ",")}, "|")
		item, ok := items[itemKey]
		if !ok {
			item = int64(len(items) + 1)
			items[itemKey] = item
		}

		var teacher domain.TeacherID
		switch len(l.Teachers) {
		case 0:
			teacher = m.teacher(fmt.Sprintf("(не указан, занятие %d)", l.ID))
			m.stand[teacher] = true
		case 1:
			teacher = m.teacherID[l.Teachers[0]]
		default:
			teacher = m.teacher(strings.Join(l.Teachers, ", "))
		}

		roomType := ""
		if l.Room != "" {
			r := rooms[l.Room]
			roomType = r.Type
			if shared(r) {
				// A shared space (sports complex) hosts many lessons at once: give each lesson
				// its own copy so that only building transitions count.
				m.lessonRoom[l.ID] = addRoom(r, r.Key)
			} else {
				m.lessonRoom[l.ID] = roomID[l.Room]
			}
		}
		m.in.Lessons = append(m.in.Lessons, engine.LessonInput{
			ID: domain.LessonID(l.ID), Item: item, Discipline: d, Kind: l.LessonType, Teacher: teacher,
			Audience: aud, Biweekly: l.Parity != "every", RoomType: roomType,
		})
	}
	return m, nil
}

// shared reports whether a room is not an exclusive resource.
func shared(r ttx.Room) bool { return r.Type == "sport" }

func (m *model) teacher(name string) domain.TeacherID {
	if id, ok := m.teacherID[name]; ok {
		return id
	}
	id := domain.TeacherID(len(m.in.Teachers) + 1)
	m.teacherID[name] = id
	m.in.Teachers = append(m.in.Teachers, engine.TeacherInput{ID: id, Name: name})
	return id
}

func (m *model) violations(p *engine.Problem, vs []hard.Violation) []Violation {
	out := make([]Violation, 0, len(vs))
	for _, v := range vs {
		cv := Violation{Rule: v.Rule.String(), Title: ruleTitles[v.Rule], Message: v.Message}
		for _, l := range v.Lessons {
			cv.Lessons = append(cv.Lessons, int(p.Lessons[l].ID))
		}
		switch v.Kind {
		case hard.TeacherResource:
			cv.Resource = &Named{Kind: "teacher", Name: p.Teachers[v.Resource].Name}
		case hard.GroupResource:
			cv.Resource = &Named{Kind: "group", Name: p.Groups[v.Resource].Name}
		case hard.RoomResource:
			cv.Resource = &Named{Kind: "room", Name: m.roomName[p.Rooms[v.Resource].ID]}
		}
		if v.Slot.Period != 0 {
			cv.Day, cv.Period = ttx.Days[v.Slot.Day], int(v.Slot.Period)
		}
		out = append(out, cv)
	}
	return out
}

func (r *Report) softCriteria(m *model, p *engine.Problem, s *engine.Schedule) {
	ev := soft.New(p)
	total := ev.Full(s)
	for t := range p.Teachers {
		if m.stand[p.Teachers[t].ID] {
			total = total.Sub(ev.ForTeacher(s, int32(t)))
		}
	}
	w := soft.DefaultWeights()
	unavailable := map[soft.Criterion]bool{soft.Preferences: true, soft.CapacityWaste: !m.capacityOK, soft.RoomCount: true}
	for c := soft.Criterion(0); c < soft.NumCriteria; c++ {
		cr := Criterion{Key: c.Key(), Title: criterionTitles[c], Value: total[c], Weight: w[c], Available: !unavailable[c]}
		if cr.Available {
			cr.Weighted = w[c] * float64(total[c])
			r.SoftTotal += cr.Weighted
		}
		r.Soft = append(r.Soft, cr)
	}
}

func (r *Report) scores(m *model, p *engine.Problem, s *engine.Schedule, vs []hard.Violation) {
	ev := soft.New(p)
	type dayKey struct {
		res  int32
		day  domain.Day
		week int
	}
	// Busy periods per resource-day: subgroup lessons in one period count once.
	groupDay, teacherDay := map[dayKey]uint32{}, map[dayKey]uint32{}
	groupLessons, teacherLessons := make([]int, len(p.Groups)), make([]int, len(p.Teachers))
	for l := range p.Lessons {
		les := &p.Lessons[l]
		a := s.Assignment(int32(l))
		var groups []int32
		for _, u := range les.Units {
			if g := p.Units[u].Group; !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
		bit := uint32(1) << a.Slot.Period
		for _, w := range weeks(a.Slot.Parity) {
			teacherDay[dayKey{les.Teacher, a.Slot.Day, w}] |= bit
			teacherLessons[les.Teacher]++
			for _, g := range groups {
				groupDay[dayKey{g, a.Slot.Day, w}] |= bit
				groupLessons[g]++
			}
		}
	}
	maxDay := func(busy map[dayKey]uint32, res int32) int {
		best := 0
		for k, periods := range busy {
			if k.res == res {
				best = max(best, bits.OnesCount32(periods))
			}
		}
		return best
	}
	conflicts := func(kind hard.ResourceKind, res int32) int {
		n := 0
		for _, v := range vs {
			if v.Kind == kind && v.Resource == res {
				n++
			}
		}
		return n
	}
	for g := range p.Groups {
		b := ev.ForGroup(s, int32(g))
		r.Groups = append(r.Groups, EntityScore{
			Name: p.Groups[g].Name, Lessons: groupLessons[g], Gaps: b[soft.Gaps], Transitions: b[soft.BuildingTransitions],
			MaxPerDay: maxDay(groupDay, int32(g)), Conflicts: conflicts(hard.GroupResource, int32(g)),
		})
	}
	for t := range p.Teachers {
		if m.stand[p.Teachers[t].ID] {
			continue
		}
		b := ev.ForTeacher(s, int32(t))
		r.Teachers = append(r.Teachers, EntityScore{
			Name: p.Teachers[t].Name, Lessons: teacherLessons[t], Gaps: b[soft.Gaps], Transitions: b[soft.BuildingTransitions],
			MaxPerDay: maxDay(teacherDay, int32(t)), Conflicts: conflicts(hard.TeacherResource, int32(t)),
		})
	}
	byName := func(a, b EntityScore) int { return cmp.Compare(a.Name, b.Name) }
	slices.SortFunc(r.Groups, byName)
	slices.SortFunc(r.Teachers, byName)
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

func (m *model) notes() []string {
	notes := []string{
		"Проверяются только занятия из загруженного файла: накладки с группами других факультетов не видны.",
		"Занятия с несколькими преподавателями проверяются как одно «составное» лицо: накладка такого преподавателя с его же занятием в другом месте не будет найдена.",
		"Общие площадки (спорткомплекс) не считаются занятыми: в них одновременно могут заниматься несколько групп.",
	}
	if !m.capacityOK {
		notes = append(notes, "Нет вместимости аудиторий или численности групп: проверка мест и критерий «пустые места» отключены.")
	}
	return notes
}
