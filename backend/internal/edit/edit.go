// Package edit applies schedule edits to stored schedules: EvaluateMove (dry run) and
// CommitMove (persisted with an audit row in one transaction), arch §10.1–10.2.
//
// Edits address lessons and rooms by database ID; the package converts them to engine moves.
package edit

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/move"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Slot is the JSON form of a slot: {"day":"TU","period":3,"parity":"every"}.
type Slot struct {
	Day    string `json:"day"`
	Period uint8  `json:"period"`
	Parity string `json:"parity,omitempty"` // defaults to "every"
}

// ToDomain parses the slot.
func (s Slot) ToDomain() (domain.Slot, error) {
	d, err := domain.ParseDay(s.Day)
	if err != nil {
		return domain.Slot{}, err
	}
	par := domain.EveryWeek
	if s.Parity != "" {
		if par, err = domain.ParseParity(s.Parity); err != nil {
			return domain.Slot{}, err
		}
	}
	return domain.Slot{Day: d, Period: s.Period, Parity: par}, nil
}

// SlotOf converts a domain slot.
func SlotOf(s domain.Slot) Slot {
	return Slot{Day: s.Day.String(), Period: s.Period, Parity: s.Parity.String()}
}

// Change sets the placement of one lesson.
type Change struct {
	Lesson domain.LessonID `json:"lesson_id"`
	// Unplace removes the lesson from the schedule; Slot and Room are ignored.
	Unplace bool `json:"unplace,omitempty"`
	Slot    Slot `json:"slot"`
	// Room is 0 when the lesson has no room.
	Room domain.RoomID `json:"room_id,omitempty"`
	// Pinned sets the pinned flag; nil keeps the current value.
	Pinned *bool `json:"pinned,omitempty"`
}

// Violation is a hard-rule violation expressed with database IDs.
type Violation struct {
	Rule       string            `json:"rule"`
	Strictness string            `json:"strictness"`
	Lessons    []domain.LessonID `json:"lesson_ids"`
	Resource   *Resource         `json:"resource,omitempty"`
	Slot       *Slot             `json:"slot,omitempty"`
	Message    string            `json:"message"`
}

// Resource identifies what a violation is about.
type Resource struct {
	Kind string `json:"kind"` // teacher, group or room
	ID   int64  `json:"id"`
}

// Evaluation is the effect of a move.
type Evaluation struct {
	Violations []Violation    `json:"violations"`
	Introduced []Violation    `json:"introduced"`
	Resolved   []Violation    `json:"resolved"`
	SoftDelta  soft.Breakdown `json:"-"`
	Soft       map[string]int `json:"soft_delta"`
	Weighted   float64        `json:"soft_delta_weighted"`
	HardCount  int            `json:"hard_count"`
	// Blocking counts hard violations that prevent a commit. H7 (unplaced lesson or missing
	// room) is reported but never blocks: completeness is a property of the whole schedule,
	// and a dispatcher must be able to take a lesson out or leave its room open.
	Blocking int `json:"blocking_count"`
}

// Errors.
var (
	ErrUnknownLesson    = errors.New("unknown lesson")
	ErrUnknownRoom      = errors.New("unknown room")
	ErrScheduleNotFound = errors.New("schedule not found")
	// ErrHardViolations is returned by Commit when the move breaks hard rules; the evaluation
	// is returned alongside.
	ErrHardViolations = errors.New("move violates hard constraints")
)

// Service evaluates and commits edits.
type Service struct {
	st      *store.Store
	policy  hard.Policy
	weights soft.Weights
}

// New creates a service with the given hard-rule policy and soft weights.
func New(st *store.Store, pol hard.Policy, w soft.Weights) *Service {
	return &Service{st: st, policy: pol, weights: w}
}

// Evaluate computes the effect of the changes on a stored schedule without writing anything.
func (s *Service) Evaluate(ctx context.Context, scheduleID int64, changes []Change) (Evaluation, error) {
	p, err := s.st.LoadProblem(ctx)
	if err != nil {
		return Evaluation{}, err
	}
	if _, err := s.st.GetSchedule(ctx, scheduleID); errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, ErrScheduleNotFound
	} else if err != nil {
		return Evaluation{}, err
	}
	sch, err := s.st.LoadSchedule(ctx, p, scheduleID)
	if err != nil {
		return Evaluation{}, err
	}
	ev, _, err := s.evaluate(p, sch, changes)
	return ev, err
}

