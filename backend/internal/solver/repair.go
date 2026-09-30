package solver

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// RepairOptions bounds the repair search.
type RepairOptions struct {
	Seed uint64
	// Deadline stops the search; zero means no time limit.
	Deadline time.Time
	// MaxSteps bounds the number of placements; zero means 200 × the number of lessons.
	MaxSteps int
	// TabuTenure is how many steps an evicted lesson may not return to the slot it left;
	// zero means 10.
	TabuTenure int
}

// Repair places the given unplaced lessons into s, displacing other lessons when needed
// (arch §3, stage A step 2). It is a conflict-directed search with forward checking:
//
//   - the most constrained waiting lesson goes first (MRV: fewest slots that are free or can
//     be freed);
//   - if it has a free slot with a room, it takes the least constraining one;
//   - otherwise it takes the slot whose blockers are cheapest to evict (lessons evicted often
//     cost more, pinned lessons are never evicted), evicts them and queues them again;
//   - a tabu list keeps evicted lessons from bouncing straight back, which would cycle.
//
// It stops when every lesson is placed, at the deadline, or after MaxSteps placements, and
// returns the lessons still unplaced. Lessons placed in s before the call may move but are
// never left unplaced unless they were evicted and the search ran out of time.
func Repair(g *Graph, s *engine.Schedule, unplaced []int32, opt RepairOptions) []int32 {
	r := rand.New(rand.NewPCG(opt.Seed, opt.Seed^0x2545f4914f6cdd1d))
	c := newColouring(g, s, r)
	return c.repair(unplaced, opt)
}

// Insert places a single lesson, e.g. a rescheduled one (arch §17), and reports whether it
// succeeded. Other lessons may be moved to make room.
func Insert(g *Graph, s *engine.Schedule, l int32, opt RepairOptions) bool {
	return len(Repair(g, s, []int32{l}, opt)) == 0
}

type tabuKey struct {
	lesson int32
	slot   domain.Slot
}

func (c *colouring) repair(queue []int32, opt RepairOptions) []int32 {
	n := len(c.g.P.Lessons)
	if opt.MaxSteps == 0 {
		opt.MaxSteps = 200 * max(n, 1)
	}
	if opt.TabuTenure == 0 {
		opt.TabuTenure = 10
	}
	c.indexRooms()
	queue = slices.Clone(queue)
	evictions := make([]int, n)
	tabu := map[tabuKey]int{}

	for step := 0; len(queue) > 0 && step < opt.MaxSteps; step++ {
		if !opt.Deadline.IsZero() && step%32 == 0 && time.Now().After(opt.Deadline) {
			break
		}
		i := c.mostConstrained(queue)
		l := queue[i]
		queue = slices.Delete(queue, i, i+1)
		if c.s.Assignment(l).Placed {
			continue
		}
		if c.choose(l) {
			c.roomAdd(l)
			continue
		}
		slot, room, victims, ok := c.cheapestEviction(l, evictions, tabu, step)
		if !ok {
			queue = append(queue, l) // nothing evictable now; retry later
			continue
		}
		for _, v := range victims {
			from := c.s.Assignment(v).Slot
			c.roomRemove(v)
			c.unplace(v)
			evictions[v]++
			tabu[tabuKey{v, from}] = step + opt.TabuTenure
			queue = append(queue, v)
		}
		c.place(l, slot, room)
		c.roomAdd(l)
	}
	slices.Sort(queue)
	return slices.Compact(queue)
}

// mostConstrained returns the index in queue of the lesson with the fewest usable slots.
func (c *colouring) mostConstrained(queue []int32) int {
	best, bestFree := 0, -1
	for i, l := range queue {
		free := len(c.g.Domain[l]) - c.sat[l]
		if bestFree < 0 || free < bestFree {
			best, bestFree = i, free
		}
	}
	return best
}

