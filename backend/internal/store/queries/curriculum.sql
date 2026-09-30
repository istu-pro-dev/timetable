-- name: ListCurriculumItems :many
SELECT * FROM curriculum_items ORDER BY id;

-- name: GetCurriculumItem :one
SELECT * FROM curriculum_items WHERE id = $1;

-- name: CreateCurriculumItem :one
INSERT INTO curriculum_items (discipline_id, kind, teacher_id, room_type, weekly_count, biweekly_count)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateCurriculumItem :one
UPDATE curriculum_items
SET discipline_id = $2, kind = $3, teacher_id = $4, room_type = $5, weekly_count = $6, biweekly_count = $7
WHERE id = $1
RETURNING *;

-- name: DeleteCurriculumItem :execrows
DELETE FROM curriculum_items WHERE id = $1;

-- name: ListCurriculumAudience :many
SELECT * FROM curriculum_audience ORDER BY curriculum_item_id, group_id, division, part;

-- name: AddCurriculumAudience :exec
INSERT INTO curriculum_audience (curriculum_item_id, group_id, division, part) VALUES ($1, $2, $3, $4);

-- name: ClearCurriculumAudience :exec
DELETE FROM curriculum_audience WHERE curriculum_item_id = $1;

-- name: ListLessons :many
SELECT * FROM lessons ORDER BY curriculum_item_id, seq;

-- name: DeleteLessonsOfItem :exec
DELETE FROM lessons WHERE curriculum_item_id = $1;

-- name: CreateLesson :one
INSERT INTO lessons (curriculum_item_id, seq, biweekly) VALUES ($1, $2, $3) RETURNING *;
