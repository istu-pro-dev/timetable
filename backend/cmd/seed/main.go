// Command seed loads the demo dataset into the database at DATABASE_URL (see docs/dev/seed.md).
//
// It applies pending migrations first and then replaces all reference data, curriculum,
// lessons and schedules with the dataset. Running it again gives the same result.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/seed"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
)

// defaultURL matches the compose database (deploy/.env.example).
const defaultURL = "postgres://timetable:timetable@localhost:5432/timetable?sslmode=disable"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = defaultURL
	}
	flag.StringVar(&url, "database-url", url, "PostgreSQL URL (default: $DATABASE_URL or the compose database)")
	wait := flag.Duration("wait", 30*time.Second, "how long to retry connecting and migrating while the database starts")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := openMigrated(ctx, logger, url, *wait)
	if err != nil {
		return err
	}
	defer st.Close()

	counts, err := seed.Load(ctx, st, seed.Build())
	if err != nil {
		return err
	}
	logger.Info("demo dataset loaded",
		"buildings", counts.Buildings, "rooms", counts.Rooms, "groups", counts.Groups,
		"subgroups", counts.Subgroups, "teachers", counts.Teachers, "disciplines", counts.Disciplines,
		"curriculum_items", counts.CurriculumItems, "lessons", counts.Lessons)
	return nil
}

// openMigrated connects and migrates, retrying until wait elapses: right after `make up` the
// database may still be starting, or the API container may be running the same migrations.
func openMigrated(ctx context.Context, logger *slog.Logger, url string, wait time.Duration) (*store.Store, error) {
	deadline := time.Now().Add(wait)
	for {
		st, err := store.Open(ctx, url)
		if err == nil {
			if err = st.Migrate(ctx); err == nil {
				return st, nil
			}
			st.Close()
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("database not ready: %w", err)
		}
		logger.Warn("database not ready, retrying", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
