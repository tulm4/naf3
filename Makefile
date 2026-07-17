# Makefile — Build and development targets for NSSAAF 3-component architecture
# Spec: TS 29.526 v18.7.0; 3-component model (Phase R)
#
# Components:
#   biz         — Biz Pod (N58/N60 SBI + EAP engine)
#   http-gateway — HTTP Gateway (stateless TLS terminator)
#   aaa-gateway — AAA Gateway (Diameter/RADIUS transport)
#   nrf-mock    — NRF Mock (for development)
#   udm-mock    — UDM Mock (for development)
#   aaa-sim     — AAA Simulator (for development)
#
# Requires: Go 1.22+, golangci-lint, docker (optional)
#
# Usage:
#   make help              # Show all targets
#   make build            # Build all binaries
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
NRFMOCK_BINARY = $(BINARY_DIR)/nrf-mock
UDMMOCK_BINARY = $(BINARY_DIR)/udm-mock

# RADIUS dictionary code generation
RADIUS_DICT_GEN = $(BINARY_DIR)/radius-dict-gen

# Linting
LINTER = golangci-lint
LINTER_FLAGS = run ./...

# Docker
# DOCKER_IMAGE_PREFIX = ghcr.io/operator/nssaaf
DOCKER_IMAGE_PREFIX = nssaaf
# DOCKER_TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
DOCKER_TAG = latest
DOCKER_BUILD = docker build
DOCKER_BUILDX = docker buildx build --platform linux/amd64,linux/arm64

# =============================================================================
# Build Configuration
# =============================================================================

# Enable BuildKit for faster Docker builds (export for docker compose)
export DOCKER_BUILDKIT := 1
COMPOSE_DOCKER_CLI_BUILD := 1

# BuildKit inline cache for CI layer reuse
BUILDKIT_INLINE_CACHE := 1

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
build: build-all ## Build all binaries
	@echo "$(GREEN)Build complete:$(NC)"
	@echo "  biz          → $(BIZ_BINARY)"
	@echo "  http-gateway → $(HTTPGW_BINARY)"
	@echo "  aaa-gateway → $(AAAGW_BINARY)"
	@echo "  nrf-mock    → $(NRFMOCK_BINARY)"
	@echo "  udm-mock    → $(UDMMOCK_BINARY)"
	@echo "  aaa-sim     → $(AAASIM_BINARY)"

build-all: build-biz build-http-gateway build-aaa-gateway build-nrf-mock build-udm-mock build-aaa-sim ## Build all binaries

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

.PHONY: build-nrf-mock
build-nrf-mock: ## Build NRF MOCK binary
	@echo "$(YELLOW)Building nrf-mock...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(NRFMOCK_BINARY) ./cmd/nrf-mock/

.PHONY: build-udm-mock
build-udm-mock: ## Build UDM mock binary
	@echo "$(YELLOW)Building udm-mock...$(NC)"
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-s -w" -o $(UDMMOCK_BINARY) ./cmd/udm-mock/

.PHONY: build-debug
build-debug: build-debug-biz build-debug-http-gateway build-debug-aaa-gateway build-debug-aaa-sim build-debug-nrm build-debug-nrf-mock build-debug-udm-mock ## Build all with debug symbols

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

build-debug-nrf-mock:
	$(GOBUILD) -o $(NRFMOCK_BINARY) ./cmd/nrf-mock/

build-debug-udm-mock:
	$(GOBUILD) -o $(UDMMOCK_BINARY) ./cmd/udm-mock/

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
	$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...

.PHONY: test-integration
test-integration: ## Run integration tests against real PostgreSQL and Redis via docker compose
	@echo "$(YELLOW)Starting test infrastructure...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull
	@echo "$(YELLOW)Waiting for infrastructure to be healthy...$(NC)"
	@sleep 5
	@TEST_DATABASE_URL="postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable" \
	TEST_REDIS_URL="redis://localhost:6379" \
	$(GOTEST) -race -v ./test/integration/... || { docker compose -f compose/fullchain-dev-tcp.yaml down; exit 1; }
	@echo "$(YELLOW)Tearing down test infrastructure...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down
	@echo "$(GREEN)Integration tests complete$(NC)"
.PHONY: test-conformance
test-conformance: ## Run 3GPP conformance tests against live services
	@echo "$(YELLOW)Running conformance tests...$(NC)"
	$(GOTEST) -race -v ./test/conformance/...

