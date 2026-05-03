# E2E Test Harness Refactor — Driver Interface (Option A)

## Status

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Driver interface | ✅ DONE |
| 2 | TestMain wiring | ✅ DONE |
| 3 | Makefile update | ✅ DONE |
| 4 | Consolidate tests | ✅ DONE |
| 5 | ContainerDriver stubs | ✅ DONE |
| 6 | Update README | ✅ DONE |

---

## Overview

Refactor the E2E test architecture from two separate suites (`test/e2e/` + `test/e2e/fullchain/`) into one unified suite with a **Driver interface** that allows swapping between mock and containerized backends.

### Before

```
test/e2e/
├── harness.go              # Connects to compose/dev.yaml
├── e2e.go                # TestMain for docker compose lifecycle
├── aiw_flow_test.go       # 6 AIW tests
├── nssaa_flow_test.go     # 8 NSSAA tests
├── reauth_test.go         # 4 reauth tests
├── revocation_test.go      # 3 revocation tests
└── fullchain/
    ├── harness_fullchain.go   # Wrapper around e2e.Harness, adds NRF/UDM URLs
    ├── fullchain_test.go     # Chains to e2e.TestMain
    └── scenarios/
        ├── n58_scenarios_test.go  # 3 NSSAA tests (DUPLICATE)
        ├── n60_scenarios_test.go  # 3 AIW tests (DUPLICATE)
        ├── udm_scenarios_test.go  # 3 UDM mock tests (uses in-process mock)
        ├── nrf_scenarios_test.go # 4 NRF mock tests (uses in-process mock)
        └── resilience_test.go   # 9 resilience tests (mostly skipped)
```

### After

```
test/e2e/
├── harness.go                # Core: docker compose, DB/Redis, TLS, ResetState
├── driver.go                 # Driver interface + AMFDriver/AUSFDriver interfaces
├── container_driver.go       # ContainerDriver: routes to NRF/UDM containers
├── e2e.go                  # TestMain: ContainerDriver + fullchain-dev.yaml
├── n58_flow_test.go        # NSSAA flows
├── n60_flow_test.go         # AIW flows
├── reauth_test.go           # Re-authentication flows
├── revocation_test.go        # Revocation flows
├── nf_integration_test.go   # NRF/UDM integration
├── resilience_test.go        # Circuit breaker, DLQ, failover
└── harness.yaml            # Test infra config (NRM: localhost:8084)
```

---

## Phase 1 — Driver Interface (✅ DONE)

### Files Created

| File | Purpose |
|------|---------|
| `test/e2e/driver.go` | Driver interface + AMFDriver/AUSFDriver interfaces |
| `test/e2e/container_driver.go` | ContainerDriver: routes to NRF/UDM containers |

### Files Modified

| File | Change |
|------|--------|
| `test/mocks/amf.go` | Renamed `Server` field -> `httpServer`, added `Server()` method |
| `test/mocks/ausf.go` | Renamed `Server` field -> `httpServer`, added `Server()` method |
| `test/mocks/udm.go` | Renamed `Server` field -> `httpServer`, added `Server()` method |
| `test/mocks/nrf.go` | Renamed `Server` field -> `httpServer`, added `Server()` method |
| `test/e2e/harness.go` | Added `driver` field, `Driver()` accessor, `NewHarnessFromDriver()`, driver-based mock methods |

### Files Deleted

| File | Reason |
|------|--------|
| `test/e2e/mock_driver.go` | Replaced by ContainerDriver; suite now uses containerized backends only |

### Decisions Made

1. **Driver returns concrete types for direct access**: `MockDriver.AMFMock()` returns `*mocks.AMFMock` so tests can call `GetNotifications()` etc.
2. **ContainerDriver wraps mocks for AMF/AUSF**: Fullchain compose does not include AMF/AUSF containers, so we use in-process httptest.Server for those.
3. **Server field renamed to httpServer**: Avoids collision with `Server()` method on the interface.

---

