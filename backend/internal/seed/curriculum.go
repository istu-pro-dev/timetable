package seed

import (
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// count is a number of lessons: w every week plus bw every other week.
type count struct{ w, bw int }

func (c count) zero() bool    { return c.w == 0 && c.bw == 0 }
func (c count) load() float64 { return float64(c.w) + 0.5*float64(c.bw) }

// entry is one discipline of a course plan.
//
// Lectures go to streams: by default ИВТ+ИСТ and ПИ; with wide ИВТ+ПИ (four groups) and ИСТ
// alone; with only set, one stream of the listed programs. Seminars and PE go to each group,
// labs to each lab subgroup (ИВТ) or whole group (ПИ, ИСТ), English to each english subgroup.
type entry struct {
	name, dept string
	lec        count
	sem        count
	lab        count
	labRoom    string
	eng        count
	pe         count
	wide       bool
	only       []string
}

var (
	onlyIVT  = []string{progIVT}
	onlyPI   = []string{progPI}
	onlyIST  = []string{progIST}
	onlyPIST = []string{progPI, progIST}
)

// plans[course-1] is the curriculum of one course. Order matters: teachers are assigned
// greedily in this order.
var plans = [4][]entry{
	{ // course 1
		{name: "Математический анализ", dept: deptMath, lec: count{1, 1}, sem: count{1, 1}},
		{name: "Линейная алгебра и аналитическая геометрия", dept: deptMath, lec: count{1, 0}, sem: count{0, 1}, wide: true},
		{name: "Дискретная математика", dept: deptMath, lec: count{0, 1}, sem: count{1, 0}},
		{name: "Основы программирования", dept: deptCS, lec: count{1, 0}, lab: count{1, 1}, labRoom: RoomComputer},
		{name: "Информатика", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Физика", dept: deptPhys, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomLab, wide: true},
		{name: "История России", dept: deptHum, lec: count{1, 0}, sem: count{0, 1}},
		{name: "Русский язык и культура речи", dept: deptHum, sem: count{0, 1}},
		{name: "Иностранный язык", dept: deptEng, eng: count{1, 1}},
		{name: "Физическая культура и спорт", dept: deptPE, pe: count{1, 0}},
		{name: "Элективные курсы по физической культуре", dept: deptPE, pe: count{0, 1}},
		{name: "Введение в профессию", dept: deptCS, lec: count{0, 1}},
		{name: "Основы российской государственности", dept: deptHum, lec: count{0, 1}, sem: count{0, 1}},
		{name: "Инженерная и компьютерная графика", dept: deptHW, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Экология", dept: deptHum, lec: count{0, 1}},
	},
	{ // course 2
		{name: "Дифференциальные уравнения", dept: deptMath, lec: count{1, 0}, sem: count{0, 1}},
		{name: "Теория вероятностей и математическая статистика", dept: deptMath, lec: count{0, 1}, sem: count{1, 0}, wide: true},
		{name: "Алгоритмы и структуры данных", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Объектно-ориентированное программирование", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Электротехника и электроника", dept: deptHW, lec: count{0, 1}, lab: count{0, 1}, labRoom: RoomLab, wide: true},
		{name: "Философия", dept: deptHum, lec: count{1, 0}, sem: count{0, 1}},
		{name: "Иностранный язык", dept: deptEng, eng: count{1, 1}},
		{name: "Физическая культура и спорт", dept: deptPE, pe: count{1, 0}},
		{name: "Базы данных", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Экономика", dept: deptHum, lec: count{0, 1}, sem: count{0, 1}},
		{name: "Теория информации", dept: deptMath, lec: count{0, 1}},
		{name: "Численные методы", dept: deptMath, lec: count{0, 1}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Социология", dept: deptHum, lec: count{0, 1}},
		{name: "Компьютерная логика", dept: deptHW, lec: count{0, 1}, sem: count{0, 1}, only: onlyIVT},
	},
	{ // course 3
		{name: "Компьютерные сети", dept: deptHW, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Операционные системы", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Программная инженерия", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Иностранный язык", dept: deptEng, eng: count{1, 0}},
		{name: "Физическая культура и спорт", dept: deptPE, pe: count{1, 0}},
		{name: "Правоведение", dept: deptHum, lec: count{0, 1}, wide: true},
		{name: "Методы оптимизации", dept: deptMath, lec: count{0, 1}, sem: count{0, 1}},
		{name: "Архитектура вычислительных систем", dept: deptHW, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomLab, only: onlyIVT},
		{name: "Схемотехника", dept: deptHW, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomLab, only: onlyIVT},
		{name: "Теория автоматов", dept: deptMath, lec: count{0, 1}, sem: count{0, 1}, only: onlyIVT},
		{name: "Технологии программирования", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyPI},
		{name: "Web-программирование", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyPI},
		{name: "Проектирование информационных систем", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyIST},
		{name: "Информационные технологии в управлении", dept: deptCS, lec: count{0, 1}, sem: count{1, 0}, only: onlyIST},
		{name: "Человеко-машинное взаимодействие", dept: deptCS, lec: count{0, 1}, sem: count{0, 1}, only: onlyPIST},
		{name: "Моделирование систем", dept: deptCS, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Безопасность жизнедеятельности", dept: deptHum, lec: count{0, 1}},
		{name: "Метрология и стандартизация", dept: deptHW, lec: count{0, 1}},
	},
	{ // course 4
		{name: "Иностранный язык в профессиональной деятельности", dept: deptEng, eng: count{0, 1}},
		{name: "Машинное обучение", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer, wide: true},
		{name: "Информационная безопасность", dept: deptHW, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Системы искусственного интеллекта", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer},
		{name: "Параллельное программирование", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyIVT},
		{name: "Встраиваемые системы", dept: deptHW, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomLab, only: onlyIVT},
		{name: "Управление программными проектами", dept: deptCS, lec: count{1, 0}, sem: count{1, 0}, only: onlyPI},
		{name: "Тестирование программного обеспечения", dept: deptCS, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyPI},
		{name: "Корпоративные информационные системы", dept: deptCS, lec: count{1, 0}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyIST},
		{name: "Анализ данных", dept: deptMath, lec: count{0, 1}, lab: count{1, 0}, labRoom: RoomComputer, only: onlyIST},
		{name: "Экономика программной инженерии", dept: deptHum, lec: count{0, 1}, sem: count{0, 1}},
		{name: "Распределённые системы", dept: deptCS, lec: count{1, 0}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Облачные технологии", dept: deptCS, lec: count{0, 1}},
		{name: "Научно-исследовательская работа", dept: deptCS, sem: count{0, 1}},
		{name: "Нормативно-правовое регулирование в ИТ", dept: deptHum, lec: count{0, 1}, wide: true},
		{name: "Администрирование информационных систем", dept: deptHW, lec: count{0, 1}, lab: count{0, 1}, labRoom: RoomComputer},
		{name: "Технологическое предпринимательство", dept: deptHum, lec: count{0, 1}},
	},
}

func (b *builder) curriculum() {
	for c := range plans {
		for _, e := range plans[c] {
			b.addEntry(c, e)
		}
	}
}

func (b *builder) addEntry(course int, e entry) {
	byProg := b.groupsByCourse[course]
	disc := b.discipline(e.name)

	var targets []int // groups taking seminars, labs, English and PE
	for _, p := range programs {
		if e.only == nil || slices.Contains(e.only, p) {
			targets = append(targets, byProg[p]...)
		}
	}

	if !e.lec.zero() {
		var streams [][]int
		switch {
		case e.only != nil:
			streams = [][]int{targets}
		case e.wide:
			streams = [][]int{
				slices.Concat(byProg[progIVT], byProg[progPI]),
				byProg[progIST],
			}
		default:
			streams = [][]int{
				slices.Concat(byProg[progIVT], byProg[progIST]),
				byProg[progPI],
			}
		}
		lecturer := b.pickTeacher(e.dept, e.lec.load()*float64(len(streams)))
		for _, s := range streams {
			aud := make([]Member, 0, len(s))
			for _, g := range s {
				aud = append(aud, Member{Group: g})
			}
			b.addItem(disc, db.LessonKindLecture, lecturer, RoomLecture, e.lec, aud)
		}
	}

	for _, g := range targets {
		if !e.sem.zero() {
			b.addItem(disc, db.LessonKindSeminar, b.pickTeacher(e.dept, e.sem.load()), RoomSeminar, e.sem,
				[]Member{{Group: g}})
		}
		if !e.pe.zero() {
			b.addItem(disc, db.LessonKindSeminar, b.pickTeacher(e.dept, e.pe.load()), RoomGym, e.pe,
				[]Member{{Group: g}})
		}
		if !e.eng.zero() {
			for part := 1; part <= 2; part++ {
				b.addItem(disc, db.LessonKindSeminar, b.pickTeacher(e.dept, e.eng.load()), RoomSeminar, e.eng,
					[]Member{{Group: g, Division: DivisionEnglish, Part: part}})
			}
		}
		if !e.lab.zero() {
			if b.hasDivision(g, DivisionLab) {
				for part := 1; part <= 2; part++ {
					b.addItem(disc, db.LessonKindLab, b.pickTeacher(e.dept, e.lab.load()), e.labRoom, e.lab,
						[]Member{{Group: g, Division: DivisionLab, Part: part}})
				}
			} else {
				b.addItem(disc, db.LessonKindLab, b.pickTeacher(e.dept, e.lab.load()), e.labRoom, e.lab,
					[]Member{{Group: g}})
			}
		}
	}
}

func (b *builder) hasDivision(g int, division string) bool {
	for _, sg := range b.d.Subgroups {
		if sg.Group == g && sg.Division == division {
			return true
		}
	}
	return false
}
