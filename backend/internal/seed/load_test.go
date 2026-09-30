package seed_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/seed"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

var tables = []string{
	"periods", "buildings", "room_types", "rooms", "groups", "subgroups", "teachers", "disciplines",
	"teacher_availability", "room_availability", "curriculum_items", "curriculum_audience", "lessons",
}

func tableCounts(ctx context.Context, t *testing.T, s *store.Store) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range tables {
		var n int
		if err := s.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

func TestLoadIsIdempotent(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	d := seed.Build()

	// A schedule left from earlier work must not block the reload.
	if _, err := s.CreateSchedule(ctx, "old"); err != nil {
		t.Fatal(err)
	}

	first, err := seed.Load(ctx, s, d)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	after1 := tableCounts(ctx, t, s)
	second, err := seed.Load(ctx, s, d)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	after2 := tableCounts(ctx, t, s)

	if first != second || first != d.Counts() {
		t.Fatalf("counts differ: %+v / %+v / %+v", first, second, d.Counts())
	}
	if !reflect.DeepEqual(after1, after2) {
		t.Fatalf("row counts changed after reload:\n%v\n%v", after1, after2)
	}
	want := map[string]int{
		"periods": first.Periods, "buildings": first.Buildings, "room_types": first.RoomTypes,
		"rooms": first.Rooms, "groups": first.Groups, "subgroups": first.Subgroups,
		"teachers": first.Teachers, "disciplines": first.Disciplines,
		"teacher_availability": first.TeacherAvailability, "room_availability": first.RoomAvailability,
		"curriculum_items": first.CurriculumItems, "curriculum_audience": first.CurriculumAudience,
		"lessons": first.Lessons,
	}
	if !reflect.DeepEqual(after2, want) {
		t.Fatalf("row counts = %v, want %v", after2, want)
	}

	var schedules, dupTeachers, maxGroupID, audits int
	pool := s.Pool()
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schedules").Scan(&schedules); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM (SELECT full_name FROM teachers GROUP BY full_name HAVING count(*) > 1) x",
	).Scan(&dupTeachers); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT max(id) FROM groups").Scan(&maxGroupID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_log WHERE actor_type = 'human' AND actor_id = 'seed' AND entity = 'seed'",
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if schedules != 0 || dupTeachers != 0 || maxGroupID != len(d.Groups) || audits != 2 {
		t.Fatalf("schedules=%d duplicate teachers=%d max group id=%d audit rows=%d",
			schedules, dupTeachers, maxGroupID, audits)
	}

	var weekly, biweekly int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FILTER (WHERE NOT biweekly), count(*) FILTER (WHERE biweekly) FROM lessons",
	).Scan(&weekly, &biweekly); err != nil {
		t.Fatal(err)
	}
	var wantWeekly, wantBiweekly int
	for _, it := range d.Items {
		wantWeekly += it.Weekly
		wantBiweekly += it.Biweekly
	}
	if weekly != wantWeekly || biweekly != wantBiweekly {
		t.Fatalf("lessons weekly/biweekly = %d/%d, want %d/%d", weekly, biweekly, wantWeekly, wantBiweekly)
	}
}