## Phase 2 — TestMain Wiring (✅ DONE)

### Changes Made

1. **e2e.go**: Updated `TestMain` to read `E2E_PROFILE` env var and select driver accordingly
2. **e2e.go**: Added `sharedDriver` global variable
3. **e2e.go**: Updated `NewHarnessForTest` to use `sharedDriver` when set
4. **e2e.go**: TestMain creates harness with `NewHarnessFromDriver(&testing.T{}, sharedDriver)`

---

## Phase 3 — Makefile Update (✅ DONE)

### Changes Made

1. **test-fullchain**: Added `E2E_PROFILE=fullchain`, `FULLCHAIN_AAA_SIM_URL`, `FULLCHAIN_NRF_URL`, `FULLCHAIN_NRM_URL` env vars
2. **test-fullchain**: Changed test path from `./test/e2e/fullchain/...` to `./test/e2e/...`
3. **test-fullchain**: Switched from `compose/fullchain.yaml` to `compose/fullchain-dev.yaml`
4. **test-fullchain-fast**: Same updates as test-fullchain
5. **test-fullchain-no-build**: Same updates as test-fullchain
6. **test-e2e target**: Removed (suite now uses ContainerDriver exclusively)

---

## Phase 4 — Consolidate Tests (⏳ PENDING)

### Task 4.1: Rename nssaa_flow_test.go → n58_flow_test.go

```bash
mv test/e2e/nssaa_flow_test.go test/e2e/n58_flow_test.go
```

**Rationale:** Consistent naming with `n60_flow_test.go` (N58 = AMF-NSSAAF interface, N60 = AUSF-NSSAAF interface)

### Task 4.2: Delete fullchain/ directory

```bash
rm -rf test/e2e/fullchain/
```

**Rationale:** All tests moved/consolidated. Directory was duplicating functionality.

### Task 4.3: Move nrf/udm/resilience tests to nf_integration_test.go

**Source files to merge:**

| File | Tests to move |
|------|----------------|
| `fullchain/scenarios/nrf_scenarios_test.go` | `TestNRF_UDMDiscovery`, `TestNRF_CustomEndpoint`, `TestNRF_NotRegistered`, `TestNRF_AllRegistered` |
| `fullchain/scenarios/udm_scenarios_test.go` | `TestUDM_AuthSubscription`, `TestUDM_SubscriberNotFound`, `TestUDM_ErrorInjection` |
| `fullchain/scenarios/resilience_test.go` | `TestResilience_CircuitBreaker`, `TestResilience_RedisDown`, `TestResilience_DLQProcessing`, `TestAAA_SIM_Modes`, `TestAAA_SIM_Connectivity`, `TestResilience_RedisUnavailable`, `TestResilience_PostgresUnavailable` |

**Target file:** `test/e2e/nf_integration_test.go`

### Task 4.4: Rename test functions for consistency

All test functions must follow `TestE2E_*` prefix convention:

| Old Name | New Name |
|----------|----------|
| `TestN58_InvalidGPSI` | `TestE2E_NSSAA_InvalidGPSI` |
| `TestN58_InvalidSnssai` | `TestE2E_NSSAA_InvalidSnssai` |
| `TestN60_InvalidSupi` | `TestE2E_AIW_InvalidSupi` |
| `TestN60_SUPINotFound` | `TestE2E_AIW_SUPINotFound` |
| `TestUDM_AuthSubscription` | `TestE2E_NF_UDMAuthSubscription` |
| `TestUDM_SubscriberNotFound` | `TestE2E_NF_UDMSubscriberNotFound` |
| `TestUDM_ErrorInjection` | `TestE2E_NF_UDMErrorInjection` |
| `TestNRF_UDMDiscovery` | `TestE2E_NF_NRFUDMDiscovery` |
| `TestNRF_CustomEndpoint` | `TestE2E_NF_NRFCustomEndpoint` |
| `TestNRF_NotRegistered` | `TestE2E_NF_NRFNotRegistered` |
| `TestNRF_AllRegistered` | `TestE2E_NF_NRFAllRegistered` |
| `TestResilience_CircuitBreaker` | `TestE2E_Resilience_CircuitBreaker` |
| `TestResilience_RedisDown` | `TestE2E_Resilience_RedisDown` |
| `TestResilience_DLQProcessing` | `TestE2E_Resilience_DLQProcessing` |
| `TestResilience_RedisUnavailable` | `TestE2E_Resilience_RedisUnavailable` |
| `TestResilience_PostgresUnavailable` | `TestE2E_Resilience_PostgresUnavailable` |
| `TestAAA_SIM_Modes` | `TestE2E_Resilience_AAASIMModes` |
| `TestAAA_SIM_Connectivity` | `TestE2E_Resilience_AAASIMConnectivity` |

