.PHONY: help build test test-coverage lint clean clean-data run dev templ-generate fmt tidy deps install-tools dev-assets

# Variables
APP_NAME := gearbox
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

clean-data: ## Clear development database (delete everything in data/ folder)
	@echo "WARNING: This will delete all data in the data/ folder!"
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		echo "Deleting data/ folder contents..."; \
		rm -rf data/*; \
		echo "✓ Data folder cleared. Database will be recreated on next startup."; \
	else \
		echo "Cancelled."; \
	fi

run: templ-generate ## Run the application with .env file loaded
	@echo "Running $(APP_NAME) with .env..."
	@if [ -f .env ]; then \
		set -a && . ./.env && set +a && go run ./cmd/server; \
	else \
		echo "Error: .env file not found. Copy .env.example to .env and configure it."; \
		exit 1; \
	fi

dev: templ-generate ## Run with hot reload and .env file loaded (requires air)
	@if [ -f .env ]; then \
		lsof -ti:3000 | xargs -r kill -9 2>/dev/null || true; \
		set -a && . ./.env && set +a && air; \
	else \
		echo "Error: .env file not found. Copy .env.example to .env and configure it."; \
		exit 1; \
	fi

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

dev-assets: ## Download CDN assets locally for secure development (CSP-compliant)
	@echo "Downloading CDN assets for local development..."
	@mkdir -p static/js/vendor static/css/vendor
	@echo "  → Tailwind CSS..."
	@curl -fsSL https://cdn.tailwindcss.com -o static/js/vendor/tailwind.js
	@echo "  → HTMX..."
	@curl -fsSL https://unpkg.com/htmx.org@1.9.10/dist/htmx.min.js -o static/js/vendor/htmx.min.js
	@echo "  → morphdom..."
	@curl -fsSL https://unpkg.com/morphdom@2.7.4/dist/morphdom-umd.min.js -o static/js/vendor/morphdom-umd.min.js
	@echo "  → Chart.js..."
	@curl -fsSL https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js -o static/js/vendor/chart.umd.min.js
	@echo "  → Hammer.js..."
	@curl -fsSL https://cdn.jsdelivr.net/npm/hammerjs@2.0.8/hammer.min.js -o static/js/vendor/hammer.min.js
	@echo "  → Chart.js Zoom Plugin..."
	@curl -fsSL https://cdn.jsdelivr.net/npm/chartjs-plugin-zoom@2.0.1/dist/chartjs-plugin-zoom.min.js -o static/js/vendor/chartjs-plugin-zoom.min.js
	@echo "  → Tabulator CSS..."
	@curl -fsSL https://unpkg.com/tabulator-tables@6.3.0/dist/css/tabulator.min.css -o static/css/vendor/tabulator.min.css
	@echo "  → Tabulator JS..."
	@curl -fsSL https://unpkg.com/tabulator-tables@6.3.0/dist/js/tabulator.min.js -o static/js/vendor/tabulator.min.js
	@echo "✓ All development assets downloaded successfully"
	@echo ""
	@echo "To use local assets (CSP-compliant), set in your .env:"
	@echo "  USE_LOCAL_ASSETS=true"
