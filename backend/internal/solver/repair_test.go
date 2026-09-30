package solver

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
	"github.com/istu-pro-dev/timetable/backend/internal/engine"
)

// plantedInput builds a tight instance that is feasible by construction: it first fills a
// hidden timetable (every group busy in about `density` of the positions, teachers and rooms
// never double-booked) and then forgets the slots. Every lesson is a one-off item, so H8 does
// not interfere.
func plantedInput(r *rand.Rand, groups int, density float64) engine.Input {
	return plantedInputRatio(r, groups, density, 1.5, 1)
}

// plantedInputRatio is plantedInput with the number of teachers and rooms given relative to
// the number of groups; ratios below one make teachers and rooms the bottleneck.
func plantedInputRatio(r *rand.Rand, groups int, density, teacherRatio, roomRatio float64) engine.Input {
	grid := domain.Grid{Days: 5, PeriodsPerDay: 4}
	positions := grid.TimeCount()
	teachers := max(1, int(float64(groups)*teacherRatio))
	rooms := max(1, int(float64(groups)*roomRatio))
	in := engine.Input{Grid: grid, Buildings: []engine.Building{{ID: 1, Name: "A"}}, Disciplines: []engine.Discipline{{ID: 1, Name: "D"}}}
	for i := range rooms {
		in.Rooms = append(in.Rooms, engine.RoomInput{ID: domain.RoomID(i + 1), Name: fmt.Sprint("R", i), Building: 1, Type: "any", Capacity: 30})
	}
	for i := range teachers {
		in.Teachers = append(in.Teachers, engine.TeacherInput{ID: domain.TeacherID(i + 1), Name: fmt.Sprint("T", i)})
	}
	for i := range groups {
		in.Groups = append(in.Groups, engine.Group{ID: domain.GroupID(i + 1), Name: fmt.Sprint("G", i), Size: 20})
	}
	teacherBusy := make([][]bool, teachers)
	for i := range teacherBusy {
		teacherBusy[i] = make([]bool, positions)
	}
	roomsUsed := make([]int, positions)
	id := 0
	for g := range groups {
		for pos := range positions {
			if r.Float64() > density || roomsUsed[pos] >= rooms {
				continue
			}
			free := []int{}
			for t := range teachers {
				if !teacherBusy[t][pos] {
					free = append(free, t)
				}
			}
			if len(free) == 0 {
				continue
			}
			t := free[r.IntN(len(free))]
			teacherBusy[t][pos] = true
			roomsUsed[pos]++
			id++
			in.Lessons = append(in.Lessons, engine.LessonInput{
				ID: domain.LessonID(id), Item: int64(id), Discipline: 1, Kind: "x", RoomType: "any",
				Teacher: domain.TeacherID(t + 1), Audience: domain.Audience{domain.WholeGroup(domain.GroupID(g + 1))},
			})
		}
	}
	return in
}

func TestRepairSolvesTightInstances(t *testing.T) {
	// 20 groups busy in every position, 0.8 teachers and 0.8 rooms per group: DSatur alone
	// fails on most of these instances, but a solution is known to exist.
	dsaturFailed := 0
	for seed := range uint64(10) {
		r := rand.New(rand.NewPCG(seed, 17))
		p := mustProblem(t, plantedInputRatio(r, 20, 1.0, 0.8, 0.8))
		g := NewGraph(p)
		s := engine.NewSchedule(p)
		unplaced := DSatur(g, s, seed)
		if len(unplaced) == 0 {
			continue
		}
		dsaturFailed++
		left := Repair(g, s, unplaced, RepairOptions{Seed: seed}) // step-bounded, deterministic
		if len(left) != 0 {
			t.Errorf("seed %d: %d lessons left after repair (DSatur left %d of %d)", seed, len(left), len(unplaced), len(p.Lessons))
		}
		if placedCount(s) != len(p.Lessons) {
			t.Errorf("seed %d: %d of %d placed", seed, placedCount(s), len(p.Lessons))
		}
		placedValid(t, s)
	}
	if dsaturFailed < 3 {
		t.Fatalf("only %d instances needed repair; the test no longer exercises it", dsaturFailed)
	}
}

