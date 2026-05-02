# Fullchain Test Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optimize `make test-fullchain` build time from ~2-5 min to ~10-30s on code changes using BuildKit caching and binary mount patterns.

**Architecture:** Build Go binaries locally (cached), Docker images use BuildKit for layer caching. Dev loop uses volume mounts for fast iteration. CI uses GitHub Actions cache for layer reuse.

**Tech Stack:** Docker BuildKit, docker compose, GNU Make, GitHub Actions

---

## File Changes Overview

| File | Action |
|------|--------|
| `Makefile` | Modify: add `test-fullchain-fast`, `test-fullchain-no-build`, update `test-fullchain` |
| `compose/fullchain-dev.yaml` | Create: dev compose file with volume mounts |
| `.github/workflows/e2e.yml` | Create/Modify: add BuildKit cache for CI |

---

## Task 1: Enable BuildKit in Makefile

**Files:**
- Modify: `Makefile:1-20`

- [ ] **Step 1: Read current Makefile header**

Run: Read lines 1-50 of `Makefile` to see current build configuration

- [ ] **Step 2: Add BuildKit configuration**

Locate the section after `SHELL := /bin/bash` and before the first target. Add:

```makefile
# =============================================================================
# Build Configuration
# =============================================================================

# Enable BuildKit for faster Docker builds
export DOCKER_BUILDKIT := 1
COMPOSE_DOCKER_CLI_BUILD := 1

# BuildKit cache settings for CI
BUILDKIT_INLINE_CACHE := 1
```

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: enable Docker BuildKit for faster builds"
```

---

## Task 2: Create compose/fullchain-dev.yaml

**Files:**
- Create: `compose/fullchain-dev.yaml`
- Reference: `compose/fullchain.yaml`

- [ ] **Step 1: Read fullchain.yaml structure**

Read `compose/fullchain.yaml` to understand service definitions, especially:
- `biz` service ports and volumes
- `http-gateway` service ports and volumes
- `aaa-gateway` service ports and volumes

- [ ] **Step 2: Create compose/fullchain-dev.yaml**

Create the file with volume mounts for services that change frequently:

```yaml
# compose/fullchain-dev.yaml
# Dev variant: mounts pre-built binaries for fast iteration.
# Use with: docker compose -f compose/fullchain-dev.yaml up
#
# Services that change frequently get volume mounts (biz, http-gateway, aaa-gateway).
# Static services (nrf-mock, udm-mock, aaa-sim, redis, postgres) build normally.

