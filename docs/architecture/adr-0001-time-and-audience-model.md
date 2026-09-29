# ADR-0001: Time grid and audience model

- Status: accepted
- Date: 2026-09-29
- Issue: #81 · Design docs: research §3.11–3.12, arch §1.1

## Context

The design documents describe slots as "day × period" and conflicts between groups, but leave
out two things that are common in Russian universities and that every layer depends on
(DB schema #20, engine model #25, hard-constraint checker #26):

1. **Week parity (числитель / знаменатель).** Many lessons take place every other week. Two
   lessons with opposite parity may share a teacher, a group and a room at the same day/period.
2. **Subgroups and streams.** A group may be split into subgroups that study in parallel
   (languages, labs, PE), and several groups may attend one joint lecture (a stream).

## Decision

Code lives in `backend/internal/domain`.

### Time

- `Day` (Monday = 0, codes `MO`..`SU`), `Period` is a 1-based pair number.
- `Parity`: `every`, `odd`, `even`. Two parities overlap unless one is `odd` and the other `even`.
- `Slot = (Day, Period, Parity)`. Two slots overlap if day and period match and parities overlap.
- `Grid{Days, PeriodsPerDay}` fixes the shape of the week. Occupancy is tracked per **cell**
  `(day, period, week ∈ {odd, even})`, so there are `Days × PeriodsPerDay × 2` cells. An every-week
  slot covers both cells of its position and an odd/even slot covers one.
  Checking H1–H3 then means looking up flat arrays indexed by cell. There are no parity special
  cases in the hot loop.
- The graph-colouring view (arch §1.1) uses `(day, period, parity)` slots as colours. Two slots
  "conflict" exactly when their cell sets intersect. A test proves this equivalence.

### Audience

- `Member = (Group, Division, Part)`. A whole group has an empty `Division`.
- A **division** is one way to split a group, such as `english` by level or `pe` by gender.
  Subgroups of the same division are disjoint, so they can run in parallel. Subgroups of
  different divisions cut across each other, so we treat them as overlapping.
- `Audience = []Member`. A single group, a subgroup and a stream all have the same shape.
  Two audiences overlap (H2) if any pair of their members overlaps.
- Sizes (for H6 capacity) belong to reference data (group size, subgroup size), not to the
  audience value.

### Identifiers

Reference entities use `int64` IDs that mirror `bigint generated always as identity` keys
in PostgreSQL. The engine maps them to dense `int32` indices internally.

## Consequences

- The DB schema stores `parity` on assignments and `(division, part)` on lesson audiences.
  EXCLUDE constraints (#23) must take parity into account. Store each assignment's occupied cells
  (or a week-range) so that `odd` and `even` rows do not collide, while `every` collides with both.
- A course with an "every week" load of N pairs can be expressed as N `every` lessons or as
  2N `odd`/`even` lessons. The curriculum stays in hours; lesson generation picks the form.
- Fewer than two weeks of periodicity (e.g. "weeks 1–8 only") is out of scope for v1.0.
