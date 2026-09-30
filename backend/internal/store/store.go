package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Store gives access to the database: generated queries plus transactions.
type Store struct {
	*db.Queries
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL and verifies the connection.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return New(pool), nil
}

// New wraps an existing pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{Queries: db.New(pool), pool: pool}
}

// Pool returns the underlying connection pool.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases all connections.
func (s *Store) Close() { s.pool.Close() }

// Migrate applies pending migrations.
func (s *Store) Migrate(ctx context.Context) error { return Migrate(ctx, s.pool) }

// InTx runs fn in a transaction. The transaction commits if fn returns nil and
// rolls back otherwise.
func (s *Store) InTx(ctx context.Context, fn func(q *db.Queries, tx pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()
	if err = fn(s.WithTx(tx), tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