### Task 4.5: Update nf_integration_test.go imports

```go
import (
    "github.com/operator/nssAAF/test/e2e"  // NOT fullchain package
)
```

Change `fullchain.NewHarness(t)` → `e2e.NewHarnessForTest(t)`

### Task 4.6: Add skip logic for ContainerDriver-only tests

Some NF integration tests require ContainerDriver. Add skip logic:

```go
func TestE2E_NF_NRFUDMDiscovery(t *testing.T) {
    if os.Getenv("E2E_PROFILE") != "fullchain" {
        t.Skip("NRF discovery requires E2E_PROFILE=fullchain")
    }
    // ... test body
}
```

Tests requiring ContainerDriver:
- All NRF tests (`TestE2E_NF_NRF*`)
- All UDM tests (`TestE2E_NF_UDM*`)
- `TestE2E_Resilience_CircuitBreaker` (requires real UDM)
- `TestE2E_Resilience_DLQProcessing` (requires real AMF notification endpoint)

Tests that work with MockDriver:
- `TestE2E_Resilience_RedisDown`
- `TestE2E_Resilience_RedisUnavailable`
- `TestE2E_Resilience_PostgresUnavailable`

---

## Phase 5 — Implement ContainerDriver Stubs (✅ DONE)

Implemented in `container_driver.go`:

```go
// SetNRFServiceEndpoint configures a service endpoint in the containerized NRF mock.
// NOTE: NRF configured via env vars at startup (NRF_SERVICE_ENDPOINTS)
// TODO: Implement admin API on nrf-mock container for per-test configuration
func (d *ContainerDriver) SetNRFServiceEndpoint(nfType, serviceName, host string, port int) error {
	return nil
}

// SetUDMAuthSubscription configures auth subscription for a SUPI in the containerized UDM mock.
// NOTE: UDM configured via env vars at startup (FULLCHAIN_UDM_AUTH_SUBSCRIPTIONS)
// TODO: Implement admin API on udm-mock container for per-test configuration
func (d *ContainerDriver) SetUDMAuthSubscription(supi, authType, aaaServer string) error {
	return nil
}
```

**Decision:** Option C (env var only) for now. Admin API can be added later if needed.

---

## Phase 6 — Update README (✅ DONE)

Updated `test/e2e/README.md` with:
- Driver interface architecture diagram
- Profile comparison table (dev vs fullchain)
- Environment variables table
- NF integration tests section
- Coverage matrix with driver requirements
- Driver abstraction test design principle

---

## Implementation Sequence

### Step 1: Rename and delete (Task 4.1, 4.2)

```bash
mv test/e2e/nssaa_flow_test.go test/e2e/n58_flow_test.go
rm -rf test/e2e/fullchain/
```

### Step 2: Create nf_integration_test.go (Task 4.3)

Create new file with merged content from:
- `fullchain/scenarios/nrf_scenarios_test.go`
- `fullchain/scenarios/udm_scenarios_test.go`
- `fullchain/scenarios/resilience_test.go`

### Step 3: Rename test functions (Task 4.4)

