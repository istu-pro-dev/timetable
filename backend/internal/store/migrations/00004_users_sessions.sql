-- +goose Up
CREATE TYPE user_role AS ENUM ('student', 'teacher', 'admin', 'ai_agent');

-- Local accounts (arch §16.3 fallback login). teacher_id / group_id bind a teacher or a student
-- account to its reference-data record; they are filled in later by the role mapping.
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    login         text NOT NULL UNIQUE CHECK (login <> '' AND login = lower(login)),
    password_hash text NOT NULL,
    role          user_role NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    teacher_id    bigint REFERENCES teachers (id) ON DELETE SET NULL,
    group_id      bigint REFERENCES groups (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    disabled      boolean NOT NULL DEFAULT false
);

-- Refresh-token sessions. Only the SHA-256 of the opaque refresh token is stored.
CREATE TABLE sessions (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
DROP TYPE user_role;
