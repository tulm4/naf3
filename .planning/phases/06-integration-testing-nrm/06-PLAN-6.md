---
phase: 06
plan: 06-PLAN-6
wave: 6
depends_on: [06-PLAN-3, 06-PLAN-4, 06-PLAN-5]
requirements: [REQ-27, REQ-28, REQ-29]
files_modified:
  - compose/test.yaml
  - compose/configs/biz-e2e.yaml
  - compose/configs/http-gateway-e2e.yaml
  - compose/configs/aaa-gateway-e2e.yaml
  - test/e2e/harness.go
  - test/mocks/compose.go
  - Makefile
  - internal/auth/middleware.go
  - compose/configs/http-gateway.yaml
  - internal/api/common/validator.go
  - internal/api/nssaa/handler.go
  - test/unit/api/nssaa_handler_gaps_test.go
  - test/integration/auth_test.go
  - test/conformance/ts29526_test.go
---

<objective>

Fix three remaining gaps from Phase 6 UAT: (1) remove obsolete separate compose config files and align with D-12 single-dev-yaml decision, (2) add HTTP Gateway auth bypass for E2E tests, and (3) validate empty S-NSSAI objects return HTTP 400 per TS 29.526, (4) add layered Makefile test targets per D-16 decision.

</objective>

<tasks>

## Task 1 — Compose Layout Cleanup (D-11 + D-12)

<read_first>
- `test/e2e/harness.go` — existing harness (currently references `compose/test.yaml`)
- `test/mocks/compose.go` — compose lifecycle helpers (has V1 docker-compose invocations at lines 71, 93, 126, 190)
- `compose/dev.yaml` — current infra compose (postgres, redis, mock-aaa-s, binary services)
- `Makefile` — existing build/test targets
- `06-CONTEXT.md` — D-11: `docker compose` V2, D-12: single `dev.yaml`, D-14: env vars
</read_first>

<action>
**Step 1 — Delete obsolete compose files** (per D-12):
Delete the following files — they are replaced by the env-var approach per D-14:
- `compose/test.yaml` — remove; test infra uses `compose/dev.yaml` directly
- `compose/configs/biz-e2e.yaml` — remove; env vars in harness override dev config values
- `compose/configs/http-gateway-e2e.yaml` — remove; same rationale
- `compose/configs/aaa-gateway-e2e.yaml` — remove; same rationale

**Step 2 — Update all docker-compose V1 invocations to `docker compose` V2** (per D-11):

There are TWO files with V1 invocations that must both be updated:

**File A: `test/e2e/harness.go`**
Update all `exec.CommandContext(ctx, "docker-compose", ...)` to:
1. Check docker compose V2 is available:
```go
if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
    t.Skip("docker compose V2 not available")
}
```
2. Change all `"docker-compose"` to `"docker", "compose"` in exec calls.
3. Remove `-f compose/test.yaml` from all docker compose commands.

**File B: `test/mocks/compose.go`** — update these 4 locations:

In `ComposeUp` (line 71), change:
```go
// Before:
cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "up", "-d")
// After:
cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d")
```

In `ComposeDown` (line 93), change:
```go
// Before:
cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "down", "--remove-orphans")
// After:
cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "down", "--remove-orphans")
```

In `checkComposeHealth` (line 126), change:
```go
// Before:
cmd := exec.Command("docker-compose", "-f", composeFile, "ps", "--format", "json")
// After:
cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "json")
```

In `GetServiceAddr` (line 190), change:
```go
// Before:
cmd := exec.Command("docker-compose", "-f", composeFile, "port", service, "0")
// After:
cmd := exec.Command("docker", "compose", "-f", composeFile, "port", service, "0")
```

Also update error messages that reference `docker-compose` to say `docker compose` in the same file.

**Step 3 — Update Makefile references**:
In `Makefile`, find any targets referencing `docker-compose` or `compose/test.yaml`. Update to:
- Use `docker compose` (not `docker-compose`)
- Remove references to `compose/test.yaml`
- Test targets use `compose/dev.yaml` only

**Step 4 — Verify no V1 invocations remain**:
```bash
grep -r "docker-compose" test/e2e/ test/mocks/ 2>/dev/null && echo "V1 INVOCATIONS FOUND" || echo "ALL CLEAN"
```

