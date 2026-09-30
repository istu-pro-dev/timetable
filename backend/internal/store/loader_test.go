package store_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

func TestLoadProblemWithoutGrid(t *testing.T) {
	s := storetest.New(t)
	if _, err := s.LoadProblem(t.Context()); !errors.Is(err, store.ErrNoTimeGrid) {
		t.Fatalf("err = %v, want ErrNoTimeGrid", err)
	}
}

func TestLoadProblemAndSchedule(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	f := newFixture(ctx, t, s.Queries)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// A biweekly lab for english/1 with its own teacher availability exception.
	_, err := s.UpsertRoomType(ctx, db.UpsertRoomTypeParams{Code: "lab", Name: "Лаборатория"})
	must(err)
	lab, err := s.CreateRoom(ctx, db.CreateRoomParams{BuildingID: f.room.BuildingID, Name: "А-201", RoomType: "lab", Capacity: 15})
	must(err)
	must(s.SetRoomAvailability(ctx, db.SetRoomAvailabilityParams{
		RoomID: lab.ID, Day: 4, Period: 1, Parity: db.ParityEvery, Status: db.AvailabilityUnavailable,
	}))
	must(s.SetTeacherAvailability(ctx, db.SetTeacherAvailabilityParams{
		TeacherID: f.teacher.ID, Day: 0, Period: 1, Parity: db.ParityOdd, Status: db.AvailabilityPreferred,
	}))
	item, err := s.CreateCurriculumItem(ctx, db.CreateCurriculumItemParams{
		DisciplineID: f.item.DisciplineID, Kind: db.LessonKindLab, TeacherID: f.teacher.ID, RoomType: "lab", BiweeklyCount: 1,
	})
	must(err)
	must(s.AddCurriculumAudience(ctx, db.AddCurriculumAudienceParams{
		CurriculumItemID: item.ID, GroupID: f.group.ID, Division: "english", Part: 1,
	}))
	labLesson, err := s.CreateLesson(ctx, db.CreateLessonParams{CurriculumItemID: item.ID, Seq: 1, Biweekly: true})
	must(err)
	must(s.UpsertAssignment(ctx, db.UpsertAssignmentParams{
		ScheduleID: f.schedule.ID, LessonID: labLesson.ID, Day: 3, Period: 2, Parity: db.ParityEven,
		RoomID: pgtype.Int8{Int64: lab.ID, Valid: true}, Pinned: true,
	}))

	p, err := s.LoadProblem(ctx)
	must(err)
	if p.Grid != (domain.Grid{Days: 6, PeriodsPerDay: 7}) || len(p.Lessons) != 2 || len(p.Rooms) != 2 {
		t.Fatalf("problem: grid %+v, %d lessons, %d rooms", p.Grid, len(p.Lessons), len(p.Rooms))
	}
	li, ok := p.LessonIndex(domain.LessonID(labLesson.ID))
	if !ok {
		t.Fatal("lab lesson missing")
	}
	l := p.Lessons[li]
	if !l.Biweekly || l.Size != 12 || l.RoomType != "lab" || len(l.Rooms) != 1 || l.Kind != "lab" {
		t.Fatalf("lab lesson = %+v", l)
	}
	if u := p.Units[l.Units[0]]; u.Division == engine.None || u.Part != 1 {
		t.Fatalf("lab unit = %+v", u)
	}
	ri, _ := p.RoomIndex(domain.RoomID(lab.ID))
	friday1 := p.Grid.Cells(domain.Slot{Day: domain.Friday, Period: 1})
	if p.RoomAvail(ri, friday1[0]) != engine.Unavailable {
		t.Fatal("room availability not loaded")
	}
	mon1odd := p.Grid.Cells(domain.Slot{Day: domain.Monday, Period: 1, Parity: domain.OddWeek})
	if p.TeacherAvail(0, mon1odd[0]) != engine.Preferred {
		t.Fatal("teacher availability not loaded")
	}

	sch, err := s.LoadSchedule(ctx, p, f.schedule.ID)
	must(err)
	a := sch.Assignment(li)
	want := domain.Slot{Day: domain.Thursday, Period: 2, Parity: domain.EvenWeek}
	if !a.Placed || a.Slot != want || a.Room != ri || !a.Pinned {
		t.Fatalf("lab assignment = %+v", a)
	}
	lecture, _ := p.LessonIndex(domain.LessonID(f.lesson.ID))
	if a := sch.Assignment(lecture); !a.Placed || a.Slot.Day != domain.Tuesday || a.Slot.Period != 3 {
		t.Fatalf("lecture assignment = %+v", a)
	}
}
