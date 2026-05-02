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
NRM_BINARY = $(BINARY_DIR)/nrm
AAASIM_BINARY = $(BINARY_DIR)/aaa-sim

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
	@echo "  nrm → $(NRM_BINARY)"
	@echo "  aaa-sim → $(AAASIM_BINARY)"

build-all: build-biz build-http-gateway build-aaa-gateway build-nrm build-aaa-sim ## Build all 3 binaries

.PHONY: build-biz
build-biz: ## Build Biz Pod binary
	@echo "$(YELLOW)Building biz...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(BIZ_BINARY) ./cmd/biz/

.PHONY: build-http-gateway
build-http-gateway: ## Build HTTP Gateway binary
	@echo "$(YELLOW)Building http-gateway...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(HTTPGW_BINARY) ./cmd/http-gateway/

.PHONY: build-aaa-gateway
build-aaa-gateway: ## Build AAA Gateway binary
	@echo "$(YELLOW)Building aaa-gateway...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(AAAGW_BINARY) ./cmd/aaa-gateway/

.PHONY: build-nrm
build-nrm: ## Build NRM binary
	@echo "$(YELLOW)Building nrm...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(NRM_BINARY) ./cmd/nrm/

.PHONY: build-aaa-sim
build-aaa-sim: ## Build AAA sim binary
	@echo "$(YELLOW)Building aaa-sim...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(AAASIM_BINARY) ./cmd/aaa-sim/

.PHONY: build-debug
build-debug: build-debug-biz build-debug-http-gateway build-debug-aaa-gateway build-debug-aaa-sim build-debug-nrm ## Build all with debug symbols

build-debug-biz:
	$(GOBUILD) -o $(BIZ_BINARY) ./cmd/biz/

build-debug-http-gateway:
	$(GOBUILD) -o $(HTTPGW_BINARY) ./cmd/http-gateway/

build-debug-aaa-gateway:
	$(GOBUILD) -o $(AAAGW_BINARY) ./cmd/aaa-gateway/

build-debug-aaa-sim:
	$(GOBUILD) -o $(AAASIM_BINARY) ./cmd/aaa-sim/

build-debug-nrm:
	$(GOBUILD) -o $(NRM_BINARY) ./cmd/nrm/

# =============================================================================
# Test targets
# =============================================================================

.PHONY: test
test: test-unit ## Run unit tests (alias for test-unit; use test-all to run all layers)

.PHONY: test-short
test-short: ## Run tests without coverage (fast mode)
	$(GOTEST) -short ./...

.PHONY: test-html
test-html: test ## Generate HTML coverage report
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "$(GREEN)Coverage report: $(COVERAGE_HTML)$(NC)"

# =============================================================================
# Layered test targets
# Each target manages its own infra lifecycle independently.
# Spec: D-16 decision — "Separate targets" per test layer.
# =============================================================================

.PHONY: test-unit
test-unit: ## Run unit tests only (fast, no infra required)
	@echo "$(YELLOW)Running unit tests...$(NC)"
	$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./internal/... ./test/unit/...

.PHONY: test-integration
test-integration: ## Run integration tests against real PostgreSQL and Redis via docker compose
	@echo "$(YELLOW)Starting test infrastructure...$(NC)"
	docker compose -f compose/dev.yaml up -d --quiet-pull
	@echo "$(YELLOW)Waiting for infrastructure to be healthy...$(NC)"
	@sleep 5
	@TEST_DATABASE_URL="postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable" \
	TEST_REDIS_URL="redis://localhost:6379" \
	$(GOTEST) -race -v ./test/integration/... || { docker compose -f compose/dev.yaml down; exit 1; }
	@echo "$(YELLOW)Tearing down test infrastructure...$(NC)"
	docker compose -f compose/dev.yaml down
	@echo "$(GREEN)Integration tests complete$(NC)"

.PHONY: test-e2e
test-e2e: gen-certs build ## Build binaries for docker images, then run E2E tests against compose containers
	@echo "$(YELLOW)Starting docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml up -d --quiet-pull
	@sleep 10
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	$(GOTEST) -tags=e2e -v -count=1 \
		./test/e2e/... \
		|| { docker compose -f compose/dev.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml down --remove-orphans
	@echo "$(GREEN)E2E tests complete$(NC)"

.PHONY: test-conformance
test-conformance: ## Run 3GPP conformance tests against live services
	@echo "$(YELLOW)Running conformance tests...$(NC)"
	$(GOTEST) -race -v ./test/conformance/...

.PHONY: test-all
test-all: test-unit test-integration test-e2e test-conformance ## Run all test layers in sequence
	@echo "$(GREEN)All tests passed$(NC)"

.PHONY: test-fullchain
test-fullchain: gen-certs build ## Run E2E fullchain tests against docker compose
	@echo "$(YELLOW)Starting docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml up -d --quiet-pull
	@sleep 10
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=5m \
		./test/e2e/fullchain/... \
		|| { docker compose -f compose/dev.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete$(NC)"

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
# TLS certificates (for E2E tests and local dev)
# =============================================================================

E2E_TLS_DIR ?= /tmp/e2e-tls

.PHONY: gen-certs
gen-certs: ## Generate self-signed TLS certificates for E2E tests
	@mkdir -p $(E2E_TLS_DIR)
	@echo "$(YELLOW)Generating TLS certificates in $(E2E_TLS_DIR)...$(NC)"
	@if [ ! -f $(E2E_TLS_DIR)/server.key ] || [ ! -f $(E2E_TLS_DIR)/server.crt ]; then \
		openssl req -x509 -nodes -newkey rsa:2048 \
			-keyout $(E2E_TLS_DIR)/server.key \
			-out $(E2E_TLS_DIR)/server.crt \
			-days 365 \
			-subj "/CN=localhost/O=nssAAF/C=US" \
			-addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
			2>/dev/null || \
		openssl req -x509 -nodes -newkey rsa:2048 \
			-keyout $(E2E_TLS_DIR)/server.key \
			-out $(E2E_TLS_DIR)/server.crt \
			-days 365 \
			-subj "/CN=localhost/O=nssAAF/C=US"; \
	fi
	@echo "$(GREEN)TLS certificates ready: $(E2E_TLS_DIR)/server.{key,crt}$(NC)"

# =============================================================================
# Cleanup
# =============================================================================

.PHONY: clean
clean: ## Remove build artifacts
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf $(BINARY_DIR)
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@echo "$(GREEN)Cleaned$(NC)"
