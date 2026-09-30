package auth

import (
	"context"
	"strconv"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Roles (arch §16.1). The set is fixed on purpose: no custom permission builder.
const (
	RoleStudent = db.UserRoleStudent
	RoleTeacher = db.UserRoleTeacher
	RoleAdmin   = db.UserRoleAdmin
	RoleAIAgent = db.UserRoleAiAgent
)

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID    int64
	Login     string
	Role      db.UserRole
	SessionID int64
}

// ActorType is how the principal appears in the audit log: the ai_agent role is its own actor
// type (arch §10.4), everybody else is a human.
func (p Principal) ActorType() db.ActorType {
	if p.Role == RoleAIAgent {
		return db.ActorTypeAiAgent
	}
	return db.ActorTypeHuman
}

// Actor returns the audit actor for changes made by this principal.
func (p Principal) Actor() store.Actor {
	return store.Actor{Type: p.ActorType(), ID: strconv.FormatInt(p.UserID, 10)}
}

type principalKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal stored by the Authenticate middleware.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
