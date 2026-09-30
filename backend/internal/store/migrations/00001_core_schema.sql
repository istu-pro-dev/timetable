-- +goose Up

-- Week shape. Single row; see ADR-0001.
CREATE TABLE time_grid (
    id              boolean PRIMARY KEY DEFAULT true CHECK (id),
    days            smallint NOT NULL CHECK (days BETWEEN 1 AND 7),
    periods_per_day smallint NOT NULL CHECK (periods_per_day BETWEEN 1 AND 16)
);

CREATE TABLE periods (
    number    smallint PRIMARY KEY CHECK (number BETWEEN 1 AND 16),
    starts_at time NOT NULL,
    ends_at   time NOT NULL CHECK (ends_at > starts_at)
);

CREATE TABLE buildings (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name    text NOT NULL UNIQUE,
    address text NOT NULL DEFAULT ''
);

CREATE TABLE room_types (
    code text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE rooms (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    building_id bigint NOT NULL REFERENCES buildings (id),
    name        text NOT NULL,
    room_type   text NOT NULL REFERENCES room_types (code),
    capacity    integer NOT NULL CHECK (capacity > 0),
    UNIQUE (building_id, name)
);

CREATE TABLE groups (
    id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name   text NOT NULL UNIQUE,
    course smallint NOT NULL DEFAULT 1 CHECK (course BETWEEN 1 AND 6),
    size   integer NOT NULL CHECK (size >= 0)
);

-- A subgroup is part `part` of one way (`division`) to split a group.
CREATE TABLE subgroups (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id bigint NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    division text NOT NULL CHECK (division <> ''),
    part     smallint NOT NULL CHECK (part >= 1),
    size     integer NOT NULL CHECK (size >= 0),
    UNIQUE (group_id, division, part)
);

CREATE TABLE teachers (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    full_name  text NOT NULL,
    short_name text NOT NULL
);

CREATE TABLE disciplines (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TYPE parity AS ENUM ('every', 'odd', 'even');
CREATE TYPE availability AS ENUM ('unavailable', 'undesired', 'preferred');

-- Explicit exceptions only: a missing row means "available, no preference".
CREATE TABLE teacher_availability (
    teacher_id bigint NOT NULL REFERENCES teachers (id) ON DELETE CASCADE,
    day        smallint NOT NULL CHECK (day BETWEEN 0 AND 6),
    period     smallint NOT NULL CHECK (period BETWEEN 1 AND 16),
    parity     parity NOT NULL DEFAULT 'every',
    status     availability NOT NULL,
    PRIMARY KEY (teacher_id, day, period, parity)
);

CREATE TABLE room_availability (
    room_id bigint NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    day     smallint NOT NULL CHECK (day BETWEEN 0 AND 6),
    period  smallint NOT NULL CHECK (period BETWEEN 1 AND 16),
    parity  parity NOT NULL DEFAULT 'every',
    status  availability NOT NULL,
    PRIMARY KEY (room_id, day, period, parity)
);

CREATE TYPE lesson_kind AS ENUM ('lecture', 'seminar', 'lab');

-- Curriculum item: who teaches what to whom, how often per week.
-- weekly_count lessons every week plus biweekly_count lessons every other week.
CREATE TABLE curriculum_items (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    discipline_id  bigint NOT NULL REFERENCES disciplines (id),
    kind           lesson_kind NOT NULL,
    teacher_id     bigint NOT NULL REFERENCES teachers (id),
    room_type      text NOT NULL REFERENCES room_types (code),
    weekly_count   smallint NOT NULL DEFAULT 0 CHECK (weekly_count >= 0),
    biweekly_count smallint NOT NULL DEFAULT 0 CHECK (biweekly_count >= 0),
    CHECK (weekly_count + biweekly_count > 0)
);

-- Audience of a curriculum item: whole groups (division = '') or subgroups.
-- Several whole groups make a stream.
CREATE TABLE curriculum_audience (
    curriculum_item_id bigint NOT NULL REFERENCES curriculum_items (id) ON DELETE CASCADE,
    group_id           bigint NOT NULL REFERENCES groups (id),
    division           text NOT NULL DEFAULT '',
    part               smallint NOT NULL DEFAULT 0,
    CHECK ((division = '' AND part = 0) OR (division <> '' AND part >= 1)),
    PRIMARY KEY (curriculum_item_id, group_id, division, part)
);

-- Lessons are the schedulable units generated from curriculum items.
CREATE TABLE lessons (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    curriculum_item_id bigint NOT NULL REFERENCES curriculum_items (id) ON DELETE CASCADE,
    seq                smallint NOT NULL CHECK (seq >= 1),
    biweekly           boolean NOT NULL,
    UNIQUE (curriculum_item_id, seq)
);

-- A schedule is one set of assignments: a solver result, a working copy or a published version.
CREATE TABLE schedules (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE assignments (
    schedule_id bigint NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    lesson_id   bigint NOT NULL REFERENCES lessons (id) ON DELETE CASCADE,
    day         smallint NOT NULL CHECK (day BETWEEN 0 AND 6),
    period      smallint NOT NULL CHECK (period BETWEEN 1 AND 16),
    parity      parity NOT NULL,
    room_id     bigint REFERENCES rooms (id),
    pinned      boolean NOT NULL DEFAULT false,
    PRIMARY KEY (schedule_id, lesson_id)
);

CREATE INDEX assignments_room_idx ON assignments (room_id);

-- +goose Down
DROP TABLE assignments;
DROP TABLE schedules;
DROP TABLE lessons;
DROP TABLE curriculum_audience;
DROP TABLE curriculum_items;
DROP TYPE lesson_kind;
DROP TABLE room_availability;
DROP TABLE teacher_availability;
DROP TYPE availability;
DROP TYPE parity;
DROP TABLE disciplines;
DROP TABLE teachers;
DROP TABLE subgroups;
DROP TABLE groups;
DROP TABLE rooms;
DROP TABLE room_types;
DROP TABLE buildings;
DROP TABLE periods;
DROP TABLE time_grid;
