package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

func TestMigrationsUpDownUp(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()

	if err := store.MigrateReset(ctx, s.Pool()); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d tables left after down migrations", n)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

// fixture creates a minimal but complete dataset: one lesson assigned in one schedule.
type fixture struct {
	room     db.Room
	group    db.Group
	teacher  db.Teacher
	item     db.CurriculumItem
	lesson   db.Lesson
	schedule db.Schedule
}

func newFixture(ctx context.Context, t *testing.T, q *db.Queries) fixture {
	t.Helper()
	var f fixture
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error

	_, err = q.UpsertTimeGrid(ctx, db.UpsertTimeGridParams{Days: 6, PeriodsPerDay: 7})
	must(err)
	b, err := q.CreateBuilding(ctx, db.CreateBuildingParams{Name: "Корпус А"})
	must(err)
	_, err = q.UpsertRoomType(ctx, db.UpsertRoomTypeParams{Code: "lecture", Name: "Лекционная"})
	must(err)
	f.room, err = q.CreateRoom(ctx, db.CreateRoomParams{BuildingID: b.ID, Name: "А-101", RoomType: "lecture", Capacity: 60})
	must(err)
	f.group, err = q.CreateGroup(ctx, db.CreateGroupParams{Name: "Б24-782-3", Course: 2, Size: 25})
	must(err)
	_, err = q.CreateSubgroup(ctx, db.CreateSubgroupParams{GroupID: f.group.ID, Division: "english", Part: 1, Size: 12})
	must(err)
	f.teacher, err = q.CreateTeacher(ctx, db.CreateTeacherParams{FullName: "Иванов Иван Иванович", ShortName: "Иванов И.И."})
	must(err)
	d, err := q.CreateDiscipline(ctx, "Базы данных")
	must(err)
	f.item, err = q.CreateCurriculumItem(ctx, db.CreateCurriculumItemParams{
		DisciplineID: d.ID, Kind: db.LessonKindLecture, TeacherID: f.teacher.ID, RoomType: "lecture", WeeklyCount: 1,
	})
	must(err)
	must(q.AddCurriculumAudience(ctx, db.AddCurriculumAudienceParams{CurriculumItemID: f.item.ID, GroupID: f.group.ID}))
	f.lesson, err = q.CreateLesson(ctx, db.CreateLessonParams{CurriculumItemID: f.item.ID, Seq: 1})
	must(err)
	f.schedule, err = q.CreateSchedule(ctx, "draft")
	must(err)
	must(q.UpsertAssignment(ctx, db.UpsertAssignmentParams{
		ScheduleID: f.schedule.ID, LessonID: f.lesson.ID, Day: 1, Period: 3,
		Parity: db.ParityEvery, RoomID: pgtype.Int8{Int64: f.room.ID, Valid: true},
	}))
	return f
}

func TestReferenceAndScheduleRoundTrip(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	f := newFixture(ctx, t, s.Queries)

	got, err := s.ListAssignments(ctx, f.schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Day != 1 || got[0].Period != 3 || got[0].RoomID.Int64 != f.room.ID {
		t.Fatalf("assignments = %+v", got)
	}

	// Moving the lesson updates the row in place.
	if err := s.UpsertAssignment(ctx, db.UpsertAssignmentParams{
		ScheduleID: f.schedule.ID, LessonID: f.lesson.ID, Day: 2, Period: 1, Parity: db.ParityOdd, Pinned: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListAssignments(ctx, f.schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Day != 2 || got[0].Parity != db.ParityOdd || got[0].RoomID.Valid || !got[0].Pinned {
		t.Fatalf("after move = %+v", got)
	}

	aud, err := s.ListCurriculumAudience(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(aud) != 1 || aud[0].GroupID != f.group.ID || aud[0].Division != "" {
		t.Fatalf("audience = %+v", aud)
	}
}

func TestConstraints(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	f := newFixture(ctx, t, s.Queries)

	tests := []struct {
		name string
		run  func() error
		code string
	}{
		{"room capacity must be positive", func() error {
			_, err := s.UpdateRoom(ctx, db.UpdateRoomParams{ID: f.room.ID, BuildingID: f.room.BuildingID, Name: f.room.Name, RoomType: "lecture", Capacity: 0})
			return err
		}, "23514"},
		{"unknown room type", func() error {
			_, err := s.UpdateRoom(ctx, db.UpdateRoomParams{ID: f.room.ID, BuildingID: f.room.BuildingID, Name: f.room.Name, RoomType: "pool", Capacity: 10})
			return err
		}, "23503"},
		{"duplicate group name", func() error {
			_, err := s.CreateGroup(ctx, db.CreateGroupParams{Name: f.group.Name, Course: 1, Size: 10})
			return err
		}, "23505"},
		{"curriculum item without lessons", func() error {
			_, err := s.UpdateCurriculumItem(ctx, db.UpdateCurriculumItemParams{
				ID: f.item.ID, DisciplineID: f.item.DisciplineID, Kind: f.item.Kind, TeacherID: f.teacher.ID, RoomType: "lecture",
			})
			return err
		}, "23514"},
		{"subgroup audience needs a part", func() error {
			return s.AddCurriculumAudience(ctx, db.AddCurriculumAudienceParams{CurriculumItemID: f.item.ID, GroupID: f.group.ID, Division: "english"})
		}, "23514"},
		{"period out of range", func() error {
			return s.UpsertAssignment(ctx, db.UpsertAssignmentParams{ScheduleID: f.schedule.ID, LessonID: f.lesson.ID, Day: 0, Period: 17, Parity: db.ParityEvery})
		}, "23514"},
		{"single time grid row", func() error {
			_, err := s.Pool().Exec(ctx, "INSERT INTO time_grid (id, days, periods_per_day) VALUES (false, 5, 6)")
			return err
		}, "23514"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pgErr *pgconn.PgError
			if err := tt.run(); !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("err = %v, want SQLSTATE %s", err, tt.code)
			}
		})
	}
}

func TestDeletingScheduleCascades(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	f := newFixture(ctx, t, s.Queries)

	if n, err := s.DeleteSchedule(ctx, f.schedule.ID); err != nil || n != 1 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	got, err := s.ListAssignments(ctx, f.schedule.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("assignments after delete = %v, %v", got, err)
	}
}

func TestInTx(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	errBoom := errors.New("boom")

	err := s.InTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if _, err := q.CreateDiscipline(ctx, "Откат"); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want boom", err)
	}

	err = s.InTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		_, err := q.CreateDiscipline(ctx, "Фиксация")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.ListDisciplines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Фиксация" {
		t.Fatalf("disciplines = %+v", list)
	}
}