// cheapestEviction finds the slot of l whose blocking lessons are cheapest to remove.
func (c *colouring) cheapestEviction(l int32, evictions []int, tabu map[tabuKey]int, step int) (domain.Slot, int32, []int32, bool) {
	var (
		bestSlot    domain.Slot
		bestRoom    = engine.None
		bestVictims []int32
		bestCost    = -1
		ties        int
	)
	for _, s := range c.g.Domain[l] {
		if until, ok := tabu[tabuKey{l, s}]; ok && until > step {
			continue
		}
		blockers, ok := c.blockers(l, s)
		if !ok {
			continue
		}
		room, roomVictims, ok := c.cheapestRoom(l, s, blockers)
		if !ok {
			continue
		}
		victims := slices.Concat(blockers, roomVictims)
		cost := 0
		for _, v := range victims {
			cost += 1 + evictions[v]
		}
		switch {
		case bestCost < 0 || cost < bestCost:
			bestSlot, bestRoom, bestVictims, bestCost, ties = s, room, victims, cost, 1
		case cost == bestCost:
			ties++
			if c.r.IntN(ties) == 0 {
				bestSlot, bestRoom, bestVictims = s, room, victims
			}
		}
	}
	return bestSlot, bestRoom, bestVictims, bestCost >= 0
}

// blockers returns the placed lessons that prevent l from taking slot s (shared teacher or
// students, or a curriculum sibling on the same day), or false if one of them is pinned.
func (c *colouring) blockers(l int32, s domain.Slot) ([]int32, bool) {
	var out []int32
	for _, v := range c.g.Adj[l] {
		a := c.s.Assignment(v)
		if a.Placed && a.Slot.Overlaps(s) {
			if a.Pinned {
				return nil, false
			}
			out = append(out, v)
		}
	}
	for _, v := range c.g.Siblings[l] {
		a := c.s.Assignment(v)
		if a.Placed && a.Slot.Day == s.Day && a.Slot.Parity.Overlaps(s.Parity) && !slices.Contains(out, v) {
			if a.Pinned {
				return nil, false
			}
			out = append(out, v)
		}
	}
	return out, true
}

// cheapestRoom picks a suitable room for l at s, preferring one that is free once the given
// blockers are gone, and otherwise the one with the fewest (unpinned) occupants to evict.
func (c *colouring) cheapestRoom(l int32, s domain.Slot, leaving []int32) (int32, []int32, bool) {
	p := c.g.P
	les := &p.Lessons[l]
	cells := p.Grid.Cells(s)
	best, bestN := engine.None, -1
	var bestOcc []int32
	for _, r := range les.Rooms {
		room := &p.Rooms[r]
		if room.Capacity < les.Size ||
			slices.ContainsFunc(cells, func(cell int) bool { return p.RoomAvail(r, cell) == engine.Unavailable }) {
			continue
		}
		var occ []int32
		pinned := false
		for _, v := range c.roomLessons[r] {
			a := c.s.Assignment(v)
			if !a.Slot.Overlaps(s) || slices.Contains(leaving, v) {
				continue
			}
			if a.Pinned {
				pinned = true
				break
			}
			occ = append(occ, v)
		}
		if pinned {
			continue
		}
		if bestN < 0 || len(occ) < bestN || (len(occ) == bestN && room.Capacity < p.Rooms[best].Capacity) {
			best, bestN, bestOcc = r, len(occ), occ
		}
	}
	return best, bestOcc, bestN >= 0
}

// indexRooms builds the room → placed lessons index used by the eviction search.
func (c *colouring) indexRooms() {
	c.roomLessons = make([][]int32, len(c.g.P.Rooms))
	for l := range c.g.P.Lessons {
		c.roomAdd(int32(l))
	}
}

func (c *colouring) roomAdd(l int32) {
	if a := c.s.Assignment(l); a.Placed && a.Room != engine.None {
		c.roomLessons[a.Room] = append(c.roomLessons[a.Room], l)
	}
}

func (c *colouring) roomRemove(l int32) {
	if a := c.s.Assignment(l); a.Placed && a.Room != engine.None {
		list := c.roomLessons[a.Room]
		if i := slices.Index(list, l); i >= 0 {
			c.roomLessons[a.Room] = slices.Delete(list, i, i+1)
		}
	}
}
