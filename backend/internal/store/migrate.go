package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func newMigrator(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	db := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, db, mustSub(migrations, "migrations"))
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, db, nil
}

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, db, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateReset rolls back every migration. Used by tests to check that down migrations work.
func MigrateReset(ctx context.Context, pool *pgxpool.Pool) error {
	p, db, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := p.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}
