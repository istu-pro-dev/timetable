package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Actor is the audit actor of a seed load.
var Actor = store.Actor{Type: db.ActorTypeHuman, ID: "seed"}

// Counts is the number of rows the seed writes per table.
type Counts struct {
	Periods             int `json:"periods"`
	Buildings           int `json:"buildings"`
	RoomTypes           int `json:"room_types"`
	Rooms               int `json:"rooms"`
	Groups              int `json:"groups"`
	Subgroups           int `json:"subgroups"`
	Teachers            int `json:"teachers"`
	Disciplines         int `json:"disciplines"`
	TeacherAvailability int `json:"teacher_availability"`
	RoomAvailability    int `json:"room_availability"`
	CurriculumItems     int `json:"curriculum_items"`
	CurriculumAudience  int `json:"curriculum_audience"`
	Lessons             int `json:"lessons"`
}

// Counts returns the number of rows the dataset produces per table.
func (d *Dataset) Counts() Counts {
	aud := 0
	for _, it := range d.Items {
		aud += len(it.Audience)
	}
	return Counts{
		Periods:             len(d.Periods),
		Buildings:           len(d.Buildings),
		RoomTypes:           len(d.RoomTypes),
		Rooms:               len(d.Rooms),
		Groups:              len(d.Groups),
		Subgroups:           len(d.Subgroups),
		Teachers:            len(d.Teachers),
		Disciplines:         len(d.Disciplines),
		TeacherAvailability: len(d.TeacherAvail),
		RoomAvailability:    len(d.RoomAvail),
		CurriculumItems:     len(d.Items),
		CurriculumAudience:  aud,
		Lessons:             d.LessonCount(),
	}
}

// Load replaces all reference data, curriculum, lessons and schedules with the dataset in one
// transaction, together with one audit row. Running it again yields the same rows and IDs
// (identities are restarted), so it is idempotent. The audit log is kept.
func Load(ctx context.Context, s *store.Store, d *Dataset) (Counts, error) {
	counts := d.Counts()
	change := store.Change{
		Entity:   "seed",
		EntityID: "demo",
		After:    counts,
		Reason:   "load demo seed dataset (replaces reference data, curriculum and schedules)",
	}
	_, err := s.WithAudit(ctx, Actor, change, func(q *db.Queries, _ pgx.Tx, _ *store.Change) error {
		if err := q.TruncateSeedData(ctx); err != nil {
			return fmt.Errorf("truncate: %w", err)
		}
		return insert(ctx, q, d)
	})
	if err != nil {
		return Counts{}, fmt.Errorf("seed: %w", err)
	}
	return counts, nil
}

