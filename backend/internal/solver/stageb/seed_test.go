package stageb

import (
	"context"
	"testing"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/seed"
	"github.com/istu-pro-dev/timetable/backend/internal/solver"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

// TestSeedDataset is the acceptance check of issue #38: on the demo dataset stage B lowers
// the stage-A penalty by at least 30% within 60 seconds and never breaks a hard rule.
// Skipped without TEST_DATABASE_URL.
func TestSeedDataset(t *testing.T) {
	st := storetest.New(t)
	if _, err := seed.Load(t.Context(), st, seed.Build()); err != nil {
		t.Fatal(err)
	}
	p, err := st.LoadProblem(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	g := solver.NewGraph(p)
	s := engine.NewSchedule(p)
	if res := solver.BuildFeasible(g, s, solver.StageAOptions{Seed: 1}); !res.Feasible {
		t.Fatalf("stage A: %d violations", len(res.Violations))
	}
	budget := 5 * time.Second
	if raceEnabled {
		budget = 15 * time.Second
	}
	o := New(g, s, Options{Budget: budget, Seed: 1})
	res := o.Run(context.Background())
	gain := 1 - res.Final.Cost/res.Initial.Cost
	t.Logf("seed: %d lessons, cost %.0f → %.0f (−%.0f%%) in %v, %d steps, best at %v",
		len(p.Lessons), res.Initial.Cost, res.Final.Cost, 100*gain, res.Duration, res.Steps, res.Final.Elapsed)
	if gain < 0.30 {
		t.Errorf("penalty decreased by %.0f%%, want ≥ 30%%", 100*gain)
	}
	if res.HardAfter != 0 {
		t.Errorf("%d hard violations after stage B", res.HardAfter)
	}
}
