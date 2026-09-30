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
| `make up` / `make down` | full stack in docker compose (`deploy/docker-compose.yml`) |

Health check: `curl localhost:8080/api/healthz` → `{"status":"ok"}`.

### Docker compose stack

| Service | Image / build | Port (host) |
|---|---|---|
| `postgres` | `postgres:17-alpine`, volume `pgdata`, `pg_isready` healthcheck | `127.0.0.1:${POSTGRES_PORT:-5432}` |
| `api` | `backend/Dockerfile` (context `backend/`, distroless static); gets `DATABASE_URL`, starts after `postgres` is healthy | internal `:8080` |
| `web` | `frontend/Dockerfile` (context repo root: Vite build → Caddy with `deploy/Caddyfile`); serves the SPA, proxies `/api/*` (incl. WebSocket) to `api` | `${WEB_PORT:-80}` |

Configuration: `cp deploy/.env.example deploy/.env` (optional — every variable has a default).
Check: `curl localhost/api/healthz` (through Caddy). If ports 80/5432 are taken, set `WEB_PORT` /
`POSTGRES_PORT`. `docker compose -f deploy/docker-compose.yml down -v` also drops the database volume.
