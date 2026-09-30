package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// ErrNoTimeGrid is returned when the time grid has not been configured yet.
var ErrNoTimeGrid = errors.New("time grid is not configured")

// LoadProblem reads reference data and lessons and builds the engine problem.
func (s *Store) LoadProblem(ctx context.Context) (*engine.Problem, error) {
	in, err := s.loadInput(ctx)
	if err != nil {
		return nil, err
	}
	return engine.NewProblem(in)
}

func (s *Store) loadInput(ctx context.Context) (engine.Input, error) {
	var in engine.Input
	grid, err := s.GetTimeGrid(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return in, ErrNoTimeGrid
	}
	if err != nil {
		return in, fmt.Errorf("load grid: %w", err)
	}
	in.Grid = domain.Grid{Days: uint8(grid.Days), PeriodsPerDay: uint8(grid.PeriodsPerDay)}

	steps := []func(context.Context, *engine.Input) error{
		s.loadBuildings, s.loadRooms, s.loadTeachers, s.loadGroups, s.loadDisciplines, s.loadLessons,
	}
	for _, step := range steps {
		if err := step(ctx, &in); err != nil {
			return in, err
		}
	}
	return in, nil
}

func (s *Store) loadBuildings(ctx context.Context, in *engine.Input) error {
	rows, err := s.ListBuildings(ctx)
	if err != nil {
		return fmt.Errorf("load buildings: %w", err)
	}
	for _, b := range rows {
		in.Buildings = append(in.Buildings, engine.Building{ID: domain.BuildingID(b.ID), Name: b.Name})
	}
	return nil
}

func (s *Store) loadRooms(ctx context.Context, in *engine.Input) error {
	rows, err := s.ListRooms(ctx)
	if err != nil {
		return fmt.Errorf("load rooms: %w", err)
	}
	avail, err := s.ListRoomAvailability(ctx)
	if err != nil {
		return fmt.Errorf("load room availability: %w", err)
	}
	byRoom := map[int64][]engine.SlotAvailability{}
	for _, a := range avail {
		sa, err := slotAvailability(a.Day, a.Period, a.Parity, a.Status)
		if err != nil {
			return fmt.Errorf("room %d: %w", a.RoomID, err)
		}
		byRoom[a.RoomID] = append(byRoom[a.RoomID], sa)
	}
	for _, r := range rows {
		in.Rooms = append(in.Rooms, engine.RoomInput{
			ID: domain.RoomID(r.ID), Name: r.Name, Building: domain.BuildingID(r.BuildingID),
			Type: r.RoomType, Capacity: int(r.Capacity), Avail: byRoom[r.ID],
		})
	}
	return nil
}

func (s *Store) loadTeachers(ctx context.Context, in *engine.Input) error {
	rows, err := s.ListTeachers(ctx)
	if err != nil {
		return fmt.Errorf("load teachers: %w", err)
	}
	avail, err := s.ListTeacherAvailability(ctx)
	if err != nil {
		return fmt.Errorf("load teacher availability: %w", err)
	}
	byTeacher := map[int64][]engine.SlotAvailability{}
	for _, a := range avail {
		sa, err := slotAvailability(a.Day, a.Period, a.Parity, a.Status)
		if err != nil {
			return fmt.Errorf("teacher %d: %w", a.TeacherID, err)
		}
		byTeacher[a.TeacherID] = append(byTeacher[a.TeacherID], sa)
	}
	for _, t := range rows {
		in.Teachers = append(in.Teachers, engine.TeacherInput{
			ID: domain.TeacherID(t.ID), Name: t.ShortName, Avail: byTeacher[t.ID],
		})
	}
	return nil
}

func (s *Store) loadGroups(ctx context.Context, in *engine.Input) error {
	rows, err := s.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("load groups: %w", err)
	}
	for _, g := range rows {
		in.Groups = append(in.Groups, engine.Group{ID: domain.GroupID(g.ID), Name: g.Name, Size: int(g.Size)})
	}
	subs, err := s.ListSubgroups(ctx)
	if err != nil {
		return fmt.Errorf("load subgroups: %w", err)
	}
	for _, sg := range subs {
		in.Subgroups = append(in.Subgroups, engine.SubgroupInput{
			Group: domain.GroupID(sg.GroupID), Division: sg.Division, Part: uint8(sg.Part), Size: int(sg.Size),
		})
	}
	return nil
}

