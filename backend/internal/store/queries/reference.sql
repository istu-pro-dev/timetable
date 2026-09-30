-- name: GetTimeGrid :one
SELECT * FROM time_grid;

-- name: UpsertTimeGrid :one
INSERT INTO time_grid (id, days, periods_per_day) VALUES (true, $1, $2)
ON CONFLICT (id) DO UPDATE SET days = EXCLUDED.days, periods_per_day = EXCLUDED.periods_per_day
RETURNING *;

-- name: ListPeriods :many
SELECT * FROM periods ORDER BY number;

-- name: UpsertPeriod :one
INSERT INTO periods (number, starts_at, ends_at) VALUES ($1, $2, $3)
ON CONFLICT (number) DO UPDATE SET starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at
RETURNING *;

-- name: ListBuildings :many
SELECT * FROM buildings ORDER BY name;

-- name: GetBuilding :one
SELECT * FROM buildings WHERE id = $1;

-- name: CreateBuilding :one
INSERT INTO buildings (name, address) VALUES ($1, $2) RETURNING *;

-- name: UpdateBuilding :one
UPDATE buildings SET name = $2, address = $3 WHERE id = $1 RETURNING *;

-- name: DeleteBuilding :execrows
DELETE FROM buildings WHERE id = $1;

-- name: ListRoomTypes :many
SELECT * FROM room_types ORDER BY code;

-- name: UpsertRoomType :one
INSERT INTO room_types (code, name) VALUES ($1, $2)
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListRooms :many
SELECT * FROM rooms ORDER BY building_id, name;

-- name: GetRoom :one
SELECT * FROM rooms WHERE id = $1;

-- name: CreateRoom :one
INSERT INTO rooms (building_id, name, room_type, capacity) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateRoom :one
UPDATE rooms SET building_id = $2, name = $3, room_type = $4, capacity = $5 WHERE id = $1 RETURNING *;

-- name: DeleteRoom :execrows
DELETE FROM rooms WHERE id = $1;

-- name: ListGroups :many
SELECT * FROM groups ORDER BY name;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: CreateGroup :one
INSERT INTO groups (name, course, size) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateGroup :one
UPDATE groups SET name = $2, course = $3, size = $4 WHERE id = $1 RETURNING *;

-- name: DeleteGroup :execrows
DELETE FROM groups WHERE id = $1;

-- name: ListSubgroups :many
SELECT * FROM subgroups ORDER BY group_id, division, part;

-- name: CreateSubgroup :one
INSERT INTO subgroups (group_id, division, part, size) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: DeleteSubgroup :execrows
DELETE FROM subgroups WHERE id = $1;

-- name: ListTeachers :many
SELECT * FROM teachers ORDER BY full_name;

-- name: GetTeacher :one
SELECT * FROM teachers WHERE id = $1;

-- name: CreateTeacher :one
INSERT INTO teachers (full_name, short_name) VALUES ($1, $2) RETURNING *;

-- name: UpdateTeacher :one
UPDATE teachers SET full_name = $2, short_name = $3 WHERE id = $1 RETURNING *;

-- name: DeleteTeacher :execrows
DELETE FROM teachers WHERE id = $1;

-- name: ListDisciplines :many
SELECT * FROM disciplines ORDER BY name;

-- name: GetDiscipline :one
SELECT * FROM disciplines WHERE id = $1;

-- name: CreateDiscipline :one
INSERT INTO disciplines (name) VALUES ($1) RETURNING *;

-- name: UpdateDiscipline :one
UPDATE disciplines SET name = $2 WHERE id = $1 RETURNING *;

-- name: DeleteDiscipline :execrows
DELETE FROM disciplines WHERE id = $1;

-- name: ListTeacherAvailability :many
SELECT * FROM teacher_availability ORDER BY teacher_id, day, period, parity;

-- name: SetTeacherAvailability :exec
INSERT INTO teacher_availability (teacher_id, day, period, parity, status) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (teacher_id, day, period, parity) DO UPDATE SET status = EXCLUDED.status;

-- name: ClearTeacherAvailability :exec
DELETE FROM teacher_availability WHERE teacher_id = $1;

-- name: ListRoomAvailability :many
SELECT * FROM room_availability ORDER BY room_id, day, period, parity;

-- name: SetRoomAvailability :exec
INSERT INTO room_availability (room_id, day, period, parity, status) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (room_id, day, period, parity) DO UPDATE SET status = EXCLUDED.status;

-- name: ClearRoomAvailability :exec
DELETE FROM room_availability WHERE room_id = $1;
