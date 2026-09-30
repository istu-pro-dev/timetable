// Package storetest provides a throwaway, migrated PostgreSQL database for integration tests.
//
// Tests are skipped unless TEST_DATABASE_URL points to a server where the user may
// create databases, e.g. postgres://postgres:postgres@localhost:5432/postgres.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
)

// New creates a fresh database with all migrations applied and drops it when the test ends.
func New(tb testing.TB) *store.Store {
	tb.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		tb.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		tb.Fatalf("connect admin: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	name := "tt_test_" + randomSuffix(tb)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		tb.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(admin)
	if err != nil {
		tb.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name

	s, err := store.Open(ctx, u.String())
	if err != nil {
		tb.Fatalf("open test database: %v", err)
	}
	tb.Cleanup(func() {
		s.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			tb.Errorf("cleanup connect: %v", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			tb.Errorf("drop database: %v", err)
		}
	})

	if err := s.Migrate(ctx); err != nil {
		tb.Fatalf("migrate: %v", err)
	}
	return s
}

func randomSuffix(tb testing.TB) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		tb.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
