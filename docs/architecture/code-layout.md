# Code layout

```
.
├── backend/                 Go module github.com/istu-pro-dev/timetable/backend
│   ├── cmd/server/          HTTP API entrypoint (single binary: API + solver workers)
│   └── internal/
│       ├── domain/          value types: time grid, slots, week parity, audiences
│       ├── engine/          in-memory schedule model, H1–H8 checker, soft penalties,
│       │                    EvaluateMove / CommitMove
│       ├── solver/          conflict graph, DSatur + backtracking (stage A),
│       │                    SA / Tabu, multi-start orchestrator (stage B)
│       ├── store/           PostgreSQL persistence (pgx + sqlc), migrations
│       ├── snapshot/        .rasp format, ScheduleExporter / ScheduleImporter
│       ├── api/             REST + WebSocket handlers
│       ├── auth/            sessions, JWT, RBAC
│       └── mcp/             MCP server for the ai_agent principal
├── frontend/                Vite + React + TypeScript (strict) SPA
├── deploy/                  docker-compose, Caddyfile
├── docs/                    design documents
├── go.work                  Go workspace (lets tools resolve the backend module from the repo root)
└── Makefile                 lint / test / build / dev / up / down
```

## Dependency direction

`api`, `mcp` → `engine`, `solver`, `snapshot`, `store`, `auth` → `domain`.

`engine` has no dependency on `store`: the store loads data and builds the engine model, so the
constraint logic stays pure and fully unit-testable (arch §19).

## Local development

| Command | Purpose |
|---|---|
| `make dev` | API on `:8080` + Vite dev server; `/api` is proxied to the API |
| `make lint test build` | same checks as CI |
| `make up` / `make down` | full stack in docker compose |

Health check: `curl localhost:8080/api/healthz` → `{"status":"ok"}`.
