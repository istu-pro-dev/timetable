package solver

import (
	"math"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// AssignRooms reassigns rooms of every placed lesson so that, at each time position, all
// lessons get a suitable, available room of sufficient capacity whenever possible, with the
// least total number of empty seats (arch §1.2: bipartite matching, min-cost assignment).
//
// Pinned lessons keep their rooms. Week parity is respected: every-week lessons are matched
// first to rooms free in both weeks, then odd-week and even-week lessons to what is left in
// their week. For a time position the new assignment is only applied if it leaves fewer
// lessons without a room, or as many with fewer empty seats, so the result is never worse.
//
// It returns the placed lessons that are still without a room.
func AssignRooms(s *engine.Schedule) []int32 {
	p := s.P
	byTime := make([][]int32, p.Grid.TimeCount())
	for l := range p.Lessons {
		if a := s.Assignment(int32(l)); a.Placed {
			t := p.Grid.TimeIndex(a.Slot)
			byTime[t] = append(byTime[t], int32(l))
		}
	}
	var roomless []int32
	for _, lessons := range byTime {
		if len(lessons) > 0 {
			roomless = append(roomless, assignTime(s, lessons)...)
		}
	}
	slices.Sort(roomless)
	return roomless
}

// assignTime matches the lessons of one time position to rooms.
func assignTime(s *engine.Schedule, lessons []int32) []int32 {
	p := s.P
	current := make([]int32, len(lessons))
	for i, l := range lessons {
		current[i] = s.Assignment(l).Room
	}

	// taken[r][w]: room r is used in week w (0 odd, 1 even) by a pinned lesson or an earlier
	// matching round.
	taken := make([][2]bool, len(p.Rooms))
	var free [3][]int32 // unpinned lessons by parity
	for _, l := range lessons {
		a := s.Assignment(l)
		if a.Pinned && a.Room != engine.None {
			for _, w := range weeksOfParity(a.Slot.Parity) {
				taken[a.Room][w] = true
			}
			continue
		}
		free[a.Slot.Parity] = append(free[a.Slot.Parity], l)
	}

	assigned := map[int32]int32{}
	for _, par := range []domain.Parity{domain.EveryWeek, domain.OddWeek, domain.EvenWeek} {
		group := free[par]
		if len(group) == 0 {
			continue
		}
		weeks := weeksOfParity(par)
		cost := make([][]int64, len(group))
		for i, l := range group {
			cost[i] = make([]int64, len(p.Rooms))
			for r := range p.Rooms {
				cost[i][r] = roomCost(s, l, int32(r), taken, weeks)
			}
		}
		for i, r := range hungarian(cost) {
			l := group[i]
			if r < 0 || cost[i][r] >= infeasible {
				assigned[l] = engine.None
				continue
			}
			assigned[l] = int32(r)
			for _, w := range weeks {
				taken[r][w] = true
			}
		}
	}

	final := func(i int, l int32) int32 {
		if r, ok := assigned[l]; ok {
			return r
		}
		return current[i] // pinned
	}
	oldMissing, newMissing := 0, 0
	oldWaste, newWaste := 0, 0
	for i, l := range lessons {
		size := p.Lessons[l].Size
		if current[i] == engine.None {
			oldMissing++
		} else {
			oldWaste += p.Rooms[current[i]].Capacity - size
		}
		if r := final(i, l); r == engine.None {
			newMissing++
		} else {
			newWaste += p.Rooms[r].Capacity - size
		}
	}
	// The phased matching (every week, then odd, then even) is not globally optimal, so keep
	// the current rooms unless the new ones are at least as good on both counts.
	if newMissing > oldMissing || (newMissing == oldMissing && newWaste >= oldWaste) {
		return missing(s, lessons)
	}

	// Clear changed rooms first so that lessons trading rooms never share one midway.
	for i, l := range lessons {
		if r := final(i, l); r != current[i] {
			mustPlace(s, l, s.Assignment(l).Slot, engine.None)
		}
	}
	for i, l := range lessons {
		if r := final(i, l); r != current[i] && r != engine.None {
			mustPlace(s, l, s.Assignment(l).Slot, r)
		}
	}
	return missing(s, lessons)
}

func missing(s *engine.Schedule, lessons []int32) []int32 {
	var out []int32
	for _, l := range lessons {
		if s.Assignment(l).Room == engine.None {
			out = append(out, l)
		}
	}
	return out
}

func mustPlace(s *engine.Schedule, l int32, slot domain.Slot, room int32) {
	if err := s.Place(l, slot, room); err != nil {
		panic(err)
	}
}

// infeasible marks a lesson-room pair that must not be matched.
const infeasible = int64(1) << 40

// roomCost is the number of empty seats if room r suits lesson l in the given weeks, or
// infeasible.
func roomCost(s *engine.Schedule, l, r int32, taken [][2]bool, weeks []int) int64 {
	p := s.P
	les := &p.Lessons[l]
	room := &p.Rooms[r]
	if room.Type != les.RoomType || room.Capacity < les.Size {
		return infeasible
	}
	a := s.Assignment(l)
	cells := p.Grid.Cells(a.Slot)
	for _, w := range weeks {
		if taken[r][w] {
			return infeasible
		}
	}
	for _, c := range cells {
		if p.RoomAvail(r, c) == engine.Unavailable {
			return infeasible
		}
	}
	return int64(room.Capacity - les.Size)
}

func weeksOfParity(p domain.Parity) []int {
	switch p {
	case domain.OddWeek:
		return []int{0}
	case domain.EvenWeek:
		return []int{1}
	default:
		return []int{0, 1}
	}
}

// hungarian solves the rectangular assignment problem for n rows and m columns: it returns,
// for every row, the column assigned to it (or -1 when there are more rows than columns),
// minimising the total cost. O(n² · m), Jonker–Volgenant style potentials.
func hungarian(cost [][]int64) []int {
	n := len(cost)
	if n == 0 {
		return nil
	}
	m := len(cost[0])
	// Pad columns so that every row can be matched: dummy columns cost "infeasible".
	cols := max(m, n)
	at := func(i, j int) int64 {
		if j < m {
			return cost[i][j]
		}
		return infeasible
	}

	const inf = math.MaxInt64 / 4
	u := make([]int64, n+1)
	v := make([]int64, cols+1)
	match := make([]int, cols+1) // match[j] = row (1-based) assigned to column j
	way := make([]int, cols+1)
	for i := 1; i <= n; i++ {
		match[0] = i
		j0 := 0
		minv := make([]int64, cols+1)
		used := make([]bool, cols+1)
		for j := range minv {
			minv[j] = inf
		}
		for {
			used[j0] = true
			i0, delta, j1 := match[j0], int64(inf), 0
			for j := 1; j <= cols; j++ {
				if used[j] {
					continue
				}
				cur := at(i0-1, j-1) - u[i0] - v[j]
				if cur < minv[j] {
					minv[j], way[j] = cur, j0
				}
				if minv[j] < delta {
					delta, j1 = minv[j], j
				}
			}
			for j := 0; j <= cols; j++ {
				if used[j] {
					u[match[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if match[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			match[j0] = match[j1]
			j0 = j1
		}
	}
	res := make([]int, n)
	for i := range res {
		res[i] = -1
	}
	for j := 1; j <= cols; j++ {
		if match[j] != 0 && j <= m {
			res[match[j]-1] = j - 1
		}
	}
	return res
}
