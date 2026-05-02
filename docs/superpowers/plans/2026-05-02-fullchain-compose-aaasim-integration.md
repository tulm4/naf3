# E2E Fullchain Testing — Compose & AAA-SIM Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gaps between the existing `compose/dev.yaml` setup and the fullchain test plan's assumptions about `aaa-sim`, and resolve the overlap between `make test-fullchain` and `make test-e2e`.

**Architecture:**
- `compose/dev.yaml` already has a `mock-aaa-s` service built from `Dockerfile.mock-aaa-s`, which currently runs the `aaa-sim` binary.
- `cmd/aaa-sim/main.go` calls `aaa_sim.Run(mode, logger)` from `test/aaa_sim/` package.
- The current setup is correct — this plan adds clarity, environment-driven mode control, and resolves the `make test-e2e` vs `make test-fullchain` overlap.
- **Decision:** `test-fullchain` is a subset of `test-e2e`. Both use the same `compose/dev.yaml`. We consolidate: `test-fullchain` delegates to `test-e2e` instead of duplicating docker compose lifecycle.

**Tech Stack:** Go, docker-compose, `test/aaa_sim/` package (RADIUS + Diameter)

---

## Task 1: Audit `aaa-sim` Integration in `compose/dev.yaml`

**Files:**
- Read: `compose/dev.yaml:40-51`
- Read: `Dockerfile.mock-aaa-s`
- Read: `cmd/aaa-sim/main.go`
- Read: `test/aaa_sim/radius.go`
- Read: `test/aaa_sim/diameter.go`

- [ ] **Step 1: Read current mock-aaa-s service definition**

Read `compose/dev.yaml` lines 40-51 and `Dockerfile.mock-aaa-s`.

Expected findings:
- `mock-aaa-s` service is built from `Dockerfile.mock-aaa-s` in the project root
- It exposes RADIUS port `18120:1812` and Diameter port `38680:3868`
- No environment variables are passed to control mode (EAP_TLS_SUCCESS vs CHALLENGE)

- [ ] **Step 2: Read `aaa_sim.Run` signature**

Read `test/aaa_sim/radius.go` to find the `Run` function signature and how it starts listeners.

Expected: `func Run(mode Mode, logger *slog.Logger)` — takes mode and logger, internally creates UDP/TCP listeners.

- [ ] **Step 3: Add AAA_SIM_MODE env var to `compose/dev.yaml`**

Add environment section to the `mock-aaa-s` service in `compose/dev.yaml`:

```yaml
  mock-aaa-s:
    build:
      context: .
      dockerfile: Dockerfile.mock-aaa-s
    image: nssaaf-mock-aaa-s:latest
    ports: ["18120:1812", "38680:3868"]
    environment:
      # Mode: EAP_TLS_SUCCESS (default), EAP_TLS_CHALLENGE, EAP_TLS_FAILURE
      # Controlled by test scenarios via docker compose exec or restart
      AAA_SIM_MODE: "${AAA_SIM_MODE:-EAP_TLS_SUCCESS}"
    networks:
      - default
```

- [ ] **Step 4: Update `cmd/aaa-sim/main.go` to read AAA_SIM_MODE**

The current `cmd/aaa-sim/main.go` already reads `AAA_SIM_MODE` from env var (line 15). Verify this matches the `aaa_sim.Mode` type:

```go
// cmd/aaa-sim/main.go already reads AAA_SIM_MODE:
modeStr := os.Getenv("AAA_SIM_MODE")
if modeStr == "" {
    modeStr = "EAP_TLS_SUCCESS"
}
mode := aaa_sim.ParseMode(modeStr)
aaa_sim.Run(mode, logger)
```

This is already correct. No change needed.

- [ ] **Step 5: Verify `test/aaa_sim/diameter.go` exists and is functional**

Read `test/aaa_sim/diameter.go`. It should implement a Diameter server listening on port 3868.

Expected: `NewDiameterServer(addr string, mode Mode, secret []byte, logger *slog.Logger) *DiameterServer` and a `Run(ctx context.Context)` method.

- [ ] **Step 6: Commit**

```bash
git add compose/dev.yaml
git commit -m "test: add AAA_SIM_MODE env var to mock-aaa-s service"
```

---

## Task 2: Consolidate `test-fullchain` into `test-e2e`

**Files:**
- Read: `Makefile:174-188` (test-e2e)
- Read: `Makefile:196-210` (test-fullchain)
- Modify: `Makefile`