**Step 5 — Verify infra still works**:
```bash
docker compose -f compose/dev.yaml version
docker compose -f compose/dev.yaml config --quiet && echo "dev.yaml valid"
```
</action>

<acceptance_criteria>
- `compose/test.yaml` does not exist
- `compose/configs/biz-e2e.yaml` does not exist
- `compose/configs/http-gateway-e2e.yaml` does not exist
- `compose/configs/aaa-gateway-e2e.yaml` does not exist
- `test/e2e/harness.go` uses `docker compose` (not `docker-compose`) and only `-f compose/dev.yaml`
- `test/mocks/compose.go` uses `docker compose` (not `docker-compose`) in all 4 invocations
- `Makefile` has no references to `docker-compose` or `compose/test.yaml`
- `grep -r "docker-compose" test/e2e/ test/mocks/` returns no results
- `go build ./...` compiles after changes
</acceptance_criteria>

---

## Task 2 — HTTP Gateway Auth Bypass for E2E Tests (Gap E2E-02)

<read_first>
- `internal/auth/middleware.go` — existing auth middleware (JWT validation)
- `compose/configs/http-gateway.yaml` — HTTP Gateway config (dev settings)
- `test/e2e/harness.go` — harness (starts HTTP Gateway binary with env vars)
- `docs/design/15_sbi_security.md` — security design
</read_first>

<action>
**Step 1 — Read existing middleware implementation**:
First, read `internal/auth/middleware.go` to understand the current middleware structure before making changes.

**Step 2 — Add AuthConfig.Disabled field**:
In `internal/auth/middleware.go`, add to the `Config` struct:
```go
type Config struct {
    // ... existing fields ...
    Disabled bool `yaml:"disabled"`
}
```

**Step 3 — Wire bypass into middleware**:
In `internal/auth/middleware.go`, modify the middleware function to check `cfg.Disabled`:
```go
// NewAuthMiddleware returns middleware that validates Bearer tokens.
// If cfg.Disabled is true (or NAF3_AUTH_DISABLED=1 env var), validation is skipped.
func NewAuthMiddleware(cfg Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if cfg.Disabled || os.Getenv("NAF3_AUTH_DISABLED") == "1" {
                slog.Debug("auth: bypassed (E2E mode)")
                next.ServeHTTP(w, r)
                return
            }
            // ... existing JWT validation ...
        })
    }
}
```

**Step 4 — Update http-gateway.yaml config**:
In `compose/configs/http-gateway.yaml`, add:
```yaml
# Auth bypass for E2E tests: set auth.disabled=true or pass NAF3_AUTH_DISABLED=1.
# NEVER enable in production.
auth:
  disabled: false
```

**Step 5 — Update harness to pass NAF3_AUTH_DISABLED**:
In `test/e2e/harness.go`, when starting the HTTP Gateway binary, add to the env vars:
```go
gwEnv = append(gwEnv,
    "NAF3_AUTH_DISABLED=1",               // E2E mode: skip JWT validation
    "NAF3_AUTH_JWT_SECRET=nssaa-test-secret", // test secret (if needed)
)
```

**Step 6 — Add integration test verifying auth bypass**:
In `test/integration/auth_test.go` (or create if not exists), add:
```go
// TestHTTPGateway_AuthBypass verifies requests succeed without JWT when auth is disabled.
func TestHTTPGateway_AuthBypass(t *testing.T) {
    if testing.Short() {
        t.Skip("requires full stack")
    }

    // Start HTTP Gateway with NAF3_AUTH_DISABLED=1
    // Using the existing integration test infra pattern
    bizURL, gwURL, teardown := startAuthDisabledStack(t)
    defer teardown()

    // POST without Authorization header — should succeed (not 401)
    body := `{"gpsi":"504217500001","snssai":{"sst":1,"sd":"000001"},"supi":"imsi-208930000000001","supiKind":"SUCI"}`
    req, _ := http.NewRequest(http.MethodPost,
        gwURL+"/nnssaaf-nssaa/v1/slice-authentications",
        strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    // Auth bypass working if not 401
    require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
        "HTTP Gateway returned 401 — auth bypass not working. Check NAF3_AUTH_DISABLED.")
}
```

