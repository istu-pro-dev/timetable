package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// ---------------------------------------------------------------- time grid

type timeGrid struct {
	Days          int16 `json:"days"`
	PeriodsPerDay int16 `json:"periods_per_day"`
}

func (in *timeGrid) validate() error {
	var v validator
	v.between("days", int64(in.Days), 1, 7)
	v.between("periods_per_day", int64(in.PeriodsPerDay), 1, 16)
	return v.err()
}

func toTimeGrid(g db.TimeGrid) timeGrid {
	return timeGrid{Days: g.Days, PeriodsPerDay: g.PeriodsPerDay}
}

func (s *server) getTimeGrid(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		g, err := q.GetTimeGrid(ctx)
		return toTimeGrid(g), orNotFound(err, "time grid")
	})
}

func (s *server) putTimeGrid(w http.ResponseWriter, r *http.Request) {
	var in timeGrid
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "time_grid", op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			switch before, err := q.GetTimeGrid(ctx); {
			case err == nil:
				c.Before = toTimeGrid(before)
			case !errors.Is(err, pgx.ErrNoRows):
				return nil, err
			}
			g, err := q.UpsertTimeGrid(ctx, db.UpsertTimeGridParams{Days: in.Days, PeriodsPerDay: in.PeriodsPerDay})
			if err != nil {
				return nil, err
			}
			out := toTimeGrid(g)
			c.After = out
			return out, nil
		}})
}

// ---------------------------------------------------------------- periods

type period struct {
	Number   int16  `json:"number"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

type periodInput struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`

	starts, ends pgtype.Time
}

func (in *periodInput) validate() error {
	var v validator
	var ok1, ok2 bool
	in.starts, ok1 = parseClock(in.StartsAt)
	in.ends, ok2 = parseClock(in.EndsAt)
	if !ok1 {
		v.addf("starts_at", "must be a time of day HH:MM")
	}
	if !ok2 {
		v.addf("ends_at", "must be a time of day HH:MM")
	}
	if ok1 && ok2 && in.ends.Microseconds <= in.starts.Microseconds {
		v.addf("ends_at", "must be after starts_at")
	}
	return v.err()
}

func parseClock(s string) (pgtype.Time, bool) {
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			us := int64(t.Hour()*3600+t.Minute()*60+t.Second()) * 1_000_000
			return pgtype.Time{Microseconds: us, Valid: true}, true
		}
	}
	return pgtype.Time{}, false
}

func formatClock(t pgtype.Time) string {
	sec := t.Microseconds / 1_000_000
	h, m, s := sec/3600, sec/60%60, sec%60
	if s != 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

func toPeriod(p db.Period) period {
	return period{Number: p.Number, StartsAt: formatClock(p.StartsAt), EndsAt: formatClock(p.EndsAt)}
}

func periodNumber(r *http.Request) (int16, error) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n < 1 || n > 16 {
		return 0, badRequest(`path parameter "number" must be an integer between 1 and 16`)
	}
	return int16(n), nil
}

func (s *server) listPeriods(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListPeriods(ctx)
		return mapSlice(rows, toPeriod), err
	})
}

func (s *server) getPeriod(w http.ResponseWriter, r *http.Request) {
	n, err := periodNumber(r)
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		p, err := q.GetPeriod(ctx, n)
		return toPeriod(p), orNotFound(err, "period")
	})
}

func (s *server) putPeriod(w http.ResponseWriter, r *http.Request) {
	n, err := periodNumber(r)
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in periodInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "period", entityID: strconv.Itoa(int(n)), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			switch before, err := q.GetPeriod(ctx, n); {
			case err == nil:
				c.Before = toPeriod(before)
			case !errors.Is(err, pgx.ErrNoRows):
				return nil, err
			}
			p, err := q.UpsertPeriod(ctx, db.UpsertPeriodParams{Number: n, StartsAt: in.starts, EndsAt: in.ends})
			if err != nil {
				return nil, err
			}
			out := toPeriod(p)
			c.After = out
			return out, nil
		}})
}

func (s *server) deletePeriod(w http.ResponseWriter, r *http.Request) {
	n, err := periodNumber(r)
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "period", entityID: strconv.Itoa(int(n)), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetPeriod(ctx, n)
			if err != nil {
				return nil, orNotFound(err, "period")
			}
			c.Before = toPeriod(before)
			_, err = q.DeletePeriod(ctx, n)
			return nil, err
		}})
}
