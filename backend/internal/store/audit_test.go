package store_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

var admin = store.Actor{Type: db.ActorTypeHuman, ID: "admin"}

func TestWithAuditCommitsChangeAndEntry(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()

	entry, err := s.WithAudit(ctx, store.Actor{Type: db.ActorTypeAiAgent, ID: "agent-1"},
		store.Change{Entity: "discipline", Reason: "created by test"},
		func(q *db.Queries, _ pgx.Tx, c *store.Change) error {
			d, err := q.CreateDiscipline(ctx, "Физика")
			if err != nil {
				return err
			}
			c.After = d
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if entry.ActorType != db.ActorTypeAiAgent || entry.Before != nil || entry.Diff != nil {
		t.Fatalf("entry = %+v", entry)
	}
	var after struct{ Name string }
	if err := json.Unmarshal(entry.After, &after); err != nil || after.Name != "Физика" {
		t.Fatalf("after = %s, %v", entry.After, err)
	}

	list, err := s.ListAudit(ctx, db.ListAuditParams{Limit: 10, Entity: pgtype.Text{String: "discipline", Valid: true}})
	if err != nil || len(list) != 1 || list[0].Reason != "created by test" {
		t.Fatalf("ListAudit = %+v, %v", list, err)
	}
}

func TestWithAuditRollsBackOnError(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()
	errBoom := errors.New("boom")

	_, err := s.WithAudit(ctx, admin, store.Change{Entity: "discipline"},
		func(q *db.Queries, _ pgx.Tx, _ *store.Change) error {
			if _, err := q.CreateDiscipline(ctx, "Химия"); err != nil {
				return err
			}
			return errBoom
		})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want boom", err)
	}
	assertEmpty(t, s)
}

// If the audit row itself cannot be written, the change must not survive either.
func TestWithAuditRollsBackWhenAuditFails(t *testing.T) {
	s := storetest.New(t)
	ctx := t.Context()

	_, err := s.WithAudit(ctx, admin, store.Change{Entity: "discipline", After: func() {}},
		func(q *db.Queries, _ pgx.Tx, _ *store.Change) error {
			_, err := q.CreateDiscipline(ctx, "Химия")
			return err
		})
	if err == nil {
		t.Fatal("expected marshal error")
	}
	assertEmpty(t, s)
}

func TestWithAuditRequiresActor(t *testing.T) {
	s := storetest.New(t)
	called := false
	fn := func(*db.Queries, pgx.Tx, *store.Change) error { called = true; return nil }

	for _, a := range []store.Actor{{}, {Type: db.ActorTypeHuman}, {Type: "robot", ID: "x"}} {
		if _, err := s.WithAudit(t.Context(), a, store.Change{Entity: "x"}, fn); !errors.Is(err, store.ErrNoActor) {
			t.Fatalf("actor %+v: err = %v, want ErrNoActor", a, err)
		}
	}
	if called {
		t.Fatal("fn must not run without an actor")
	}
}

func TestWithAuditStoresRawJSON(t *testing.T) {
	s := storetest.New(t)
	entry, err := s.WithAudit(t.Context(), admin,
		store.Change{Entity: "assignment", Before: json.RawMessage(`{"day":1}`), Diff: map[string]int{"day": 2}},
		func(*db.Queries, pgx.Tx, *store.Change) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if string(entry.Before) != `{"day": 1}` || string(entry.Diff) != `{"day": 2}` {
		t.Fatalf("before = %s, diff = %s", entry.Before, entry.Diff)
	}
}

func assertEmpty(t *testing.T, s *store.Store) {
	t.Helper()
	ds, err := s.ListDisciplines(t.Context())
	if err != nil || len(ds) != 0 {
		t.Fatalf("disciplines = %+v, %v; want none", ds, err)
	}
	list, err := s.ListAudit(t.Context(), db.ListAuditParams{Limit: 10})
	if err != nil || len(list) != 0 {
		t.Fatalf("audit = %+v, %v; want none", list, err)
	}
}
