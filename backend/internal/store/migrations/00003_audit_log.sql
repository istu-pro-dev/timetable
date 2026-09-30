-- +goose Up
CREATE TYPE actor_type AS ENUM ('human', 'ai_agent', 'solver');

-- Every change to a schedule or reference data is written together with its audit row
-- in one transaction (arch §10.2, §10.4).
CREATE TABLE audit_log (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at            timestamptz NOT NULL DEFAULT now(),
    actor_type    actor_type NOT NULL,
    actor_id      text NOT NULL,
    via_assistant boolean NOT NULL DEFAULT false,
    entity        text NOT NULL,
    entity_id     text NOT NULL DEFAULT '',
    before        jsonb,
    after         jsonb,
    diff          jsonb,
    reason        text NOT NULL DEFAULT ''
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id, at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_type, actor_id, at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TYPE actor_type;