**Analysis:** Both `test-e2e` and `test-fullchain` use the same `compose/dev.yaml` and the same docker compose lifecycle (up → test → down). `test-fullchain` runs a subset of tests (`./test/e2e/fullchain/...`) while `test-e2e` runs all tests (`./test/e2e/...`).

**Decision:** `test-fullchain` should be a Make target that delegates to the existing `test-e2e` with filtered test paths, OR it should be removed entirely if `make test-e2e` already covers everything. Check if `test/e2e/` has other test files besides `fullchain/`:

- [ ] **Step 1: List all test packages in `test/e2e/`**

```bash
find test/e2e -name "*_test.go" | grep -v vendor | sort
```

Expected output:
```
test/e2e/fullchain/fullchain_test.go
test/e2e/fullchain/scenarios/resilience_test.go
test/e2e/fullchain/scenarios/n58_scenarios_test.go
test/e2e/fullchain/scenarios/n60_scenarios_test.go
test/e2e/fullchain/scenarios/nrf_scenarios_test.go
test/e2e/fullchain/scenarios/udm_scenarios_test.go
```

If there are no other test files outside `fullchain/`, the test suites are identical.

- [ ] **Step 2: Refactor `test-fullchain` to delegate to `test-e2e`**

Replace the `test-fullchain` target in `Makefile` with a thin alias that runs the same docker compose stack but with filtered test paths:

```makefile
.PHONY: test-fullchain
test-fullchain: ## Run fullchain E2E tests (subset of test-e2e targeting ./test/e2e/fullchain/...)
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
```

Note: The implementation is already correct in the Makefile. The key insight is that `test-fullchain` and `test-e2e` share the same infrastructure — they are NOT conflicting. Both start/stop the same `compose/dev.yaml`. They can run sequentially but NOT concurrently.

- [ ] **Step 3: Add comment explaining relationship**

Add to the `test-fullchain` target comment:

```makefile
.PHONY: test-fullchain
test-fullchain: gen-certs build ## Run E2E fullchain tests (subset of test-e2e targeting ./test/e2e/fullchain/...)
	# NOTE: Uses the same docker compose stack as test-e2e.
	# Do NOT run test-e2e and test-fullchain concurrently.
```

- [ ] **Step 4: Verify both targets work**

```bash
# Verify test-fullchain compiles
go build -tags=e2e ./test/e2e/fullchain/...

# Show that test-e2e and test-fullchain are compatible
grep -n "test-e2e\|test-fullchain" Makefile
```

Expected: Both targets exist, both reference `compose/dev.yaml`, `test-fullchain` runs `./test/e2e/fullchain/...` subset.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "docs: clarify test-fullchain as subset of test-e2e, no infrastructure conflict"
```

---

## Task 3: Document AAA-SIM Architecture in `test/aaa_sim/README.md`

**Files:**
- Create: `test/aaa_sim/README.md`
- Read: `test/aaa_sim/mode.go`

- [ ] **Step 1: Read mode.go to understand Mode enum**

Read `test/aaa_sim/mode.go`:

Expected content:
```go
type Mode int

const (
    ModeEAP_TLS_SUCCESS  Mode = iota
    ModeEAP_TLS_CHALLENGE
    ModeEAP_TLS_FAILURE
)
```

- [ ] **Step 2: Create `test/aaa_sim/README.md`**

```markdown
# AAA-SIM: AAA Server Simulator

AAA-SIM is a test simulator for the AAA server (RADIUS + Diameter) that the AAA Gateway connects to during E2E tests.

## Architecture

```
AAA Gateway (docker compose) → RADIUS/Diameter → mock-aaa-s (docker compose service)
                                              → aaa-sim (standalone binary, uses test/aaa_sim/)
```

## Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `EAP_TLS_SUCCESS` | Returns Access-Accept immediately | Happy path tests |
| `EAP_TLS_CHALLENGE` | Returns Access-Challenge, then Access-Accept on second request | Challenge flow tests |
| `EAP_TLS_FAILURE` | Returns Access-Reject | Negative path tests |

## Usage in Docker Compose

```yaml
mock-aaa-s:
  build:
    context: .
    dockerfile: Dockerfile.mock-aaa-s
  environment:
    AAA_SIM_MODE: "${AAA_SIM_MODE:-EAP_TLS_SUCCESS}"
  ports:
    - "18120:1812"   # RADIUS UDP
    - "38680:3868"   # Diameter TCP
```

## Usage as Standalone Binary

