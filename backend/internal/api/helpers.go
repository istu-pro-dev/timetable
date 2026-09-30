package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/auth"
	"github.com/istu-pro-dev/timetable/backend/internal/httpx"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// maxBody limits JSON request bodies of the CRUD endpoints.
const maxBody = 256 << 10

// withStore answers 503 when the server runs without a database.
func (s *server) withStore(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "no_database", "the server runs without a database")
			return
		}
		h(w, r)
	})
}

// validatable request bodies normalise themselves (trim strings) and check their fields.
type validatable interface {
	validate() error
}

// decode reads and validates a JSON body; on failure it writes the error response and
// returns false.
func (s *server) decode(w http.ResponseWriter, r *http.Request, in validatable) bool {
	if err := httpx.DecodeJSON(w, r, in, maxBody); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	if err := in.validate(); err != nil {
		s.writeError(w, err, opWrite)
		return false
	}
	return true
}

// read runs a read-only query function and writes its result as JSON.
func (s *server) read(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, q *db.Queries) (any, error)) {
	out, err := fn(r.Context(), s.store.Queries)
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// mutation is an audited change made through Store.WithAudit.
type mutation struct {
	entity   string
	entityID string
	op       op
	status   int // response status on success; 204 writes no body
	run      func(ctx context.Context, q *db.Queries, c *store.Change) (any, error)
}

// mutate runs m in a transaction together with its audit entry (actor = the authenticated
// principal) and writes the result.
func (s *server) mutate(w http.ResponseWriter, r *http.Request, m mutation) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var out any
	_, err := s.store.WithAudit(r.Context(), p.Actor(), store.Change{Entity: m.entity, EntityID: m.entityID},
		func(q *db.Queries, _ pgx.Tx, c *store.Change) error {
			var err error
			out, err = m.run(r.Context(), q, c)
			return err
		})
	if err != nil {
		s.writeError(w, err, m.op)
		return
	}
	if m.status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpx.WriteJSON(w, m.status, out)
}

// pathID parses a positive integer path parameter.
func pathID(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, badRequest(fmt.Sprintf("path parameter %q must be a positive integer", name))
	}
	return v, nil
}

// queryID parses an optional positive integer query parameter.
func queryID(r *http.Request, name string) (pgtype.Int8, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return pgtype.Int8{}, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return pgtype.Int8{}, badRequest(fmt.Sprintf("query parameter %q must be a positive integer", name))
	}
	return pgtype.Int8{Int64: v, Valid: true}, nil
}

func idString(id int64) string { return strconv.FormatInt(id, 10) }

// validator collects field errors into one 422 response.
type validator struct {
	errs []string
}

func (v *validator) addf(field, format string, args ...any) {
	v.errs = append(v.errs, field+": "+fmt.Sprintf(format, args...))
}

// text trims *s and checks its length in characters.
func (v *validator) text(field string, s *string, minLen, maxLen int) {
	*s = strings.TrimSpace(*s)
	if n := utf8.RuneCountInString(*s); n < minLen || n > maxLen {
		if minLen > 0 {
			v.addf(field, "must be %d to %d characters long", minLen, maxLen)
		} else {
			v.addf(field, "must be at most %d characters long", maxLen)
		}
	}
}

func (v *validator) between(field string, x, lo, hi int64) {
	if x < lo || x > hi {
		v.addf(field, "must be between %d and %d", lo, hi)
	}
}

func (v *validator) positive(field string, x int64) {
	if x <= 0 {
		v.addf(field, "must be a positive id")
	}
}

var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (v *validator) code(field, s string) {
	if len(s) == 0 || len(s) > 32 || !codePattern.MatchString(s) {
		v.addf(field, "must be 1 to 32 characters of a-z, 0-9, '_' or '-', starting with a letter or digit")
	}
}

func (v *validator) err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return unprocessable(strings.Join(v.errs, "; "))
}
