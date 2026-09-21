.PHONY: help build test lint docker-build docker-push dev clean

# Default image tag
IMAGE_NAME ?= linux-environment-manager
IMAGE_TAG  ?= latest

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Local development
# ---------------------------------------------------------------------------

build: build-server build-agent build-web ## Build all components

build-server: ## Build server binary
	cd server && go build -o ./server ./cmd/server

build-agent: ## Build agent binary
	cd agent && go build -o ./agent ./cmd/agent

build-web: ## Build web dashboard
	cd web && npm ci && npm run build

dev: ## Start server in development mode
	docker compose up --build

# ---------------------------------------------------------------------------
# Testing
# ---------------------------------------------------------------------------

test: test-server test-agent test-web ## Run all tests

test-server: ## Run server tests
	cd server && go test ./...

test-agent: ## Run agent tests
	cd agent && go test ./...

test-web: ## Run web type-check
	cd web && npm run check

# ---------------------------------------------------------------------------
# Linting
# ---------------------------------------------------------------------------

lint: lint-go lint-web ## Run all linters

lint-go: ## Run Go linter
	cd server && golangci-lint run ./...
	cd agent && golangci-lint run ./...

lint-web: ## Run web linter
	cd web && npx svelte-check --tsconfig ./tsconfig.app.json && npx tsc -p tsconfig.node.json

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------

docker-build: ## Build Docker image
	docker build -f docker/Dockerfile.server -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-push: ## Push Docker image to Docker Hub
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

docker-up: ## Start with Docker Compose (dev)
	docker compose up -d

docker-down: ## Stop Docker Compose
	docker compose down

docker-logs: ## Follow container logs
	docker compose logs -f server

# ---------------------------------------------------------------------------
# Production
# ---------------------------------------------------------------------------

prod-build: ## Build production Docker image
	docker build -f docker/Dockerfile.server -t $(IMAGE_NAME):$(IMAGE_TAG) .

prod-up: ## Start production stack
	docker compose -f compose.prod.yaml up -d

prod-down: ## Stop production stack
	docker compose -f compose.prod.yaml down

prod-logs: ## Follow production logs
	docker compose -f compose.prod.yaml logs -f server

# ---------------------------------------------------------------------------
# Utilities
# ---------------------------------------------------------------------------

clean: ## Remove build artifacts
	cd server && rm -f server
	cd agent && rm -f agent
	rm -rf web/dist
	rm -rf data/
