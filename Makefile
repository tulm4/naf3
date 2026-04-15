# Makefile — Build and development targets for nssAAF
#
# Requires: Go 1.22+, golangci-lint, docker (optional)
#
# Usage:
#   make help              # Show all targets
#   make build            # Build the binary
#   make test             # Run unit tests
#   make lint             # Run linter
#   make docker-build     # Build Docker image
#   make clean            # Remove build artifacts

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOTEST = $(GOCMD) test
GOGET = $(GOCMD) get
GOMOD = $(GOCMD) mod
GOFMT = $(GOCMD) fmt
GOVET = $(GOCMD) vet

# Binary output
BINARY_NAME = nssAAF
BINARY_DIR = bin
BINARY_PATH = $(BINARY_DIR)/$(BINARY_NAME)
GO_PACKAGE = ./cmd/nssAAF/

# Linting
LINTER = golangci-lint
LINTER_FLAGS = run ./...

# Docker
DOCKER_IMAGE = ghcr.io/operator/nssAAF
DOCKER_TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
DOCKER_BUILD = docker build --platform linux/amd64
DOCKER_RUN = docker run --rm

# Test coverage
COVERAGE_FILE = coverage.out
COVERAGE_HTML = coverage.html

# Colors for output
RED = \033[0;31m
GREEN = \033[0;32m
YELLOW = \033[0;33m
NC = \033[0m # No Color

.PHONY: help
help: ## Show all available make targets
	@echo "$(GREEN)nssAAF Makefile$(NC)"
	@echo ""
	@echo "Build targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "Quick start:"
	@echo "  make build          # Compile the binary"
	@echo "  make test          # Run tests"
	@echo "  make lint          # Run linter"
	@echo "  make run           # Build and run locally"

.PHONY: build
build: ## Build the nssAAF binary
	@echo "$(YELLOW)Building $(BINARY_NAME)...$(NC)"
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -ldflags="-s -w" -o $(BINARY_PATH) $(GO_PACKAGE)
	@echo "$(GREEN)Built $(BINARY_PATH)$(NC)"

.PHONY: build-debug
build-debug: ## Build with debug symbols
	$(GOBUILD) -o $(BINARY_PATH) $(GO_PACKAGE)

.PHONY: test
test: ## Run all unit tests with race detector and coverage
	@echo "$(YELLOW)Running tests...$(NC)"
	$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...

.PHONY: test-short
test-short: ## Run tests without coverage (fast mode)
	$(GOTEST) -short ./...

.PHONY: test-html
test-html: test ## Generate HTML coverage report
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "$(GREEN)Coverage report: $(COVERAGE_HTML)$(NC)"

.PHONY: lint
lint: ## Run golangci-lint
	@echo "$(YELLOW)Running linter...$(NC)"
	$(LINTER) $(LINTER_FLAGS)

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	$(LINTER) run --fix ./...

.PHONY: vet
vet: ## Run go vet
	$(GOVET) ./...

.PHONY: fmt
fmt: ## Format all Go source files
	$(GOFMT) ./...

.PHONY: tidy
tidy: ## Clean up go.mod and go.sum
	$(GOMOD) tidy

.PHONY: mod-download
mod-download: ## Download all dependencies
	$(GOMOD) download

.PHONY: clean
clean: ## Remove build artifacts
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf $(BINARY_DIR)
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@echo "$(GREEN)Cleaned$(NC)"

.PHONY: docker-build
docker-build: ## Build Docker image
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

.PHONY: docker-buildx
docker-buildx: ## Build multi-platform Docker image
	$(DOCKER_BUILD) \
		--platform linux/amd64,linux/arm64 \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		--push \
		.

.PHONY: docker-run
docker-run: ## Run the Docker image locally
	$(DOCKER_RUN) -p 8080:8080 $(DOCKER_IMAGE):$(DOCKER_TAG)

.PHONY: run
run: build ## Build and run the binary locally
	./$(BINARY_PATH) -config configs/staging.yaml

.PHONY: run-dev
run-dev: ## Run with development config and debug logging
	BINARY_PATH=$(BINARY_PATH) go run ./cmd/nssAAF/ -config configs/example.yaml

.PHONY: deps
deps: ## Install development dependencies
	go install github.com/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: vuln
vuln: ## Run security vulnerability check
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

.PHONY: ci
ci: lint test build ## Run full CI pipeline (lint + test + build)
	@echo "$(GREEN)CI pipeline passed$(NC)"
