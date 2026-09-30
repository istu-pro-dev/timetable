package edit_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/edit"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/seed"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

var admin = store.Actor{Type: db.ActorTypeHuman, ID: "42"}

type env struct {
	t        *testing.T
	st       *store.Store
	svc      *edit.Service
	schedule int64
	// a and b are weekly lessons of the same teacher; ra and rb suitable rooms for them.
	a, b   domain.LessonID
	ra, rb domain.RoomID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := storetest.New(t)
	ctx := t.Context()
	if _, err := seed.Load(ctx, st, seed.Build()); err != nil {
		t.Fatal(err)
	}
	sch, err := st.CreateSchedule(ctx, "draft")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, svc: edit.New(st, nil, soft.DefaultWeights()), schedule: sch.ID}

	p, err := st.LoadProblem(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Find two weekly lessons of one teacher from different curriculum items with a
	// sufficiently large suitable room each.
	roomFor := func(l int) (domain.RoomID, bool) {
		les := p.Lessons[l]
		for _, r := range les.Rooms {
			if p.Rooms[r].Capacity >= les.Size {
				return p.Rooms[r].ID, true
			}
		}
		return 0, false
	}
	for i := range p.Lessons {
		for j := i + 1; j < len(p.Lessons); j++ {
			x, y := p.Lessons[i], p.Lessons[j]
			if x.Teacher != y.Teacher || x.Item == y.Item || x.Biweekly || y.Biweekly {
				continue
			}
			rx, okx := roomFor(i)
			ry, oky := roomFor(j)
			if okx && oky && rx != ry {
				e.a, e.b, e.ra, e.rb = x.ID, y.ID, rx, ry
				return e
			}
		}
	}
	t.Fatal("seed has no suitable lesson pair")
	return nil
}

// freeSlot finds a slot where placing lesson l into room r violates nothing.
func (e *env) freeSlot(l domain.LessonID, r domain.RoomID, not ...edit.Slot) edit.Slot {
	e.t.Helper()
	for _, d := range []string{"TU", "WE", "TH", "MO", "FR", "SA"} {
		for p := uint8(2); p <= 6; p++ {
			sl := edit.Slot{Day: d, Period: p}
			if slices.Contains(not, sl) {
				continue
			}
			ev, err := e.svc.Evaluate(e.t.Context(), e.schedule, []edit.Change{{Lesson: l, Slot: sl, Room: r}})
			if err != nil {
				e.t.Fatal(err)
			}
			if len(ev.Violations) == 0 {
				return sl
			}
		}
	}
	e.t.Fatal("no free slot")
	return edit.Slot{}
}

func (e *env) assignments() []db.Assignment {
	e.t.Helper()
	rows, err := e.st.ListAssignments(e.t.Context(), e.schedule)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) auditCount() int {
	e.t.Helper()
	rows, err := e.st.ListAudit(e.t.Context(), db.ListAuditParams{Limit: 1000})
	if err != nil {
		e.t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.Entity == "schedule" {
			n++
		}
	}
	return n
}