**Step 7 — Verify build**:
```bash
go build ./cmd/http-gateway/
```
</action>

<acceptance_criteria>
- `internal/auth/middleware.go` checks `cfg.Disabled` or `NAF3_AUTH_DISABLED=1` and bypasses if set
- `test/e2e/harness.go` sets `NAF3_AUTH_DISABLED=1` for HTTP Gateway
- `TestHTTPGateway_AuthBypass` integration test exists with assertions
- `go build ./cmd/http-gateway/` compiles without errors
</acceptance_criteria>

---

## Task 3 — Empty S-NSSAI Validation (Gap E2E-01)

<read_first>
- `internal/api/common/validator.go` — `ValidateSnssai` function (lines 51-69)
- `internal/api/nssaa/handler.go` — `CreateSliceAuthenticationContext` handler (lines 158-202)
- `test/unit/api/nssaa_handler_gaps_test.go` — existing gap test patterns
- `docs/design/02_nssaa_api.md` — TS 29.526 Snssai requirements
</read_first>

<action>
**Step 1 — Fix ValidateSnssai to reject empty objects**:
In `internal/api/common/validator.go`, after the existing `missing` check (around line 57), insert:

```go
// Reject explicitly empty Snssai: both sst=0 and sd="" means empty object {} was sent.
// This is different from missing (snssai not present at all).
if !missing && sst == 0 && sd == "" {
    return ValidationProblem("snssai", "snssai.sst or snssai.sd must be provided")
}
```

Insert between the `if missing {` block and `if sst < 0 || sst > 255 {` block.

The existing `missing` check handles the case where `snssai` field is absent entirely.
The new check handles the case where `snssai: {}` (empty object) is sent.

**Step 2 — Verify ConfirmSliceAuthenticationContext Snssai validation**:
Run:
```bash
grep -n "ValidateSnssai\|snssai" internal/api/nssaa/handler.go | grep -i confirm
```
If no Snssai validation is found in the PUT handler, add:
```go
// Reject empty snssai object in PUT
if body.Snssai != nil && body.Snssai.Sst == 0 && body.Snssai.Sd == "" {
    common.WriteProblem(w, common.ValidationProblem("snssai", "snssai.sst or snssai.sd must be provided"))
    return
}
```

**Step 3 — Add unit tests**:
In `test/unit/api/nssaa_handler_gaps_test.go`, add:

```go
func TestCreateSliceAuth_EmptySnssai(t *testing.T) {
    handler := nssaa.NewHandler(mockStore{}, nssaa.WithAAA(mockAAA{}))

    // snssai: {} — empty object should be rejected with 400
    body := `{"gpsi":"504217500001","snssai":{},"supi":"imsi-208930000000001","supiKind":"SUCI","eapIdRsp":"dGVzdA=="}`
    req := httptest.NewRequest(http.MethodPost,
        "/nnssaaf-nssaa/v1/slice-authentications",
        strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusBadRequest, rr.Code,
        "empty snssai {} should return 400, got %d: %s", rr.Code, rr.Body.String())
    require.Contains(t, rr.Body.String(), "snssai",
        "error should mention snssai field")
}

func TestCreateSliceAuth_MissingSnssai(t *testing.T) {
    handler := nssaa.NewHandler(mockStore{}, nssaa.WithAAA(mockAAA{}))

    // No snssai field at all — should also return 400
    body := `{"gpsi":"504217500001","supi":"imsi-208930000000001","supiKind":"SUCI","eapIdRsp":"dGVzdA=="}`
    req := httptest.NewRequest(http.MethodPost,
        "/nnssaaf-nssaa/v1/slice-authentications",
        strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusBadRequest, rr.Code,
        "missing snssai should return 400, got %d", rr.Code)
}
```

**Step 4 — Add conformance test**:
In `test/conformance/ts29526_test.go`, add after the existing CreateSession tests:

