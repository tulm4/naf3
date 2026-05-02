# Fullchain Test Optimization Design

**Date:** 2026-05-02
**Status:** Approved
**Goal:** Optimize `make test-fullchain` build time from ~2-5 min to ~10-30s on code changes

## Overview

The current `make test-fullchain` rebuilds Docker images from scratch every run due to multi-stage builds downloading Go modules and compiling code inside Docker. This design optimizes for two use cases: fast dev loop and CI/CD with layer caching.

## Architecture

### Approach 2: Go Binary Mount Pattern

Build Go binaries locally (fast, cached), Docker images only contain runtime. Binaries are mounted into containers at runtime.

```
Dev Loop (test-fullchain-fast):
  go build        docker build       docker compose up
  (~5-10s)       (~30s first run)    + volume mounts
       \                 \                  /
        \_________________\________________/
                    |
            binaries mounted into
            containers at runtime

CI/CD (test-fullchain):
  Docker build + cache push    docker compose pull + build
  to GitHub Actions cache       (cache hit on layers)
  (~2-5 min first run)         (~30-60s cached)
```

## Changes Required

### 1. New Makefile Target: `test-fullchain-fast`

Target for fast dev loop:

```makefile
test-fullchain-fast: build-all  ## Fast dev loop with binary mounts
	docker compose -f compose/fullchain.yaml build
	docker compose -f compose/fullchain.yaml up -d --quiet-pull
	# ... test execution
```

**Behavior:**
- First run: Go binaries (~5-10s) + Docker images (~30-60s BuildKit) = ~40-70s total
- Code unchanged: Docker images use BuildKit cache → ~5-10s
- Code changed: Binary rebuild (~5-10s) + image copy binary (~10-20s) = ~15-30s
- Config/env only: `SKIP_BUILD=1 make test-fullchain-fast` → ~5s

### 2. Modified `test-fullchain` Target (CI)

CI target with full rebuild and layer caching:

```makefile
test-fullchain: gen-certs build  ## Full E2E test (for CI)
	docker compose -f compose/fullchain.yaml build \
		--build-arg BUILDKIT_INLINE_CACHE=1
	docker compose -f compose/fullchain.yaml up -d --quiet-pull
	# ... test execution
```

**Behavior:**
- Uses BuildKit inline cache pushed to GitHub Actions cache
- Full rebuild ensures production-like behavior
- Cache hit on subsequent runs (~30-60s vs ~2-5 min)

### 3. New Compose File: `compose/fullchain-dev.yaml`

Volume mount binaries from `bin/` into containers:

```yaml
services:
  biz:
    build:
      context: ..
      dockerfile: Dockerfile.biz
    volumes:
      - ./bin/biz:/app/biz:ro  # Mount binary, no rebuild needed
```

Services that change frequently (biz, http-gateway, aaa-gateway) get volume mounts. Static services (nrf-mock, udm-mock, aaa-sim) build normally.

### 4. BuildKit Configuration

Enable BuildKit for all Docker builds:

```makefile
DOCKER_BUILD = DOCKER_BUILDKIT=1 docker build
```

Add cache configuration in `~/.docker/config.json`:

```json
{
  "features": {
    "builder": "default"
  },
  "cache-from": ["type=gha"],
  "cache-to": ["type=gha,mode=max"]
}
```

## Error Handling

| Error | Behavior |
|-------|----------|
| Binary not found | Fail with message: "Run 'make build' first" |
| Docker daemon not running | Friendly error with hint |
| Build fails | Show which service failed, exit code 1 |
| Test fails | Stack remains up for debugging, clean on exit |

## New Targets

| Target | Purpose |
|--------|---------|
| `make test-fullchain-fast` | Dev loop: binary mount, fast rebuild |
| `make test-fullchain` | CI: full rebuild, layer caching |
| `make test-fullchain-no-build` | Skip build, just docker compose up |

## CI/CD Configuration

GitHub Actions workflow:

```yaml
- name: Build Docker images
  run: |
    docker compose -f compose/fullchain.yaml build \
      --build-arg BUILDKIT_INLINE_CACHE=1 \
      --cache-from nssaaf/nssAAF:${{ github.sha }} \
      --cache-to nssaaf/nssAAF:${{ github.sha }}
```

## Files to Modify

| File | Change |
|------|--------|
| `Makefile` | Add `test-fullchain-fast`, `test-fullchain-no-build` targets |
| `compose/fullchain.yaml` | No change |
| `compose/fullchain-dev.yaml` | New: volume mounts for biz, http-gateway, aaa-gateway |
| `Dockerfile.biz` | No change (multi-stage build removed, binary only) |
| `.github/workflows/e2e.yml` | Add BuildKit cache config |

## Performance Targets

| Scenario | Before | After |
|----------|--------|-------|
| First run | ~2-5 min | ~40-70s |
| Code unchanged | ~2-5 min | ~5-10s |
| Code changed | ~2-5 min | ~15-30s |
| Config only | ~2-5 min | ~5s |
