# Timetable

Self-hosted system for **automatic generation and editing of university timetables**.

The system builds a schedule that satisfies all hard constraints (no teacher / group / room double-booking, room type and capacity, availability), then keeps improving it against weighted soft criteria (gaps, building transitions, compactness, preferences, …) in the background. Administrators, teachers and an AI agent (via MCP) all edit the schedule through the same move-evaluation pipeline.

## Stack

| Layer | Technology |
|---|---|
| Solver | Go — DSatur + backtracking (feasibility), Simulated Annealing / Tabu Search (optimization), multi-start + island model + successive halving |
| API | Go, REST + WebSocket |
| Frontend | React + TypeScript |
| Storage | PostgreSQL (EXCLUDE constraints, transactional checkpoints) |
| Deployment | docker compose (local server / VPS) |

## Documentation

- [Problem research: constraints, requirements](docs/timetabling-problem-research.md) (RU)
- [Algorithms and architecture design](docs/timetabling-algorithms-architecture.md) (RU)

## Status

Early development. Planning and progress: [project board](https://github.com/orgs/istu-pro-dev/projects/1).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