```go
// TestTS29526_CreateSession_EmptySnssai verifies that an empty snssai object
// is rejected with HTTP 400 per TS 29.526 requirements.
// TC: snssai: {} → 400 Bad Request, cause=BAD_REQUEST
// Covers Gap E2E-01 fix.
func TestTS29526_CreateSession_EmptySnssai(t *testing.T) {
    // Use existing test setup pattern from ts29526_test.go
    handler := nssaa.NewHandler(/* ... same deps as other tests ... */)

    // TC: Empty snssai object → 400 Bad Request
    body := map[string]interface{}{
        "gpsi":    "504217500001",
        "snssai":  map[string]interface{}{}, // empty object
        "supi":    "imsi-208930000000001",
        "supiKind": "SUCI",
        "eapIdRsp": base64.StdEncoding.EncodeToString([]byte("test")),
    }
    bodyBytes, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost,
        "/nnssaaf-nssaa/v1/slice-authentications",
        bytes.NewReader(bodyBytes))
    req.Header.Set("Content-Type", "application/json")

    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusBadRequest, rr.Code,
        "empty snssai object should return 400, got %d", rr.Code)
    assert.Contains(t, rr.Body.String(), "snssai",
        "error response should mention snssai field")
}
```

**Step 5 — Verify fixes**:
```bash
go test ./test/unit/api/... -v -run "EmptySnssai|MissingSnssai"
go test ./test/conformance/... -v -run "EmptySnssai"
go test ./test/unit/api/... -v -run "Snssai" -count=1
```
</action>

<acceptance_criteria>
- `internal/api/common/validator.go` contains new check: `if !missing && sst == 0 && sd == ""`
- `TestCreateSliceAuth_EmptySnssai` exists in `test/unit/api/nssaa_handler_gaps_test.go` with assertions
- `TestTS29526_CreateSession_EmptySnssai` exists in `test/conformance/ts29526_test.go` with assertion body
- `go test ./test/unit/api/... -v -run "EmptySnssai"` passes
- `go test ./test/conformance/... -v -run "EmptySnssai"` passes
- `go build ./...` compiles without errors
</acceptance_criteria>

</tasks>

---

## Task 4 — Makefile Test Targets (D-16)

<read_first>
- `Makefile` — existing build/test targets (already read)
- `compose/dev.yaml` — infra compose (postgres, redis, mock-aaa-s)
- `test/integration/integration.go` — integration test harness
- `test/e2e/harness.go` — E2E test harness (already read above)
- `docs/roadmap/PHASE_*.md` — Phase 6 context
</read_first>

<action>
The user selected **"Separate targets"** for makefile test organization: each test layer (`test-unit`, `test-integration`, `test-e2e`, `test-conformance`) manages its own infra lifecycle. The existing `make test` target is renamed/repurposed. `test-all` runs all four in sequence.

**Step 1 — Add test-unit target**:
In `Makefile`, add after the existing `test` section (line 115):

```makefile
# =============================================================================
# Layered test targets
# Each target manages its own infra lifecycle independently.
# Spec: D-16 decision — "Separate targets" per test layer.
# =============================================================================

.PHONY: test-unit
test-unit: ## Run unit tests only (fast, no infra required)
	@echo "$(YELLOW)Running unit tests...$(NC)"
	$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./test/unit/...

.PHONY: test-integration
test-integration: ## Run integration tests against real PostgreSQL and Redis via docker compose
	@echo "$(YELLOW)Starting test infrastructure...$(NC)"
	docker compose -f compose/dev.yaml up -d --quiet-pull
	@echo "$(YELLOW)Waiting for infrastructure to be healthy...$(NC)"
	@sleep 5
	@$(GOTEST) -race -v ./test/integration/... || { docker compose -f compose/dev.yaml down; exit 1; }
	@echo "$(YELLOW)Tearing down test infrastructure...$(NC)"
	docker compose -f compose/dev.yaml down
	@echo "$(GREEN)Integration tests complete$(NC)"

.PHONY: test-e2e
test-e2e: ## Run E2E tests — full stack: docker compose infra + binary processes
	@echo "$(YELLOW)Running E2E tests (full stack)...$(NC)"
	$(GOTEST) -v ./test/e2e/...

.PHONY: test-conformance
test-conformance: ## Run 3GPP conformance tests against live services
	@echo "$(YELLOW)Running conformance tests...$(NC)"
	$(GOTEST) -race -v ./test/conformance/...

.PHONY: test-all
test-all: test-unit test-integration test-e2e test-conformance ## Run all test layers in sequence
	@echo "$(GREEN)All tests passed$(NC)"
```

