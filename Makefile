# =============================================================================
# SyncWin
# =============================================================================
#   make dev    full development stack with containers (server hot-reload + web HMR)
#   make prod   production stack with containers (built image, persistent data)
#   make help   list every target
# =============================================================================

SHELL := /bin/bash
.DEFAULT_GOAL := help
ROOT := $(CURDIR)

# Prefer the v2 compose plugin, fall back to the standalone binary.
COMPOSE ?= $(shell if docker compose version >/dev/null 2>&1; then echo "docker compose"; else echo "docker-compose"; fi)

DEV_FILE   := compose.dev.yaml
PROD_FILE  := compose.prod.yaml
LOCAL_FILE := compose.yaml

# Development uses 8088; production keeps SYNCWIN_HTTP_PORT=8080.
DEV_HTTP_PORT ?= 8088
export DEV_HTTP_PORT

DEV   := $(COMPOSE) -f $(DEV_FILE)
PROD  := $(COMPOSE) -f $(PROD_FILE)
LOCAL := $(COMPOSE) -f $(LOCAL_FILE)

# Local demo account for manual testing. There is no default/seeded login in
# the product: create one with `make dev-account` (dev stack must be running).
DEMO_EMAIL    ?= demo@sync-win.local
DEMO_PASSWORD ?= sync-win-demo-password

# Optional Ed25519 public key embedded into the agent to authenticate updates.
AGENT_LDFLAGS := $(if $(AGENT_UPDATE_PUBLIC_KEY),-ldflags "-X main.agentUpdatePublicKey=$(AGENT_UPDATE_PUBLIC_KEY)")

# Keep the agent version in sync with the collection contract.
AGENT_VERSION := $(shell jq -r '.agent_version // empty' agent/internal/contract/contract.json 2>/dev/null)
export SYNCWIN_AGENT_VERSION ?= $(if $(AGENT_VERSION),$(AGENT_VERSION),0.6.1)

