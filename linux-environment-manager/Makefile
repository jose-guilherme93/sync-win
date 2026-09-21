# =============================================================================
# LEM — Linux Environment Manager
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

DEV   := $(COMPOSE) -f $(DEV_FILE)
PROD  := $(COMPOSE) -f $(PROD_FILE)
LOCAL := $(COMPOSE) -f $(LOCAL_FILE)

# Keep the agent version in sync with the collection contract.
AGENT_VERSION := $(shell jq -r '.agent_version // empty' agent/internal/contract/contract.json 2>/dev/null)
export LEM_AGENT_VERSION ?= $(if $(AGENT_VERSION),$(AGENT_VERSION),0.6.1)

C := \033[36m
G := \033[32m
Y := \033[33m
N := \033[0m

.PHONY: help
help: ## Show this help
	@printf "$(C)LEM — Linux Environment Manager$(N)\n\n"
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
	@printf "  $(G)%-16s$(N) %s\n" "build-web"    "Build the web dashboard (dist/)"
	@printf "  $(G)%-16s$(N) %s\n" "docker-build" "Build the production Docker image"
	@printf "\n$(C)Quality$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "test"       "Run server, agent and web tests"
	@printf "  $(G)%-16s$(N) %s\n" "lint"       "Run Go and web linters"
	@printf "  $(G)%-16s$(N) %s\n" "fmt"        "Format Go sources"
	@printf "\n$(C)Utilities$(N)\n"
	@printf "  $(G)%-16s$(N) %s\n" "env"        "Create .env from .env.example with a generated secret"
	@printf "  $(G)%-16s$(N) %s\n" "secret"     "Print a fresh LEM_SECRET_KEY"
	@printf "  $(G)%-16s$(N) %s\n" "clean"      "Remove build artifacts"
	@printf "  $(G)%-16s$(N) %s\n" "clean-dev"  "Remove dev data and volumes"
	@printf "\nAgent version: $(G)$(LEM_AGENT_VERSION)$(N)\n"

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
	@printf "$(G)Dev stack up:$(N) dashboard http://localhost:%s · api http://localhost:%s\n" "$${WEB_PORT:-5173}" "$${LEM_HTTP_PORT:-8080}"

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
	@test -f .env || { printf "$(Y).env not found. Run 'make env' first and set LEM_SECRET_KEY.$(N)\n"; exit 1; }
	@$(PROD) config >/dev/null 2>&1 || { printf "$(Y)Set LEM_SECRET_KEY in .env (run 'make secret').$(N)\n"; exit 1; }
	@mkdir -p data
	$(PROD) up -d --build
	@printf "$(G)Production stack up:$(N) http://localhost:%s\n" "$${LEM_HTTP_PORT:-8080}"

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
	cd server && LEM_ENV=development LEM_DATA_DIR=../data-dev LEM_HTTP_ADDR=:8080 go run ./cmd/server

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
build-agent: ## Build the agent binary
	cd agent && go build -o ./agent ./cmd/agent

.PHONY: build-web
build-web: ## Build the web dashboard
	cd web && npm ci && npm run build

.PHONY: docker-build
docker-build: ## Build the production Docker image
	docker build -f docker/Dockerfile.server -t $${IMAGE_NAME:-linux-environment-manager}:$${IMAGE_TAG:-latest} .

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
test-web: ## Run web type-check
	cd web && npm run check

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
		sed -i "s|^LEM_SECRET_KEY=.*|LEM_SECRET_KEY=$$secret|" .env; \
		printf "$(G)Created .env with a generated LEM_SECRET_KEY.$(N)\n"; \
	fi

.PHONY: secret
secret: ## Print a fresh LEM_SECRET_KEY
	@openssl rand -hex 32

.PHONY: clean
clean: ## Remove build artifacts
	rm -f server/server agent/agent
	rm -rf web/dist

.PHONY: clean-dev
clean-dev: ## Remove dev data and volumes
	$(DEV) down -v --remove-orphans 2>/dev/null || true
	@if [ -d data-dev ]; then docker run --rm -v "$(ROOT)/data-dev:/d" alpine sh -c 'rm -rf /d/* /d/.[!.]*' 2>/dev/null || true; fi
	rm -rf data-dev 2>/dev/null || true
