.PHONY: help build test lint clean run dev docker-build docker-run docker-build-dev templ-generate

# Variables
APP_NAME := gearbox
DOCKER_REGISTRY := ghcr.io/sarg3nt/ubuntu-ha-proxy-install
VERSION ?= dev
COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitSHA=$(COMMIT_SHA) -X main.BuildDate=$(BUILD_DATE)"

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

templ-generate: ## Generate Go code from Templ templates
	@echo "Generating Templ templates..."
	@templ generate

build: templ-generate ## Build the application binary
	@echo "Building $(APP_NAME)..."
	@CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(APP_NAME) ./cmd/server

test: ## Run tests
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests and show coverage
	@go tool cover -html=coverage.out

lint: ## Run linter
	@echo "Running linter..."
	@golangci-lint run

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/ coverage.out
	@find . -name "*_templ.go" -delete

run: templ-generate ## Run the application locally
	@echo "Running $(APP_NAME)..."
	@go run ./cmd/server

run-local: templ-generate ## Run locally with .env file loaded
	@echo "Running $(APP_NAME) with .env..."
	@if [ -f .env ]; then \
		set -a && . ./.env && set +a && go run ./cmd/server; \
	else \
		echo "Error: .env file not found. Copy .env.example to .env and configure it."; \
		exit 1; \
	fi

dev: ## Run with hot reload (requires air)
	@air

dev-local: ## Run with hot reload and .env file loaded (requires air)
	@if [ -f .env ]; then \
		lsof -ti:3000 | xargs -r kill -9 2>/dev/null || true; \
		set -a && . ./.env && set +a && air; \
	else \
		echo "Error: .env file not found. Copy .env.example to .env and configure it."; \
		exit 1; \
	fi

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(APP_NAME):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_SHA=$(COMMIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		.

docker-run: ## Run Docker container
	@docker run --rm -p 3000:3000 \
		-e HAPROXY_SERVERS='[{"id":"test","name":"Test Server","stats_url":"http://localhost:8404/stats","ssh_host":"localhost:22"}]' \
		-e AUTH_USERS="admin:admin" \
		-e SESSION_SECRET="dev-secret-change-in-production-min-64-chars-long-please" \
		$(APP_NAME):$(VERSION)

docker-build-dev: ## Build amd64 Docker image, tag as :dev, and push to GHCR
	@echo "Building $(DOCKER_REGISTRY)/$(APP_NAME):dev for linux/amd64..."
	@docker buildx build --platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_SHA=$(COMMIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_REGISTRY)/$(APP_NAME):dev \
		--load \
		.
	@echo "Pushing $(DOCKER_REGISTRY)/$(APP_NAME):dev..."
	@docker push $(DOCKER_REGISTRY)/$(APP_NAME):dev
	@echo "Triggering Portainer webhook to update deployment..."
	@curl -X POST "https://portainer.sarg3.net/api/stacks/webhooks/11d98e5a-4ab8-4749-b77c-b965ffc5e419" || echo "Failed to trigger Portainer webhook"
	@echo "Done."

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...
	@gofmt -s -w .

tidy: ## Tidy dependencies
	@echo "Tidying dependencies..."
	@go mod tidy

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download

install-tools: ## Install development tools
	@echo "Installing development tools..."
	@go install github.com/a-h/templ/cmd/templ@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/air-verse/air@latest