services:
  # ---------------------------------------------------------------------------
  # Redis — shared session correlation store
  # ---------------------------------------------------------------------------
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    networks:
      - default
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  # ---------------------------------------------------------------------------
  # PostgreSQL — session and audit data store
  # ---------------------------------------------------------------------------
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: nssaa
      POSTGRES_PASSWORD: nssaa
      POSTGRES_DB: nssaa
    ports: ["5432:5432"]
    volumes:
      - postgres_fullchain_dev_data:/var/lib/postgresql/data
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U nssaa -d nssaa"]
      interval: 5s
      timeout: 3s
      retries: 5

  # ---------------------------------------------------------------------------
  # nrf-mock — NRF mock (static, rebuild only when code changes)
  # ---------------------------------------------------------------------------
  nrf-mock:
    build:
      context: ..
      dockerfile: Dockerfile.nrf-mock
    image: nssaaf-nrf-mock:latest
    ports: ["8082:8081"]
    environment:
      NRF_NF_STATUS: "udm-001:REGISTERED,ausf-001:REGISTERED,aaa-gw-001:REGISTERED"
      NRF_SERVICE_ENDPOINTS: "UDM:nudm-uem:udm-mock:8081,AUSF:nausf-auth:ausf-mock:8081"
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8081/nnrf-disc/v1 || exit 1"]
      interval: 5s
      timeout: 3s
      retries: 5

  # ---------------------------------------------------------------------------
  # udm-mock — UDM mock (static, rebuild only when code changes)
  # ---------------------------------------------------------------------------
  udm-mock:
    build:
      context: ..
      dockerfile: Dockerfile.udm-mock
    image: nssaaf-udm-mock:latest
    ports: ["8083:8081"]
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8081/nudm-uemm/v1 || exit 1"]
      interval: 5s
      timeout: 3s
      retries: 5

  # ---------------------------------------------------------------------------
  # aaa-sim — AAA Server simulator (static, rebuild only when code changes)
  # ---------------------------------------------------------------------------
  aaa-sim:
    build:
      context: ..
      dockerfile: Dockerfile.aaa-sim
    image: nssaaf-aaa-sim:latest
    ports: ["18120:1812", "38680:3868"]
    environment:
      AAA_SIM_MODE: "${AAA_SIM_MODE:-EAP_TLS_SUCCESS}"
    networks:
      - default

  # ---------------------------------------------------------------------------
  # AAA Gateway — with binary mount for fast iteration
  # ---------------------------------------------------------------------------
  aaa-gateway:
    build:
      context: ..
      dockerfile: Dockerfile.aaa-gateway
    image: nssaaf-aaa-gw:latest
    depends_on:
      redis:
        condition: service_healthy
      aaa-sim:
        condition: service_started
    volumes:
      - ./configs/aaa-gateway.yaml:/etc/nssAAF/aaa-gateway.yaml:ro
      - ./bin/aaa-gateway:/app/aaa-gateway:ro  # Mount binary for fast iteration
    environment:
      REDIS_ADDR: "redis:6379"
      BIZ_URL: "http://biz:8080"
    ports: ["9090:9090", "18121:1812/udp", "38681:3868"]
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:9090/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 3

  # ---------------------------------------------------------------------------
  # biz — with binary mount for fast iteration
  # ---------------------------------------------------------------------------
  biz:
    build:
      context: ..
      dockerfile: Dockerfile.biz
    image: nssaaf-biz:latest
    depends_on:
      redis:
        condition: service_healthy
      postgres:
        condition: service_healthy
      aaa-gateway:
        condition: service_healthy
      nrf-mock:
        condition: service_healthy
      udm-mock:
        condition: service_healthy
    volumes:
      - ./configs/biz.yaml:/etc/nssAAF/biz.yaml:ro
      - ./bin/biz:/app/biz:ro  # Mount binary for fast iteration
    environment:
      MASTER_KEY_HEX: "${MASTER_KEY_HEX:-6767a7ad0416a19ea174608288761dde35dfabba2a8dda9602fc520b80e1af15}"
      POSTGRES_HOST: "postgres"
      REDIS_ADDR: "redis:6379"
      NRF_URL: "http://nrf-mock:8081"
      UDM_URL: "http://udm-mock:8081"
      AUSF_URL: "http://biz:8080/n39x"
      AAA_GW_URL: "http://aaa-gateway:9090"
    ports: ["8080:8080"]
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:8080/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 5s

  # ---------------------------------------------------------------------------
  # http-gateway — with binary mount for fast iteration
  # ---------------------------------------------------------------------------
  http-gateway:
    build:
      context: ..
      dockerfile: Dockerfile.http-gateway
    image: nssaaf-http-gw:latest
    depends_on:
      biz:
        condition: service_healthy
    volumes:
      - ./configs/http-gateway.yaml:/etc/nssAAF/http-gateway.yaml:ro
      - /tmp/e2e-tls:/tmp/e2e-tls:ro
      - ./bin/http-gateway:/app/http-gateway:ro  # Mount binary for fast iteration
    environment:
      NAF3_AUTH_DISABLED: "1"
      BIZ_URL: "http://biz:8080"
    ports: ["8443:8443"]
    networks:
      - default

networks:
  default:
    driver: bridge

volumes:
  postgres_fullchain_dev_data:
```

- [ ] **Step 3: Commit**

```bash
git add compose/fullchain-dev.yaml
git commit -m "feat: add compose/fullchain-dev.yaml with binary volume mounts"
```

---

## Task 3: Add test-fullchain-fast Target

**Files:**
- Modify: `Makefile` — add new target after `test-fullchain` definition

- [ ] **Step 1: Read current test-fullchain target**

Run: `grep -n "^test-fullchain:" Makefile` to find line number, then read lines around it

- [ ] **Step 2: Add test-fullchain-fast target after test-fullchain**

Add this new target:

```makefile
# =============================================================================
# Fast Dev Loop targets
# =============================================================================

