-- +goose Up
-- Double-booking protection (H1–H3) enforced by the database itself (arch §7).
--
-- Every assignment occupies a range of cells (ADR-0001): cell = (day * 16 + period - 1) * 2 + week,
-- where week 0 is odd and week 1 is even. An every-week assignment covers both weeks.
-- Ranges overlap exactly when two assignments meet in at least one week.

CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS cube;

ALTER TABLE assignments
    ADD COLUMN cells int4range GENERATED ALWAYS AS (
        int4range(
            (day * 16 + period - 1) * 2 + CASE WHEN parity = 'even' THEN 1 ELSE 0 END,
            (day * 16 + period - 1) * 2 + CASE WHEN parity = 'odd' THEN 1 ELSE 2 END
        )
    ) STORED,
    ADD COLUMN teacher_id bigint REFERENCES teachers (id);

-- +goose StatementBegin
CREATE FUNCTION assignments_before_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    is_biweekly boolean;
BEGIN
    SELECT l.biweekly, ci.teacher_id INTO is_biweekly, NEW.teacher_id
    FROM lessons l JOIN curriculum_items ci ON ci.id = l.curriculum_item_id
    WHERE l.id = NEW.lesson_id;

    IF is_biweekly <> (NEW.parity <> 'every') THEN
        RAISE EXCEPTION 'lesson % is %, parity % does not match', NEW.lesson_id,
            CASE WHEN is_biweekly THEN 'biweekly' ELSE 'weekly' END, NEW.parity
            USING ERRCODE = 'check_violation', CONSTRAINT = 'assignments_parity_matches_lesson';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER assignments_before_write
    BEFORE INSERT OR UPDATE OF lesson_id, parity ON assignments
    FOR EACH ROW EXECUTE FUNCTION assignments_before_write();

UPDATE assignments SET parity = parity;
ALTER TABLE assignments ALTER COLUMN teacher_id SET NOT NULL;

ALTER TABLE assignments
    ADD CONSTRAINT assignments_teacher_no_overlap
        EXCLUDE USING gist (schedule_id WITH =, teacher_id WITH =, cells WITH &&),
    ADD CONSTRAINT assignments_room_no_overlap
        EXCLUDE USING gist (schedule_id WITH =, room_id WITH =, cells WITH &&) WHERE (room_id IS NOT NULL);

-- Group occupancy: one row per (assignment, audience member).
--
-- Subgroup semantics (ADR-0001) are encoded as boxes in an 8-dimensional cube: dimension k stands
-- for the group's k-th division. A whole group spans every dimension fully; part p of division k
-- is pinned to p on dimension k and spans the rest. Parts of one division are therefore disjoint
-- while different divisions always intersect.
--
-- Division indices are allocated once per group and never change, so boxes stored earlier stay
-- comparable with new ones.
CREATE TABLE group_divisions (
    group_id bigint NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    division text NOT NULL CHECK (division <> ''),
    idx      smallint NOT NULL CHECK (idx BETWEEN 1 AND 8),
    PRIMARY KEY (group_id, division),
    UNIQUE (group_id, idx)
);

CREATE TABLE assignment_audience (
    schedule_id bigint NOT NULL,
    lesson_id   bigint NOT NULL,
    group_id    bigint NOT NULL REFERENCES groups (id),
    division    text NOT NULL,
    part        smallint NOT NULL,
    box         cube NOT NULL,
    cells       int4range NOT NULL,
    FOREIGN KEY (schedule_id, lesson_id) REFERENCES assignments (schedule_id, lesson_id) ON DELETE CASCADE,
    PRIMARY KEY (schedule_id, lesson_id, group_id, division, part),
    CONSTRAINT assignments_group_no_overlap
        EXCLUDE USING gist (schedule_id WITH =, group_id WITH =, cells WITH &&, box WITH &&)
);

-- +goose StatementBegin
CREATE FUNCTION audience_box(p_group bigint, p_division text, p_part smallint) RETURNS cube
LANGUAGE plpgsql AS $$
DECLARE
    lo float8[] := array_fill(0::float8, ARRAY[8]);
    hi float8[] := array_fill(32767::float8, ARRAY[8]);
    k int;