```bash
# Default mode (EAP_TLS_SUCCESS)
./aaa-sim

# Challenge mode
AAA_SIM_MODE=EAP_TLS_CHALLENGE ./aaa-sim

# Failure mode
AAA_SIM_MODE=EAP_TLS_FAILURE ./aaa-sim
```

## Integration with Fullchain Tests

Fullchain tests use `mock-aaa-s` via docker compose. The service is started by `make test-e2e` or `make test-fullchain`.

Test scenarios that need specific AAA-S modes can control the mode by:
1. Setting `AAA_SIM_MODE` env var before `docker compose up`
2. Using `docker compose exec mock-aaa-s bash` to inspect logs
3. For advanced scenarios: running a separate `aaa-sim` binary alongside the compose stack

## Files

- `radius.go` — RFC 2865 RADIUS server (UDP, port 1812)
- `diameter.go` — RFC 6733 Diameter server (TCP, port 3868)
- `mode.go` — Mode enum and parsing
- `cmd/aaa-sim/main.go` — Standalone binary entry point
```

- [ ] **Step 3: Commit**

```bash
git add test/aaa_sim/README.md
git commit -m "docs: add AAA-SIM architecture documentation"
```

---

## Task 4: Add AAA-SIM Mode Control to Fullchain Harness

**Files:**
- Modify: `test/e2e/fullchain/harness_fullchain.go`

- [ ] **Step 1: Add SetAAASimMode to Harness**

The harness should allow tests to control the AAA-SIM mode. Add this to `Harness` struct and `NewHarness`:

```go
// Harness extends e2e.Harness with NRF/UDM mock integration and AAA-SIM control.
type Harness struct {
    *e2e.Harness
    NRFMock    *mocks.NRFMock
    UDMMock    *mocks.UDMMock
    aaasSimMode string  // "EAP_TLS_SUCCESS", "EAP_TLS_CHALLENGE", "EAP_TLS_FAILURE"
}
```

- [ ] **Step 2: Add SetAAASimMode method**

```go
// SetAAASimMode configures the AAA-SIM mode for subsequent tests.
// This is stored on the harness and can be used to configure mock-aaa-s service
// or a separate aaa-sim binary started alongside the compose stack.
func (h *Harness) SetAAASimMode(mode string) {
    h.aaasSimMode = mode
}
```

- [ ] **Step 3: Update ResetState to include AAA-SIM mode**

In `ResetState()`, add a comment noting that AAA-SIM mode can be controlled per-test:

```go
// ResetState clears state for both infrastructure and mocks.
// Note: AAA-SIM mode is controlled via SetAAASimMode() before ResetState().
// For challenge mode tests, call h.SetAAASimMode("EAP_TLS_CHALLENGE") first.
```

- [ ] **Step 4: Commit**

```bash
git add test/e2e/fullchain/harness_fullchain.go
git commit -m "test: add SetAAASimMode to fullchain harness for mode control"
```

---

## Verification Checklist

- [ ] `docker compose -f compose/dev.yaml config` — valid YAML, no errors
- [ ] `make test-fullchain` builds and shows correct test output (skips if no docker)
- [ ] `grep AAA_SIM_MODE compose/dev.yaml` — env var is defined
- [ ] `test/aaa_sim/README.md` exists and documents all 3 modes
- [ ] `go build ./test/e2e/fullchain/...` — compiles without errors
- [ ] `go test ./test/mocks/... -v` — 2/2 pass
- [ ] `golangci-lint run ./test/e2e/fullchain/... ./test/mocks/...` — no new warnings

---

## Self-Review Checklist

**Spec coverage:**
- [x] `aaa-sim` integration gap: documented, env var added, README created
- [x] `make test-fullchain` vs `make test-e2e` overlap: resolved, consolidated, documented

**Placeholder scan:**
- All steps contain actual code examples
- No "TBD" or "TODO" markers
- All file paths are exact
- All commands have expected output

**Type consistency:**
- `Harness.SetAAASimMode(mode string)` — consistent with existing `SetAuthSubscription` pattern
- `AAA_SIM_MODE` env var — consistent naming with `E2E_TLS_CA`, `BIZ_PG_URL` patterns

**Architecture decisions:**
- `test-fullchain` is a subset of `test-e2e`, NOT a separate stack
- Both share `compose/dev.yaml` — cannot run concurrently but can run sequentially
- `aaa-sim` is already integrated via `mock-aaa-s` service; this plan adds mode control and documentation
