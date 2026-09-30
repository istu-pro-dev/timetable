package solver

import (
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// Graph is the conflict graph of a problem (arch §1.1): lessons are vertices, and two lessons
// are adjacent when they cannot share a slot because they have the same teacher or
// overlapping audiences. Colours are slots; every lesson also carries a list of forbidden
// slots derived from availability.
type Graph struct {
	P *engine.Problem
	// Adj lists the neighbours of every lesson, sorted, without duplicates.
	Adj [][]int32
	// Domain lists the candidate slots of every lesson: all (day, period) positions with parity
	// "every" for weekly lessons, "odd" and "even" for biweekly ones, in grid order.
	// Forbidden slots are excluded.
	Domain [][]domain.Slot
	// Siblings lists lessons of the same curriculum item (H8), excluding the lesson itself.
	Siblings [][]int32
}

// NewGraph builds the conflict graph.
func NewGraph(p *engine.Problem) *Graph {
	n := len(p.Lessons)
	g := &Graph{P: p, Adj: make([][]int32, n), Domain: make([][]domain.Slot, n), Siblings: make([][]int32, n)}

	byTeacher := make([][]int32, len(p.Teachers))
	byGroup := make([][]int32, len(p.Groups))
	byItem := map[int64][]int32{}
	for li, l := range p.Lessons {
		idx := int32(li)
		byTeacher[l.Teacher] = append(byTeacher[l.Teacher], idx)
		byItem[l.Item] = append(byItem[l.Item], idx)
		var seen []int32
		for _, u := range l.Units {
			grp := p.Units[u].Group
			if !slices.Contains(seen, grp) {
				seen = append(seen, grp)
				byGroup[grp] = append(byGroup[grp], idx)
			}
		}
	}

	for _, ls := range byTeacher {
		for i, a := range ls {
			for _, b := range ls[i+1:] {
				g.link(a, b)
			}
		}
	}
	for grp, ls := range byGroup {
		for i, a := range ls {
			for _, b := range ls[i+1:] {
				if audiencesOverlapIn(p, a, b, int32(grp)) {
					g.link(a, b)
				}
			}
		}
	}
	for i := range g.Adj {
		slices.Sort(g.Adj[i])
		g.Adj[i] = slices.Compact(g.Adj[i])
	}
	for _, ls := range byItem {
		for _, a := range ls {
			for _, b := range ls {
				if a != b {
					g.Siblings[a] = append(g.Siblings[a], b)
				}
			}
		}
	}

	for li := range p.Lessons {
		g.Domain[li] = allowedSlots(p, int32(li))
	}
	return g
}

func (g *Graph) link(a, b int32) {
	g.Adj[a] = append(g.Adj[a], b)
	g.Adj[b] = append(g.Adj[b], a)
}

// Edges returns the number of edges.
func (g *Graph) Edges() int {
	n := 0
	for _, a := range g.Adj {
		n += len(a)
	}
	return n / 2
}

// audiencesOverlapIn reports whether lessons a and b share students of group grp
// (ADR-0001: parallel subgroups of one division do not overlap).
func audiencesOverlapIn(p *engine.Problem, a, b, grp int32) bool {
	for _, ua := range p.Lessons[a].Units {
		x := p.Units[ua]
		if x.Group != grp {
			continue
		}
		for _, ub := range p.Lessons[b].Units {
			y := p.Units[ub]
			if y.Group != grp {
				continue
			}
			if x.Division == engine.None || y.Division == engine.None || x.Division != y.Division || x.Part == y.Part {
				return true
			}
		}
	}
	return false
}

// allowedSlots returns the lesson's candidate slots: positions of the grid with the lesson's
// parity kind, minus those where the teacher is unavailable or no suitable room is available.
func allowedSlots(p *engine.Problem, l int32) []domain.Slot {
	les := &p.Lessons[l]
	parities := []domain.Parity{domain.EveryWeek}
	if les.Biweekly {
		parities = []domain.Parity{domain.OddWeek, domain.EvenWeek}
	}
	var out []domain.Slot
	for _, s := range p.Grid.Slots(parities...) {
		cells := p.Grid.Cells(s)
		if slices.ContainsFunc(cells, func(c int) bool { return p.TeacherAvail(les.Teacher, c) == engine.Unavailable }) {
			continue
		}
		roomOK := slices.ContainsFunc(les.Rooms, func(r int32) bool {
			return p.Rooms[r].Capacity >= les.Size &&
				!slices.ContainsFunc(cells, func(c int) bool { return p.RoomAvail(r, c) == engine.Unavailable })
		})
		if roomOK {
			out = append(out, s)
		}
	}
	return out
}
