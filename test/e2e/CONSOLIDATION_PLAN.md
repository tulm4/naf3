# E2E Test Consolidation — Single Compose File

## Status

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Add NRM service to fullchain-dev.yaml | ⏳ PENDING |
| 2 | Update harness | ⏳ PENDING |
| 3 | Update test wiring | ⏳ PENDING |
| 4 | Update Makefile | ⏳ PENDING |
| 5 | Update CI workflow | ⏳ PENDING |
| 6 | Delete obsolete files | ⏳ PENDING |
| 7 | Update docs | ⏳ PENDING |

---

## Root Cause

`compose/fullchain-dev.yaml` is missing the `nrm` service entirely, while `dev.yaml` and `fullchain.yaml` both include it. The biz service also lacks `NRM_URL` pointing to the NRM RESTCONF endpoint. This means the E2E harness cannot reach the NRM for OAM operations.

Note: The existing NRF/UDM/AUSF URL env vars in `fullchain-dev.yaml` are already correct — they point to `nrf-mock:8081` and `udm-mock:8081`. The plan was incorrect in flagging these as bugs.

---

## Overview

Consolidate to a single docker compose file (`fullchain-dev.yaml`) and a single `make test-fullchain` target. The `dev.yaml` suite (compose + MockDriver) is removed entirely.

### Before

```
compose/
├── dev.yaml          # Redis, PG, mock-aaa-s, NRM, Biz, HTTP GW
└── fullchain.yaml    # Above + nrf-mock, udm-mock, aaa-sim
    └── fullchain-dev.yaml (dev variant with binary mounts)
        └── BUG: missing nrm service, missing NRM_URL

make test-e2e         # Uses dev.yaml + MockDriver (to be removed)
make test-fullchain   # Uses fullchain.yaml + ContainerDriver
```

### After

```
compose/
└── fullchain-dev.yaml  # All services: Redis, PG, nrf-mock, udm-mock,
                         # aaa-sim, aaa-gw, biz, http-gw, nrm (NEW)
    └── biz: NRF → nrf-mock:8081, UDM → udm-mock:8081, AUSF → biz:8080/n39x
                       NRM_URL → nrm:8084 (new dedicated port)

make test-fullchain   # Single target: fullchain-dev.yaml + ContainerDriver
make test-integration # Also uses fullchain-dev.yaml (Redis + PG)
```

---

## Phase 1 — Add NRM service to fullchain-dev.yaml

### 1.1 Add nrm service definition

Insert the `nrm` service between `http-gateway` and `networks:`. Copy from `compose/dev.yaml` (lines 91–108) but change the port mapping to avoid collision with `nrf-mock` (8082) and `udm-mock` (8083):

```yaml
  # ---------------------------------------------------------------------------
  # NRM — Network Resource Manager / RESTCONF server
  # ---------------------------------------------------------------------------
  nrm:
    build:
      context: ..
      dockerfile: Dockerfile.nrm
    image: nssaaf-nrm:latest
    depends_on:
      postgres:
        condition: service_healthy
    volumes:
      - ./configs/nrm.yaml:/etc/nssAAF/nrm.yaml:ro
    ports: ["8084:8081"]
    networks:
      - default
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8081/healthz || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 3
```

### 1.2 Add nrm to biz depends_on

In the `biz` service, add `nrm` as a dependency with `condition: service_healthy`, alongside the existing `nrf-mock` and `udm-mock` entries.

### 1.3 Add NRM_URL to biz env vars

Add the following env var to the `biz` service `environment:` block:

```yaml
NRM_URL: "http://nrm:8084"
```

This tells the biz pod where to reach the NRM RESTCONF server. The existing NRF/UDM/AUSF env vars are already correct and do not need changing.

---

## Phase 2 — Update harness

### 2.1 Update harness.yaml

```yaml
# test/e2e/harness.yaml — before
nrmUrl: "http://localhost:8081"

# test/e2e/harness.yaml — after
nrmUrl: "http://localhost:8084"
```

### 2.2 Update smoke_manual_test.go NRM health check URL

In `skipIfServicesNotUp()`, change:

