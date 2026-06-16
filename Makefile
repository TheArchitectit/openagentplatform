.PHONY: up down test lint build migrate seed fmt clean build-agent build-agent-all setup setup-dev reset logs help

COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_DEV_FILE := deploy/docker-compose.dev.yml

help:
	@echo "OpenAgentPlatform - Common targets:"
	@echo "  make setup         - First-time setup: copy .env, install deps, start stack"
	@echo "  make up            - Start the production stack in background"
	@echo "  make up-dev        - Start with hot reload for development"
	@echo "  make down          - Stop the stack"
	@echo "  make logs          - Tail logs from all services"
	@echo "  make migrate       - Run database migrations"
	@echo "  make seed          - Load sample data"
	@echo "  make reset         - Destroy volumes and start fresh"
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters"
	@echo "  make build         - Build server and web"
	@echo "  make build-agent   - Build the endpoint agent"
	@echo "  make clean         - Remove build artifacts and volumes"

setup:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env from .env.example"; fi
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@$(MAKE) migrate
	@$(MAKE) seed
	@echo ""
	@echo "✅ Setup complete!"
	@echo "   Web UI:    http://localhost:5173"
	@echo "   Login:     [email protected] / password"
	@echo "   API:       http://localhost:8080"
	@echo "   Health:    curl http://localhost:8080/health"

setup-dev:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env from .env.example"; fi
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@$(MAKE) migrate
	@echo ""
	@echo "✅ Development stack ready!"
	@echo "   Start dev mode with: make up-dev"

up:
	docker compose -f $(COMPOSE_FILE) --env-file .env up -d

down:
	docker compose -f $(COMPOSE_FILE) down

up-dev:
	docker compose -f $(COMPOSE_FILE) -f $(COMPOSE_DEV_FILE) --env-file .env up

logs:
	docker compose -f $(COMPOSE_FILE) logs -f

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

build-agent:
	go build -o bin/oap-agent ./cmd/agent

build-agent-all:
	GOOS=linux   go build -o bin/oap-agent-linux   ./cmd/agent
	GOOS=darwin  go build -o bin/oap-agent-darwin  ./cmd/agent
	GOOS=windows go build -o bin/oap-agent-windows.exe ./cmd/agent

migrate:
	cd py && uv run alembic upgrade head

migrate-new:
	cd py && uv run alembic revision --autogenerate -m "$(name)"

seed:
	cd py && uv run python -m oap.scripts.seed

reset:
	@echo "⚠️  This will destroy all data. Press Ctrl+C to abort, or wait 5 seconds..."
	@sleep 5
	docker compose -f $(COMPOSE_FILE) down -v
	rm -rf bin/ web/dist/ web/node_modules/ py/.venv/
	@echo "✅ Reset complete. Run 'make setup' to start fresh."

fmt:
	go fmt ./...
	cd py && uv run ruff format .
	cd web && pnpm exec prettier --write .

clean:
	docker compose -f $(COMPOSE_FILE) down -v
	rm -rf bin/ web/dist/ web/node_modules/ py/.venv/
