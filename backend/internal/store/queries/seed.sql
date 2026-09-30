-- name: TruncateSeedData :exec
-- Wipes reference data, curriculum, lessons and schedules (not the audit log) and restarts
-- identities. Used by the demo seed (cmd/seed) to reload its dataset idempotently.
TRUNCATE time_grid, periods, buildings, room_types, rooms, groups, subgroups, teachers,
    disciplines, teacher_availability, room_availability, curriculum_items, curriculum_audience,
    lessons, schedules, assignments
    RESTART IDENTITY CASCADE;
