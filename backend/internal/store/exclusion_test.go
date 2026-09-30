package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

// bookingEnv builds reference data for double-booking tests.
type bookingEnv struct {
	t        *testing.T
	ctx      context.Context
	s        *store.Store
	schedule int64
	disc     int64
}

func newBookingEnv(t *testing.T) *bookingEnv {
	t.Helper()
	s := storetest.New(t)
	ctx := t.Context()
	e := &bookingEnv{t: t, ctx: ctx, s: s}

	_, err := s.UpsertRoomType(ctx, db.UpsertRoomTypeParams{Code: "any", Name: "Любая"})
	e.must(err)
	d, err := s.CreateDiscipline(ctx, "Математика")
	e.must(err)
	e.disc = d.ID
	sch, err := s.CreateSchedule(ctx, "draft")
	e.must(err)
	e.schedule = sch.ID
	return e
}

func (e *bookingEnv) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *bookingEnv) teacher() int64 {
	e.t.Helper()
	tch, err := e.s.CreateTeacher(e.ctx, db.CreateTeacherParams{FullName: "T", ShortName: "T"})
	e.must(err)
	return tch.ID
}

func (e *bookingEnv) group(name string) int64 {
	e.t.Helper()
	g, err := e.s.CreateGroup(e.ctx, db.CreateGroupParams{Name: name, Course: 1, Size: 20})
	e.must(err)
	return g.ID
}

func (e *bookingEnv) room(name string) int64 {
	e.t.Helper()
	b, err := e.s.CreateBuilding(e.ctx, db.CreateBuildingParams{Name: "B-" + name})
	e.must(err)
	r, err := e.s.CreateRoom(e.ctx, db.CreateRoomParams{BuildingID: b.ID, Name: name, RoomType: "any", Capacity: 100})
	e.must(err)
	return r.ID
}

// lesson creates a one-lesson curriculum item for the teacher and audience.
func (e *bookingEnv) lesson(teacher int64, biweekly bool, audience ...db.AddCurriculumAudienceParams) int64 {
	e.t.Helper()
	p := db.CreateCurriculumItemParams{DisciplineID: e.disc, Kind: db.LessonKindSeminar, TeacherID: teacher, RoomType: "any"}
	if biweekly {
		p.BiweeklyCount = 1
	} else {
		p.WeeklyCount = 1
	}
	item, err := e.s.CreateCurriculumItem(e.ctx, p)
	e.must(err)
	for _, a := range audience {
		a.CurriculumItemID = item.ID
		e.must(e.s.AddCurriculumAudience(e.ctx, a))
	}
	l, err := e.s.CreateLesson(e.ctx, db.CreateLessonParams{CurriculumItemID: item.ID, Seq: 1, Biweekly: biweekly})
	e.must(err)
	return l.ID
}

func (e *bookingEnv) place(lesson int64, day, period int16, parity db.Parity, room int64) error {
	p := db.UpsertAssignmentParams{ScheduleID: e.schedule, LessonID: lesson, Day: day, Period: period, Parity: parity}
	if room != 0 {
		p.RoomID = pgtype.Int8{Int64: room, Valid: true}
	}
	return store.MapError(e.s.UpsertAssignment(e.ctx, p))
}

func whole(g int64) db.AddCurriculumAudienceParams {
	return db.AddCurriculumAudienceParams{GroupID: g}
}

func sub(g int64, division string, part int16) db.AddCurriculumAudienceParams {
	return db.AddCurriculumAudienceParams{GroupID: g, Division: division, Part: part}
}

func TestExclusionTeacher(t *testing.T) {
	e := newBookingEnv(t)
	tch := e.teacher()
	g1, g2 := e.group("G1"), e.group("G2")
	a := e.lesson(tch, false, whole(g1))
	b := e.lesson(tch, false, whole(g2))

	e.must(e.place(a, 0, 1, db.ParityEvery, 0))
	if err := e.place(b, 0, 1, db.ParityEvery, 0); !errors.Is(err, store.ErrTeacherBusy) {
		t.Fatalf("same teacher, same slot: err = %v, want ErrTeacherBusy", err)
	}
	e.must(e.place(b, 0, 2, db.ParityEvery, 0))
}

func TestExclusionRoom(t *testing.T) {
	e := newBookingEnv(t)
	r := e.room("101")
	a := e.lesson(e.teacher(), false, whole(e.group("G1")))
	b := e.lesson(e.teacher(), false, whole(e.group("G2")))

	e.must(e.place(a, 2, 3, db.ParityEvery, r))
	if err := e.place(b, 2, 3, db.ParityEvery, r); !errors.Is(err, store.ErrRoomBusy) {
		t.Fatalf("same room, same slot: err = %v, want ErrRoomBusy", err)
	}
	// Different room or no room yet is fine.
	e.must(e.place(b, 2, 3, db.ParityEvery, e.room("102")))
	c := e.lesson(e.teacher(), false, whole(e.group("G3")))
	e.must(e.place(c, 2, 3, db.ParityEvery, 0))
}

