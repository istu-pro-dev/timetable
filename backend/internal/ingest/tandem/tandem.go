// Package tandem converts the weekly timetable workbooks that the «Тандем.Университет»
// system publishes on the ISTU site (layout T1, docs/istu-schedule-excel-analysis.md) into a
// ttx batch.
//
// Layout: a header row "месяц | число | <group> | время | № ауд | Ссылка | <group> | …";
// column A holds the week half (над/под чертой), column B the day; each lesson takes three
// rows in the group column (type, discipline, teachers) with the time and room merged on the
// right. Several lessons inside one merged time cell are subgroups, numbered by position.
package tandem

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/ingest/sheet"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
)

// Adapter is the value of ttx.Source.Adapter.
const Adapter = "tandem-xls-t1"

// ErrNotTandem is returned when no sheet has the T1 layout.
var ErrNotTandem = errors.New("workbook does not look like a Тандем weekly timetable")

// ErrNoLessons is returned when the header is found but no lesson block is: usually another
// Тандем layout (part-time schedule by dates, schedule by days) that has no adapter yet.
var ErrNoLessons = errors.New("no lessons found: only the weekly layout (над/под чертой) is supported so far")

// Periods is the ISTU bell schedule (90-minute pairs).
var Periods = []ttx.Period{
	{N: 1, Start: "08:30", End: "10:00"},
	{N: 2, Start: "10:10", End: "11:40"},
	{N: 3, Start: "12:20", End: "13:50"},
	{N: 4, Start: "14:00", End: "15:30"},
	{N: 5, Start: "15:40", End: "17:10"},
	{N: 6, Start: "17:20", End: "18:50"},
	{N: 7, Start: "19:00", End: "20:30"},
}

// Detect reports whether the workbook has at least one T1 sheet.
func Detect(wb *sheet.Workbook) bool {
	return slices.ContainsFunc(wb.Sheets, func(s *sheet.Sheet) bool { _, ok := headerRow(s); return ok })
}

// entry is one lesson block of one group as it appears in the sheet.
type entry struct {
	ref        ttx.Ref
	group      string
	parity     string
	day        int
	period     int
	pos        int // index of the block inside the merged time cell
	typ        string
	discipline string
	teachers   []string
	room       string
	place      string
	note       string
	flags      []string
	raw        map[string]string
}

func (e *entry) content() string {
	return strings.Join([]string{e.typ, e.discipline, strings.Join(e.teachers, ","), e.room, e.place}, "|")
}

type converter struct {
	rooms   map[string]ttx.Room
	issues  []ttx.Issue
	counted map[string]*countedIssue
	seen    map[string]bool
}

type countedIssue struct {
	idx   int
	n     int
	label string
}

