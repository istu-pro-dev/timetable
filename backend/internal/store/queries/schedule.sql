-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY created_at DESC, id DESC;

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = $1;

-- name: CreateSchedule :one
INSERT INTO schedules (name) VALUES ($1) RETURNING *;

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE id = $1;

-- name: ListAssignments :many
SELECT * FROM assignments WHERE schedule_id = $1 ORDER BY lesson_id;

-- name: UpsertAssignment :exec
INSERT INTO assignments (schedule_id, lesson_id, day, period, parity, room_id, pinned)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (schedule_id, lesson_id) DO UPDATE
SET day = EXCLUDED.day, period = EXCLUDED.period, parity = EXCLUDED.parity,
    room_id = EXCLUDED.room_id, pinned = EXCLUDED.pinned;

-- name: DeleteAssignment :execrows
DELETE FROM assignments WHERE schedule_id = $1 AND lesson_id = $2;

-- name: LockSchedule :one
SELECT id FROM schedules WHERE id = $1 FOR UPDATE;
