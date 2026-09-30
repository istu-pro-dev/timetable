package solver

import (
	"container/heap"
	"math/rand/v2"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// colouring tracks, for every unplaced lesson, which of its domain slots are blocked by
// already placed neighbours (H1, H2) or siblings (H8). It is the shared state of DSatur and
// the repair search.
type colouring struct {
	g *Graph
	s *engine.Schedule
	r *rand.Rand

	// key maps a lesson's slot to its domain index: TimeIndex for weekly lessons,
	// TimeIndex*2 + week for biweekly ones; -1 if the slot is not in the domain.
	key [][]int16
	// blocked counts, per lesson and domain index, the placed lessons that exclude the slot.
	blocked [][]uint16
	// sat is the number of blocked domain slots per lesson (DSatur saturation).
	sat []int
	// roomLessons lists placed lessons per room; maintained only by the repair search.
	roomLessons [][]int32
}

func newColouring(g *Graph, s *engine.Schedule, r *rand.Rand) *colouring {
	p := g.P
	c := &colouring{
		g: g, s: s, r: r,
		key:     make([][]int16, len(p.Lessons)),
		blocked: make([][]uint16, len(p.Lessons)),
		sat:     make([]int, len(p.Lessons)),
	}
	for l := range p.Lessons {
		size := p.Grid.TimeCount()
		if p.Lessons[l].Biweekly {
			size *= 2
		}
		k := make([]int16, size)
		for i := range k {
			k[i] = -1
		}
		for i, sl := range g.Domain[l] {
			k[c.slotKey(int32(l), sl)] = int16(i)
		}
		c.key[l] = k
		c.blocked[l] = make([]uint16, len(g.Domain[l]))
	}
	for l := range p.Lessons {
		if a := s.Assignment(int32(l)); a.Placed {
			c.propagate(int32(l), a.Slot, +1)
		}
	}
	return c
}

func (c *colouring) slotKey(l int32, s domain.Slot) int {
	t := c.g.P.Grid.TimeIndex(s)
	if !c.g.P.Lessons[l].Biweekly {
		return t
	}
	return t*2 + int(s.Parity-domain.OddWeek)
}

// block adjusts the counters of v's domain slots that overlap slot s at the same time position.
func (c *colouring) block(v int32, s domain.Slot, delta int) {
	t := c.g.P.Grid.TimeIndex(s)
	var keys []int
	if !c.g.P.Lessons[v].Biweekly {
		keys = []int{t}
	} else {
		switch s.Parity {
		case domain.OddWeek:
			keys = []int{t * 2}
		case domain.EvenWeek:
			keys = []int{t*2 + 1}
		default:
			keys = []int{t * 2, t*2 + 1}
		}
	}
	for _, k := range keys {
		c.bump(v, k, delta)
	}
}

// blockDay adjusts every slot of v on the day of s whose weeks overlap s (H8 siblings).
func (c *colouring) blockDay(v int32, s domain.Slot, delta int) {
	for p := uint8(1); p <= c.g.P.Grid.PeriodsPerDay; p++ {
		c.block(v, domain.Slot{Day: s.Day, Period: p, Parity: s.Parity}, delta)
	}
}

func (c *colouring) bump(v int32, k, delta int) {
	i := c.key[v][k]
	if i < 0 {
		return
	}
	old := int(c.blocked[v][i])
	now := old + delta
	c.blocked[v][i] = uint16(now)
	switch {
	case old == 0 && now > 0:
		c.sat[v]++
	case old > 0 && now == 0:
		c.sat[v]--
	}
}

// propagate updates the neighbours and siblings of u placed (+1) or removed (-1) at slot s.
func (c *colouring) propagate(u int32, s domain.Slot, delta int) {
	for _, v := range c.g.Adj[u] {
		c.block(v, s, delta)
	}
	for _, v := range c.g.Siblings[u] {
		c.blockDay(v, s, delta)
	}
}

// place puts u at slot s and room r and updates the counters.
func (c *colouring) place(u int32, s domain.Slot, r int32) {
	if err := c.s.Place(u, s, r); err != nil {
		panic(err) // slots come from the domain, rooms from suitable rooms
	}
	c.propagate(u, s, +1)
}

// unplace removes u and updates the counters.
func (c *colouring) unplace(u int32) {
	a := c.s.Assignment(u)
	if !a.Placed {
		return
	}
	c.propagate(u, a.Slot, -1)
	c.s.Unplace(u)
}

// free reports whether domain slot i of lesson v is not blocked by any placed lesson.
func (c *colouring) free(v int32, i int) bool { return c.blocked[v][i] == 0 }

// room returns the smallest free, available suitable room of sufficient capacity for lesson l
// at slot s, or None.
func (c *colouring) room(l int32, s domain.Slot) int32 {
	p := c.g.P
	les := &p.Lessons[l]
	cells := p.Grid.Cells(s)
	best := engine.None
	for _, r := range les.Rooms {
		room := &p.Rooms[r]
		if room.Capacity < les.Size || (best != engine.None && room.Capacity >= p.Rooms[best].Capacity) {
			continue
		}
		ok := true
		for _, cell := range cells {
			if c.s.RoomLoad(r, cell) > 0 || p.RoomAvail(r, cell) == engine.Unavailable {
				ok = false
				break
			}
		}
		if ok {
			best = r
		}
	}
	return best
}

// candidates returns the free domain slots of l that have a room, each with its room.
func (c *colouring) candidates(l int32) (slots []domain.Slot, rooms []int32) {
	for i, s := range c.g.Domain[l] {
		if !c.free(l, i) {
			continue
		}
		if r := c.room(l, s); r != engine.None {
			slots = append(slots, s)
			rooms = append(rooms, r)
		}
	}
	return slots, rooms
}

// impact counts the unplaced neighbours of l for which slot s is still free, i.e. how many
// options placing l at s takes away (least-constraining-value heuristic).
func (c *colouring) impact(l int32, s domain.Slot) int {
	n := 0
	for _, v := range c.g.Adj[l] {
		if c.s.Assignment(v).Placed {
			continue
		}
		for _, k := range c.overlappingKeys(v, s) {
			if i := c.key[v][k]; i >= 0 && c.free(v, int(i)) {
				n++
			}
		}
	}
	return n
}

func (c *colouring) overlappingKeys(v int32, s domain.Slot) []int {
	t := c.g.P.Grid.TimeIndex(s)
	if !c.g.P.Lessons[v].Biweekly {
		return []int{t}
	}
	switch s.Parity {
	case domain.OddWeek:
		return []int{t * 2}
	case domain.EvenWeek:
		return []int{t*2 + 1}
	}
	return []int{t * 2, t*2 + 1}
}

// choose places l at its least constraining candidate slot (random tie-break) and reports
// whether a candidate existed.
func (c *colouring) choose(l int32) bool {
	slots, rooms := c.candidates(l)
	if len(slots) == 0 {
		return false
	}
	best, bestImpact, ties := -1, 0, 0
	for i, s := range slots {
		imp := c.impact(l, s)
		switch {
		case best < 0 || imp < bestImpact:
			best, bestImpact, ties = i, imp, 1
		case imp == bestImpact:
			ties++
			if c.r.IntN(ties) == 0 { // reservoir sampling over equal candidates
				best = i
			}
		}
	}
	c.place(l, slots[best], rooms[best])
	return true
}

// DSatur places every unplaced lesson of s with the DSatur heuristic (Brélaz, 1979): the next
// lesson is the one with the fewest free slots left (highest saturation), ties broken by degree
// and then by a seeded random order; its slot is the least constraining free slot that still
// has a room. Lessons already placed in s (e.g. pinned) are kept.
//
// It returns the lessons that could not be placed. The result is deterministic for a seed.
func DSatur(g *Graph, s *engine.Schedule, seed uint64) []int32 {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	c := newColouring(g, s, r)
	return c.dsatur()
}

func (c *colouring) dsatur() []int32 {
	n := len(c.g.P.Lessons)
	tie := c.r.Perm(n)
	h := &satHeap{}
	for l := range n {
		if !c.s.Assignment(int32(l)).Placed {
			h.items = append(h.items, c.entry(int32(l), tie))
		}
	}
	heap.Init(h)

	var unplaced []int32
	done := make([]bool, n)
	for h.Len() > 0 {
		it := heap.Pop(h).(satItem)
		l := it.lesson
		if done[l] || it.free != len(c.g.Domain[l])-c.sat[l] {
			continue // stale entry
		}
		done[l] = true
		if !c.choose(l) {
			unplaced = append(unplaced, l)
			continue
		}
		for _, v := range c.g.Adj[l] {
			if !done[v] && !c.s.Assignment(v).Placed {
				heap.Push(h, c.entry(v, tie))
			}
		}
		for _, v := range c.g.Siblings[l] {
			if !done[v] && !c.s.Assignment(v).Placed {
				heap.Push(h, c.entry(v, tie))
			}
		}
	}
	slices.Sort(unplaced)
	return unplaced
}

func (c *colouring) entry(l int32, tie []int) satItem {
	return satItem{lesson: l, free: len(c.g.Domain[l]) - c.sat[l], degree: len(c.g.Adj[l]), tie: tie[l]}
}

type satItem struct {
	lesson int32
	free   int // unblocked domain slots: fewer = more saturated
	degree int
	tie    int
}

type satHeap struct{ items []satItem }

func (h *satHeap) Len() int { return len(h.items) }
func (h *satHeap) Less(i, j int) bool {
	a, b := h.items[i], h.items[j]
	if a.free != b.free {
		return a.free < b.free
	}
	if a.degree != b.degree {
		return a.degree > b.degree
	}
	return a.tie < b.tie
}
func (h *satHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *satHeap) Push(x any)    { h.items = append(h.items, x.(satItem)) }
func (h *satHeap) Pop() any {
	old := h.items
	it := old[len(old)-1]
	h.items = old[:len(old)-1]
	return it
}
