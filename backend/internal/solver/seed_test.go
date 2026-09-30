package solver

import (
	"testing"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/seed"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

// seedProblem loads the demo dataset through the database (skipped without TEST_DATABASE_URL).
func seedProblem(t *testing.T) *engine.Problem {
	t.Helper()
	st := storetest.New(t)
	if _, err := seed.Load(t.Context(), st, seed.Build()); err != nil {
		t.Fatal(err)
	}
	p, err := st.LoadProblem(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDSaturSeedDataset(t *testing.T) {
	p := seedProblem(t)
	start := time.Now()
	s := engine.NewSchedule(p)
	unplaced := DSatur(NewGraph(p), s, 1)
	elapsed := time.Since(start)
	t.Logf("seed: %d lessons, %d unplaced after DSatur, %v", len(p.Lessons), len(unplaced), elapsed)
	if !raceEnabled && elapsed > time.Second {
		t.Fatalf("DSatur on the seed dataset took %v", elapsed)
	}
	placedValid(t, s)
}
