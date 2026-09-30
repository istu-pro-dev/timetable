package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Actor identifies who makes a change. Humans, the autonomous AI agent and the solver are all
// audited the same way (arch §10.4). ViaAssistant marks a human action prepared by an AI
// assistant on the human's behalf (arch §16.4).
type Actor struct {
	Type         db.ActorType
	ID           string
	ViaAssistant bool
}

// Change describes the audited change. Before, After and Diff are marshalled to JSON;
// nil values are stored as SQL NULL.
type Change struct {
	Entity   string
	EntityID string
	Before   any
	After    any
	Diff     any
	Reason   string
}

// ErrNoActor is returned when an audited change has no actor.
var ErrNoActor = errors.New("audit: actor type and id are required")

// WithAudit runs fn and records the audit row in the same transaction: either both the change
// and its audit entry are committed or neither is. fn may fill in the Change (for example the
// "after" state known only once the change is applied) through the pointer it receives.
func (s *Store) WithAudit(ctx context.Context, actor Actor, change Change,
	fn func(q *db.Queries, tx pgx.Tx, change *Change) error,
) (db.AuditLog, error) {
	if !actor.Type.Valid() || actor.ID == "" {
		return db.AuditLog{}, ErrNoActor
	}
	var entry db.AuditLog
	err := s.InTx(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if err := fn(q, tx, &change); err != nil {
			return err
		}
		params, err := auditParams(actor, change)
		if err != nil {
			return err
		}
		entry, err = q.InsertAudit(ctx, params)
		return err
	})
	return entry, err
}

func auditParams(actor Actor, c Change) (db.InsertAuditParams, error) {
	p := db.InsertAuditParams{
		ActorType:    actor.Type,
		ActorID:      actor.ID,
		ViaAssistant: actor.ViaAssistant,
		Entity:       c.Entity,
		EntityID:     c.EntityID,
		Reason:       c.Reason,
	}
	var err error
	if p.Before, err = jsonOrNil(c.Before); err != nil {
		return p, fmt.Errorf("audit before: %w", err)
	}
	if p.After, err = jsonOrNil(c.After); err != nil {
		return p, fmt.Errorf("audit after: %w", err)
	}
	if p.Diff, err = jsonOrNil(c.Diff); err != nil {
		return p, fmt.Errorf("audit diff: %w", err)
	}
	return p, nil
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
