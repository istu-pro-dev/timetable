-- Queries used by the reference-data REST API (single-row access for CRUD).

-- name: GetRoomType :one
SELECT * FROM room_types WHERE code = $1;

-- name: CreateRoomType :one
INSERT INTO room_types (code, name) VALUES ($1, $2) RETURNING *;

-- name: UpdateRoomType :one
UPDATE room_types SET name = $2 WHERE code = $1 RETURNING *;

-- name: DeleteRoomType :execrows
DELETE FROM room_types WHERE code = $1;

-- name: ListRoomsFiltered :many
SELECT * FROM rooms
WHERE (sqlc.narg(building_id)::bigint IS NULL OR building_id = sqlc.narg(building_id))
  AND (sqlc.narg(room_type)::text IS NULL OR room_type = sqlc.narg(room_type))
ORDER BY building_id, name;

-- name: ListRoomAvailabilityOf :many
SELECT * FROM room_availability WHERE room_id = $1 ORDER BY day, period, parity;

-- name: ListTeacherAvailabilityOf :many
SELECT * FROM teacher_availability WHERE teacher_id = $1 ORDER BY day, period, parity;

-- name: ListSubgroupsOf :many
SELECT * FROM subgroups WHERE group_id = $1 ORDER BY division, part;

-- name: GetSubgroupOf :one
SELECT * FROM subgroups WHERE group_id = $1 AND id = $2;

-- name: UpdateSubgroupOf :one
UPDATE subgroups SET division = $3, part = $4, size = $5 WHERE group_id = $1 AND id = $2 RETURNING *;

-- name: DeleteSubgroupOf :execrows
DELETE FROM subgroups WHERE group_id = $1 AND id = $2;

-- name: GetPeriod :one
SELECT * FROM periods WHERE number = $1;

-- name: DeletePeriod :execrows
DELETE FROM periods WHERE number = $1;

-- name: ListCurriculumItemsFiltered :many
SELECT * FROM curriculum_items ci
WHERE (sqlc.narg(teacher_id)::bigint IS NULL OR ci.teacher_id = sqlc.narg(teacher_id))
  AND (sqlc.narg(discipline_id)::bigint IS NULL OR ci.discipline_id = sqlc.narg(discipline_id))
  AND (sqlc.narg(group_id)::bigint IS NULL OR EXISTS (
        SELECT 1 FROM curriculum_audience ca
        WHERE ca.curriculum_item_id = ci.id AND ca.group_id = sqlc.narg(group_id)))
ORDER BY ci.id;

-- name: ListCurriculumAudienceOf :many
SELECT * FROM curriculum_audience WHERE curriculum_item_id = $1 ORDER BY group_id, division, part;

-- name: ListLessonsOf :many
SELECT * FROM lessons WHERE curriculum_item_id = $1 ORDER BY seq;