.PHONY: test-all
test-all: test-unit test-integration test-conformance ## Run all test layers in sequence
	@echo "$(GREEN)All tests passed$(NC)"

.PHONY: test-fullchain
test-fullchain: gen-certs build ## Run fullchain E2E tests (real containers for NRF/UDM/AAA-SIM)
	# E2E_PROFILE=fullchain: ContainerDriver + compose/fullchain-dev-tcp.yaml
	@echo "$(YELLOW)Starting fullchain docker compose stack...$(NC)"
	# docker compose -f compose/fullchain-dev-tcp.yaml build \
	# 	--build-arg BUILDKIT_INLINE_CACHE=1
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull
	@sleep 10
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379/0 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/... \
		|| { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete$(NC)"

# =============================================================================
# Fast Dev Loop targets
# =============================================================================

.PHONY: test-fullchain-fast
test-fullchain-fast: gen-certs ## Fast dev loop: binary mount pattern for ~15-30s iteration
	# E2E_PROFILE=fullchain: ContainerDriver + compose/fullchain-dev-tcp.yaml
	@echo "$(YELLOW)Starting fullchain docker compose stack (fast mode)...$(NC)"
	@echo "$(YELLOW)Using pre-built binaries from bin/ with volume mounts...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml build
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull
	@sleep 15
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379/0 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/... \
		|| { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete (fast mode)$(NC)"

.PHONY: test-fullchain-no-build
test-fullchain-no-build: ## Run tests with existing images (skip build, ~5s startup)
	# E2E_PROFILE=fullchain: ContainerDriver + compose/fullchain-dev-tcp.yaml
	@echo "$(YELLOW)Starting fullchain stack (no build)...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml up -d
	@sleep 15
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379/0 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/... \
		|| { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete (no-build mode)$(NC)"

# =============================================================================
# Per-UE Debug Tracing E2E (gated on RUN_E2E=1)
# =============================================================================

.PHONY: test-debug-full-radius
test-debug-full-radius: build ## Run per-UE debug RADIUS full-flow tests (RUN_E2E=1 required)
	@echo "$(YELLOW)Starting fullchain TCP stack for debug RADIUS tests...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml build
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running debug full-flow RADIUS tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	FULLCHAIN_AAA_GW_URL=http://localhost:9090 \
	RUN_E2E=1 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		-run 'TestDebugFullFlow_(RADIUS_Forward|AMFCallback)' \
		./test/e2e/... \
		|| { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Debug RADIUS full-flow tests complete$(NC)"

.PHONY: test-debug-full-diameter
test-debug-full-diameter: build ## Run per-UE debug Diameter full-flow tests (RUN_E2E=1 required)
	@echo "$(YELLOW)Starting fullchain TCP stack for debug Diameter tests...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml build
	DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running debug full-flow Diameter tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_PROFILE=fullchain \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	FULLCHAIN_AAA_GW_URL=http://localhost:9090 \
	RUN_E2E=1 \
	DIAMETER_TRANSPORT=tcp \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		-run 'TestDebugFullFlow_DIAMETER_Forward' \
		./test/e2e/... \
		|| { DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	DIAMETER_TRANSPORT=tcp docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Debug Diameter full-flow tests complete$(NC)"

.PHONY: test-debug-full
test-debug-full: test-debug-full-radius test-debug-full-diameter ## Run all per-UE debug full-flow tests

# =============================================================================
# Diameter + RADIUS E2E (logs-only, Makefile-owned compose lifecycle)
# =============================================================================

.PHONY: test-diameter-radius
test-diameter-radius: gen-certs build ## Diameter TCP + RADIUS E2E (logs-only)
	@echo "$(YELLOW)Starting fullchain TCP stack for Diameter/RADIUS tests...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running Diameter TCP + RADIUS tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-tcp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379/0 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	FULLCHAIN_AAA_GW_URL=http://localhost:9090 \
	$(GOTEST) -tags=e2e -run 'TestDiameter_TCP|TestRadius' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down TCP stack...$(NC)"
	docker compose -f compose/fullchain-dev-tcp.yaml down --remove-orphans
	@echo "$(GREEN)Diameter TCP + RADIUS tests complete$(NC)"

.PHONY: test-diameter-radius-sctp
test-diameter-radius-sctp: gen-certs build ## Diameter SCTP E2E (logs-only; skips on non-SCTP hosts)
	@echo "$(YELLOW)Starting fullchain SCTP stack for Diameter tests...$(NC)"
	docker compose -f compose/fullchain-dev-sctp.yaml up -d --quiet-pull --wait
	@echo "$(YELLOW)Running Diameter SCTP tests...$(NC)"
	E2E_DOCKER_MANAGED=1 \
	E2E_COMPOSE_FILE=compose/fullchain-dev-sctp.yaml \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379/0 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	FULLCHAIN_AAA_SIM_URL=http://localhost:18120 \
	FULLCHAIN_NRM_URL=http://localhost:8084 \
	FULLCHAIN_HTTP_GW_URL=https://localhost:8443 \
	FULLCHAIN_AAA_GW_URL=http://localhost:9090 \
	$(GOTEST) -tags=e2e -run 'TestDiameter_SCTP' -v -count=1 -timeout=10m ./test/e2e/... \
	  || { docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down SCTP stack...$(NC)"
	docker compose -f compose/fullchain-dev-sctp.yaml down --remove-orphans
	@echo "$(GREEN)Diameter SCTP tests complete$(NC)"

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
docker-build: docker-build-biz docker-build-http-gateway docker-build-aaa-gateway docker-build-nrf-mock docker-build-udm-mock docker-build-aaa-sim ## Build all Docker images
	@echo "$(GREEN)Docker build complete:$(NC)"
	@echo "  biz          → $(DOCKER_IMAGE_PREFIX)-biz:$(DOCKER_TAG)"
	@echo "  http-gateway → $(DOCKER_IMAGE_PREFIX)-http-gw:$(DOCKER_TAG)"
	@echo "  aaa-gateway → $(DOCKER_IMAGE_PREFIX)-aaa-gw:$(DOCKER_TAG)"
	@echo "  nrf-mock    → $(DOCKER_IMAGE_PREFIX)-nrf-mock:$(DOCKER_TAG)"
	@echo "  udm-mock    → $(DOCKER_IMAGE_PREFIX)-udm-mock:$(DOCKER_TAG)"
	@echo "  aaa-sim     → $(DOCKER_IMAGE_PREFIX)-aaa-sim:$(DOCKER_TAG)"

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

.PHONY: docker-build-nrf-mock
docker-build-nrf-mock: ## Build NRF Mock Docker image
	@echo "$(YELLOW)Building NRF Mock image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-nrf-mock:$(DOCKER_TAG) -f Dockerfile.nrf-mock .

.PHONY: docker-build-udm-mock
docker-build-udm-mock: ## Build UDM Mock Docker image
	@echo "$(YELLOW)Building UDM Mock image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-udm-mock:$(DOCKER_TAG) -f Dockerfile.udm-mock .

.PHONY: docker-build-aaa-sim
docker-build-aaa-sim: ## Build AAA Simulator Docker image
	@echo "$(YELLOW)Building AAA Simulator image...$(NC)"
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE_PREFIX)-aaa-sim:$(DOCKER_TAG) -f Dockerfile.aaa-sim .

.PHONY: docker-buildx
docker-buildx: docker-buildx-biz docker-buildx-http-gateway docker-buildx-aaa-gateway docker-buildx-nrf-mock docker-buildx-udm-mock docker-buildx-aaa-sim ## Build multi-platform Docker images (amd64 + arm64)

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

docker-buildx-nrf-mock:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-nrf-mock:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-nrf-mock:latest \
		--push \
		-f Dockerfile.nrf-mock .

docker-buildx-udm-mock:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-udm-mock:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-udm-mock:latest \
		--push \
		-f Dockerfile.udm-mock .

docker-buildx-aaa-sim:
	$(DOCKER_BUILDX) \
		-t $(DOCKER_IMAGE_PREFIX)-aaa-sim:$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_PREFIX)-aaa-sim:latest \
		--push \
		-f Dockerfile.aaa-sim .

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
# Code Generation
# =============================================================================

$(RADIUS_DICT_GEN):
	go install layeh.com/radius/cmd/radius-dict-gen@latest

.PHONY: gen-radius-dict
gen-radius-dict: $(RADIUS_DICT_GEN) ## Generate RADIUS dictionary code from dictionaries
	@echo "Generating RADIUS dictionary code..."
	@mkdir -p internal/radius/layeh/gen
	cd data/dictionaries && $(CURDIR)/$(RADIUS_DICT_GEN) -output $(CURDIR)/internal/radius/layeh/gen/dict.go \
		-package gen composite.dict
	@echo "Done: internal/radius/layeh/gen/dict.go"

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