BEGIN
    IF p_division = '' THEN
        RETURN cube(lo, hi);
    END IF;
    SELECT idx INTO k FROM group_divisions WHERE group_id = p_group AND division = p_division;
    IF k IS NULL THEN
        PERFORM 1 FROM groups WHERE id = p_group FOR UPDATE;
        SELECT COALESCE(max(idx), 0) + 1 INTO k FROM group_divisions WHERE group_id = p_group;
        IF k > 8 THEN
            RAISE EXCEPTION 'group % has more than 8 divisions', p_group USING ERRCODE = 'check_violation';
        END IF;
        INSERT INTO group_divisions (group_id, division, idx) VALUES (p_group, p_division, k);
    END IF;
    lo[k] := p_part;
    hi[k] := p_part;
    RETURN cube(lo, hi);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION refresh_assignment_audience(p_schedule bigint, p_lesson bigint) RETURNS void
LANGUAGE sql AS $$
    DELETE FROM assignment_audience WHERE schedule_id = p_schedule AND lesson_id = p_lesson;
    INSERT INTO assignment_audience (schedule_id, lesson_id, group_id, division, part, box, cells)
    SELECT a.schedule_id, a.lesson_id, ca.group_id, ca.division, ca.part,
           audience_box(ca.group_id, ca.division, ca.part), a.cells
    FROM assignments a
    JOIN lessons l ON l.id = a.lesson_id
    JOIN curriculum_audience ca ON ca.curriculum_item_id = l.curriculum_item_id
    WHERE a.schedule_id = p_schedule AND a.lesson_id = p_lesson;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION assignments_after_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_assignment_audience(NEW.schedule_id, NEW.lesson_id);
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER assignments_after_write
    AFTER INSERT OR UPDATE ON assignments
    FOR EACH ROW EXECUTE FUNCTION assignments_after_write();

-- Keep derived data in sync when the curriculum changes under existing assignments.
-- +goose StatementBegin
CREATE FUNCTION curriculum_audience_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    item bigint := COALESCE(NEW.curriculum_item_id, OLD.curriculum_item_id);
BEGIN
    PERFORM refresh_assignment_audience(a.schedule_id, a.lesson_id)
    FROM assignments a JOIN lessons l ON l.id = a.lesson_id
    WHERE l.curriculum_item_id = item;
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER curriculum_audience_changed
    AFTER INSERT OR UPDATE OR DELETE ON curriculum_audience
    FOR EACH ROW EXECUTE FUNCTION curriculum_audience_changed();

-- +goose StatementBegin
CREATE FUNCTION curriculum_teacher_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE assignments a SET teacher_id = NEW.teacher_id
    FROM lessons l
    WHERE l.id = a.lesson_id AND l.curriculum_item_id = NEW.id;
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER curriculum_teacher_changed
    AFTER UPDATE OF teacher_id ON curriculum_items
    FOR EACH ROW WHEN (OLD.teacher_id IS DISTINCT FROM NEW.teacher_id)
    EXECUTE FUNCTION curriculum_teacher_changed();

SELECT refresh_assignment_audience(schedule_id, lesson_id) FROM assignments;

-- +goose Down
DROP TRIGGER curriculum_teacher_changed ON curriculum_items;
DROP FUNCTION curriculum_teacher_changed();
DROP TRIGGER curriculum_audience_changed ON curriculum_audience;
DROP FUNCTION curriculum_audience_changed();
DROP TRIGGER assignments_after_write ON assignments;
DROP FUNCTION assignments_after_write();
DROP FUNCTION refresh_assignment_audience(bigint, bigint);
DROP FUNCTION audience_box(bigint, text, smallint);
DROP TABLE assignment_audience;
DROP TABLE group_divisions;
ALTER TABLE assignments
    DROP CONSTRAINT assignments_room_no_overlap,
    DROP CONSTRAINT assignments_teacher_no_overlap;
DROP TRIGGER assignments_before_write ON assignments;
DROP FUNCTION assignments_before_write();
ALTER TABLE assignments DROP COLUMN teacher_id, DROP COLUMN cells;