// Convert parses the workbook. name is the original file name, data its bytes (for the hash).
func Convert(wb *sheet.Workbook, name string, data []byte) (*ttx.Batch, error) {
	if !Detect(wb) {
		return nil, ErrNotTandem
	}
	c := &converter{rooms: map[string]ttx.Room{}, counted: map[string]*countedIssue{}, seen: map[string]bool{}}
	var entries []*entry
	for _, s := range wb.Sheets {
		entries = append(entries, c.sheet(s)...)
	}
	if len(entries) == 0 {
		if len(c.issues) > 0 {
			return nil, fmt.Errorf("%w (%s)", ErrNoLessons, c.issues[0].Message)
		}
		return nil, ErrNoLessons
	}
	assignSubgroups(entries)
	lessons := mergeLessons(entries)

	sum := sha256.Sum256(data)
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	b := &ttx.Batch{
		Source: ttx.Source{
			ID: fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:4])), Adapter: Adapter,
			File: filepath.Base(name), SHA256: hex.EncodeToString(sum[:]), Title: base,
		},
		Periods: slices.Clone(Periods),
		Lessons: lessons,
		Issues:  c.issues,
	}
	groups := map[string]bool{}
	teachers := map[string]bool{}
	places := map[string]bool{}
	for _, l := range lessons {
		for _, m := range l.Audience {
			groups[m.Group] = true
		}
		for _, t := range l.Teachers {
			teachers[t] = true
		}
		if l.Place != "" {
			places[l.Place] = true
		}
	}
	for _, g := range sortedKeys(groups) {
		b.Groups = append(b.Groups, parseGroup(g))
	}
	for _, t := range sortedKeys(teachers) {
		b.Teachers = append(b.Teachers, ttx.Teacher{Key: t})
	}
	for _, k := range sortedKeys(c.rooms) {
		b.Rooms = append(b.Rooms, c.rooms[k])
	}
	for _, p := range sortedKeys(places) {
		b.Places = append(b.Places, ttx.Place{Key: p, Title: placeTitles[p], Virtual: true})
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

var placeTitles = map[string]string{"online": "Онлайн / дистант", "none": "Без аудитории"}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func headerRow(s *sheet.Sheet) (int, bool) {
	for p, v := range s.Cells {
		if p.Col <= 1 && norm(v) == "месяц" {
			return p.Row, true
		}
	}
	return 0, false
}

// sheet extracts the lesson blocks of one worksheet.
func (c *converter) sheet(s *sheet.Sheet) []*entry {
	hdr, ok := headerRow(s)
	if !ok {
		return nil
	}
	var out []*entry
	for gc := 2; gc+2 <= s.MaxCol; gc++ {
		group := collapse(s.At(sheet.Pos{Row: hdr, Col: gc}))
		if group == "" || !strings.HasPrefix(norm(s.At(sheet.Pos{Row: hdr, Col: gc + 1})), "время") {
			continue
		}
		for r := hdr + 1; r <= s.MaxRow; r++ {
			p := sheet.Pos{Row: r, Col: gc}
			if !s.IsOwner(p) || strings.TrimSpace(s.Raw(p)) == "" {
				continue
			}
			if e := c.block(s, group, gc, r); e != nil {
				out = append(out, e)
			}
			r += 2
		}
	}
	return out
}

func (c *converter) block(s *sheet.Sheet, group string, gc, r int) *entry {
	at := func(row, col int) string { return collapse(s.At(sheet.Pos{Row: row, Col: col})) }
	ref := ttx.Ref{Sheet: s.Name, Row: r + 1, Col: sheet.ColName(gc)}
	timeTop := s.Owner(sheet.Pos{Row: r, Col: gc + 1}).Row
	e := &entry{
		ref: ref, group: group, pos: (r - timeTop) / 3,
		raw: map[string]string{
			"type": at(r, gc), "discipline": at(r+1, gc), "teachers": at(r+2, gc),
			"time": at(timeTop, gc+1), "room": at(r, gc+2), "week": at(r, 0), "day": at(r, 1),
		},
	}
	if _, isType := lessonTypes[norm(e.raw["teachers"])]; isType {
		e.raw["teachers"] = "" // the block is two rows high: no teacher row
	}

	var ok bool
	if e.parity, ok = c.parity(e.raw["week"], ref); !ok {
		return nil
	}
	if e.day, ok = c.day(e.raw["day"], ref); !ok {
		return nil
	}
	if e.period, ok = c.period(e.raw["time"], ref); !ok {
		return nil
	}
	e.discipline = e.raw["discipline"]
	e.typ = c.lessonType(e)
	for t := range strings.SplitSeq(e.raw["teachers"], ",") {
		if t = collapse(t); t != "" {
			e.teachers = append(e.teachers, t)
		}
	}
	slices.Sort(e.teachers)
	if len(e.teachers) == 0 {
		e.flags = append(e.flags, "no_teacher")
	}
	c.location(e)
	return e
}

func (c *converter) parity(week string, ref ttx.Ref) (string, bool) {
	w := norm(week)
	switch {
	case w == "над чертой":
		return "odd", true
	case w == "под чертой":
		return "even", true
	case strings.HasPrefix(w, "над"):
		c.count("typo_week", ref, fmt.Sprintf("опечатка в метке недели %q", week))
		return "odd", true
	case strings.HasPrefix(w, "под"):
		c.count("typo_week", ref, fmt.Sprintf("опечатка в метке недели %q", week))
		return "even", true
	}
	c.once("unknown_week:"+week, ttx.Issue{Code: "unknown_week", Ref: &ref, Message: fmt.Sprintf("не распознана неделя %q, блок пропущен", week)})
	return "", false
}

var dayCodes = map[string]int{"пн": 0, "вт": 1, "ср": 2, "чт": 3, "пт": 4, "сб": 5, "вс": 6}

// latinLookalikes maps Latin letters typed by mistake in Cyrillic day names.
var latinLookalikes = strings.NewReplacer("c", "с", "C", "с", "p", "р", "o", "о", "a", "а", "e", "е")

func (c *converter) day(day string, ref ttx.Ref) (int, bool) {
	d := norm(day)
	if n, ok := dayCodes[d]; ok {
		return n, true
	}
	if n, ok := dayCodes[norm(latinLookalikes.Replace(d))]; ok {
		c.count("typo_day", ref, fmt.Sprintf("день %q набран латиницей", day))
		return n, true
	}
	c.once("unknown_day:"+day, ttx.Issue{Code: "unknown_day", Ref: &ref, Message: fmt.Sprintf("не распознан день %q, блок пропущен", day)})
	return 0, false
}

var timeRe = regexp.MustCompile(`^(\d{1,2})\s*[-:.]\s*(\d{2})`)

func (c *converter) period(t string, ref ttx.Ref) (int, bool) {
	if m := timeRe.FindStringSubmatch(t); m != nil {
		h, _ := strconv.Atoi(m[1])
		hm := fmt.Sprintf("%02d:%s", h, m[2])
		for _, p := range Periods {
			if p.Start == hm {
				return p.N, true
			}
		}
	}
	c.once("unknown_time:"+t, ttx.Issue{Code: "unknown_time", Ref: &ref, Message: fmt.Sprintf("время %q не совпадает с сеткой звонков, блок пропущен", t)})
	return 0, false
}

var lessonTypes = map[string]string{
	"лекции": "lecture",
	"практические занятия":     "practice",
	"лабораторные занятия":     "lab",
	"кураторские часы":         "curator",
	"зачет":                    "credit",
	"зачёт":                    "credit",
	"дифференцированный зачет": "graded_credit",
	"экзамен":                  "exam",
	"консультация":             "consultation",
	"консультации":             "consultation",
}

func (c *converter) lessonType(e *entry) string {
	raw := e.raw["type"]
	var kinds []string
	for part := range strings.SplitSeq(raw, ",") {
		if k, ok := lessonTypes[norm(part)]; ok && !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	if strings.Contains(norm(e.discipline), "практика") {
		if len(kinds) == 0 || kinds[0] != "internship" {
			e.flags = append(e.flags, "type_from_discipline")
		}
		return "internship"
	}
	switch len(kinds) {
	case 0:
		c.once("unknown_type:"+raw, ttx.Issue{Code: "unknown_type", Ref: &e.ref, Message: fmt.Sprintf("неизвестный вид занятия %q", raw)})
		return "other"
	case 1:
		return kinds[0]
	default:
		e.flags = append(e.flags, "multi_type")
		return kinds[0]
	}
}

var (
	roomRe     = regexp.MustCompile(`(?i)(ауд\.?\s*спец\.?|ауд\.|выч\.?\s*кл\.?|лаб\.?\s*спец\.?)\s*(\d+-[^\s(,]+)`)
	buildingRe = regexp.MustCompile(`\(корп\.\s*([^)\s]+)\)`)
	capacityRe = regexp.MustCompile(`(\d+)\s*мест`)
	dateRe     = regexp.MustCompile(`\d{2}\.\d{2}\.\d{4}`)
	onlineRe   = regexp.MustCompile(`(?i)https?://|телемост|дистант|онлайн|zoom|teams`)
)

var roomTypes = map[string]string{"выч": "computer", "лаб": "lab", "ауд спец": "special", "ауд": "general"}

// location fills room, place and note from the room cell.
func (c *converter) location(e *entry) {
	t := e.raw["room"]
	switch {
	case t == "" || t == "-" || t == "—":
		e.place = "none"
		return
	case onlineRe.MatchString(t):
		e.place = "online"
		return
	}
	if dateRe.MatchString(t) {
		e.note = t
	}
	if strings.Contains(strings.ToLower(t), "интеграл") {
		const key = "Интеграл"
		e.room = key
		c.addRoom(ttx.Room{Key: key, Building: key, Type: "sport", Title: "СК «Интеграл»"})
		if t != `СЦ "Интеграл"` && t != `ДС "Интеграл"` {
			e.flags = append(e.flags, "room_alias")
		}
		return
	}
	m := roomRe.FindStringSubmatch(t)
	if m == nil {
		e.room = t
		e.flags = append(e.flags, "room_unparsed")
		c.addRoom(ttx.Room{Key: t, Building: "?", Type: "unknown", Title: t})
		c.once("room_unparsed:"+t, ttx.Issue{Code: "room_unparsed", Ref: &e.ref, Message: fmt.Sprintf("не разобрана аудитория %q", t)})
		return
	}
	code := m[2]
	room := ttx.Room{Key: code, Title: t, Type: "general"}
	kind := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer(".", " ").Replace(m[1])), " "))
	for _, prefix := range []string{"ауд спец", "выч", "лаб", "ауд"} {
		if strings.HasPrefix(kind, prefix) {
			room.Type = roomTypes[prefix]
			break
		}
	}
	if b := buildingRe.FindStringSubmatch(t); b != nil {
		room.Building = b[1]
	} else {
		room.Building, _, _ = strings.Cut(code, "-")
	}
	if cm := capacityRe.FindStringSubmatch(t); cm != nil {
		if n, err := strconv.Atoi(cm[1]); err == nil && n > 0 {
			room.Capacity = &n
		}
	}
	e.room = code
	c.addRoom(room)
}

