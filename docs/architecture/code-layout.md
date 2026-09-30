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

### Authentication

Local accounts (arch §16.3 fallback login) live in `users`; passwords are hashed with argon2id.
`POST /api/auth/login` returns a short-lived HS256 access JWT (15 min) and starts a refresh
session (30 days, stored hashed in `sessions`). Both are set as `HttpOnly`, `SameSite=Lax` cookies
(`tt_access`, `tt_refresh` scoped to `/api/auth`); API clients may send the access token as
`Authorization: Bearer`. `POST /api/auth/refresh` rotates the refresh token, `POST /api/auth/logout`
revokes it, `GET /api/auth/me` returns the current account. Protected routes answer 401 without a
valid token and 403 for a wrong role (`auth.Require`). Failed logins are throttled in memory per
login and per client IP with exponential backoff (429 + `Retry-After`).

| Variable | Meaning |
|---|---|
| `APP_ENV` | `dev` allows a built-in insecure `JWT_SECRET` and defaults cookies to non-`Secure` (`make dev` sets it) |
| `JWT_SECRET` | HS256 key, ≥ 32 bytes; the server refuses to start without it unless `APP_ENV=dev` |
| `COOKIE_SECURE` | `Secure` cookie attribute; default `true`, `false` in dev |
| `TRUST_PROXY` | take the client IP from the last `X-Forwarded-For` hop (behind Caddy) |
| `ADMIN_LOGIN`, `ADMIN_PASSWORD` | create the first admin on startup if no active admin exists (idempotent; a warning is logged when unset) |

An access token stays valid until it expires even after logout or disabling the account; refresh
sessions are revoked immediately.

### REST API

The contract is the hand-written OpenAPI 3.1 spec `backend/api/openapi.yaml`, served at
`GET /api/openapi.yaml`; a test fails when a registered route is missing from the spec or the spec
lists a route that does not exist. Reference data (buildings, room types, rooms + availability,
groups + subgroups, teachers + availability, disciplines, time grid, periods, curriculum items) has
list/get/create/update/delete endpoints under `/api/`. Reads need any authenticated user, changes
need `admin`; every change goes through `Store.WithAudit` with the user as the actor.

Errors are `{"error":{"code":"...","message":"..."}}`: malformed JSON or unknown fields → 400,
invalid values → 422 `validation_failed`, missing record → 404, unique key → 409 `already_exists`,
deleting a referenced record → 409 `in_use`, reference to a missing record → 422 `invalid_reference`.

Creating a curriculum item generates its lessons in the same transaction (`weekly_count` weekly
lessons, then `biweekly_count` biweekly ones, `seq` 1..N). Updating an item regenerates the lessons
only when a count changes; the old lessons' assignments are deleted with them (`ON DELETE CASCADE`),
so the item must be placed again in existing schedules.

### Frontend

`frontend/src`: `api/` (typed fetch client + TanStack Query hooks), `auth/` (session provider,
route guards), `components/` (UI kit on Tailwind v4 with light/dark tokens in `index.css`),
`i18n/` (typed RU dictionary, `t('nav.home')`), `layout/` (shell with role-aware sidebar),
`pages/` (lazy-loaded routes, `router.tsx`).

- API types are generated from the spec: `npm run gen:api` writes `src/api/schema.ts`
  (openapi-typescript; `src/api/types.ts` gives the schemas short names). A test fails when the
  committed file is stale, so **a change to `backend/api/openapi.yaml` needs `cd frontend && npm run gen:api`**.
- Session: on start the app calls `GET /api/auth/me`; any 401 triggers one `POST /api/auth/refresh`
  (shared by concurrent requests) and a retry, then a redirect to `/login`.
- Dev: `API_URL=http://localhost:8081 npm run dev` proxies `/api` to another API port.

### Docker compose stack

| Service | Image / build | Port (host) |
|---|---|---|
| `postgres` | `postgres:17-alpine`, volume `pgdata`, `pg_isready` healthcheck | `127.0.0.1:${POSTGRES_PORT:-5432}` |
| `api` | `backend/Dockerfile` (context `backend/`, distroless static); gets `DATABASE_URL`, starts after `postgres` is healthy | internal `:8080` |
| `web` | `frontend/Dockerfile` (context repo root: Vite build → Caddy with `deploy/Caddyfile`); serves the SPA, proxies `/api/*` (incl. WebSocket) to `api` | `${WEB_PORT:-80}` |

Configuration: `cp deploy/.env.example deploy/.env` (optional — every variable has a default).
Check: `curl localhost/api/healthz` (through Caddy). If ports 80/5432 are taken, set `WEB_PORT` /
`POSTGRES_PORT`. `docker compose -f deploy/docker-compose.yml down -v` also drops the database volume.
