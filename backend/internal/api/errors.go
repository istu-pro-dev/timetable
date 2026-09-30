package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/istu-pro-dev/timetable/backend/internal/httpx"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
)

// apiError is an error with a ready HTTP status and error code.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func badRequest(msg string) *apiError {
	return &apiError{http.StatusBadRequest, "invalid_request", msg}
}

func notFound(what string) *apiError {
	return &apiError{http.StatusNotFound, "not_found", what + " not found"}
}

func unprocessable(msg string) *apiError {
	return &apiError{http.StatusUnprocessableEntity, "validation_failed", msg}
}

// orNotFound turns pgx.ErrNoRows into a 404 naming what was not found.
func orNotFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(what)
	}
	return err
}

// op tells writeError whether a foreign key violation came from deleting a referenced row
// (409: still in use) or from writing a reference to a missing row (422).
type op int

const (
	opRead op = iota
	opWrite
	opDelete
)

// sqlstate for exclusion constraint violations not mapped by store.MapError.
const codeExclusionViolation = "23P01"

// writeError maps err to the JSON error response:
// not found → 404, unique → 409, FK → 409 on delete / 422 otherwise, check → 422,
// double booking (EXCLUDE) → 409, anything else → 500.
func (s *server) writeError(w http.ResponseWriter, err error, o op) {
	err = store.MapError(err)
	var ae *apiError
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &ae):
		httpx.WriteError(w, ae.status, ae.code, ae.message)
	case errors.Is(err, pgx.ErrNoRows):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "record not found")
	case errors.Is(err, store.ErrTeacherBusy), errors.Is(err, store.ErrGroupBusy),
		errors.Is(err, store.ErrRoomBusy), errors.Is(err, store.ErrParityMismatch):
		httpx.WriteError(w, http.StatusConflict, "schedule_conflict",
			"the change conflicts with existing schedule assignments: "+firstLine(err))
	case store.IsUniqueViolation(err):
		httpx.WriteError(w, http.StatusConflict, "already_exists", "a record with the same unique key already exists"+detail(err))
	case store.IsForeignKeyViolation(err) && o == opDelete:
		httpx.WriteError(w, http.StatusConflict, "in_use", "the record is referenced by other records"+detail(err))
	case store.IsForeignKeyViolation(err):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_reference", "a referenced record does not exist"+detail(err))
	case store.IsCheckViolation(err):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "constraint_violation", "the data violates a database constraint"+detail(err))
	case errors.As(err, &pgErr) && pgErr.Code == codeExclusionViolation:
		httpx.WriteError(w, http.StatusConflict, "schedule_conflict", "the change conflicts with existing schedule assignments")
	default:
		s.logger.Error("request failed", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}

// detail returns the PostgreSQL error detail (it names the key or constraint involved).
func detail(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Detail != "":
			return ": " + pgErr.Detail
		case pgErr.ConstraintName != "":
			return ": " + pgErr.ConstraintName
		}
	}
	return ""
}

func firstLine(err error) string {
	msg := err.Error()
	for i, r := range msg {
		if r == '\n' {
			return msg[:i]
		}
	}
	return msg
}