func TestExclusionGroup(t *testing.T) {
	e := newBookingEnv(t)
	g := e.group("G1")
	other := e.group("G2")

	tests := []struct {
		name     string
		first    db.AddCurriculumAudienceParams
		second   []db.AddCurriculumAudienceParams
		conflict bool
	}{
		{"whole vs whole", whole(g), []db.AddCurriculumAudienceParams{whole(g)}, true},
		{"whole vs subgroup", whole(g), []db.AddCurriculumAudienceParams{sub(g, "english", 1)}, true},
		{"subgroup vs whole", sub(g, "english", 1), []db.AddCurriculumAudienceParams{whole(g)}, true},
		{"parallel subgroups", sub(g, "english", 1), []db.AddCurriculumAudienceParams{sub(g, "english", 2)}, false},
		{"same subgroup", sub(g, "english", 2), []db.AddCurriculumAudienceParams{sub(g, "english", 2)}, true},
		{"cross divisions", sub(g, "english", 1), []db.AddCurriculumAudienceParams{sub(g, "pe", 2)}, true},
		{"stream containing the group", whole(g), []db.AddCurriculumAudienceParams{whole(other), whole(g)}, true},
		{"other group", whole(g), []db.AddCurriculumAudienceParams{whole(other)}, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			day := int16(i % 6)
			period := int16(1 + i/6)
			a := e.lesson(e.teacher(), false, tt.first)
			b := e.lesson(e.teacher(), false, tt.second...)
			e.must(e.place(a, day, period, db.ParityEvery, 0))
			err := e.place(b, day, period, db.ParityEvery, 0)
			if tt.conflict && !errors.Is(err, store.ErrGroupBusy) {
				t.Fatalf("err = %v, want ErrGroupBusy", err)
			}
			if !tt.conflict && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExclusionParity(t *testing.T) {
	e := newBookingEnv(t)
	tch := e.teacher()
	g := e.group("G1")
	odd := e.lesson(tch, true, whole(g))
	even := e.lesson(tch, true, whole(g))
	weekly := e.lesson(tch, false, whole(g))

	// Odd and even weeks never meet: same teacher and group are fine.
	e.must(e.place(odd, 0, 1, db.ParityOdd, 0))
	e.must(e.place(even, 0, 1, db.ParityEven, 0))
	// An every-week lesson collides with both.
	if err := e.place(weekly, 0, 1, db.ParityEvery, 0); !errors.Is(err, store.ErrTeacherBusy) {
		t.Fatalf("every vs odd/even: err = %v, want ErrTeacherBusy", err)
	}
	// Parity must match the lesson kind.
	if err := e.place(weekly, 0, 2, db.ParityOdd, 0); !errors.Is(err, store.ErrParityMismatch) {
		t.Fatalf("weekly lesson on odd slot: err = %v, want ErrParityMismatch", err)
	}
	if err := e.place(odd, 0, 2, db.ParityEvery, 0); !errors.Is(err, store.ErrParityMismatch) {
		t.Fatalf("biweekly lesson on every slot: err = %v, want ErrParityMismatch", err)
	}
}

func TestExclusionFollowsCurriculumChanges(t *testing.T) {
	e := newBookingEnv(t)
	t1, t2 := e.teacher(), e.teacher()
	g1, g2 := e.group("G1"), e.group("G2")
	a := e.lesson(t1, false, whole(g1))
	b := e.lesson(t2, false, whole(g2))
	e.must(e.place(a, 0, 1, db.ParityEvery, 0))
	e.must(e.place(b, 0, 1, db.ParityEvery, 0))

	items, err := e.s.ListCurriculumItems(e.ctx)
	e.must(err)
	itemB := items[1]

	// Reassigning b's curriculum item to teacher t1 double-books t1.
	_, err = e.s.UpdateCurriculumItem(e.ctx, db.UpdateCurriculumItemParams{
		ID: itemB.ID, DisciplineID: itemB.DisciplineID, Kind: itemB.Kind, TeacherID: t1,
		RoomType: itemB.RoomType, WeeklyCount: itemB.WeeklyCount,
	})
	if !errors.Is(store.MapError(err), store.ErrTeacherBusy) {
		t.Fatalf("teacher change: err = %v, want ErrTeacherBusy", err)
	}

	// Adding g1 to b's audience double-books g1.
	err = e.s.AddCurriculumAudience(e.ctx, db.AddCurriculumAudienceParams{CurriculumItemID: itemB.ID, GroupID: g1})
	if !errors.Is(store.MapError(err), store.ErrGroupBusy) {
		t.Fatalf("audience change: err = %v, want ErrGroupBusy", err)
	}
}

func TestMapErrorPassesThrough(t *testing.T) {
	plain := errors.New("plain")
	if got := store.MapError(plain); !errors.Is(got, plain) || errors.Is(got, store.ErrTeacherBusy) {
		t.Fatalf("MapError(plain) = %v", got)
	}
	if store.MapError(nil) != nil {
		t.Fatal("MapError(nil) != nil")
	}
}