// Commit evaluates the changes against the current state of the schedule and, if no hard
// rule is broken, stores them together with an audit row. The schedule row is locked for the
// duration, so concurrent commits to one schedule are serialized.
func (s *Service) Commit(ctx context.Context, actor store.Actor, scheduleID int64, changes []Change, reason string) (Evaluation, error) {
	p, err := s.st.LoadProblem(ctx)
	if err != nil {
		return Evaluation{}, err
	}
	var ev Evaluation
	_, err = s.st.WithAudit(ctx, actor, store.Change{Entity: "schedule", EntityID: fmt.Sprint(scheduleID), Reason: reason},
		func(q *db.Queries, _ pgx.Tx, audit *store.Change) error {
			if _, err := q.LockSchedule(ctx, scheduleID); errors.Is(err, pgx.ErrNoRows) {
				return ErrScheduleNotFound
			} else if err != nil {
				return err
			}
			sch, err := store.LoadSchedule(ctx, q, p, scheduleID)
			if err != nil {
				return err
			}
			var m move.Move
			ev, m, err = s.evaluate(p, sch, changes)
			if err != nil {
				return err
			}
			if ev.Blocking > 0 {
				return ErrHardViolations
			}
			before := snapshot(p, sch, m.Lessons())
			if _, err := move.Apply(sch, m); err != nil {
				return err
			}
			after := snapshot(p, sch, m.Lessons())
			audit.Before, audit.After = before, after
			audit.Diff = map[string]any{"soft_delta": ev.Soft, "soft_delta_weighted": ev.Weighted}
			return store.MapError(write(ctx, q, scheduleID, after))
		})
	return ev, err
}

// evaluate converts the changes and runs the engine evaluation.
func (s *Service) evaluate(p *engine.Problem, sch *engine.Schedule, changes []Change) (Evaluation, move.Move, error) {
	m, err := toMove(p, sch, changes)
	if err != nil {
		return Evaluation{}, move.Move{}, err
	}
	res, err := move.NewEvaluator(p, s.policy).Evaluate(sch, m)
	if err != nil {
		return Evaluation{}, move.Move{}, err
	}
	delta := res.SoftDelta()
	ev := Evaluation{
		Violations: convert(p, res.Violations),
		Introduced: convert(p, res.Introduced),
		Resolved:   convert(p, res.Resolved),
		SoftDelta:  delta,
		Soft:       map[string]int{},
		Weighted:   delta.Total(s.weights),
		HardCount:  res.HardCount(),
	}
	for _, v := range res.Violations {
		if v.Strictness == hard.Hard && v.Rule != hard.H7Completeness {
			ev.Blocking++
		}
	}
	for c := range soft.NumCriteria {
		if delta[c] != 0 {
			ev.Soft[c.Key()] = delta[c]
		}
	}
	return ev, m, nil
}

func toMove(p *engine.Problem, sch *engine.Schedule, changes []Change) (move.Move, error) {
	var m move.Move
	for _, c := range changes {
		l, ok := p.LessonIndex(c.Lesson)
		if !ok {
			return m, fmt.Errorf("%w: %d", ErrUnknownLesson, c.Lesson)
		}
		to := sch.Assignment(l)
		if c.Unplace {
			to = engine.Assignment{Room: engine.None, Pinned: to.Pinned}
		} else {
			room := engine.None
			if c.Room != 0 {
				r, ok := p.RoomIndex(c.Room)
				if !ok {
					return m, fmt.Errorf("%w: %d", ErrUnknownRoom, c.Room)
				}
				room = r
			}
			slot, err := c.Slot.ToDomain()
			if err != nil {
				return m, err
			}
			to = engine.Assignment{Placed: true, Slot: slot, Room: room, Pinned: to.Pinned}
		}
		if c.Pinned != nil {
			to.Pinned = *c.Pinned
		}
		m.Changes = append(m.Changes, move.Change{Lesson: l, To: to})
	}
	if len(m.Changes) == 0 {
		return m, move.ErrEmptyMove
	}
	return m, nil
}