C := \033[36m
G := \033[32m
Y := \033[33m
N := \033[0m

.PHONY: help
help: ## Show this help
	@printf "$(C)SyncWin$(N)\n\n"
	@printf "Usage: $(G)make <target>$(N)\n\n"
	@printf "$(C)Environments$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "dev"        "Start the full dev stack (server hot-reload + web HMR)"
	@printf "  $(G)%-16s$(N) %s\n" "prod"       "Build and start the production stack (detached)"
	@printf "  $(G)%-16s$(N) %s\n" "down"       "Stop the dev stack (alias: dev-down)"
	@printf "\n$(C)Containers$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "dev-d"      "Start dev stack detached"
	@printf "  $(G)%-16s$(N) %s\n" "dev-down"   "Stop and remove the dev stack"
	@printf "  $(G)%-16s$(N) %s\n" "dev-logs"   "Follow dev stack logs"
	@printf "  $(G)%-16s$(N) %s\n" "dev-ps"     "Show dev stack status"
	@printf "  $(G)%-16s$(N) %s\n" "dev-shell"  "Open a shell in the dev server container"
	@printf "  $(G)%-16s$(N) %s\n" "prod-down"  "Stop and remove the production stack"
	@printf "  $(G)%-16s$(N) %s\n" "prod-logs"  "Follow production logs"
	@printf "  $(G)%-16s$(N) %s\n" "prod-ps"    "Show production stack status"
	@printf "\n$(C)Local (no containers)$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "run-server" "Run the Go server locally (go run)"
	@printf "  $(G)%-16s$(N) %s\n" "run-web"    "Run the Vite dev server locally"
	@printf "  $(G)%-16s$(N) %s\n" "run-agent"  "Run the agent daemon locally"
	@printf "\n$(C)Build$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "build"      "Build server, agent and web dashboard"
	@printf "  $(G)%-16s$(N) %s\n" "build-server" "Build the server binary"
	@printf "  $(G)%-16s$(N) %s\n" "build-agent"  "Build the agent binary"
	@printf "  $(G)%-16s$(N) %s\n" "agent-keygen" "Generate an Ed25519 keypair for signed agent updates"
	@printf "  $(G)%-16s$(N) %s\n" "build-web"    "Build the web dashboard (dist/)"
	@printf "  $(G)%-16s$(N) %s\n" "docker-build" "Build the production Docker image"
	@printf "\n$(C)Quality$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "test"       "Run server, agent and web tests"
	@printf "  $(G)%-16s$(N) %s\n" "test-race"  "Run Go tests with the race detector"
	@printf "  $(G)%-16s$(N) %s\n" "lint"       "Run Go and web linters"
	@printf "  $(G)%-16s$(N) %s\n" "fmt"        "Format Go sources"
	@printf "\n$(C)Utilities$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "env"        "Create .env from .env.example with a generated secret"
	@printf "  $(G)%-16s$(N) %s\n" "secret"     "Print a fresh SYNCWIN_SECRET_KEY"
	@printf "  $(G)%-16s$(N) %s\n" "dev-account" "Create a local demo account (no default login exists)"
	@printf "  $(G)%-16s$(N) %s\n" "clean"      "Remove build artifacts"
	@printf "  $(G)%-16s$(N) %s\n" "clean-dev"  "Remove dev data and volumes"
	@printf "\nAgent version: $(G)$(SYNCWIN_AGENT_VERSION)$(N)\n"

# -----------------------------------------------------------------------------
# Environments
# -----------------------------------------------------------------------------

.PHONY: dev
dev: ## Start the full development stack (foreground, rebuilt)
	@mkdir -p data-dev
	$(DEV) up --build

.PHONY: dev-d
dev-d: ## Start the full development stack detached
	@mkdir -p data-dev
	$(DEV) up --build -d
	@printf "$(G)Dev stack up:$(N) dashboard http://localhost:%s · api http://localhost:%s\n" "$${WEB_PORT:-5173}" "$${DEV_HTTP_PORT:-8088}"

.PHONY: dev-down
dev-down: ## Stop and remove the dev stack
	$(DEV) down

.PHONY: dev-logs
dev-logs: ## Follow dev stack logs
	$(DEV) logs -f

.PHONY: dev-ps
dev-ps: ## Show dev stack status
	$(DEV) ps

.PHONY: dev-shell
dev-shell: ## Open a shell in the dev server container
	$(DEV) exec server sh

.PHONY: prod
prod: ## Build and start the production stack (detached)
	@test -f .env || { printf "$(Y).env not found. Run 'make env' first and set SYNCWIN_SECRET_KEY.$(N)\n"; exit 1; }
	@$(PROD) config >/dev/null 2>&1 || { printf "$(Y)Set SYNCWIN_SECRET_KEY in .env (run 'make secret').$(N)\n"; exit 1; }
	@mkdir -p data
	$(PROD) up -d --build
	@printf "$(G)Production stack up:$(N) http://localhost:%s\n" "$${SYNCWIN_HTTP_PORT:-8080}"

.PHONY: prod-down
prod-down: ## Stop and remove the production stack
	$(PROD) down

.PHONY: prod-logs
prod-logs: ## Follow production logs
	$(PROD) logs -f

.PHONY: prod-ps
prod-ps: ## Show production stack status
	$(PROD) ps

.PHONY: up
up: dev ## Alias for dev

.PHONY: down
down: dev-down ## Alias for dev-down

# -----------------------------------------------------------------------------
# Local development (no containers)
# -----------------------------------------------------------------------------

.PHONY: run-server
run-server: ## Run the Go server locally
	cd server && SYNCWIN_ENV=development SYNCWIN_DATA_DIR=../data-dev SYNCWIN_HTTP_ADDR=:8080 go run ./cmd/server

.PHONY: run-web
run-web: ## Run the Vite dev server locally
	cd web && npm run dev

.PHONY: run-agent
run-agent: ## Run the agent daemon locally (requires enrollment credentials)
	cd agent && go run ./cmd/agent daemon

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------

.PHONY: build
build: build-server build-agent build-web ## Build server, agent and web dashboard

.PHONY: build-server
build-server: ## Build the server binary
	cd server && go build -o ./server ./cmd/server

.PHONY: build-agent
build-agent: ## Build the agent binary (set AGENT_UPDATE_PUBLIC_KEY to embed the update signing key)
	cd agent && go build $(AGENT_LDFLAGS) -o ./agent ./cmd/agent

.PHONY: agent-keygen
agent-keygen: ## Generate an Ed25519 keypair for signed agent updates
	cd server && go run ./cmd/agentsign -genkey

.PHONY: build-web
build-web: ## Build the web dashboard
	cd web && npm ci && npm run build

.PHONY: docker-build
docker-build: ## Build the production Docker image
	docker build -f docker/Dockerfile.server -t $${IMAGE_NAME:-sync-win}:$${IMAGE_TAG:-latest} .

# -----------------------------------------------------------------------------
# Quality
# -----------------------------------------------------------------------------

.PHONY: test
test: test-server test-agent test-web ## Run all tests

.PHONY: test-server
test-server: ## Run server tests
	cd server && go test ./...

.PHONY: test-agent
test-agent: ## Run agent tests
	cd agent && go test ./...

.PHONY: test-web
test-web: ## Run web unit tests and type-check
	cd web && npm run test && npm run check

.PHONY: test-race
test-race: ## Run Go tests for server and agent with the race detector
	cd server && go test -race -count=1 ./...
	cd agent && go test -race -count=1 ./...

.PHONY: lint
lint: ## Run Go and web linters
	cd server && go vet ./...
	cd agent && go vet ./...
	cd web && npm run check

.PHONY: fmt
fmt: ## Format Go sources
	cd server && gofmt -w .
	cd agent && gofmt -w .

# -----------------------------------------------------------------------------
# Utilities
# -----------------------------------------------------------------------------

.PHONY: env
env: ## Create .env from .env.example with a generated secret
	@if [ -f .env ]; then printf "$(Y).env already exists — leaving it untouched.$(N)\n"; else \
		cp .env.example .env; \
		secret=$$(openssl rand -hex 32); \
		sed -i "s|^SYNCWIN_SECRET_KEY=.*|SYNCWIN_SECRET_KEY=$$secret|" .env; \
		printf "$(G)Created .env with a generated SYNCWIN_SECRET_KEY.$(N)\n"; \
	fi

.PHONY: secret
secret: ## Print a fresh SYNCWIN_SECRET_KEY
	@openssl rand -hex 32

.PHONY: dev-account
dev-account: ## Create a local demo account on the running dev stack (there is no default login)
	@printf "$(C)Creating demo account $(DEMO_EMAIL) on http://localhost:%s$(N)\n" "$${DEV_HTTP_PORT:-8088}"
	@curl -fsS -X POST "http://localhost:$${DEV_HTTP_PORT:-8088}/api/auth/register" \
		-H 'Content-Type: application/json' \
		-d "{\"email\":\"$(DEMO_EMAIL)\",\"password\":\"$(DEMO_PASSWORD)\"}" > /dev/null \
		&& printf "$(G)OK$(N) — sign in with $(G)$(DEMO_EMAIL)$(N) / $(G)$(DEMO_PASSWORD)$(N)\n" \
		|| { printf "$(Y)Failed.$(N) Is the dev stack running ('make dev-d')? The account may already exist.\n"; exit 1; }

.PHONY: clean
clean: ## Remove build artifacts
	rm -f server/server agent/agent
	rm -rf web/dist

.PHONY: clean-dev
clean-dev: ## Remove dev data and volumes
	$(DEV) down -v --remove-orphans 2>/dev/null || true
	@if [ -d data-dev ]; then docker run --rm -v "$(ROOT)/data-dev:/d" alpine sh -c 'rm -rf /d/* /d/.[!.]*' 2>/dev/null || true; fi
	rm -rf data-dev 2>/dev/null || true
