.PHONY: up down test lint build migrate seed fmt clean

COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_DEV_FILE := deploy/docker-compose.dev.yml

up:
	docker compose -f $(COMPOSE_FILE) --env-file .env up -d

down:
	docker compose -f $(COMPOSE_FILE) down

up-dev:
	docker compose -f $(COMPOSE_FILE) -f $(COMPOSE_DEV_FILE) --env-file .env up

test:
	cd cmd/server && go test ./...
	cd internal && go test ./...
	cd py && uv run pytest
	cd web && pnpm test

lint:
	cd cmd/server && go vet ./...
	cd internal && go vet ./...
	cd pkg && go vet ./...
	cd py && uv run ruff check .
	cd web && pnpm lint

build:
	go build -o bin/server ./cmd/server
	cd web && pnpm build

migrate:
	cd py && uv run alembic upgrade head

migrate-new:
	cd py && uv run alembic revision --autogenerate -m "$(name)"

seed:
	cd py && uv run python -m oap.scripts.seed

fmt:
	go fmt ./...
	cd py && uv run ruff format .
	cd web && pnpm exec prettier --write .

clean:
	docker compose -f $(COMPOSE_FILE) down -v
	rm -rf bin/ web/dist/ web/node_modules/ py/.venv/
