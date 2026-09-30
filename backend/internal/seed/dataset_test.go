package seed

import (
	"cmp"
	"reflect"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Feasibility limits: the dataset must be solvable in principle.
const (
	maxGroupLoad   = 24.0 // pairs per week
	maxTeacherLoad = 20.0
	// Share of open (room, slot) cells that the lessons needing those rooms may use.
	maxRoomUtilisation = 0.8
)

func TestBuildIsDeterministic(t *testing.T) {
	if !reflect.DeepEqual(Build(), Build()) {
		t.Fatal("Build returned different datasets")
	}
}

func TestSizes(t *testing.T) {
	d := Build()
	t.Logf("buildings=%d rooms=%d groups=%d subgroups=%d teachers=%d disciplines=%d items=%d lessons=%d",
		len(d.Buildings), len(d.Rooms), len(d.Groups), len(d.Subgroups), len(d.Teachers),
		len(d.Disciplines), len(d.Items), d.LessonCount())

	check := func(name string, got, lo, hi int) {
		t.Helper()
		if got < lo || got > hi {
			t.Errorf("%s = %d, want %d..%d", name, got, lo, hi)
		}
	}
	check("days", d.Days, 6, 6)
	check("periods per day", d.PeriodsPerDay, 7, 7)
	check("periods", len(d.Periods), d.PeriodsPerDay, d.PeriodsPerDay)
	check("buildings", len(d.Buildings), 2, 3)
	check("rooms", len(d.Rooms), 22, 28)
	check("groups", len(d.Groups), 18, 22)
	check("teachers", len(d.Teachers), 36, 44)
	check("disciplines", len(d.Disciplines), 55, 65)
	check("lessons", d.LessonCount(), 370, 430)

	for i, p := range d.Periods {
		if p.Number != i+1 || p.End <= p.Start || (i > 0 && p.Start <= d.Periods[i-1].End) {
			t.Errorf("bad period %+v", p)
		}
	}
}

func TestReferentialIntegrity(t *testing.T) {
	d := Build()
	types := map[string]bool{}
	for _, rt := range d.RoomTypes {
		types[rt.Code] = true
	}
	names := map[string]bool{}
	for _, r := range d.Rooms {
		key := d.Buildings[r.Building].Name + "/" + r.Name
		if names[key] || !types[r.Type] || r.Capacity <= 0 {
			t.Errorf("bad room %+v", r)
		}
		names[key] = true
	}
	for gi, g := range d.Groups {
		if g.Size < 18 || g.Size > 30 || g.Course < 1 || g.Course > 4 {
			t.Errorf("bad group %+v", g)
		}
		sums := map[string]int{}
		for _, sg := range d.Subgroups {
			if sg.Group == gi {
				sums[sg.Division] += sg.Size
			}
		}
		if _, ok := sums[DivisionEnglish]; !ok {
			t.Errorf("group %s has no english division", g.Name)
		}
		for div, s := range sums {
			if s != g.Size {
				t.Errorf("group %s: division %s sizes sum to %d, want %d", g.Name, div, s, g.Size)
			}
		}
	}
	for _, it := range d.Items {
		if it.Weekly < 0 || it.Biweekly < 0 || it.Lessons() == 0 || len(it.Audience) == 0 || !types[it.RoomType] {
			t.Errorf("bad item %+v", it)
		}
		for _, m := range it.Audience {
			if d.MemberSize(m) < 0 {
				t.Errorf("item %s: unknown audience member %+v", d.Disciplines[it.Discipline], m)
			}
		}
	}
}

func TestEdgeCasesPresent(t *testing.T) {
	d := Build()
	maxStream, labItems, biweekly := 0, 0, 0
	for _, it := range d.Items {
		if it.Kind == db.LessonKindLecture {
			maxStream = max(maxStream, len(it.Audience))
		}
		if it.Kind == db.LessonKindLab && it.Audience[0].Division == DivisionLab {
			labItems++
		}
		if it.Biweekly > 0 {
			biweekly++
		}
	}
	if maxStream < 3 || maxStream > 4 {
		t.Errorf("largest stream has %d groups, want 3..4", maxStream)
	}
	if labItems == 0 || biweekly == 0 {
		t.Errorf("lab subgroup items = %d, biweekly items = %d; want both > 0", labItems, biweekly)
	}
	for _, pt := range PartTimeDays {
		if n := len(openDays(d, teacherIndex(t, d, pt.Teacher))); n != 2 {
			t.Errorf("part-time teacher %s is available on %d days, want 2", pt.Teacher, n)
		}
	}
	statuses := map[db.Availability]bool{}
	for _, a := range d.TeacherAvail {
		statuses[a.Status] = true
	}
	for _, s := range db.AllAvailabilityValues() {
		if !statuses[s] {
			t.Errorf("no teacher availability row with status %s", s)
		}
	}
	if len(d.RoomAvail) == 0 {
		t.Error("no room availability rows")
	}
}

// TestGroupLoad: every group fits into the week. Parts of one division run in parallel,
// different divisions do not, so the load is whole-group load plus, per division, the load of
// its busiest part.
func TestGroupLoad(t *testing.T) {
	d := Build()
	whole := make([]float64, len(d.Groups))
	parts := make([]map[string]map[int]float64, len(d.Groups))
	for _, it := range d.Items {
		for _, m := range it.Audience {
			if m.Division == "" {
				whole[m.Group] += it.Load()
				continue
			}
			if parts[m.Group] == nil {
				parts[m.Group] = map[string]map[int]float64{}
			}
			if parts[m.Group][m.Division] == nil {
				parts[m.Group][m.Division] = map[int]float64{}
			}
			parts[m.Group][m.Division][m.Part] += it.Load()
		}
	}
	for gi, g := range d.Groups {
		load := whole[gi]
		for _, byPart := range parts[gi] {
			busiest := 0.0
			for _, l := range byPart {
				busiest = max(busiest, l)
			}
			load += busiest
		}
		t.Logf("%s: %.1f pairs", g.Name, load)
		if load > maxGroupLoad || load < 8 {
			t.Errorf("group %s has %.1f pairs per week, want 8..%.0f", g.Name, load, maxGroupLoad)
		}
	}
}

func TestTeacherLoad(t *testing.T) {
	d := Build()
	load := make([]float64, len(d.Teachers))
	for _, it := range d.Items {
		load[it.Teacher] += it.Load()
	}
	for ti, tc := range d.Teachers {
		open := float64(openSlots(d, ti))
		if load[ti] > maxTeacherLoad || load[ti] > open*maxRoomUtilisation {
			t.Errorf("teacher %s: %.1f pairs per week, %.0f open slots", tc.FullName, load[ti], open)
		}
		if load[ti] == 0 {
			t.Errorf("teacher %s teaches nothing", tc.FullName)
		}
	}
}

// TestRoomSupply is a Hall-style necessary condition per room type: lessons that need at least
// c seats fit into the open slots of rooms with at least c seats.
func TestRoomSupply(t *testing.T) {
	d := Build()
	slots := d.Days * d.PeriodsPerDay
	closed := make([]float64, len(d.Rooms))
	for _, a := range d.RoomAvail {
		if a.Status == db.AvailabilityUnavailable {
			closed[a.Room] += parityWeight(a.Parity)
		}
	}
	for _, rt := range d.RoomTypes {
		type need struct {
			size int
			load float64
		}
		var needs []need
		for _, it := range d.Items {
			if it.RoomType == rt.Code {
				needs = append(needs, need{d.AudienceSize(it), it.Load()})
			}
		}
		slices.SortFunc(needs, func(a, b need) int { return cmp.Compare(b.size, a.size) })
		demand := 0.0
		for _, n := range needs {
			demand += n.load
			supply := 0.0
			for ri, r := range d.Rooms {
				if r.Type == rt.Code && r.Capacity >= n.size {
					supply += float64(slots) - closed[ri]
				}
			}
			if demand > supply*maxRoomUtilisation {
				t.Errorf("room type %s: %.1f pairs need >= %d seats, only %.0f room slots", rt.Code, demand, n.size, supply)
				break
			}
		}
		t.Logf("room type %s: %.1f pairs per week", rt.Code, demand)
	}
}

func parityWeight(p db.Parity) float64 {
	if p == db.ParityEvery {
		return 1
	}
	return 0.5
}

func openSlots(d *Dataset, teacher int) int {
	closed := 0.0
	for _, a := range d.TeacherAvail {
		if a.Teacher == teacher && a.Status == db.AvailabilityUnavailable {
			closed += parityWeight(a.Parity)
		}
	}
	return d.Days*d.PeriodsPerDay - int(closed+0.5)
}

func openDays(d *Dataset, teacher int) []int {
	var days []int
	for day := 0; day < d.Days; day++ {
		closed := 0
		for _, a := range d.TeacherAvail {
			if a.Teacher == teacher && a.Day == day && a.Status == db.AvailabilityUnavailable && a.Parity == db.ParityEvery {
				closed++
			}
		}
		if closed < d.PeriodsPerDay {
			days = append(days, day)
		}
	}
	return days
}

func teacherIndex(t *testing.T, d *Dataset, name string) int {
	t.Helper()
	for i, tc := range d.Teachers {
		if tc.FullName == name {
			return i
		}
	}
	t.Fatalf("unknown teacher %s", name)
	return -1
}

func TestShortName(t *testing.T) {
	if got := ShortName("Иванов Иван Петрович"); got != "Иванов И.П." {
		t.Fatalf("ShortName = %q", got)
	}
}