**Step 2 — Update existing `test` target description**:
Change the existing `test:` target (line 116) to clarify it is an alias for `test-unit`:
```makefile
.PHONY: test
test: test-unit ## Run unit tests (alias for test-unit)
```

**Step 3 — Verify no V1 `docker-compose` invocations** (belt-and-suspenders):
```bash
grep -rn "docker-compose" Makefile 2>/dev/null && echo "V1 FOUND" || echo "CLEAN"
```

**Step 4 — Verify all targets parse correctly**:
```bash
make -n test-unit && echo "test-unit: OK"
make -n test-integration && echo "test-integration: OK"
make -n test-e2e && echo "test-e2e: OK"
make -n test-conformance && echo "test-conformance: OK"
make -n test-all && echo "test-all: OK"
```

**Step 5 — Verify test directories exist**:
```bash
test -d test/unit && echo "unit: OK"
test -d test/integration && echo "integration: OK"
test -d test/e2e && echo "e2e: OK"
test -d test/conformance && echo "conformance: OK"
```
</action>

<acceptance_criteria>
- `Makefile` contains `test-unit`, `test-integration`, `test-e2e`, `test-conformance`, `test-all` targets
- `test-unit` runs only `./test/unit/...`
- `test-integration` runs `docker compose up` before tests and `docker compose down` after
- `test-e2e` runs only `./test/e2e/...` (harness manages its own lifecycle)
- `test-conformance` runs only `./test/conformance/...`
- `test-all` runs all four in sequence
- `test` is an alias for `test-unit` with updated description
- `make -n` dry-run for all five targets parses without errors
- `grep "docker-compose" Makefile` returns no results
</acceptance_criteria>

</verification>

```bash
# Compose cleanup
test ! -f compose/test.yaml && echo "compose/test.yaml removed"
test ! -f compose/configs/biz-e2e.yaml && echo "biz-e2e.yaml removed"
test ! -f compose/configs/http-gateway-e2e.yaml && echo "http-gateway-e2e.yaml removed"
test ! -f compose/configs/aaa-gateway-e2e.yaml && echo "aaa-gateway-e2e.yaml removed"
grep -r "docker-compose" test/e2e/ test/mocks/ 2>/dev/null && echo "V1 FOUND" || echo "ALL CLEAN"

# Auth bypass
go build ./cmd/http-gateway/
grep -q "NAF3_AUTH_DISABLED" test/e2e/harness.go && echo "auth disabled in harness"

# Empty Snssai
grep -q "!missing && sst == 0 && sd == \"\"" internal/api/common/validator.go && echo "empty snssai check added"
go test ./test/unit/api/... -v -run "EmptySnssai"
go test ./test/conformance/... -v -run "EmptySnssai"

# Overall
go build ./...
go test ./test/unit/... ./test/conformance/... -short
```

</verification>

<success_criteria>

- [ ] 4 obsolete compose files removed
- [ ] `test/e2e/harness.go` uses `docker compose` V2 and only `compose/dev.yaml`
- [ ] `test/mocks/compose.go` uses `docker compose` V2 in all 4 invocations
- [ ] `Makefile` has no references to `docker-compose` or `compose/test.yaml`
- [ ] HTTP Gateway auth bypass works via `NAF3_AUTH_DISABLED=1` env var
- [ ] `TestHTTPGateway_AuthBypass` integration test exists with assertions
- [ ] Empty `snssai: {}` returns HTTP 400 from POST handler
- [ ] `TestCreateSliceAuth_EmptySnssai` unit test exists and passes
- [ ] `TestTS29526_CreateSession_EmptySnssai` conformance test exists with assertion body
- [ ] `test-unit`, `test-integration`, `test-e2e`, `test-conformance`, `test-all` Makefile targets exist and parse
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./test/unit/... ./test/conformance/... -short` all pass

</success_criteria>