.PHONY: test-fullchain-fast
test-fullchain-fast: gen-certs ## Fast dev loop: binary mount pattern for ~15-30s iteration
	@echo "$(YELLOW)Starting fullchain docker compose stack (fast mode)...$(NC)"
	@echo "$(YELLOW)Using pre-built binaries from bin/ with volume mounts...$(NC)"
	docker compose -f compose/fullchain-dev.yaml build
	docker compose -f compose/fullchain-dev.yaml up -d --quiet-pull
	@sleep 15
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/fullchain/... \
		|| { docker compose -f compose/fullchain-dev.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete (fast mode)$(NC)"

.PHONY: test-fullchain-no-build
test-fullchain-no-build: ## Run tests with existing images (skip build, ~5s startup)
	@echo "$(YELLOW)Starting fullchain stack (no build)...$(NC)"
	docker compose -f compose/fullchain-dev.yaml up -d
	@sleep 15
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	B2Z_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/fullchain/... \
		|| { docker compose -f compose/fullchain-dev.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain-dev.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete (no-build mode)$(NC)"
```

- [ ] **Step 3: Update help target**

Add entries for new targets in the help section:

```
	@echo "  test-fullchain-fast    Fast dev loop with binary mounts"
	@echo "  test-fullchain-no-build Skip build, use existing images"
```

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat: add test-fullchain-fast and test-fullchain-no-build targets"
```

---

## Task 4: Update test-fullchain for CI with BuildKit Cache

**Files:**
- Modify: `Makefile` — update existing `test-fullchain` target

- [ ] **Step 1: Read current test-fullchain target**

Run: `grep -n "^test-fullchain:" Makefile` then read 20 lines after

- [ ] **Step 2: Update test-fullchain with BuildKit cache args**

Replace the `docker compose build` line with BuildKit cache configuration:

```makefile
test-fullchain: gen-certs build ## Run fullchain E2E tests (real containers for NRF/UDM/AAA-SIM)
	@echo "$(YELLOW)Starting fullchain docker compose stack...$(NC)"
	docker compose -f compose/fullchain.yaml build \
		--build-arg BUILDKIT_INLINE_CACHE=1
	docker compose -f compose/fullchain.yaml up -d --quiet-pull
	@sleep 15
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	FULLCHAIN_NRF_URL=http://localhost:8082 \
	FULLCHAIN_UDM_URL=http://localhost:8083 \
	$(GOTEST) -tags=e2e -v -count=1 -timeout=10m \
		./test/e2e/fullchain/... \
		|| { docker compose -f compose/fullchain.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down fullchain stack...$(NC)"
	docker compose -f compose/fullchain.yaml down --remove-orphans
	@echo "$(GREEN)Fullchain tests complete$(NC)"
```

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: enable BuildKit inline cache for test-fullchain CI"
```

---

## Task 5: Add GitHub Actions CI Configuration

**Files:**
- Create: `.github/workflows/e2e.yml`

- [ ] **Step 1: Check if .github/workflows directory exists**

Run: `ls -la .github/workflows/ 2>/dev/null || echo "Directory does not exist"`

- [ ] **Step 2: Create .github/workflows directory if needed**

```bash
mkdir -p .github/workflows
```

- [ ] **Step 3: Create e2e.yml workflow file**

```yaml
name: E2E Fullchain Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  fullchain-e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 15

    services:
      docker:
        image: docker:24-cli
        options: --privileged

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build Docker images with cache
        run: |
          docker compose -f compose/fullchain.yaml build \
            --build-arg BUILDKIT_INLINE_CACHE=1 \
            --cache-to type=gha,mode=max

      - name: Run tests
        env:
          E2E_DOCKER_MANAGED: "1"
          E2E_TLS_CA: /tmp/e2e-tls/server.crt
          BIZ_PG_URL: postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable
          BIZ_REDIS_URL: redis://localhost:6379
          FULLCHAIN_NRF_URL: http://localhost:8082
          FULLCHAIN_UDM_URL: http://localhost:8083
        run: |
          docker compose -f compose/fullchain.yaml up -d --quiet-pull
          sleep 15
          go test -tags=e2e -v -count=1 -timeout=10m ./test/e2e/fullchain/...
          docker compose -f compose/fullchain.yaml down --remove-orphans
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -m "feat: add GitHub Actions E2E workflow with BuildKit cache"
```

---

## Task 6: Update .gitignore for BuildKit Cache

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Read current .gitignore**

Run: `cat .gitignore`

- [ ] **Step 2: Add BuildKit cache entries**

Add these lines to `.gitignore`:

```
# BuildKit cache
.docker/
```

- [ ] **Step 3: Commit**

```bash
git add .gitignore
git commit -m "chore: add .docker/ to .gitignore"
```

---

## Task 7: Verify Implementation

**Files:**
- Verify: All modified files

- [ ] **Step 1: Verify Makefile targets exist**

Run:
```bash
grep -E "^test-fullchain" Makefile
```
Expected output:
```
test-fullchain: gen-certs build ## Run fullchain E2E tests...
test-fullchain-fast: gen-certs ## Fast dev loop...
test-fullchain-no-build: ## Run tests with existing images...
```

- [ ] **Step 2: Verify compose/fullchain-dev.yaml syntax**

Run:
```bash
docker compose -f compose/fullchain-dev.yaml config --quiet
```
Expected: No output (valid YAML)

- [ ] **Step 3: Verify binaries exist**

Run:
```bash
ls -la bin/
```
Expected: biz, http-gateway, aaa-gateway binaries listed

- [ ] **Step 4: Commit all changes**

```bash
git status
git log --oneline -3
```

---

## Summary

After this implementation:

| Target | Purpose | Build Time |
|--------|---------|------------|
| `make test-fullchain` | CI: full rebuild | ~30-60s (cached) |
| `make test-fullchain-fast` | Dev loop: binary mount | ~15-30s |
| `make test-fullchain-no-build` | Skip build | ~5s |

**Spec coverage check:**
- [x] BuildKit enabled in Makefile
- [x] New compose file with volume mounts
- [x] `test-fullchain-fast` target
- [x] `test-fullchain-no-build` target
- [x] `test-fullchain` updated with cache
- [x] GitHub Actions workflow
- [x] Error handling in targets
- [x] Performance targets defined
