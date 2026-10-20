// Package stageb is solver stage B (arch §3): simulated annealing over a complete schedule.
//
// It improves the weighted soft penalty without breaking hard rules: a move is accepted only
// if the moved lessons end up with no more hard conflicts than before (none, when the start is
// feasible). An optional penalty per lesson that differs from a baseline keeps the result
// close to an existing draft. The search is anytime: the best schedule so far can be read at
// any moment, and the run stops on its time or step budget or when the context is cancelled.
package stageb

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/move"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/solver"
)

// Options configures a run.
type Options struct {
	// Weights of the soft criteria; the zero value means soft.DefaultWeights().
	Weights *soft.Weights
	// Policy is the strictness of hard rules; only Hard rules are enforced.
	Policy hard.Policy
	// Budget bounds the run time; zero means 60 seconds.
	Budget time.Duration
	// MaxSteps bounds the number of attempted moves; zero means no limit. When set, cooling
	// follows the step count instead of the clock, which makes runs reproducible.
	MaxSteps int64
	Seed     uint64
	// ChangePenalty is added for every lesson whose slot or room differs from Baseline.
	ChangePenalty float64
	// Baseline is the reference placement for ChangePenalty; nil means the starting schedule.
	Baseline []engine.Assignment
	// T0 and TEnd are the start and final temperatures; zero means automatic: T0 accepts an
	// average uphill move with probability 0.3, TEnd is T0/1000.
	T0, TEnd float64
	// Resume continues from a saved state (same problem and options).
	Resume *State
}

// hardWeight makes one hard conflict cost more than any realistic soft gain, so an
// infeasible start (stage A fallback) is repaired first.
const hardWeight = 1e6

// Snapshot is the best schedule found so far.
type Snapshot struct {
	Assignments []engine.Assignment
	Cost        float64
	Soft        soft.Breakdown
	Changes     int
	Step        int64
	Elapsed     time.Duration
}

// State is what a checkpoint needs to continue a run (issue #52).
type State struct {
	Step        int64
	Elapsed     time.Duration
	Temperature float64
	T0, TEnd    float64
	RNG         []byte
	Current     []engine.Assignment
	Best        *Snapshot
}

// Stats summarises a run.
type Stats struct {
	Steps, Accepted, Improved, Infeasible int64
	Duration                              time.Duration
	Initial, Final                        Snapshot
	// HardBefore and HardAfter count Hard violations (hard.Check) at the start and the end.
	HardBefore, HardAfter int
}

// Optimizer runs stage B on one schedule. Run must not be called concurrently; Best may be
// called from any goroutine.
type Optimizer struct {
	s     *engine.Schedule
	p     *engine.Problem
	g     *solver.Graph
	opt   Options
	w     soft.Weights
	ev    *soft.Evaluator
	chk   *checker
	rng   *rand.Rand
	pcg   *rand.PCG
	base  []engine.Assignment
	moves []int32 // lessons that may move (not pinned)

	cur     float64 // current cost
	curSoft soft.Breakdown
	changes int
	// conflicts is the sum of local hard conflicts over all lessons. Moves change it by the
	// moved lessons' share only, so it is exact for feasible schedules and a guide otherwise.
	conflicts int

	scopes soft.ScopeSet
	best   atomic.Pointer[Snapshot]
	state  State
}

// New prepares an optimizer for s, which it modifies in place.
func New(g *solver.Graph, s *engine.Schedule, opt Options) *Optimizer {
	if opt.Budget == 0 {
		opt.Budget = 60 * time.Second
	}
	o := &Optimizer{s: s, p: s.P, g: g, opt: opt, w: soft.DefaultWeights()}
	if opt.Weights != nil {
		o.w = *opt.Weights
	}
	o.ev = soft.New(o.p)
	o.chk = newChecker(o.p, opt.Policy, g.Siblings)
	o.pcg = rand.NewPCG(opt.Seed, opt.Seed^0x9E3779B97F4A7C15)
	o.rng = rand.New(o.pcg)

	if r := opt.Resume; r != nil {
		_ = o.pcg.UnmarshalBinary(r.RNG)
		for l, a := range r.Current {
			o.set(int32(l), a)
		}
	}
	o.base = opt.Baseline
	if o.base == nil {
		o.base = s.Assignments()
	}
	for l := range o.p.Lessons {
		if !s.Assignment(int32(l)).Pinned {
			o.moves = append(o.moves, int32(l))
		}
	}
	o.recompute()
	if r := opt.Resume; r != nil && r.Best != nil {
		o.best.Store(r.Best)
	} else {
		o.saveBest(0, 0)
	}
	return o
}

func (o *Optimizer) set(l int32, a engine.Assignment) {
	if a.Placed {
		_ = o.s.Place(l, a.Slot, a.Room)
	} else {
		o.s.Unplace(l)
	}
	o.s.SetPinned(l, a.Pinned)
}

