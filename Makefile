# Makefile — Build and development targets for NSSAAF 3-component architecture
# Spec: TS 29.526 v18.7.0; 3-component model (Phase R)
#
# Components:
#   biz         — Biz Pod (N58/N60 SBI + EAP engine)
#   http-gateway — HTTP Gateway (stateless TLS terminator)
#   aaa-gateway — AAA Gateway (Diameter/RADIUS transport)
#
# Requires: Go 1.22+, golangci-lint, docker (optional)
#
# Usage:
#   make help              # Show all targets
#   make build            # Build all 3 binaries
#   make test             # Run unit tests
#   make lint             # Run linter
#   make docker-build     # Build all Docker images
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
BINARY_DIR = bin

# Component binaries
BIZ_BINARY = $(BINARY_DIR)/biz
HTTPGW_BINARY = $(BINARY_DIR)/http-gateway
AAAGW_BINARY = $(BINARY_DIR)/aaa-gateway

# Linting
LINTER = golangci-lint
LINTER_FLAGS = run ./...

# Docker
DOCKER_IMAGE_PREFIX = ghcr.io/operator/nssaaf
DOCKER_TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
DOCKER_BUILD = docker build --platform linux/amd64
DOCKER_BUILDX = docker buildx build --platform linux/amd64,linux/arm64

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
	@echo "$(GREEN)nssAAF 3-Component Makefile$(NC)"
	@echo ""
	@echo "Component targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-22s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "Quick start:"
	@echo "  make build              # Build all 3 components"
	@echo "  make docker-build       # Build all Docker images"
	@echo "  make test              # Run tests"
	@echo "  make lint              # Run linter"

# =============================================================================
# Build targets
# =============================================================================

.PHONY: build
build: build-all ## Build all 3 component binaries
	@echo "$(GREEN)Build complete:$(NC)"
	@echo "  biz          → $(BIZ_BINARY)"
	@echo "  http-gateway → $(HTTPGW_BINARY)"
	@echo "  aaa-gateway → $(AAAGW_BINARY)"

build-all: build-biz build-http-gateway build-aaa-gateway ## Build all 3 binaries

.PHONY: build-biz
build-biz: ## Build Biz Pod binary
	@echo "$(YELLOW)Building biz...$(NC)"
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -ldflags="-s -w" -o $(BIZ_BINARY) ./cmd/biz/

.PHONY: build-http-gateway
build-http-gateway: ## Build HTTP Gateway binary
	@echo "$(YELLOW)Building http-gateway...$(NC)"
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -ldflags="-s -w" -o $(HTTPGW_BINARY) ./cmd/http-gateway/

.PHONY: build-aaa-gateway
build-aaa-gateway: ## Build AAA Gateway binary
	@echo "$(YELLOW)Building aaa-gateway...$(NC)"
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -ldflags="-s -w" -o $(AAAGW_BINARY) ./cmd/aaa-gateway/

.PHONY: build-debug
build-debug: build-debug-biz build-debug-http-gateway build-debug-aaa-gateway ## Build all with debug symbols

build-debug-biz:
	$(GOBUILD) -o $(BIZ_BINARY) ./cmd/biz/

build-debug-http-gateway:
	$(GOBUILD) -o $(HTTPGW_BINARY) ./cmd/http-gateway/

build-debug-aaa-gateway:
	$(GOBUILD) -o $(AAAGW_BINARY) ./cmd/aaa-gateway/

# =============================================================================
# Test targets
# =============================================================================

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

# =============================================================================
# Lint targets
# =============================================================================

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

# =============================================================================
# Docker targets
# =============================================================================

.PHONY: docker-build
docker-build: docker-build-biz docker-build-http-gateway docker-build-aaa-gateway ## Build all component Docker images
	@echo "$(GREEN)Docker build complete:$(NC)"
	@echo "  biz          → $(DOCKER_IMAGE_PREFIX)-biz:$(DOCKER_TAG)"
	@echo "  http-gateway → $(DOCKER_IMAGE_PREFIX)-http-gw:$(DOCKER_TAG)"
	@echo "  aaa-gateway → $(DOCKER_IMAGE_PREFIX)-aaa-gw:$(DOCKER_TAG)"

.PHONY: docker-build-biz
docker-build-biz: ## Build Biz Pod Docker image
	@echo "$(YELLOW)Building Biz Pod image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-biz:$(DOCKER_TAG) -f Dockerfile.biz .

.PHONY: docker-build-http-gateway
docker-build-http-gateway: ## Build HTTP Gateway Docker image
	@echo "$(YELLOW)Building HTTP Gateway image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-http-gw:$(DOCKER_TAG) -f Dockerfile.http-gateway .

.PHONY: docker-build-aaa-gateway
docker-build-aaa-gateway: ## Build AAA Gateway Docker image
	@echo "$(YELLOW)Building AAA Gateway image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-aaa-gw:$(DOCKER_TAG) -f Dockerfile.aaa-gateway .

.PHONY: docker-buildx
docker-buildx: docker-buildx-biz docker-buildx-http-gateway docker-buildx-aaa-gateway ## Build multi-platform Docker images (amd64 + arm64)

docker-buildx-biz:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-biz:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-biz:latest \
		--push \
		-f Dockerfile.biz .

docker-buildx-http-gateway:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-http-gw:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-http-gw:latest \
		--push \
		-f Dockerfile.http-gateway .

docker-buildx-aaa-gateway:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-aaa-gw:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-aaa-gw:latest \
		--push \
		-f Dockerfile.aaa-gateway .

# =============================================================================
# Dev targets
# =============================================================================

.PHONY: run-biz
run-biz: build-biz ## Build and run Biz Pod locally
	./$(BIZ_BINARY) -config compose/configs/biz.yaml

.PHONY: run-http-gateway
run-http-gateway: build-http-gateway ## Build and run HTTP Gateway locally
	./$(HTTPGW_BINARY) -config compose/configs/http-gateway.yaml

.PHONY: run-aaa-gateway
run-aaa-gateway: build-aaa-gateway ## Build and run AAA Gateway locally
	./$(AAAGW_BINARY) -config compose/configs/aaa-gateway.yaml

.PHONY: run
run: run-biz ## Build and run Biz Pod locally (default)

# =============================================================================
# Compose targets
# =============================================================================

.PHONY: compose-up
compose-up: docker-build ## Build images and start all services
	docker compose -f compose/dev.yaml up

.PHONY: compose-down
compose-down: ## Stop all services
	docker compose -f compose/dev.yaml down

.PHONY: compose-logs
compose-logs: ## Tail logs from all services
	docker compose -f compose/dev.yaml logs -f

# =============================================================================
# Dependency targets
# =============================================================================

.PHONY: deps
deps: ## Install development dependencies
	go install github.com/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: vuln
vuln: ## Run security vulnerability check
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# =============================================================================
# CI target
# =============================================================================

.PHONY: ci
ci: lint test build ## Run full CI pipeline (lint + test + build)
	@echo "$(GREEN)CI pipeline passed$(NC)"

# =============================================================================
# Cleanup
# =============================================================================

.PHONY: clean
clean: ## Remove build artifacts
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf $(BINARY_DIR)
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@echo "$(GREEN)Cleaned$(NC)"
