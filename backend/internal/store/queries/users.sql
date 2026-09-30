-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByLogin :one
SELECT * FROM users WHERE login = $1;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE role = 'admin' AND NOT disabled;

-- name: CreateUser :one
INSERT INTO users (login, password_hash, role, display_name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();