// addRoom keeps the first title seen but fills in a capacity found later.
func (c *converter) addRoom(r ttx.Room) {
	old, ok := c.rooms[r.Key]
	if !ok {
		c.rooms[r.Key] = r
		return
	}
	if old.Capacity == nil && r.Capacity != nil {
		old.Capacity = r.Capacity
		old.Title = r.Title
		c.rooms[r.Key] = old
	}
}

// count aggregates repeated issues of one kind into a single record.
func (c *converter) count(code string, ref ttx.Ref, label string) {
	if ci, ok := c.counted[code]; ok {
		ci.n++
		c.issues[ci.idx].Message = fmt.Sprintf("%s (%d раз, первое: %s)", ci.label, ci.n, c.issues[ci.idx].Ref)
		return
	}
	r := ref
	c.issues = append(c.issues, ttx.Issue{Code: code, Ref: &r, Message: label})
	c.counted[code] = &countedIssue{idx: len(c.issues) - 1, n: 1, label: label}
}

func (c *converter) once(key string, is ttx.Issue) {
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.issues = append(c.issues, is)
}

type slotKey struct {
	group, parity, sheet string
	day, period          int
}

// assignSubgroups numbers the blocks that share one group and time cell. When all blocks of a
// cell are identical (two subgroups with the same teacher and room) it is one lesson for the
// whole group.
func assignSubgroups(entries []*entry) {
	cells := map[slotKey][]*entry{}
	for _, e := range entries {
		k := slotKey{group: e.group, parity: e.parity, sheet: e.ref.Sheet, day: e.day, period: e.period}
		cells[k] = append(cells[k], e)
	}
	for _, list := range cells {
		first := list[0].content()
		split := slices.ContainsFunc(list[1:], func(e *entry) bool { return e.content() != first })
		for _, e := range list {
			if !split {
				e.pos = 0 // whole group
				continue
			}
			e.pos++ // 1-based subgroup number
			e.flags = append(e.flags, "subgroup_by_position")
		}
	}
}

