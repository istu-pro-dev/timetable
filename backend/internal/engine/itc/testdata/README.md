# ITC-2007 Track 3 test data

Used by `itc_test.go` to cross-check the hard-constraint checker on public benchmark data
(arch §19).

## Instances — `instances/comp01.ctt` … `comp21.ctt`

The 21 official instances of the Second International Timetabling Competition (ITC-2007),
Track 3 "Curriculum-based Course Timetabling", by L. Di Gaspero, B. McCollum and A. Schaerf
(University of Udine / Queen's University Belfast). Official pages:

- https://www.eeecs.qub.ac.uk/itc2007/curriculmcourse/course_curriculm_index.htm
- https://opthub.uniud.it (CB-CTT benchmark site)

The files were taken byte-for-byte from the copy in
https://github.com/timobertram/ERM-ITC/tree/main/data/ITC2007/real.
They are freely distributed benchmark data; cite the competition report when publishing results:
Di Gaspero, McCollum, Schaerf, "The Second International Timetabling Competition (ITC-2007):
Curriculum-based Course Timetabling (Track 3)", Technical Report QUB/IEEE/Tech/ITC2007/CurriculumCTT/v1.0, 2007.

## Solutions — `solutions/<source>/compNN.sol`

One line `course room day period` per lecture (0-based day and period), the standard ITC-2007
solution format.

| Directory | Instances | Source | Produced by |
|---|---|---|---|
| `erm/` | comp01–comp21 | https://github.com/timobertram/ERM-ITC/tree/main/data/ITC2007/real (`compNN_solution.json`, status FEASIBLE/OPTIMAL) | OR-Tools CP-SAT model of that repository; converted from JSON with `jq` (`assignment` → `course room day period_in_day`) |
| `docheinstein/` | comp01–comp07 | https://github.com/Docheinstein/itc2007-cct/tree/master/results/races (`compNN.ctt.sol`) | the repository's local-search solver, copied unchanged |

Neither repository states a license; the solutions are machine-generated assignments kept here
only as test fixtures, with attribution. The tests do not trust them blindly: every solution is
first validated by an independent from-scratch checker in `itc_test.go` (lecture counts, room
occupancy, teacher and curriculum conflicts, course unavailability), and only then compared with
the engine.
