// Package move defines schedule edits and evaluates them without side effects.
//
// Every edit — drag and drop by a human, an MCP tool call by the AI agent, or a local-search
// step of a solver worker — is a Move evaluated here (arch §10.1).
package move

import (
	"errors"
	"fmt"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
)

// Change sets the full assignment of one lesson. To.Placed == false removes the lesson from
// the schedule.
type Change struct {
	Lesson int32
	To     engine.Assignment
}

// Move is an atomic batch of changes: it is evaluated and applied as a whole, so a swap never
// passes through an intermediate state.
type Move struct {
	Changes []Change
}

// Relocate moves a lesson to a slot and room, keeping its pinned flag.
func Relocate(s *engine.Schedule, l int32, slot domain.Slot, room int32) Move {
	a := s.Assignment(l)
	return Move{Changes: []Change{{Lesson: l, To: engine.Assignment{Placed: true, Slot: slot, Room: room, Pinned: a.Pinned}}}}
}

// ChangeRoom keeps the lesson's slot and changes its room.
func ChangeRoom(s *engine.Schedule, l int32, room int32) Move {
	a := s.Assignment(l)
	a.Room = room
	return Move{Changes: []Change{{Lesson: l, To: a}}}
}

// Swap exchanges the slots and rooms of two lessons; pinned flags stay with the lessons.
func Swap(s *engine.Schedule, a, b int32) Move {
	x, y := s.Assignment(a), s.Assignment(b)
	x.Slot, y.Slot = y.Slot, x.Slot
	x.Room, y.Room = y.Room, x.Room
	x.Placed, y.Placed = y.Placed, x.Placed
	return Move{Changes: []Change{{Lesson: a, To: x}, {Lesson: b, To: y}}}
}

// Unplace removes a lesson from the schedule.
func Unplace(s *engine.Schedule, l int32) Move {
	return Move{Changes: []Change{{Lesson: l, To: engine.Assignment{Room: engine.None, Pinned: s.Assignment(l).Pinned}}}}
}

// Pin sets the pinned flag without moving the lesson.
func Pin(s *engine.Schedule, l int32, pinned bool) Move {
	a := s.Assignment(l)
	a.Pinned = pinned
	return Move{Changes: []Change{{Lesson: l, To: a}}}
}

// Then concatenates moves into one batch. Later changes of the same lesson win.
func (m Move) Then(o Move) Move {
	return Move{Changes: append(slices.Clone(m.Changes), o.Changes...)}
}

// Lessons returns the distinct lessons the move touches.
func (m Move) Lessons() []int32 {
	out := make([]int32, 0, len(m.Changes))
	for _, c := range m.Changes {
		if !slices.Contains(out, c.Lesson) {
			out = append(out, c.Lesson)
		}
	}
	return out
}

// Errors returned for malformed moves.
var (
	ErrEmptyMove     = errors.New("move has no changes")
	ErrUnknownLesson = errors.New("unknown lesson")
)

// Validate checks that every change is structurally possible (slot in grid, parity matches
// the lesson, room exists). It does not look at constraints.
func Validate(s *engine.Schedule, m Move) error {
	if len(m.Changes) == 0 {
		return ErrEmptyMove
	}
	for _, c := range m.Changes {
		if c.Lesson < 0 || int(c.Lesson) >= len(s.P.Lessons) {
			return fmt.Errorf("%w: %d", ErrUnknownLesson, c.Lesson)
		}
		if !c.To.Placed {
			continue
		}
		if err := s.CanPlace(c.Lesson, c.To.Slot, c.To.Room); err != nil {
			return err
		}
	}
	return nil
}

// Apply performs the move on s and returns the move that undoes it. Nothing is changed if the
// move is invalid.
func Apply(s *engine.Schedule, m Move) (undo Move, err error) {
	if err := Validate(s, m); err != nil {
		return Move{}, err
	}
	undo.Changes = make([]Change, 0, len(m.Changes))
	for _, l := range m.Lessons() {
		undo.Changes = append(undo.Changes, Change{Lesson: l, To: s.Assignment(l)})
	}
	for _, c := range m.Changes {
		set(s, c)
	}
	return undo, nil
}

func set(s *engine.Schedule, c Change) {
	if c.To.Placed {
		// Validated before: Place cannot fail here.
		if err := s.Place(c.Lesson, c.To.Slot, c.To.Room); err != nil {
			panic(err)
		}
	} else {
		s.Unplace(c.Lesson)
	}
	s.SetPinned(c.Lesson, c.To.Pinned)
}

// Result describes the effect of a move.
type Result struct {
	// Violations involving the moved lessons after the move (hard and heavy-soft).
	Violations []hard.Violation
	// Introduced are violations present after the move but not before; Resolved the opposite.
	Introduced []hard.Violation
	Resolved   []hard.Violation
	// SoftBefore and SoftAfter are the penalties of the scopes the move touches, so
	// SoftAfter - SoftBefore is the change of the whole schedule's penalty.
	SoftBefore soft.Breakdown
	SoftAfter  soft.Breakdown
}

// SoftDelta returns the per-criterion change of the soft penalty.
func (r Result) SoftDelta() soft.Breakdown { return r.SoftAfter.Sub(r.SoftBefore) }

// HardCount returns the number of violations with Hard strictness after the move.
func (r Result) HardCount() int {
	n := 0
	for _, v := range r.Violations {
		if v.Strictness == hard.Hard {
			n++
		}
	}
	return n
}

// Evaluator evaluates moves for one problem.
type Evaluator struct {
	Soft   *soft.Evaluator
	Policy hard.Policy
}

// NewEvaluator creates an evaluator with the given hard-rule policy.
func NewEvaluator(p *engine.Problem, pol hard.Policy) *Evaluator {
	return &Evaluator{Soft: soft.New(p), Policy: pol}
}

// Evaluate computes the effect of the move in dry-run mode: s is modified temporarily and
// restored before returning.
func (e *Evaluator) Evaluate(s *engine.Schedule, m Move) (Result, error) {
	if err := Validate(s, m); err != nil {
		return Result{}, err
	}
	lessons := m.Lessons()

	var scopes soft.ScopeSet
	for _, l := range lessons {
		e.Soft.AddLesson(&scopes, l, s.Assignment(l))
	}
	for _, c := range m.Changes {
		e.Soft.AddLesson(&scopes, c.Lesson, c.To)
	}

	var res Result
	res.SoftBefore = e.Soft.Measure(s, &scopes)
	before := hard.Involving(hard.Check(s, e.Policy), lessons...)

	undo, err := Apply(s, m)
	if err != nil {
		return Result{}, err
	}
	res.SoftAfter = e.Soft.Measure(s, &scopes)
	res.Violations = hard.Involving(hard.Check(s, e.Policy), lessons...)
	if _, err := Apply(s, undo); err != nil {
		panic(fmt.Sprintf("move: undo failed: %v", err))
	}

	res.Introduced = difference(res.Violations, before)
	res.Resolved = difference(before, res.Violations)
	return res, nil
}

// difference returns violations of a that have no equal counterpart in b.
func difference(a, b []hard.Violation) []hard.Violation {
	var out []hard.Violation
	for _, v := range a {
		if !slices.ContainsFunc(b, func(w hard.Violation) bool { return sameViolation(v, w) }) {
			out = append(out, v)
		}
	}
	return out
}

func sameViolation(a, b hard.Violation) bool {
	return a.Rule == b.Rule && a.Kind == b.Kind && a.Resource == b.Resource && a.Slot == b.Slot &&
		slices.Equal(a.Lessons, b.Lessons)
}