// recompute derives the current cost from scratch.
func (o *Optimizer) recompute() {
	o.curSoft = o.ev.Full(o.s)
	o.changes, o.conflicts = 0, 0
	for l := range o.p.Lessons {
		li := int32(l)
		if differs(o.s.Assignment(li), o.base[l]) {
			o.changes++
		}
		o.conflicts += o.chk.local(o.s, li)
	}
	o.cur = o.cost(o.curSoft, o.changes, o.conflicts)
}

func (o *Optimizer) cost(b soft.Breakdown, changes, conflicts int) float64 {
	return b.Total(o.w) + o.opt.ChangePenalty*float64(changes) + hardWeight*float64(conflicts)
}

func differs(a, b engine.Assignment) bool {
	return a.Placed != b.Placed || a.Placed && (a.Slot != b.Slot || a.Room != b.Room)
}

// Best returns the best schedule found so far.
func (o *Optimizer) Best() *Snapshot { return o.best.Load() }

// State returns the state to resume from; valid after Run returns.
func (o *Optimizer) State() State { return o.state }

func (o *Optimizer) saveBest(step int64, elapsed time.Duration) {
	o.best.Store(&Snapshot{
		Assignments: o.s.Assignments(), Cost: o.cur, Soft: o.curSoft, Changes: o.changes,
		Step: step, Elapsed: elapsed,
	})
}

// delta is the evaluated effect of a candidate move.
type delta struct {
	soft      soft.Breakdown
	changes   int
	conflicts int
	feasible  bool
}

// try applies m, measures it and leaves it applied; the caller undoes it if rejected.
func (o *Optimizer) try(m move.Move) (delta, move.Move, bool) {
	lessons := m.Lessons()
	o.scopes.Reset()
	var d delta
	before := 0
	for _, l := range lessons {
		a := o.s.Assignment(l)
		o.ev.AddLesson(&o.scopes, l, a)
		before += o.chk.local(o.s, l)
		if differs(a, o.base[l]) {
			d.changes--
		}
	}
	for _, c := range m.Changes {
		o.ev.AddLesson(&o.scopes, c.Lesson, c.To)
	}
	softBefore := o.ev.Measure(o.s, &o.scopes)
	undo, err := move.Apply(o.s, m)
	if err != nil {
		return delta{}, move.Move{}, false
	}
	after := 0
	for _, l := range lessons {
		after += o.chk.local(o.s, l)
		if differs(o.s.Assignment(l), o.base[l]) {
			d.changes++
		}
	}
	d.conflicts = after - before
	d.feasible = after <= before
	d.soft = o.ev.Measure(o.s, &o.scopes).Sub(softBefore)
	return d, undo, true
}

// Run searches until the budget is spent or ctx is cancelled, then leaves the best schedule
// in s.
func (o *Optimizer) Run(ctx context.Context) Stats {
	start := time.Now()
	st := Stats{Initial: *o.Best(), HardBefore: o.hardCount()}
	if len(o.moves) == 0 {
		st.Final, st.HardAfter = st.Initial, st.HardBefore
		return st
	}
	t0, tEnd := o.opt.T0, o.opt.TEnd
	step, elapsed0 := int64(0), time.Duration(0)
	if r := o.opt.Resume; r != nil {
		t0, tEnd, step, elapsed0 = r.T0, r.TEnd, r.Step, r.Elapsed
	}
	if t0 == 0 {
		t0 = o.calibrate()
	}
	if tEnd == 0 {
		tEnd = t0 / 1000
	}
	temp := t0
	firstStep := step
	for o.opt.MaxSteps == 0 || step < o.opt.MaxSteps {
		if step&255 == 0 {
			elapsed := elapsed0 + time.Since(start)
			frac := elapsed.Seconds() / o.opt.Budget.Seconds()
			if o.opt.MaxSteps > 0 {
				frac = float64(step) / float64(o.opt.MaxSteps)
			}
			if frac >= 1 || ctx.Err() != nil || (o.opt.MaxSteps == 0 && elapsed >= o.opt.Budget) {
				break
			}
			temp = t0 * math.Pow(tEnd/t0, frac)
		}
		step++
		m, ok := o.propose()
		if !ok {
			continue
		}
		d, undo, ok := o.try(m)
		if !ok {
			continue
		}
		if !d.feasible {
			st.Infeasible++
			o.undo(undo)
			continue
		}
		dc := d.soft.Total(o.w) + o.opt.ChangePenalty*float64(d.changes) + hardWeight*float64(d.conflicts)
		if dc <= 0 || o.rng.Float64() < math.Exp(-dc/temp) {
			st.Accepted++
			o.cur += dc
			o.curSoft = o.curSoft.Add(d.soft)
			o.changes += d.changes
			o.conflicts += d.conflicts
			if o.cur < o.Best().Cost-1e-9 {
				st.Improved++
				o.saveBest(step, elapsed0+time.Since(start))
			}
		} else {
			o.undo(undo)
		}
	}

	best := o.Best()
	for l, a := range best.Assignments {
		o.set(int32(l), a)
	}
	o.recompute()
	rng, _ := o.pcg.MarshalBinary()
	o.state = State{
		Step: step, Elapsed: elapsed0 + time.Since(start), Temperature: temp, T0: t0, TEnd: tEnd,
		RNG: rng, Current: o.s.Assignments(), Best: best,
	}
	st.Steps = step - firstStep
	st.Duration = time.Since(start)
	st.Final = *best
	st.HardAfter = o.hardCount()
	return st
}

