.PHONY: help build test test-coverage lint clean clean-data run dev templ-generate fmt tidy deps install-tools dev-assets deploy deploy-build deploy-restart

# Variables
APP_NAME := gearbox
VERSION ?= dev
COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitSHA=$(COMMIT_SHA) -X main.BuildDate=$(BUILD_DATE)"

# Deploy configuration — overridable on the command line:
#   make deploy MJOLNIR_HOST=otherbox@10.0.0.99
MJOLNIR_HOST ?= dave@10.0.0.1
MJOLNIR_HOMELAB_PATH ?= /mnt/StormEdge/apps/homelab
# Match the tag the homelab compose pulls so docker compose up -d picks
# the freshly-loaded image without any compose-file edits.
DEPLOY_IMAGE_TAG ?= ghcr.io/sarg3nt/gearbox/gearbox:dev

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

# `--build.cmd` overrides whatever the developer's local .air.toml uses
# (the file is gitignored), so `-tags dev` is always passed regardless of
# per-developer config drift. The tag compiles in the loopback auto-login
# bypass — see gearbox/docs/development.md and issue #83. `make build`
# (production) deliberately omits the tag.
DEV_BUILD_CMD = templ generate && CGO_ENABLED=1 go build -tags dev -o ./tmp/main ./cmd/server

dev: templ-generate ## Run with hot reload + .env loaded + dev auto-login bypass (requires air)
	@if [ -f .env ]; then \
		lsof -ti:3000 | xargs -r kill -9 2>/dev/null || true; \
		set -a && . ./.env && set +a && air --build.cmd "$(DEV_BUILD_CMD)"; \
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
	@echo "  → D3.js..."
	@curl -fsSL https://d3js.org/d3.v7.min.js -o static/js/vendor/d3.v7.min.js
	@echo "  → SortableJS..."
	@curl -fsSL https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/Sortable.min.js -o static/js/vendor/sortable.min.js
	@echo "  → Tabulator CSS..."
	@curl -fsSL https://unpkg.com/tabulator-tables@6.3.0/dist/css/tabulator.min.css -o static/css/vendor/tabulator.min.css
	@echo "  → Tabulator JS..."
	@curl -fsSL https://unpkg.com/tabulator-tables@6.3.0/dist/js/tabulator.min.js -o static/js/vendor/tabulator.min.js
	@echo "✓ All development assets downloaded successfully"
	@echo ""
	@echo "To use local assets (CSP-compliant), set in your .env:"
	@echo "  USE_LOCAL_ASSETS=true"

# ---------------------------------------------------------------------------
# Deployment — push the local working copy to mjolnir as a Docker image,
# bypassing the GitHub Actions registry round-trip. Useful for fast feature-
# branch testing.
#
# Build target is linux/amd64 (mjolnir runs TrueNAS SCALE on x86_64).
# Build runs through Orbstack on the dev box; no buildx setup required.
# The image is loaded into mjolnir's Docker daemon directly via SSH, then
# the gearbox compose stack is recreated. ~30s round trip when the layer
# cache is warm; no public push of WIP code.
#
# Override on the command line: `make deploy MJOLNIR_HOST=user@host`
# ---------------------------------------------------------------------------

deploy: deploy-build deploy-restart ## Build linux/amd64 image and deploy to mjolnir (build + ship + restart)
	@echo "✓ Deployed $(APP_NAME) ($(VERSION) / $(COMMIT_SHA)) to $(MJOLNIR_HOST)"
	@echo "  Tail logs with:  ssh $(MJOLNIR_HOST) 'sudo docker logs -f gearbox'"

deploy-build: templ-generate ## Build the linux/amd64 image and stream it to mjolnir
	@echo "Building $(DEPLOY_IMAGE_TAG) for linux/amd64..."
	@docker build \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_SHA=$(COMMIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--tag $(DEPLOY_IMAGE_TAG) \
		.
	@echo "Streaming image to $(MJOLNIR_HOST) (gzipped over SSH)..."
	@docker save $(DEPLOY_IMAGE_TAG) | gzip | ssh $(MJOLNIR_HOST) 'gunzip | sudo docker load'

deploy-restart: ## Recreate the gearbox container on mjolnir using the homelab compose file
	@echo "Recreating gearbox container on $(MJOLNIR_HOST)..."
	@ssh $(MJOLNIR_HOST) 'sudo docker compose -p gearbox -f $(MJOLNIR_HOMELAB_PATH)/apps/gearbox/docker-compose.yml up -d'