func TestRepairNeverMovesPinned(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 17))
	p := mustProblem(t, plantedInput(r, 10, 0.9))
	g := NewGraph(p)
	s := engine.NewSchedule(p)
	unplaced := DSatur(g, s, 3)
	// Pin a third of the placed lessons, then unplace some others and repair.
	pinned := map[int32]engine.Assignment{}
	for l := range p.Lessons {
		a := s.Assignment(int32(l))
		if a.Placed && l%3 == 0 {
			s.SetPinned(int32(l), true)
			pinned[int32(l)] = s.Assignment(int32(l))
		}
	}
	for l := range p.Lessons {
		if l%3 == 1 && s.Assignment(int32(l)).Placed {
			s.Unplace(int32(l))
			unplaced = append(unplaced, int32(l))
		}
	}
	Repair(g, s, unplaced, RepairOptions{Seed: 1, MaxSteps: 20000})
	for l, a := range pinned {
		if s.Assignment(l) != a {
			t.Fatalf("pinned lesson %d moved from %+v to %+v", l, a, s.Assignment(l))
		}
	}
	placedValid(t, s)
}

func TestRepairReturnsUnplacedOnTimeout(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 17))
	p := mustProblem(t, plantedInput(r, 10, 0.9))
	g := NewGraph(p)
	s := engine.NewSchedule(p)
	all := make([]int32, len(p.Lessons))
	for i := range all {
		all[i] = int32(len(all) - 1 - i)
	}
	left := Repair(g, s, all, RepairOptions{Deadline: time.Now().Add(-time.Second)})
	if len(left) != len(all) || !slices.IsSorted(left) {
		t.Fatalf("expired deadline: %d left of %d", len(left), len(all))
	}
	left = Repair(g, s, all, RepairOptions{MaxSteps: 5})
	if len(left) == 0 || len(left) > len(all) {
		t.Fatalf("step limit: %d left", len(left))
	}
	placedValid(t, s)
}

func TestInsertIntoFullSchedule(t *testing.T) {
	r := rand.New(rand.NewPCG(6, 17))
	in := plantedInput(r, 10, 0.85)
	p := mustProblem(t, in)
	g := NewGraph(p)
	s := engine.NewSchedule(p)
	unplaced := DSatur(g, s, 6)
	if len(Repair(g, s, unplaced, RepairOptions{Seed: 6})) != 0 {
		t.Skip("instance not solved; nothing to test")
	}
	// Take one lesson out and insert it again (the rescheduling path of arch §17).
	victim := int32(len(p.Lessons) / 2)
	s.Unplace(victim)
	if !Insert(g, s, victim, RepairOptions{Seed: 1, Deadline: time.Now().Add(time.Second)}) {
		t.Fatal("insert failed")
	}
	if vs := placedCount(s); vs != len(p.Lessons) {
		t.Fatalf("%d of %d placed", vs, len(p.Lessons))
	}
	placedValid(t, s)
}

func TestRepairImpossibleLessonStaysUnplaced(t *testing.T) {
	in := smallInput()
	in.Groups[1].Size = 100 // lecA and semB have empty domains
	p := mustProblem(t, in)
	g := NewGraph(p)
	s := engine.NewSchedule(p)
	left := Repair(g, s, DSatur(g, s, 1), RepairOptions{MaxSteps: 1000})
	if !slices.Equal(left, []int32{lecA, semB}) {
		t.Fatalf("left = %v", left)
	}
	placedValid(t, s)
}

func placedCount(s *engine.Schedule) int {
	n := 0
	for _, a := range s.Assignments() {
		if a.Placed {
			n++
		}
	}
	return n
}
