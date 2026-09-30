.PHONY: lint test build dev up down \
	backend-lint backend-test backend-build \
	frontend-lint frontend-test frontend-build sqlc seed

BACKEND_DIR  := backend
FRONTEND_DIR := frontend
# Database for `make seed`; defaults to the compose database (deploy/.env.example).
DATABASE_URL ?= postgres://timetable:timetable@localhost:5432/timetable?sslmode=disable

lint: backend-lint frontend-lint
test: backend-test frontend-test
build: backend-build frontend-build

backend-lint:
	cd $(BACKEND_DIR) && golangci-lint run ./...

backend-test:
	cd $(BACKEND_DIR) && go test -race ./...

sqlc:
	cd $(BACKEND_DIR) && sqlc generate

backend-build:
	cd $(BACKEND_DIR) && go build -o bin/server ./cmd/server

frontend-lint:
	cd $(FRONTEND_DIR) && npm run lint && npm run typecheck

frontend-test:
	cd $(FRONTEND_DIR) && npm test

frontend-build:
	cd $(FRONTEND_DIR) && npm run build

# Runs the API and the Vite dev server (which proxies /api to the API).
dev:
	cd $(BACKEND_DIR) && go run ./cmd/server & \
	cd $(FRONTEND_DIR) && npm run dev; \
	kill %1

# Loads the demo dataset (docs/dev/seed.md). Replaces reference data, curriculum and schedules.
seed:
	cd $(BACKEND_DIR) && DATABASE_URL='$(DATABASE_URL)' go run ./cmd/seed

up:
	docker compose -f deploy/docker-compose.yml up -d --build

down:
	docker compose -f deploy/docker-compose.yml down
