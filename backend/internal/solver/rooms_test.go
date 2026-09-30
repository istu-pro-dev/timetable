package solver

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
)

// bruteForce returns the minimum total cost of assigning every row to a distinct column, or
// -1 if impossible (rows > columns).
func bruteForce(cost [][]int64) int64 {
	n := len(cost)
	if n == 0 {
		return 0
	}
	m := len(cost[0])
	best := int64(-1)
	used := make([]bool, m)
	var rec func(i int, sum int64)
	rec = func(i int, sum int64) {
		if i == n {
			if best < 0 || sum < best {
				best = sum
			}
			return
		}
		for j := range m {
			if !used[j] {
				used[j] = true
				rec(i+1, sum+cost[i][j])
				used[j] = false
			}
		}
	}
	rec(0, 0)
	return best
}

func TestHungarianMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		n := 1 + r.IntN(5)
		m := n + r.IntN(3)
		cost := make([][]int64, n)
		for i := range cost {
			cost[i] = make([]int64, m)
			for j := range cost[i] {
				cost[i][j] = int64(r.IntN(20))
				if r.IntN(5) == 0 {
					cost[i][j] = infeasible
				}
			}
		}
		res := hungarian(cost)
		var sum int64
		seen := map[int]bool{}
		for i, j := range res {
			if j < 0 || seen[j] {
				t.Fatalf("invalid assignment %v", res)
			}
			seen[j] = true
			sum += cost[i][j]
		}
		if want := bruteForce(cost); sum != want {
			t.Fatalf("cost %d, optimum %d for %v", sum, want, cost)
		}
	}
}

func TestHungarianMoreRowsThanColumns(t *testing.T) {
	res := hungarian([][]int64{{5}, {1}, {3}})
	assigned := 0
	for i, j := range res {
		if j == 0 {
			assigned++
			if i != 1 {
				t.Fatalf("column 0 should go to the cheapest row 1, got %v", res)
			}
		} else if j != -1 {
			t.Fatalf("res = %v", res)
		}
	}
	if assigned != 1 || hungarian(nil) != nil {
		t.Fatalf("res = %v", res)
	}
}

// roomsInput: seminar lessons A (16 students) and B (18) fit rooms X (20) and Z (30); two
// biweekly labs share lab room Y.
func roomsInput() engine.Input {
	return engine.Input{
		Grid:      domain.Grid{Days: 1, PeriodsPerDay: 2},
		Buildings: []engine.Building{{ID: 1, Name: "A"}},
		Rooms: []engine.RoomInput{
			{ID: 1, Name: "X", Building: 1, Type: "sem", Capacity: 20},
			{ID: 2, Name: "Z", Building: 1, Type: "sem", Capacity: 30},
			{ID: 3, Name: "Y", Building: 1, Type: "lab", Capacity: 18},
		},
		Teachers:    []engine.TeacherInput{{ID: 1, Name: "T1"}, {ID: 2, Name: "T2"}, {ID: 3, Name: "T3"}, {ID: 4, Name: "T4"}},
		Groups:      []engine.Group{{ID: 1, Name: "G1", Size: 16}, {ID: 2, Name: "G2", Size: 18}, {ID: 3, Name: "G3", Size: 10}, {ID: 4, Name: "G4", Size: 10}},
		Disciplines: []engine.Discipline{{ID: 1, Name: "D"}},
		Lessons: []engine.LessonInput{
			{ID: 1, Item: 1, Discipline: 1, Teacher: 1, RoomType: "sem", Audience: domain.Audience{domain.WholeGroup(1)}},
			{ID: 2, Item: 2, Discipline: 1, Teacher: 2, RoomType: "sem", Audience: domain.Audience{domain.WholeGroup(2)}},
			{ID: 3, Item: 3, Discipline: 1, Teacher: 3, RoomType: "lab", Audience: domain.Audience{domain.WholeGroup(3)}, Biweekly: true},
			{ID: 4, Item: 4, Discipline: 1, Teacher: 4, RoomType: "lab", Audience: domain.Audience{domain.WholeGroup(4)}, Biweekly: true},
		},
	}
}

func TestAssignRoomsFindsFeasibleMatching(t *testing.T) {
	p := mustProblem(t, roomsInput())
	s := engine.NewSchedule(p)
	at := domain.Slot{Day: domain.Monday, Period: 1}
	mustPlace(s, 0, at, 0)           // A → X
	mustPlace(s, 1, at, engine.None) // B left without a room
	if got := AssignRooms(s); len(got) != 0 {
		t.Fatalf("roomless = %v", got)
	}
	if _, waste := roomStats(s); waste != 16 {
		t.Fatalf("waste = %d, want 16", waste)
	}
	if vs := hard.Check(s, hard.Policy{hard.H7Completeness: hard.Off}); len(vs) != 0 { // labs unplaced
		t.Fatalf("violations: %+v", vs)
	}
}