```go
// before
{"NRM", "http://localhost:8081/healthz", false},

// after
{"NRM", "http://localhost:8084/healthz", false},
```

### 2.3 harness.go — no change needed

`h.nrmURL` is sourced from `cfg.Services.NRMUrl` which reads from `harness.yaml`. Once Phase 2.1 is done, the value propagates correctly. The `waitHealthy()` method already uses `h.nrmURL + "/healthz"` which will resolve to the correct port.

---

## Phase 3 — Update test wiring

### 3.1 Update e2e.go: use ContainerDriver by default

```go
// test/e2e/e2e.go — before (lines 62-64)
default:
    composeFile = "-f compose/dev.yaml"
    sharedDriver = NewMockDriver()

// test/e2e/e2e.go — after
default:
    composeFile = "-f compose/fullchain-dev.yaml"
    sharedDriver = NewContainerDriver()
    if sharedDriver == nil {
        fmt.Fprintf(os.Stderr, "FULLCHAIN_NRF_URL is not set; cannot use ContainerDriver\n")
        os.Exit(1)
    }
```

Also update the `fullchain` case to use the same compose file:

```go
// before
composeFile = "-f compose/fullchain.yaml"

// after
composeFile = "-f compose/fullchain-dev.yaml"
```

Also update the `DOCKER_COMPOSE` override comment and the TestMain doc comment to remove `dev` references.

### 3.2 Update harness.go comment for NewHarness

```go
// NewHarness uses ContainerDriver (E2E_PROFILE=fullchain or unset).
// For in-process mocks only, use NewHarnessFromDriver with a custom driver.
```

### 3.3 Update driver.go comment

Update the `Driver` type doc comment to remove `MockDriver` and `E2E_PROFILE=dev` references:

```go
// Driver abstracts the backend for E2E tests.
//
// ContainerDriver routes to the containerized NRF/UDM/AAA-S services
// defined in compose/fullchain-dev.yaml. AMF and AUSF callbacks are
// mocked in-process via httptest.Server.
//
// Driver is selected at test startup via the E2E_PROFILE environment variable:
//   - "" or "fullchain": ContainerDriver + compose/fullchain-dev.yaml
//   - "mock":             MockDriver + in-process mocks (unit-level testing only)
```

---

## Phase 4 — Update Makefile

### 4.1 Remove test-e2e target

Delete the entire `test-e2e` target block (lines 207–225). The target references `compose/dev.yaml` which will be deleted.

### 4.2 Update test-integration to use fullchain-dev.yaml

Change all compose file references from `compose/dev.yaml` to `compose/fullchain-dev.yaml` (lines 197, 202, 204). The `test-integration` target also has a `build` prerequisite — remove it since integration tests do not need built binaries (they connect directly to containers started by compose).

### 4.3 Update test-fullchain

Change all `compose/fullchain.yaml` references to `compose/fullchain-dev.yaml` (lines 240–256). Keep the `build` prerequisite — this target does a full rebuild of Docker images.

Add `FULLCHAIN_NRM_URL=http://localhost:8084` to the env vars passed to the test binary.

### 4.4 Update test-fullchain-fast

Change all `compose/fullchain.yaml` references to `compose/fullchain-dev.yaml` (lines 268–283). This target already does not have a `build` prerequisite (uses binary mounts), so keep that as-is.

Add `FULLCHAIN_NRM_URL=http://localhost:8084` to the env vars.

### 4.5 Update test-fullchain-no-build

Change all `compose/fullchain.yaml` references to `compose/fullchain-dev.yaml` (lines 289–304). This target already does not have a `build` prerequisite.

Add `FULLCHAIN_NRM_URL=http://localhost:8084` to the env vars.

### 4.6 Update test-all

```makefile
# before: test-all: test-unit test-integration test-e2e test-conformance
# after:  test-all: test-unit test-integration test-conformance
```

---

## Phase 5 — Update CI workflow

### 5.1 Update .github/workflows/fullchain-tests.yml

