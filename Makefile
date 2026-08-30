.PHONY: up up-dev down test lint build migrate migrate-status migrate-new seed fmt clean build-agent build-agent-all setup setup-dev reset logs help

COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_DEV_FILE := deploy/docker-compose.dev.yml

help:
	@echo "OpenAgentPlatform - Common targets:"
	@echo "  make setup         - First-time setup: copy .env, install deps, start stack"
	@echo "  make up            - Start the production stack in background"
	@echo "  make up-dev        - Start with hot reload for development"
	@echo "  make down          - Stop the stack"
	@echo "  make logs          - Tail logs from all services"
	@echo "  make migrate       - Apply schema migrations (embedded set; server also auto-migrates at boot)"
	@echo "  make migrate-status- Show current vs pending schema versions"
	@echo "  make migrate-new name=add_foo - Scaffold the next migration pair"
	@echo "  make seed          - Load sample data"
	@echo "  make reset         - Destroy volumes and start fresh"
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters"
	@echo "  make build         - Build server and web"
	@echo "  make build-agent   - Build the endpoint agent"
	@echo "  make clean         - Remove build artifacts and volumes"

setup:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env from .env.example"; fi
	@# Generate the mTLS certificates the NATS server requires (nats.conf
	@# references deploy/nats/certs/{server-cert,server-key,ca}.pem). Without
	@# this, NATS exits on boot with "file not found" and the whole stack's
	@# depends_on: nats condition: service_healthy never resolves.
	@if [ ! -f deploy/nats/certs/server-cert.pem ]; then \
		echo "Generating NATS mTLS certificates..."; \
		bash deploy/nats/scripts/gen-certs.sh; \
	fi
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@# No migrate step: the server applies the embedded schema at boot
	@# (OAP_AUTO_MIGRATE=true, and the stack's compose file sets it).
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
	@# Applies pending schema migrations from the canonical embedded set
	@# (internal/db/migrations). The server does this itself at boot
	@# (OAP_AUTO_MIGRATE=true); this target is for OAP_AUTO_MIGRATE=false
	@# setups, CI, and humans checking ledger state.
	go run ./cmd/migrate status
	go run ./cmd/migrate up

migrate-status:
	go run ./cmd/migrate status

migrate-new:
	@./scripts/new-migration.sh "$(name)"

seed:
	@# Sample data: the built-in check library is seeded automatically when the
	@# server boots (internal/checklib.Seed, called from cmd/server/main.go).
	@# There is no separate Python seeder module (py/oap/scripts/seed does not
	@# exist), so this target is a documented no-op rather than a failing
	@# `python -m oap.scripts.seed`. Add a dedicated seeder here if you extend
	@# the sample dataset (sites/agents/alert rules).
	@echo "Seed: built-in check library is seeded on server boot (checklib.Seed)."

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
