package stageb

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/enginetest"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/hard"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/solver"
)

// TestLocalMatchesHardCheck: a lesson has zero local conflicts exactly when hard.Check
// reports no Hard violation involving it.
func TestLocalMatchesHardCheck(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		_, p := enginetest.DrawProblem(t, 30)
		s := enginetest.DrawSchedule(t, p)
		pol := hard.Policy{}
		for _, r := range hard.Rules {
			if rapid.IntRange(0, 4).Draw(t, "off_"+r.String()) == 0 {
				pol[r] = hard.Off
			}
		}
		c := newChecker(p, pol, solver.NewGraph(p).Siblings)
		vs := hard.Check(s, pol)
		for l := range p.Lessons {
			li := int32(l)
			involved := len(hard.Involving(vs, li)) > 0
			if local := c.local(s, li); (local > 0) != involved {
				t.Fatalf("lesson %d: local = %d, violations involving it: %v", l, local, hard.Involving(vs, li))
			}
		}
	})
}

// feasible builds a random instance with a feasible stage-A schedule, trying seeds from seed
// on (random instances are sometimes over-constrained).
func feasible(t *testing.T, seed uint64, lessons int) (*solver.Graph, *engine.Schedule) {
	t.Helper()
	for i := range uint64(50) {
		p, err := engine.NewProblem(enginetest.RandomInput(rand.New(rand.NewPCG(seed+i, 1)), lessons))
		if err != nil {
			t.Fatal(err)
		}
		g := solver.NewGraph(p)
		s := engine.NewSchedule(p)
		if res := solver.BuildFeasible(g, s, solver.StageAOptions{Seed: seed, RepairTime: time.Second}); res.Feasible {
			return g, s
		}
	}
	t.Fatalf("no feasible instance with %d lessons from seed %d", lessons, seed)
	return nil, nil
}

func TestRunImprovesWithoutHardViolations(t *testing.T) {
	g, s := feasible(t, 7, 300)
	o := New(g, s, Options{MaxSteps: 60_000, Seed: 1})
	st := o.Run(context.Background())
	t.Logf("cost %.0f → %.0f, steps %d, accepted %d, improved %d, infeasible %d, %v",
		st.Initial.Cost, st.Final.Cost, st.Steps, st.Accepted, st.Improved, st.Infeasible, st.Duration)
	if st.Final.Cost >= st.Initial.Cost {
		t.Errorf("no improvement: %.0f → %.0f", st.Initial.Cost, st.Final.Cost)
	}
	if st.HardBefore != 0 || st.HardAfter != 0 {
		t.Errorf("hard violations: before %d, after %d", st.HardBefore, st.HardAfter)
	}
	// The schedule left in s is the best one, and its cost matches a fresh evaluation.
	if got := soft.New(s.P).Full(s).Total(soft.DefaultWeights()); got != st.Final.Soft.Total(soft.DefaultWeights()) {
		t.Errorf("schedule penalty %.0f, reported %.0f", got, st.Final.Soft.Total(soft.DefaultWeights()))
	}
}

func TestRunIsReproducible(t *testing.T) {
	run := func() []engine.Assignment {
		g, s := feasible(t, 3, 120)
		New(g, s, Options{MaxSteps: 10_000, Seed: 42}).Run(context.Background())
		return s.Assignments()
	}
	a, b := run(), run()
	for l := range a {
		if a[l] != b[l] {
			t.Fatalf("lesson %d differs: %+v vs %+v", l, a[l], b[l])
		}
	}
}

func TestPinnedLessonsStay(t *testing.T) {
	g, s := feasible(t, 5, 150)
	before := s.Assignments()
	for l := 0; l < len(before); l += 2 {
		s.SetPinned(int32(l), true)
	}
	New(g, s, Options{MaxSteps: 20_000, Seed: 1}).Run(context.Background())
	for l := 0; l < len(before); l += 2 {
		if a := s.Assignment(int32(l)); a.Slot != before[l].Slot || a.Room != before[l].Room {
			t.Fatalf("pinned lesson %d moved: %+v → %+v", l, before[l], a)
		}
	}
}

func TestChangePenaltyLimitsChanges(t *testing.T) {
	changes := func(penalty float64) int {
		g, s := feasible(t, 11, 200)
		return New(g, s, Options{MaxSteps: 30_000, Seed: 1, ChangePenalty: penalty}).Run(context.Background()).Final.Changes
	}
	free, strict := changes(0), changes(50)
	t.Logf("changed lessons: penalty 0 → %d, penalty 50 → %d", free, strict)
	if strict >= free {
		t.Errorf("change penalty did not reduce changes: %d vs %d", strict, free)
	}
}

func TestWeightsSteerTheSearch(t *testing.T) {
	gaps := func(w soft.Weights) int {
		g, s := feasible(t, 13, 200)
		return New(g, s, Options{MaxSteps: 30_000, Seed: 1, Weights: &w}).Run(context.Background()).Final.Soft[soft.Gaps]
	}
	var onlyGaps, noGaps soft.Weights
	onlyGaps[soft.Gaps] = 10
	noGaps[soft.EdgeSlots] = 10
	if a, b := gaps(onlyGaps), gaps(noGaps); a >= b {
		t.Errorf("gaps with gap weight %d, without %d", a, b)
	}
}

func TestResumeContinues(t *testing.T) {
	g, s := feasible(t, 17, 150)
	o := New(g, s, Options{MaxSteps: 20_000, Seed: 1})
	first := o.Run(context.Background())
	state := o.State()
	if state.Step != 20_000 || state.Best == nil || len(state.RNG) == 0 {
		t.Fatalf("state = step %d, best %v, rng %d bytes", state.Step, state.Best != nil, len(state.RNG))
	}
	o2 := New(g, s, Options{MaxSteps: 40_000, Seed: 1, Resume: &state})
	second := o2.Run(context.Background())
	if second.Steps != 20_000 {
		t.Errorf("resumed run made %d steps, want 20000", second.Steps)
	}
	if second.Final.Cost > first.Final.Cost {
		t.Errorf("resumed best %.0f is worse than %.0f", second.Final.Cost, first.Final.Cost)
	}
}

func TestCancel(t *testing.T) {
	g, s := feasible(t, 19, 150)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	New(g, s, Options{Budget: time.Minute, Seed: 1}).Run(ctx)
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancelled run took %v", time.Since(start))
	}
}

// TestRepairsInfeasibleStart: a schedule with conflicts loses them before soft gains count.
func TestRepairsInfeasibleStart(t *testing.T) {
	g, s := feasible(t, 23, 150)
	p := s.P
	// Put three lessons of one teacher into the same slot.
	var same []int32
	for l := range p.Lessons {
		if p.Lessons[l].Teacher == p.Lessons[0].Teacher && p.Lessons[l].Biweekly == p.Lessons[0].Biweekly {
			same = append(same, int32(l))
		}
	}
	if len(same) < 3 {
		t.Skip("teacher has fewer than three lessons")
	}
	a := s.Assignment(same[0])
	for _, l := range same[1:3] {
		if err := s.Place(l, a.Slot, s.Assignment(l).Room); err != nil {
			t.Fatal(err)
		}
	}
	st := New(g, s, Options{MaxSteps: 30_000, Seed: 1}).Run(context.Background())
	if st.HardBefore == 0 || st.HardAfter >= st.HardBefore {
		t.Errorf("hard violations %d → %d", st.HardBefore, st.HardAfter)
	}
}