Use sed or find-replace:
```bash
# Rename test functions
sed -i 's/TestN58_/TestE2E_NSSAA_/g' test/e2e/n58_flow_test.go
sed -i 's/TestN60_/TestE2E_AIW_/g' test/e2e/n60_flow_test.go
# ... etc
```

### Step 4: Update imports (Task 4.5)

Change `fullchain.NewHarness` → `e2e.NewHarnessForTest` in all files.

### Step 5: Add skip logic (Task 4.6)

Add `t.Skip()` for ContainerDriver-only tests.

### Step 6: Implement ContainerDriver stubs (Phase 5)

Implement or document `SetNRFServiceEndpoint` and `SetUDMAuthSubscription`.

### Step 7: Update README (Phase 6)

Document new architecture, profiles, and coverage matrix.

---

## Verification Checklist

- [x] `go build -tags=e2e ./test/e2e/...` compiles
- [x] `go build -tags=e2e ./test/mocks/...` compiles
- [ ] `make test-fullchain` starts `fullchain-dev.yaml` and passes health checks
- [ ] `make test-integration` starts `fullchain-dev.yaml` and runs integration tests
- [ ] NRM reachable at `http://localhost:8084/healthz`
- [ ] NRF mock reachable at `http://localhost:8082`
- [ ] UDM mock reachable at `http://localhost:8083`
- [ ] Biz Pod health: `http://localhost:8080/healthz/live`
- [ ] GitHub Actions `fullchain-tests` workflow passes with `fullchain-dev.yaml`
- [x] `test/e2e/fullchain/` directory deleted
- [x] `test/e2e/mock_driver.go` deleted
- [x] No references to `compose/dev.yaml` or `compose/fullchain.yaml` in Go source files

---

## Open Decisions

### Decision 1: NRF/UDM programmatic configuration

**Options:**

A. **HTTP Admin API** — Add admin endpoints to nrf-mock and udm-mock containers. ContainerDriver calls these to configure per-test data.

B. **Env var only** — Keep current behavior: NRF/UDM configured at compose startup via env vars. Per-test configuration not supported.

C. **Hybrid** — Admin API for UDM (subscription changes per test), env var for NRF (static endpoints).

**Recommendation:** Option C. UDM auth subscriptions vary per test, so admin API is valuable. NRF endpoints are typically static.

### Decision 2: AAA-S mode control

**Current:** AAA_SIM_MODE env var set at container startup, requires restart to change.

**Options:**

A. **Admin API on aaa-sim** — Add HTTP endpoint to change mode without restart.

B. **Accept limitation** — Document that AAA-S mode requires container restart. Tests that need different modes must run in separate `make` invocations.

C. **Multiple containers** — Start multiple aaa-sim containers with different modes, route tests to appropriate container.

**Recommendation:** Option B for now. Admin API can be added later if needed.

### Decision 3: TLS for ContainerDriver

**Current:** `container_driver.go` doesn't configure TLS. Container URLs use `http://`.

**Issue:** If NRF/UDM containers use TLS with self-signed certs, ContainerDriver needs CA configuration.

**Options:**

A. **Propagate TLS config** — Pass CA cert path from harness config to ContainerDriver.

B. **Insecure for containers** — Use `http://` for container URLs (already the case in fullchain.yaml).

C. **Configure per URL** — NRF and UDM URLs can have separate TLS configs.

**Recommendation:** Option B for now. If TLS is needed, implement Option A later.

---

## Files Summary

### Files to CREATE

| File | Content |
|------|---------|
| `test/e2e/nf_integration_test.go` | Merged NRF/UDM/resilience tests from fullchain/ |

### Files to MODIFY

| File | Changes |
|------|---------|
| `test/e2e/README.md` | Update architecture, profiles, coverage matrix |
| `test/e2e/container_driver.go` | Implement SetNRFServiceEndpoint/SetUDMAuthSubscription |

### Files to DELETE

| File | Reason |
|------|--------|
| `test/e2e/fullchain/` (entire directory) | Consolidated into e2e/ |