Change all `compose/fullchain.yaml` references to `compose/fullchain-dev.yaml` (lines 29, 54, 64, 65). The workflow runs the same test suite (`./test/e2e/...`) with the same env vars, just using the dev variant compose file.

---

## Phase 6 — Delete obsolete files

### 6.1 Delete compose/dev.yaml

```bash
rm compose/dev.yaml
```

### 6.2 Delete compose/fullchain.yaml

```bash
rm compose/fullchain.yaml
```

### 6.3 Delete test/e2e/mock_driver.go

```bash
rm test/e2e/mock_driver.go
```

After this, `MockDriver` type is gone. Any remaining references to `NewMockDriver()` or `MockDriver` in source files will cause compile errors and must be fixed. The `Driver` interface in `driver.go` will be updated to remove the `MockDriver` references (Phase 3.3).

---

## Phase 7 — Update docs

### 7.1 Update test/e2e/README.md

- Remove references to `E2E_PROFILE=dev` and `compose/dev.yaml`
- Update architecture diagram to show single `fullchain-dev.yaml`
- Update environment variables table: add `FULLCHAIN_NRM_URL`, update `E2E_PROFILE` description (drop `dev` default)
- Update smoke test instructions: NRM at port 8084
- Remove "Fast dev loop (MockDriver + compose/dev.yaml)" section
- Update the "Before / After" overview to match new structure

### 7.2 Update test/e2e/E2E_REFactor_PLAN.md

Specific sections to update:

| Section | Change |
|---------|--------|
| Phase 1 "Files to CREATE" | Remove `mock_driver.go` from to-create list (it was already created) |
| Phase 1 "Files to MODIFY" | Update container_driver.go description |
| Phase 3 "Before/After architecture" | Update compose file references |
| Phase 6 verification checklist | Remove `make test-e2e` item; add `make test-fullchain` |
| "Files to DELETE" | Confirm `test/e2e/fullchain/` already deleted (Phase 4.2 of that plan) |

### 7.3 Update smoke_manual_test.go package comment

```go
// Package e2e provides E2E smoke tests against the running NSSAAF stack
// via docker compose containers managed by `make test-fullchain`.
```

### 7.4 Update harness.go comment

```go
// NewHarness uses ContainerDriver (E2E_PROFILE=fullchain or unset).
// For in-process mocks only, use NewHarnessFromDriver with a custom driver.
```

---

## Verification Checklist

- [ ] `go build -tags=e2e ./test/e2e/...` compiles after removing `mock_driver.go`
- [ ] `make test-fullchain` starts `fullchain-dev.yaml` and passes health checks
- [ ] `make test-integration` starts `fullchain-dev.yaml` and runs integration tests
- [ ] `make test-all` runs without `test-e2e`
- [ ] NRM reachable at `http://localhost:8084/healthz`
- [ ] NRF mock reachable at `http://localhost:8082`
- [ ] UDM mock reachable at `http://localhost:8083`
- [ ] Biz Pod health: `http://localhost:8080/healthz/live`
- [ ] `smoke_manual_test.go` NRM health check passes
- [ ] GitHub Actions `fullchain-tests` workflow passes with `fullchain-dev.yaml`
- [ ] `test/e2e/README.md` updated
- [ ] `test/e2e/E2E_REFactor_PLAN.md` updated
- [ ] No references to `compose/dev.yaml` remain in Go source files

---

## Files Changed Summary

| Action | File |
|--------|------|
| MODIFY | `compose/fullchain-dev.yaml` |
| MODIFY | `test/e2e/harness.yaml` |
| MODIFY | `test/e2e/smoke_manual_test.go` |
| MODIFY | `test/e2e/e2e.go` |
| MODIFY | `test/e2e/driver.go` |
| MODIFY | `test/e2e/harness.go` |
| MODIFY | `Makefile` |
| MODIFY | `.github/workflows/fullchain-tests.yml` |
| MODIFY | `test/e2e/README.md` |
| MODIFY | `test/e2e/E2E_REFactor_PLAN.md` |
| DELETE | `compose/dev.yaml` |
| DELETE | `compose/fullchain.yaml` |
| DELETE | `test/e2e/mock_driver.go` |
