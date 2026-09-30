package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Double-booking errors raised by the database EXCLUDE constraints (H1–H3).
var (
	ErrTeacherBusy = errors.New("teacher is already busy at this time")
	ErrGroupBusy   = errors.New("group is already busy at this time")
	ErrRoomBusy    = errors.New("room is already occupied at this time")
	// ErrParityMismatch means a weekly lesson got an odd/even slot or vice versa.
	ErrParityMismatch = errors.New("slot parity does not match the lesson")
)

var constraintErrors = map[string]error{
	"assignments_teacher_no_overlap":    ErrTeacherBusy,
	"assignments_group_no_overlap":      ErrGroupBusy,
	"assignments_room_no_overlap":       ErrRoomBusy,
	"assignments_parity_matches_lesson": ErrParityMismatch,
}

// MapError translates known constraint violations into typed errors that wrap the original
// database error. Other errors are returned unchanged.
func MapError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if typed, ok := constraintErrors[pgErr.ConstraintName]; ok {
		return errors.Join(typed, err)
	}
	return err
}