// mergeLessons joins blocks into lessons: identical blocks of different groups in one slot
// are one stream lesson, identical lessons in both week halves are one every-week lesson.
func mergeLessons(entries []*entry) []ttx.Lesson {
	type acc struct {
		l    ttx.Lesson
		seen map[string]bool
	}
	bySlot := map[string]*acc{}
	var order []string
	for _, e := range entries {
		k := fmt.Sprint(e.parity, e.day, e.period, "|", e.content())
		a, ok := bySlot[k]
		if !ok {
			a = &acc{seen: map[string]bool{}, l: ttx.Lesson{
				Day: ttx.Days[e.day], Period: e.period, Parity: e.parity, LessonType: e.typ, Discipline: e.discipline,
				Teachers: e.teachers, Room: e.room, Place: e.place, Raw: e.raw, Note: e.note,
			}}
			bySlot[k] = a
			order = append(order, k)
		}
		m := ttx.Member{Group: e.group}
		if e.pos > 0 {
			sg := e.pos
			m.Subgroup = &sg
		}
		mk := memberKey(m)
		if !a.seen[mk] {
			a.seen[mk] = true
			a.l.Audience = append(a.l.Audience, m)
		}
		a.l.Refs = append(a.l.Refs, e.ref)
		for _, f := range e.flags {
			if !slices.Contains(a.l.Flags, f) {
				a.l.Flags = append(a.l.Flags, f)
			}
		}
		if a.l.Note == "" {
			a.l.Note = e.note
		}
	}

	byWeekless := map[string]int{}
	var out []ttx.Lesson
	for _, k := range order {
		l := bySlot[k].l
		slices.SortFunc(l.Audience, func(a, b ttx.Member) int { return strings.Compare(memberKey(a), memberKey(b)) })
		wk := weeklessKey(&l)
		if i, ok := byWeekless[wk]; ok && out[i].Parity != l.Parity && out[i].Parity != "every" {
			out[i].Parity = "every"
			out[i].Refs = append(out[i].Refs, l.Refs...)
			continue
		}
		byWeekless[wk] = len(out)
		out = append(out, l)
	}

	parityOrder := map[string]int{"every": 0, "odd": 1, "even": 2}
	slices.SortStableFunc(out, func(a, b ttx.Lesson) int {
		return cmp.Or(
			cmp.Compare(slices.Index(ttx.Days, a.Day), slices.Index(ttx.Days, b.Day)),
			cmp.Compare(a.Period, b.Period),
			cmp.Compare(parityOrder[a.Parity], parityOrder[b.Parity]),
			strings.Compare(memberKey(a.Audience[0]), memberKey(b.Audience[0])),
		)
	})
	for i := range out {
		out[i].ID = i + 1
	}
	return out
}