func insert(ctx context.Context, q *db.Queries, d *Dataset) error {
	if _, err := q.UpsertTimeGrid(ctx, db.UpsertTimeGridParams{
		Days: int16(d.Days), PeriodsPerDay: int16(d.PeriodsPerDay),
	}); err != nil {
		return fmt.Errorf("time grid: %w", err)
	}
	for _, p := range d.Periods {
		if _, err := q.UpsertPeriod(ctx, db.UpsertPeriodParams{
			Number:   int16(p.Number),
			StartsAt: clock(p.Start),
			EndsAt:   clock(p.End),
		}); err != nil {
			return fmt.Errorf("period %d: %w", p.Number, err)
		}
	}
	for _, rt := range d.RoomTypes {
		if _, err := q.UpsertRoomType(ctx, db.UpsertRoomTypeParams{Code: rt.Code, Name: rt.Name}); err != nil {
			return fmt.Errorf("room type %s: %w", rt.Code, err)
		}
	}

	buildings := make([]int64, len(d.Buildings))
	for i, b := range d.Buildings {
		row, err := q.CreateBuilding(ctx, db.CreateBuildingParams{Name: b.Name, Address: b.Address})
		if err != nil {
			return fmt.Errorf("building %s: %w", b.Name, err)
		}
		buildings[i] = row.ID
	}
	rooms := make([]int64, len(d.Rooms))
	for i, r := range d.Rooms {
		row, err := q.CreateRoom(ctx, db.CreateRoomParams{
			BuildingID: buildings[r.Building], Name: r.Name, RoomType: r.Type, Capacity: int32(r.Capacity),
		})
		if err != nil {
			return fmt.Errorf("room %s: %w", r.Name, err)
		}
		rooms[i] = row.ID
	}
	groups := make([]int64, len(d.Groups))
	for i, g := range d.Groups {
		row, err := q.CreateGroup(ctx, db.CreateGroupParams{Name: g.Name, Course: int16(g.Course), Size: int32(g.Size)})
		if err != nil {
			return fmt.Errorf("group %s: %w", g.Name, err)
		}
		groups[i] = row.ID
	}
	for _, sg := range d.Subgroups {
		if _, err := q.CreateSubgroup(ctx, db.CreateSubgroupParams{
			GroupID: groups[sg.Group], Division: sg.Division, Part: int16(sg.Part), Size: int32(sg.Size),
		}); err != nil {
			return fmt.Errorf("subgroup %s/%s/%d: %w", d.Groups[sg.Group].Name, sg.Division, sg.Part, err)
		}
	}
	teachers := make([]int64, len(d.Teachers))
	for i, t := range d.Teachers {
		row, err := q.CreateTeacher(ctx, db.CreateTeacherParams{FullName: t.FullName, ShortName: t.ShortName})
		if err != nil {
			return fmt.Errorf("teacher %s: %w", t.FullName, err)
		}
		teachers[i] = row.ID
	}
	disciplines := make([]int64, len(d.Disciplines))
	for i, name := range d.Disciplines {
		row, err := q.CreateDiscipline(ctx, name)
		if err != nil {
			return fmt.Errorf("discipline %s: %w", name, err)
		}
		disciplines[i] = row.ID
	}
	for _, a := range d.TeacherAvail {
		if err := q.SetTeacherAvailability(ctx, db.SetTeacherAvailabilityParams{
			TeacherID: teachers[a.Teacher], Day: int16(a.Day), Period: int16(a.Period), Parity: a.Parity, Status: a.Status,
		}); err != nil {
			return fmt.Errorf("teacher availability: %w", err)
		}
	}
	for _, a := range d.RoomAvail {
		if err := q.SetRoomAvailability(ctx, db.SetRoomAvailabilityParams{
			RoomID: rooms[a.Room], Day: int16(a.Day), Period: int16(a.Period), Parity: a.Parity, Status: a.Status,
		}); err != nil {
			return fmt.Errorf("room availability: %w", err)
		}
	}

	for _, it := range d.Items {
		row, err := q.CreateCurriculumItem(ctx, db.CreateCurriculumItemParams{
			DisciplineID:  disciplines[it.Discipline],
			Kind:          it.Kind,
			TeacherID:     teachers[it.Teacher],
			RoomType:      it.RoomType,
			WeeklyCount:   int16(it.Weekly),
			BiweeklyCount: int16(it.Biweekly),
		})
		if err != nil {
			return fmt.Errorf("curriculum item %s: %w", d.Disciplines[it.Discipline], err)
		}
		for _, m := range it.Audience {
			if err := q.AddCurriculumAudience(ctx, db.AddCurriculumAudienceParams{
				CurriculumItemID: row.ID, GroupID: groups[m.Group], Division: m.Division, Part: int16(m.Part),
			}); err != nil {
				return fmt.Errorf("curriculum audience: %w", err)
			}
		}
		for seq := 1; seq <= it.Lessons(); seq++ {
			if _, err := q.CreateLesson(ctx, db.CreateLessonParams{
				CurriculumItemID: row.ID, Seq: int16(seq), Biweekly: seq > it.Weekly,
			}); err != nil {
				return fmt.Errorf("lesson: %w", err)
			}
		}
	}
	return nil
}

// clock converts minutes since midnight to a SQL time.
func clock(minutes int) pgtype.Time {
	return pgtype.Time{Microseconds: int64(minutes) * 60 * 1_000_000, Valid: true}
}