func (s *Store) loadDisciplines(ctx context.Context, in *engine.Input) error {
	rows, err := s.ListDisciplines(ctx)
	if err != nil {
		return fmt.Errorf("load disciplines: %w", err)
	}
	for _, d := range rows {
		in.Disciplines = append(in.Disciplines, engine.Discipline{ID: domain.DisciplineID(d.ID), Name: d.Name})
	}
	return nil
}

func (s *Store) loadLessons(ctx context.Context, in *engine.Input) error {
	items, err := s.ListCurriculumItems(ctx)
	if err != nil {
		return fmt.Errorf("load curriculum: %w", err)
	}
	byID := make(map[int64]db.CurriculumItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	aud, err := s.ListCurriculumAudience(ctx)
	if err != nil {
		return fmt.Errorf("load audience: %w", err)
	}
	audience := map[int64]domain.Audience{}
	for _, a := range aud {
		audience[a.CurriculumItemID] = append(audience[a.CurriculumItemID], domain.Member{
			Group: domain.GroupID(a.GroupID), Division: a.Division, Part: uint8(a.Part),
		})
	}
	lessons, err := s.ListLessons(ctx)
	if err != nil {
		return fmt.Errorf("load lessons: %w", err)
	}
	for _, l := range lessons {
		it := byID[l.CurriculumItemID]
		in.Lessons = append(in.Lessons, engine.LessonInput{
			ID: domain.LessonID(l.ID), Item: it.ID, Discipline: domain.DisciplineID(it.DisciplineID),
			Kind: string(it.Kind), Teacher: domain.TeacherID(it.TeacherID), Audience: audience[it.ID],
			Biweekly: l.Biweekly, RoomType: it.RoomType,
		})
	}
	return nil
}

// LoadSchedule reads the assignments of a stored schedule into a new engine schedule.
func (s *Store) LoadSchedule(ctx context.Context, p *engine.Problem, scheduleID int64) (*engine.Schedule, error) {
	return LoadSchedule(ctx, s.Queries, p, scheduleID)
}

// LoadSchedule reads a schedule through q, e.g. inside a transaction.
func LoadSchedule(ctx context.Context, q *db.Queries, p *engine.Problem, scheduleID int64) (*engine.Schedule, error) {
	rows, err := q.ListAssignments(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("load assignments: %w", err)
	}
	sch := engine.NewSchedule(p)
	for _, a := range rows {
		l, ok := p.LessonIndex(domain.LessonID(a.LessonID))
		if !ok {
			return nil, fmt.Errorf("assignment for unknown lesson %d", a.LessonID)
		}
		room := engine.None
		if a.RoomID.Valid {
			r, ok := p.RoomIndex(domain.RoomID(a.RoomID.Int64))
			if !ok {
				return nil, fmt.Errorf("assignment for unknown room %d", a.RoomID.Int64)
			}
			room = r
		}
		slot, err := toSlot(a.Day, a.Period, a.Parity)
		if err != nil {
			return nil, err
		}
		sch.SetPinned(l, a.Pinned)
		if err := sch.Place(l, slot, room); err != nil {
			return nil, fmt.Errorf("lesson %d: %w", a.LessonID, err)
		}
	}
	return sch, nil
}

func toSlot(day, period int16, parity db.Parity) (domain.Slot, error) {
	par, err := domain.ParseParity(string(parity))
	if err != nil {
		return domain.Slot{}, err
	}
	return domain.Slot{Day: domain.Day(day), Period: uint8(period), Parity: par}, nil
}

func slotAvailability(day, period int16, parity db.Parity, status db.Availability) (engine.SlotAvailability, error) {
	slot, err := toSlot(day, period, parity)
	if err != nil {
		return engine.SlotAvailability{}, err
	}
	var st engine.Availability
	switch status {
	case db.AvailabilityUnavailable:
		st = engine.Unavailable
	case db.AvailabilityUndesired:
		st = engine.Undesired
	case db.AvailabilityPreferred:
		st = engine.Preferred
	default:
		return engine.SlotAvailability{}, fmt.Errorf("unknown availability %q", status)
	}
	return engine.SlotAvailability{Slot: slot, Status: st}, nil
}