// Placement is the stored state of one lesson, used for audit before/after.
type Placement struct {
	Lesson domain.LessonID `json:"lesson_id"`
	Placed bool            `json:"placed"`
	Day    string          `json:"day,omitempty"`
	Period uint8           `json:"period,omitempty"`
	Parity string          `json:"parity,omitempty"`
	Room   domain.RoomID   `json:"room_id,omitempty"`
	Pinned bool            `json:"pinned"`
}

func snapshot(p *engine.Problem, sch *engine.Schedule, lessons []int32) []Placement {
	out := make([]Placement, 0, len(lessons))
	for _, l := range lessons {
		a := sch.Assignment(l)
		pl := Placement{Lesson: p.Lessons[l].ID, Placed: a.Placed, Pinned: a.Pinned}
		if a.Placed {
			pl.Day, pl.Period, pl.Parity = a.Slot.Day.String(), a.Slot.Period, a.Slot.Parity.String()
			if a.Room != engine.None {
				pl.Room = p.Rooms[a.Room].ID
			}
		}
		out = append(out, pl)
	}
	return out
}

// write stores the new placements. All touched rows are deleted first and then re-inserted, so
// a batch (e.g. a swap) never passes through a state the EXCLUDE constraints would reject.
func write(ctx context.Context, q *db.Queries, scheduleID int64, placements []Placement) error {
	for _, pl := range placements {
		if _, err := q.DeleteAssignment(ctx, db.DeleteAssignmentParams{ScheduleID: scheduleID, LessonID: int64(pl.Lesson)}); err != nil {
			return err
		}
	}
	for _, pl := range placements {
		if !pl.Placed {
			continue // no row; the pinned flag lives on the assignment row only
		}
		day, err := domain.ParseDay(pl.Day)
		if err != nil {
			return err
		}
		params := db.UpsertAssignmentParams{
			ScheduleID: scheduleID, LessonID: int64(pl.Lesson), Day: int16(day), Period: int16(pl.Period),
			Parity: db.Parity(pl.Parity), Pinned: pl.Pinned,
		}
		if pl.Room != 0 {
			params.RoomID = pgtype.Int8{Int64: int64(pl.Room), Valid: true}
		}
		if err := q.UpsertAssignment(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

var (
	strictnessNames = map[hard.Strictness]string{hard.Hard: "hard", hard.HeavySoft: "heavy_soft", hard.Off: "off"}
	kindNames       = map[hard.ResourceKind]string{hard.TeacherResource: "teacher", hard.GroupResource: "group", hard.RoomResource: "room"}
)

// ConvertViolations expresses engine violations with database IDs, in the same format
// EvaluateMove uses; the solver's stage A report uses it too (arch §5).
func ConvertViolations(p *engine.Problem, vs []hard.Violation) []Violation {
	return convert(p, vs)
}

func convert(p *engine.Problem, vs []hard.Violation) []Violation {
	out := make([]Violation, 0, len(vs))
	for _, v := range vs {
		cv := Violation{Rule: v.Rule.String(), Strictness: strictnessNames[v.Strictness], Message: v.Message}
		for _, l := range v.Lessons {
			cv.Lessons = append(cv.Lessons, p.Lessons[l].ID)
		}
		if v.Kind != hard.NoResource {
			r := &Resource{Kind: kindNames[v.Kind]}
			switch v.Kind {
			case hard.TeacherResource:
				r.ID = int64(p.Teachers[v.Resource].ID)
			case hard.GroupResource:
				r.ID = int64(p.Groups[v.Resource].ID)
			case hard.RoomResource:
				r.ID = int64(p.Rooms[v.Resource].ID)
			}
			cv.Resource = r
		}
		if v.Slot.Period != 0 {
			sl := SlotOf(v.Slot)
			cv.Slot = &sl
		}
		out = append(out, cv)
	}
	return out
}