func (o *Optimizer) hardCount() int {
	n, _ := hard.Split(hard.Check(o.s, o.opt.Policy))
	return len(n)
}

func (o *Optimizer) undo(m move.Move) {
	if _, err := move.Apply(o.s, m); err != nil {
		panic("stageb: undo failed: " + err.Error())
	}
}

// calibrate samples feasible moves and returns the temperature that accepts the average
// uphill move with probability 0.3.
func (o *Optimizer) calibrate() float64 {
	var sum float64
	n := 0
	for range 2000 {
		m, ok := o.propose()
		if !ok {
			continue
		}
		d, undo, ok := o.try(m)
		if !ok {
			continue
		}
		o.undo(undo)
		if !d.feasible {
			continue
		}
		if dc := d.soft.Total(o.w) + o.opt.ChangePenalty*float64(d.changes); dc > 0 {
			sum += dc
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return -(sum / float64(n)) / math.Log(0.3)
}

// propose draws a random move: relocate (50%), swap (35%) or room change (15%). With a change
// penalty, one move in ten instead puts a changed lesson back where the baseline has it:
// random relocations almost never hit that exact slot and room.
func (o *Optimizer) propose() (move.Move, bool) {
	l := o.moves[o.rng.IntN(len(o.moves))]
	a := o.s.Assignment(l)
	if o.opt.ChangePenalty > 0 && o.rng.IntN(10) == 0 {
		if b := o.base[l]; differs(a, b) && b.Placed {
			b.Pinned = a.Pinned
			return move.Move{Changes: []move.Change{{Lesson: l, To: b}}}, true
		}
	}
	switch k := o.rng.IntN(100); {
	case k < 50 || !a.Placed:
		dom := o.g.Domain[l]
		if len(dom) == 0 {
			return move.Move{}, false
		}
		slot := dom[o.rng.IntN(len(dom))]
		if a.Placed && slot == a.Slot {
			return move.Move{}, false
		}
		room, ok := o.pickRoom(l, slot, a.Room)
		if !ok {
			return move.Move{}, false
		}
		return move.Relocate(o.s, l, slot, room), true
	case k < 85:
		v, ok := o.partner(l)
		if !ok {
			return move.Move{}, false
		}
		return move.Swap(o.s, l, v), true
	default:
		if a.Room == engine.None {
			return move.Move{}, false
		}
		room, ok := o.pickRoom(l, a.Slot, engine.None)
		if !ok || room == a.Room {
			return move.Move{}, false
		}
		return move.ChangeRoom(o.s, l, room), true
	}
}

// partner picks a lesson to swap with l: often a conflict-graph neighbour (they compete for
// the same teacher or students), otherwise any movable lesson of the same parity kind.
func (o *Optimizer) partner(l int32) (int32, bool) {
	les := &o.p.Lessons[l]
	cand := o.moves
	if adj := o.g.Adj[l]; len(adj) > 0 && o.rng.IntN(2) == 0 {
		cand = adj
	}
	for range 4 {
		v := cand[o.rng.IntN(len(cand))]
		if v != l && o.p.Lessons[v].Biweekly == les.Biweekly && !o.s.Assignment(v).Pinned && o.s.Assignment(v).Placed {
			return v, true
		}
	}
	return 0, false
}

// pickRoom chooses a room for l at slot: keep (if given) when it is free there, otherwise a
// random free room of the right type and size. Lessons without a room keep none.
func (o *Optimizer) pickRoom(l int32, slot domain.Slot, keep int32) (int32, bool) {
	les := &o.p.Lessons[l]
	if o.s.Assignment(l).Placed && o.s.Assignment(l).Room == engine.None && len(les.Rooms) == 0 {
		return engine.None, true
	}
	cells := o.p.Grid.Cells(slot)
	free := func(r int32) bool {
		if o.p.Rooms[r].Capacity < les.Size {
			return false
		}
		cur := o.s.Assignment(l)
		for _, c := range cells {
			load := o.s.RoomLoad(r, c)
			if cur.Placed && cur.Room == r && slices.Contains(o.p.Grid.Cells(cur.Slot), c) {
				load-- // l itself leaves the cell
			}
			if load > 0 || o.p.RoomAvail(r, c) == engine.Unavailable {
				return false
			}
		}
		return true
	}
	if keep != engine.None && free(keep) {
		return keep, true
	}
	if len(les.Rooms) == 0 {
		return engine.None, false
	}
	for range 6 {
		if r := les.Rooms[o.rng.IntN(len(les.Rooms))]; free(r) {
			return r, true
		}
	}
	return engine.None, false
}