func TestCommitEvaluateSwap(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()

	// Place a and b in different slots.
	sa := e.freeSlot(e.a, e.ra)
	if _, err := e.svc.Commit(ctx, admin, e.schedule, []edit.Change{{Lesson: e.a, Slot: sa, Room: e.ra}}, "place a"); err != nil {
		t.Fatal(err)
	}
	sb := e.freeSlot(e.b, e.rb, sa)
	if _, err := e.svc.Commit(ctx, admin, e.schedule, []edit.Change{{Lesson: e.b, Slot: sb, Room: e.rb}}, "place b"); err != nil {
		t.Fatal(err)
	}
	if n := len(e.assignments()); n != 2 {
		t.Fatalf("assignments = %d", n)
	}
	if n := e.auditCount(); n != 2 {
		t.Fatalf("audit rows = %d", n)
	}

	// Dry run: b onto a's slot breaks H1 (same teacher) and writes nothing.
	before := e.assignments()
	ev, err := e.svc.Evaluate(ctx, e.schedule, []edit.Change{{Lesson: e.b, Slot: sa, Room: e.rb}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.HardCount == 0 || !slices.ContainsFunc(ev.Introduced, func(v edit.Violation) bool { return v.Rule == "H1" }) {
		t.Fatalf("evaluation = %+v", ev)
	}
	h1 := ev.Introduced[slices.IndexFunc(ev.Introduced, func(v edit.Violation) bool { return v.Rule == "H1" })]
	if h1.Resource == nil || h1.Resource.Kind != "teacher" || h1.Slot == nil || h1.Slot.Day != sa.Day ||
		!slices.Contains(h1.Lessons, e.a) || !slices.Contains(h1.Lessons, e.b) {
		t.Fatalf("H1 = %+v", h1)
	}
	if !slices.Equal(e.assignments(), before) {
		t.Fatal("Evaluate wrote to the database")
	}

	// Commit of the same move is refused and leaves no trace.
	if _, err := e.svc.Commit(ctx, admin, e.schedule, []edit.Change{{Lesson: e.b, Slot: sa, Room: e.rb}}, "bad"); !errors.Is(err, edit.ErrHardViolations) {
		t.Fatalf("err = %v, want ErrHardViolations", err)
	}
	if !slices.Equal(e.assignments(), before) || e.auditCount() != 2 {
		t.Fatal("refused commit changed the database")
	}

	// Swapping the slots of the two lessons as one batch is fine even though each half alone
	// conflicts (same teacher).
	pinned := true
	swap := []edit.Change{
		{Lesson: e.a, Slot: sb, Room: e.ra, Pinned: &pinned},
		{Lesson: e.b, Slot: sa, Room: e.rb},
	}
	ev, err = e.svc.Commit(ctx, admin, e.schedule, swap, "swap")
	if err != nil {
		t.Fatalf("swap: %v (%+v)", err, ev.Violations)
	}
	got := map[int64]db.Assignment{}
	for _, a := range e.assignments() {
		got[a.LessonID] = a
	}
	if a := got[int64(e.a)]; a.Day != dayOf(t, sb) || a.Period != int16(sb.Period) || !a.Pinned {
		t.Fatalf("a after swap = %+v", a)
	}
	if b := got[int64(e.b)]; b.Day != dayOf(t, sa) || b.Period != int16(sa.Period) || b.Pinned {
		t.Fatalf("b after swap = %+v", b)
	}

	// The audit row carries the actor and before/after placements.
	rows, err := e.st.ListAudit(ctx, db.ListAuditParams{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	last := rows[0]
	if last.ActorID != "42" || last.Reason != "swap" || last.EntityID == "" {
		t.Fatalf("audit = %+v", last)
	}
	var after []edit.Placement
	if err := json.Unmarshal(last.After, &after); err != nil || len(after) != 2 || !after[0].Pinned {
		t.Fatalf("audit after = %s (%v)", last.After, err)
	}

	// Unplacing removes the row: H7 is reported but does not block.
	ev, err = e.svc.Commit(ctx, admin, e.schedule, []edit.Change{{Lesson: e.b, Unplace: true}}, "remove")
	if err != nil {
		t.Fatal(err)
	}
	if ev.HardCount != 1 || ev.Blocking != 0 || ev.Violations[0].Rule != "H7" {
		t.Fatalf("unplace evaluation = %+v", ev)
	}
	if n := len(e.assignments()); n != 1 {
		t.Fatalf("assignments after unplace = %d", n)
	}
}

func dayOf(t *testing.T, s edit.Slot) int16 {
	t.Helper()
	d, err := s.ToDomain()
	if err != nil {
		t.Fatal(err)
	}
	return int16(d.Day)
}

func TestErrors(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	ok := e.freeSlot(e.a, e.ra)
	tests := []struct {
		name     string
		schedule int64
		changes  []edit.Change
		want     error
	}{
		{"unknown schedule", 999, []edit.Change{{Lesson: e.a, Slot: ok, Room: e.ra}}, edit.ErrScheduleNotFound},
		{"unknown lesson", e.schedule, []edit.Change{{Lesson: 999999, Slot: ok}}, edit.ErrUnknownLesson},
		{"unknown room", e.schedule, []edit.Change{{Lesson: e.a, Slot: ok, Room: 999999}}, edit.ErrUnknownRoom},
		{"bad day", e.schedule, []edit.Change{{Lesson: e.a, Slot: edit.Slot{Day: "XX", Period: 1}}}, nil},
		{"bad parity", e.schedule, []edit.Change{{Lesson: e.a, Slot: edit.Slot{Day: "MO", Period: 1, Parity: "x"}}}, nil},
		{"empty", e.schedule, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errE := e.svc.Evaluate(ctx, tt.schedule, tt.changes)
			_, errC := e.svc.Commit(ctx, admin, tt.schedule, tt.changes, "")
			for _, err := range []error{errE, errC} {
				if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			}
		})
	}
	if len(e.assignments()) != 0 {
		t.Fatal("failed commits wrote assignments")
	}
}

func TestSlotConversion(t *testing.T) {
	s := edit.SlotOf(domain.Slot{Day: domain.Thursday, Period: 4, Parity: domain.EvenWeek})
	if s != (edit.Slot{Day: "TH", Period: 4, Parity: "even"}) {
		t.Fatalf("SlotOf = %+v", s)
	}
	d, err := edit.Slot{Day: "TH", Period: 4}.ToDomain()
	if err != nil || d.Parity != domain.EveryWeek || d.Day != domain.Thursday {
		t.Fatalf("ToDomain = %+v, %v", d, err)
	}
}