func memberKey(m ttx.Member) string {
	if m.Subgroup == nil {
		return m.Group
	}
	return fmt.Sprintf("%s/%d", m.Group, *m.Subgroup)
}

func weeklessKey(l *ttx.Lesson) string {
	members := make([]string, len(l.Audience))
	for i, m := range l.Audience {
		members[i] = memberKey(m)
	}
	return strings.Join([]string{
		l.Day, strconv.Itoa(l.Period), l.LessonType, l.Discipline, strings.Join(l.Teachers, ","), l.Room, l.Place,
		strings.Join(members, ","),
	}, "|")
}

var groupRe = regexp.MustCompile(`^([БСМА])(\d{2})-(\d{3})-?(\d+)?(зу|з)?$`)

var levels = map[string]string{"Б": "bachelor", "С": "specialist", "М": "master", "А": "postgraduate"}

var forms = map[string]string{"": "full_time", "з": "part_time", "зу": "part_time_accelerated"}

func parseGroup(code string) ttx.Group {
	g := ttx.Group{Key: code}
	m := groupRe.FindStringSubmatch(code)
	if m == nil {
		return g
	}
	g.Level = levels[m[1]]
	y, _ := strconv.Atoi(m[2])
	g.Intake = 2000 + y
	g.ProgramNo = m[3]
	g.No, _ = strconv.Atoi(m[4])
	g.Form = forms[m[5]]
	return g
}

func norm(s string) string { return strings.ToLower(collapse(s)) }

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
