package solver

import (
	"slices"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

// StageAOptions configures BuildFeasible.
type StageAOptions struct {
	Seed uint64
	// RepairTime bounds the repair search; zero means 10 seconds.
	RepairTime time.Duration
	// Policy is the strictness of hard rules used for the final report.
	Policy hard.Policy
}

// StageAResult is the outcome of stage A.
type StageAResult struct {
	Schedule *engine.Schedule
	// Feasible is true when every lesson is placed with a room and no hard rule is broken.
	Feasible bool
	// Forced lists lessons that could only be placed by breaking hard rules (fallback mode).
	Forced []int32
	// Unplaced lists lessons that could not be placed at all (no slot of their parity kind).
	Unplaced []int32
	// Violations of the final schedule. In fallback mode the forced lessons' conflicts appear
	// here with HeavySoft strictness (arch §5).
	Violations []hard.Violation
	// Stats for logging and the UI.
	DSaturUnplaced int
	Duration       time.Duration
}

// BuildFeasible runs stage A on s: DSatur, then the repair search for leftovers, then optimal
// room matching. Lessons already placed in s are kept (pinned lessons never move).
//
// If the repair search cannot place everything in time, the remaining lessons are forced into
// the least conflicting slots and the violated hard rules are reported as heavy soft
// constraints instead of failing (arch §5): stage B then works to remove them, and the
// administrator gets an explicit list.
func BuildFeasible(g *Graph, s *engine.Schedule, opt StageAOptions) StageAResult {
	start := time.Now()
	if opt.RepairTime == 0 {
		opt.RepairTime = 10 * time.Second
	}
	res := StageAResult{Schedule: s}

	leftovers := DSatur(g, s, opt.Seed)
	res.DSaturUnplaced = len(leftovers)
	if len(leftovers) > 0 {
		leftovers = Repair(g, s, leftovers, RepairOptions{Seed: opt.Seed, Deadline: time.Now().Add(opt.RepairTime)})
	}
	AssignRooms(s)

	for _, l := range leftovers {
		if forcePlace(g, s, l) {
			res.Forced = append(res.Forced, l)
		} else {
			res.Unplaced = append(res.Unplaced, l)
		}
	}

	pol := opt.Policy
	if len(res.Forced) > 0 {
		pol = heavy(pol, s, res.Forced)
	}
	res.Violations = hard.Check(s, pol)
	hardOnes, _ := hard.Split(res.Violations)
	res.Feasible = len(hardOnes) == 0 && len(res.Forced) == 0 && len(res.Unplaced) == 0
	res.Duration = time.Since(start)
	return res
}

// forcePlace puts l into the slot of its parity kind that breaks the fewest hard rules, with
// the best room available there. It reports false if the lesson has no slot at all.
func forcePlace(g *Graph, s *engine.Schedule, l int32) bool {
	p := g.P
	les := &p.Lessons[l]
	parities := []domain.Parity{domain.EveryWeek}
	if les.Biweekly {
		parities = []domain.Parity{domain.OddWeek, domain.EvenWeek}
	}
	best, bestCost, bestRoom := domain.Slot{}, -1, engine.None
	for _, sl := range p.Grid.Slots(parities...) {
		cost, room := forceCost(g, s, l, sl)
		if bestCost < 0 || cost < bestCost {
			best, bestCost, bestRoom = sl, cost, room
		}
	}
	if bestCost < 0 {
		return false
	}
	mustPlace(s, l, best, bestRoom)
	return true
}

// forceCost counts the hard-rule violations l would cause at slot sl and picks its room.
func forceCost(g *Graph, s *engine.Schedule, l int32, sl domain.Slot) (int, int32) {
	p := g.P
	les := &p.Lessons[l]
	cost := 0
	for _, v := range g.Adj[l] {
		if a := s.Assignment(v); a.Placed && a.Slot.Overlaps(sl) {
			cost += 10 // H1/H2
		}
	}
	for _, v := range g.Siblings[l] {
		if a := s.Assignment(v); a.Placed && a.Slot.Day == sl.Day && a.Slot.Parity.Overlaps(sl.Parity) {
			cost += 3 // H8
		}
	}
	cells := p.Grid.Cells(sl)
	if slices.ContainsFunc(cells, func(c int) bool { return p.TeacherAvail(les.Teacher, c) == engine.Unavailable }) {
		cost += 5 // H4
	}

	// Room: a suitable one that is free and big enough, else free of the right type (H6),
	// else any free room (H5), else none (H7).
	free := func(r int32) bool {
		return !slices.ContainsFunc(cells, func(c int) bool {
			return s.RoomLoad(r, c) > 0 || p.RoomAvail(r, c) == engine.Unavailable
		})
	}
	pick := func(ok func(r int32) bool, candidates []int32) int32 {
		best := engine.None
		for _, r := range candidates {
			if ok(r) && free(r) && (best == engine.None || fit(p, l, r) < fit(p, l, best)) {
				best = r
			}
		}
		return best
	}
	all := make([]int32, len(p.Rooms))
	for i := range all {
		all[i] = int32(i)
	}
	if r := pick(func(r int32) bool { return p.Rooms[r].Capacity >= les.Size }, les.Rooms); r != engine.None {
		return cost, r
	}
	if r := pick(func(int32) bool { return true }, les.Rooms); r != engine.None {
		return cost + 2, r
	}
	if r := pick(func(r int32) bool { return p.Rooms[r].Capacity >= les.Size }, all); r != engine.None {
		return cost + 2, r
	}
	return cost + 4, engine.None
}

// fit orders rooms for a lesson: rooms that fit come first, smallest first; rooms that are
// too small come after, largest first.
func fit(p *engine.Problem, l, r int32) int {
	d := p.Rooms[r].Capacity - p.Lessons[l].Size
	if d >= 0 {
		return d
	}
	return 1<<20 - d
}

// heavy returns a copy of pol where every rule broken by a forced lesson is HeavySoft, unless
// the policy switched it off.
func heavy(pol hard.Policy, s *engine.Schedule, forced []int32) hard.Policy {
	out := hard.Policy{}
	for r, st := range pol {
		out[r] = st
	}
	for _, v := range hard.Involving(hard.Check(s, pol), forced...) {
		if out.Of(v.Rule) == hard.Hard {
			out[v.Rule] = hard.HeavySoft
		}
	}
	return out
}
