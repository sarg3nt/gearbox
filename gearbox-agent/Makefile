.PHONY: help build build-linux build-release test lint clean deploy run swagger swagger-install upload-binary

# Variables
APP_NAME := gearbox-agent
VERSION ?= dev
COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitSHA=$(COMMIT_SHA) -X main.BuildDate=$(BUILD_DATE)"

# Deployment settings
DEPLOY_HOST ?= 10.0.0.3
DEPLOY_USER ?= dave
DEPLOY_PATH := /usr/local/bin/$(APP_NAME)

# Web server for binary hosting (used during cloud-init provisioning)
WEB_SERVER_HOST ?= 10.0.0.1
WEB_SERVER_USER ?= dave
WEB_SERVER_PATH ?= /mnt/StormEdge/apps/web/data/light-hugger

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build for current platform
	@echo "Building $(APP_NAME)..."
	@go build $(LDFLAGS) -o bin/$(APP_NAME) ./cmd/gearbox-agent

build-linux: ## Build for Linux amd64 (deployment target)
	@echo "Building $(APP_NAME) for Linux amd64..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(APP_NAME)-linux-amd64 ./cmd/gearbox-agent

build-release: ## Build release tarball for Linux amd64 (includes service file)
	@echo "Building release tarball for $(APP_NAME) $(VERSION)..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(APP_NAME) ./cmd/gearbox-agent
	@mkdir -p bin/$(APP_NAME)_$(VERSION)_linux_amd64
	@cp bin/$(APP_NAME) bin/$(APP_NAME)_$(VERSION)_linux_amd64/
	@cp deploy/$(APP_NAME).service bin/$(APP_NAME)_$(VERSION)_linux_amd64/
	@cd bin && tar -czf $(APP_NAME)_$(VERSION)_linux_amd64.tar.gz $(APP_NAME)_$(VERSION)_linux_amd64
	@cd bin && sha256sum $(APP_NAME)_$(VERSION)_linux_amd64.tar.gz > checksums.txt
	@rm -rf bin/$(APP_NAME)_$(VERSION)_linux_amd64 bin/$(APP_NAME)
	@echo "Created: bin/$(APP_NAME)_$(VERSION)_linux_amd64.tar.gz"
	@echo "Checksums: bin/checksums.txt"

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

run: build ## Build and run locally (for development only - won't work on macOS for Linux syscalls)
	@echo "Running $(APP_NAME)..."
	@./bin/$(APP_NAME)

deploy: build-linux ## Build and deploy to server
	@echo "Deploying to $(DEPLOY_USER)@$(DEPLOY_HOST)..."
	@scp bin/$(APP_NAME)-linux-amd64 $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/$(APP_NAME)
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo /usr/bin/systemctl stop $(APP_NAME) && sudo /usr/bin/cp /tmp/$(APP_NAME) $(DEPLOY_PATH) && sudo /usr/bin/chmod +x $(DEPLOY_PATH) && sudo /usr/bin/systemctl start $(APP_NAME) && sudo /usr/bin/systemctl status $(APP_NAME)'
	@echo "Deployment complete."

deploy-rebrand: build-linux ## One-time rebrand deployment (haproxy-agent -> gearbox-agent)
	@echo "Rebrand deployment: haproxy-agent -> gearbox-agent..."
	@scp bin/$(APP_NAME)-linux-amd64 $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/$(APP_NAME)
	@scp deploy/$(APP_NAME).service $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/$(APP_NAME).service
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) '\
		echo "Installing new gearbox-agent binary..." && \
		sudo /usr/bin/cp /tmp/$(APP_NAME) $(DEPLOY_PATH) && \
		sudo /usr/bin/chmod +x $(DEPLOY_PATH) && \
		echo "Installing new gearbox-agent service..." && \
		sudo /usr/bin/cp /tmp/$(APP_NAME).service /etc/systemd/system/$(APP_NAME).service && \
		echo "Migrating data directory..." && \
		sudo mv /var/lib/haproxy-agent /var/lib/gearbox-agent 2>/dev/null || sudo mkdir -p /var/lib/gearbox-agent && \
		echo "Starting gearbox-agent service..." && \
		sudo /usr/bin/systemctl daemon-reload && \
		sudo /usr/bin/systemctl enable $(APP_NAME) && \
		sudo /usr/bin/systemctl start $(APP_NAME) && \
		sudo /usr/bin/systemctl status $(APP_NAME)'
	@echo "Rebrand deployment complete!"
	@echo "Old haproxy-agent service has been replaced with gearbox-agent"

deploy-first: build-linux ## First-time deployment (creates service file)
	@echo "First-time deployment to $(DEPLOY_USER)@$(DEPLOY_HOST)..."
	@scp bin/$(APP_NAME)-linux-amd64 $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/$(APP_NAME)
	@scp deploy/$(APP_NAME).service $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/$(APP_NAME).service
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) '\
		sudo /usr/bin/cp /tmp/$(APP_NAME) $(DEPLOY_PATH) && \
		sudo /usr/bin/chmod +x $(DEPLOY_PATH) && \
		sudo /usr/bin/cp /tmp/$(APP_NAME).service /etc/systemd/system/$(APP_NAME).service && \
		sudo mkdir -p /var/lib/gearbox-agent && \
		sudo /usr/bin/systemctl daemon-reload && \
		sudo systemctl enable $(APP_NAME) && \
		sudo /usr/bin/systemctl start $(APP_NAME) && \
		sudo /usr/bin/systemctl status $(APP_NAME)'
	@echo "First-time deployment complete."

show-api-key: ## Show the API key from the server
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo /usr/bin/cat /var/lib/gearbox-agent/api-key'

status: ## Show service status on server
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo /usr/bin/systemctl status $(APP_NAME)'

logs: ## Show recent logs from server
	@ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo /usr/bin/journalctl -u $(APP_NAME) -n 50'

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...

tidy: ## Tidy dependencies
	@echo "Tidying dependencies..."
	@go mod tidy

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download

swagger-install: ## Install swag CLI tool
	@echo "Installing swag CLI..."
	@go install github.com/swaggo/swag/cmd/swag@latest

swagger: ## Generate OpenAPI/Swagger documentation
	@echo "Generating Swagger documentation..."
	@swag init -g cmd/gearbox-agent/main.go -o docs --parseDependency --parseInternal
	@echo "Swagger docs generated in docs/"
	@echo "  - docs/swagger.json"
	@echo "  - docs/swagger.yaml"
	@echo "  - docs/docs.go"

upload-binary: build-linux ## Build and upload binary to web server for cloud-init provisioning
	@echo "Uploading $(APP_NAME) binary to $(WEB_SERVER_USER)@$(WEB_SERVER_HOST)..."
	@ssh $(WEB_SERVER_USER)@$(WEB_SERVER_HOST) 'mkdir -p $(WEB_SERVER_PATH)'
	@scp bin/$(APP_NAME)-linux-amd64 $(WEB_SERVER_USER)@$(WEB_SERVER_HOST):$(WEB_SERVER_PATH)/$(APP_NAME)
	@echo "Binary uploaded to $(WEB_SERVER_HOST):$(WEB_SERVER_PATH)/$(APP_NAME)"
	@echo ""
	@echo "The binary is now available for cloud-init provisioning at:"
	@echo "  http://$(WEB_SERVER_HOST):8080/light-hugger/gearbox-agent"