func TestAssignRoomsParityAndPinned(t *testing.T) {
	p := mustProblem(t, roomsInput())
	s := engine.NewSchedule(p)
	// Two biweekly labs in odd and even weeks share the only lab room.
	mustPlace(s, 2, domain.Slot{Day: domain.Monday, Period: 2, Parity: domain.OddWeek}, engine.None)
	mustPlace(s, 3, domain.Slot{Day: domain.Monday, Period: 2, Parity: domain.EvenWeek}, engine.None)
	// A is pinned into Z at Monday 2; B must then take X.
	mustPlace(s, 0, domain.Slot{Day: domain.Monday, Period: 2}, 1)
	s.SetPinned(0, true)
	mustPlace(s, 1, domain.Slot{Day: domain.Monday, Period: 2}, engine.None)
	if got := AssignRooms(s); len(got) != 0 {
		t.Fatalf("roomless = %v", got)
	}
	if s.Assignment(2).Room != 2 || s.Assignment(3).Room != 2 {
		t.Fatal("odd and even labs should share the lab room")
	}
	if s.Assignment(0).Room != 1 || s.Assignment(1).Room != 0 {
		t.Fatal("pinned room changed or B not in X")
	}
	if vs := hard.Check(s, nil); len(vs) != 0 {
		t.Fatalf("violations: %+v", vs)
	}
}

func TestAssignRoomsKeepsBetterCurrent(t *testing.T) {
	// Only one seminar room for two lessons: one stays roomless either way; the existing
	// assignment is kept when the new one is not better.
	in := roomsInput()
	in.Rooms = in.Rooms[:1]
	p := mustProblem(t, in)
	s := engine.NewSchedule(p)
	at := domain.Slot{Day: domain.Monday, Period: 1}
	mustPlace(s, 0, at, 0)
	mustPlace(s, 1, at, engine.None)
	if got := AssignRooms(s); !slices.Equal(got, []int32{0}) && !slices.Equal(got, []int32{1}) {
		t.Fatalf("roomless = %v", got)
	}
	if vs := hard.Check(s, hard.Policy{hard.H7Completeness: hard.Off}); len(vs) != 0 {
		t.Fatalf("violations: %+v", vs)
	}
}

// On random DSatur schedules, matching never loses rooms and never increases waste; starting
// from a deliberately bad worst-fit room choice it strictly reduces waste.
func TestAssignRoomsNotWorseThanGreedy(t *testing.T) {
	for seed := range uint64(6) {
		r := rand.New(rand.NewPCG(seed, 5))
		p := mustProblem(t, enginetest.RandomInput(r, 400))
		s := engine.NewSchedule(p)
		DSatur(NewGraph(p), s, seed)

		roomlessBefore, wasteBefore := roomStats(s)
		roomlessAfter := len(AssignRooms(s))
		_, wasteAfter := roomStats(s)
		if roomlessAfter > roomlessBefore || wasteAfter > wasteBefore {
			t.Fatalf("seed %d best-fit: roomless %d→%d, waste %d→%d", seed, roomlessBefore, roomlessAfter, wasteBefore, wasteAfter)
		}

		worstFit(s)
		roomlessBefore, wasteBefore = roomStats(s)
		roomlessAfter = len(AssignRooms(s))
		_, wasteAfter = roomStats(s)
		if roomlessAfter > roomlessBefore || wasteAfter >= wasteBefore {
			t.Fatalf("seed %d worst-fit: roomless %d→%d, waste %d→%d", seed, roomlessBefore, roomlessAfter, wasteBefore, wasteAfter)
		}
		for _, v := range hard.Check(s, nil) {
			if v.Rule != hard.H7Completeness {
				t.Fatalf("seed %d: %v %s", seed, v.Rule, v.Message)
			}
		}
	}
}

// worstFit reassigns rooms lesson by lesson, taking the largest free suitable room.
func worstFit(s *engine.Schedule) {
	p := s.P
	for l := range p.Lessons {
		if a := s.Assignment(int32(l)); a.Placed {
			mustPlace(s, int32(l), a.Slot, engine.None)
		}
	}
	for l := range p.Lessons {
		a := s.Assignment(int32(l))
		if !a.Placed {
			continue
		}
		best := engine.None
		for _, r := range p.Lessons[l].Rooms {
			if p.Rooms[r].Capacity < p.Lessons[l].Size || (best != engine.None && p.Rooms[r].Capacity <= p.Rooms[best].Capacity) {
				continue
			}
			free := true
			for _, c := range p.Grid.Cells(a.Slot) {
				if s.RoomLoad(r, c) > 0 || p.RoomAvail(r, c) == engine.Unavailable {
					free = false
				}
			}
			if free {
				best = r
			}
		}
		mustPlace(s, int32(l), a.Slot, best)
	}
}

func roomStats(s *engine.Schedule) (roomless, waste int) {
	for l, a := range s.Assignments() {
		if !a.Placed {
			continue
		}
		if a.Room == engine.None {
			roomless++
			continue
		}
		waste += s.P.Rooms[a.Room].Capacity - s.P.Lessons[l].Size
	}
	return roomless, waste
}

func ExampleAssignRooms() {
	p, _ := engine.NewProblem(roomsInput())
	s := engine.NewSchedule(p)
	at := domain.Slot{Day: domain.Monday, Period: 1}
	_ = s.Place(0, at, 0)
	_ = s.Place(1, at, engine.None)
	fmt.Println("roomless:", len(AssignRooms(s)))
	// Output: roomless: 0
}
