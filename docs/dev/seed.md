# Demo seed dataset

A small, realistic dataset for one faculty (an IT institute). Use it to demo features and
test them end to end. The code is in `backend/internal/seed` and the command in
`backend/cmd/seed`. The dataset is built deterministically in Go, with no randomness, so every
run produces the same rows and the same IDs.

## Running

```sh
make up seed                # start the compose stack, then load the dataset
make seed                   # load into DATABASE_URL (default: the compose database)
make seed DATABASE_URL='postgres://user:pass@host:5432/db?sslmode=disable'
```

The default URL is `postgres://timetable:timetable@localhost:5432/timetable?sslmode=disable`,
which matches `deploy/.env.example`. The command:

1. connects and applies pending migrations (`store.Migrate`). It retries for up to 30 s
   (`-wait`) while the database or the API container's migrations are still starting;
2. in **one transaction**, runs `TRUNCATE ... RESTART IDENTITY CASCADE` on reference data,
   curriculum, lessons and schedules, inserts the dataset and writes one `audit_log` row
   (`actor_type = 'human'`, `actor_id = 'seed'`, `entity = 'seed'`, row counts in `after`).

**Idempotency: replace, don't merge.** Every run leaves the database in the same state.
Identities restart, so IDs are stable too. The only thing that grows is `audit_log`, with one
row per run. Anything else in those tables is lost, **including schedules and assignments**.
Use it only on dev and demo databases.

Check it after loading:

```sh
psql "$DATABASE_URL" -c "SELECT (SELECT count(*) FROM groups) groups, (SELECT count(*) FROM lessons) lessons"
```

## What's inside

| Table | Rows | Notes |
|---|---:|---|
| `time_grid` | 1 | 6 days (Mon–Sat) × 7 pairs |
| `periods` | 7 | 08:00–09:30, 09:40–11:10, 11:20–12:50, 13:20–14:50, 15:00–16:30, 16:40–18:10, 18:20–19:50 |
| `buildings` | 3 | Корпус А (lectures, seminars), Б (computer classes, labs), В (physics labs, gym) |
| `room_types` | 5 | `lecture`, `seminar`, `lab`, `computer`, `gym` |
| `rooms` | 25 | 4 lecture halls (80–150 seats), 9 seminar rooms (16–32), 6 computer classes (16–30), 5 labs (14–25), 1 gym (60) |
| `groups` | 20 | courses 1–4, five per course: ИВТб-YY-1/2, ПИб-YY-1/2, ИСТб-YY-1, sizes 18–30 |
| `subgroups` | 56 | division `english` parts 1–2 on every group; division `lab` parts 1–2 on ИВТ groups |
| `teachers` | 40 | seven departments (used only to pick who teaches what), `short_name` like `Иванов И.П.` |
| `disciplines` | 60 | 14–18 per course; `Иностранный язык` and `Физическая культура и спорт` run across courses |
| `teacher_availability` | 110 | see edge cases |
| `room_availability` | 9 | see edge cases |
| `curriculum_items` | 377 | lectures, seminars, labs, English, PE |
| `curriculum_audience` | 503 | |
| `lessons` | 411 | 185 weekly + 226 biweekly (298 pairs per week counting biweekly as ½) |

The curriculum is written as a plan per course in `internal/seed/curriculum.go`:

- **Lectures** go to streams: ИВТ-1 + ИВТ-2 + ИСТ (3 groups) and ПИ-1 + ПИ-2 (2 groups). "Wide"
  disciplines, such as linear algebra, physics and ML, stream ИВТ + ПИ (4 groups, up to 107
  students) and give ИСТ its own lecture. Program-specific disciplines in courses 2–4 (ИВТ-,
  ПИ- or ИСТ-only) have one lecture stream for that program.
- **Seminars** and **PE** (`gym` rooms) go to each whole group.
- **Labs** go to each `lab` subgroup of ИВТ groups. The smaller ПИ and ИСТ groups do labs as a
  whole group. Labs use `computer` or `lab` rooms.
- **English** goes to each `english` subgroup.
- Each item has `weekly_count` + `biweekly_count` lessons. Lesson rows get `seq` 1..N: the weekly
  ones first (`biweekly = false`), then the biweekly ones (`biweekly = true`).
- A department's teachers are assigned greedily: the least loaded teacher who stays under the
  cap (18 pairs, 6 for part-timers) gets the item.

Weekly load by room type (pairs): lecture 65.5, seminar 94, computer 104, lab 17, gym 17.5.
Group load is 19 pairs in course 1 and goes down to 14.5 in course 4. Teacher load is
1.5–11 pairs.

## Edge cases

| Case | Where |
|---|---|
| Streamed lectures of 2, 3 and 4 groups, plus single-group lectures | wide disciplines, ИСТ lectures |
| Parallel subgroups (two divisions per ИВТ group) | `english` + `lab` |
| Whole-group labs vs. subgroup labs | ПИ/ИСТ vs. ИВТ |
| Biweekly lessons (odd/even weeks) | 226 of 411 lessons |
| Part-time teachers available on two days only (`unavailable`, parity `every`, on the other four days) | Андреев Е.П. (Mon, Thu), Воробьёва Л.Ю. (Tue, Fri), Дмитриева П.А. (Wed, Sat) |
| Parity-specific unavailability | Лебедева И.П.: Friday pairs 5–7, `odd` weeks only |
| `undesired` slots | Иванов И.П., Волкова А.Д.: first pair every day; Соловьёв И.А.: Saturday |
| `preferred` slots | Морозов А.В.: Tuesday and Thursday, pairs 2–3 |
| Room closed for a whole day | А-206: `unavailable` all Wednesday |
| Room `undesired` slots | Спортзал: Saturday pairs 6–7 |
| Lab-only rooms and a single gym | `lab` rooms Б-201..203, В-110, В-112; `gym` Спортзал |
| Tight capacities | the largest 4-group streams (up to 107 students) fit only А-101/А-201; the Б-203 lab (14 seats) is too small for some lab subgroups |

## Feasibility

`internal/seed/dataset_test.go` checks the in-memory dataset. These are necessary conditions, so a
solver has a chance:

- every group has ≤ 24 pairs per week. Whole-group load is added to the busiest part of each
  division, because parts of one division run in parallel and different divisions don't;
- every teacher has ≤ 20 pairs per week and ≤ 80 % of their open slots;
- per room type, lessons that need at least *c* seats take ≤ 80 % of the open slots of rooms
  with at least *c* seats (a Hall-style condition that respects closed rooms);
- referential integrity, subgroup sizes adding up to the group size, and determinism.

`internal/seed/load_test.go` (needs `TEST_DATABASE_URL`) loads the dataset twice and checks that
the row counts and IDs match the dataset. It also checks that old schedules are removed and that
each run writes one audit row.
