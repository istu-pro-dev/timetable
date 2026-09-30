-- name: InsertAudit :one
INSERT INTO audit_log (actor_type, actor_id, via_assistant, entity, entity_id, before, after, diff, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.narg(entity)::text IS NULL OR entity = sqlc.narg(entity))
  AND (sqlc.narg(entity_id)::text IS NULL OR entity_id = sqlc.narg(entity_id))
ORDER BY at DESC, id DESC
LIMIT $1 OFFSET $2;
