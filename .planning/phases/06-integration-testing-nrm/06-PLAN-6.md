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
  - internal/api/common/validator.go
  - internal/api/nssaa/handler.go
  - test/unit/api/nssaa_handler_gaps_test.go
  - test/conformance/ts29526_test.go
---

<objective>

Fix three remaining gaps from Phase 6 UAT: (1) remove obsolete separate compose config files and align with D-12 single-dev-yaml decision, (2) add HTTP Gateway auth bypass for E2E tests, and (3) validate empty S-NSSAI objects return HTTP 400 per TS 29.526.

</objective>

<tasks>

## Task 1 — Compose Layout Cleanup (D-11 + D-12)

<read_first>
- `test/e2e/harness.go` — existing harness (currently references `compose/test.yaml`)
- `compose/dev.yaml` — current infra compose (postgres, redis, mock-aaa-s, binary services)
- `Makefile` — existing build/test targets (lines ~220-260)
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

**File A: `test/e2e/harness.go`** — update all `exec.CommandContext(ctx, "docker-compose", ...)` to:
```go
// Verify docker compose V2 is available
if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
    t.Skip("docker compose V2 not available")
}
exec.CommandContext(ctx, "docker", "compose", args...)
```
Remove `-f compose/test.yaml` from all docker compose commands.

**File B: `test/mocks/compose.go`** — update these 5 V1 invocations (lines 71, 93, 126, 190):

In `ComposeUp` (line 71):
```go
// Before:
cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "up", "-d")
// After:
cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d")
```

In `ComposeDown` (line 93):
```go
// Before:
cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "down", "--remove-orphans")
// After:
cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "down", "--remove-orphans")
```

In `checkComposeHealth` (line 126):
```go
// Before:
cmd := exec.Command("docker-compose", "-f", composeFile, "ps", "--format", "json")
// After:
cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "json")
```

In `GetServiceAddr` (line 190):
```go
// Before:
cmd := exec.Command("docker-compose", "-f", composeFile, "port", service, "0")
// After:
cmd := exec.Command("docker", "compose", "-f", composeFile, "port", service, "0")
```

Also update all error messages from `docker-compose` to `docker compose` in the same file.

**Step 3 — Update Makefile references**:
In `Makefile`, check for any targets referencing `docker-compose` or `compose/test.yaml`. Update to:
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
- `test/mocks/compose.go` uses `docker compose` (not `docker-compose`) in all 5 invocations
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
**Step 1 — Add AUTH_DISABLED env var to HTTP Gateway config**:
In `compose/configs/http-gateway.yaml`, add:
```yaml
auth:
  disabled: false  # Set to true in E2E to bypass JWT validation
```

**Step 2 — Wire AUTH_DISABLED into middleware**:
In `internal/auth/middleware.go`, add:
```go
// NewAuthMiddleware returns middleware that validates Bearer tokens.
// If NAF3_AUTH_DISABLED=1, validation is skipped and requests pass through.
func NewAuthMiddleware(cfg Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if os.Getenv("NAF3_AUTH_DISABLED") == "1" {
                slog.Debug("auth: bypassed (E2E mode)")
                next.ServeHTTP(w, r)
                return
            }
            // ... existing JWT validation ...
        })
    }
}
```

Alternatively, read from the config struct:
```go
// Config struct already has auth section; add:
type AuthConfig struct {
    Disabled bool `yaml:"disabled"`
}
```

Read from config in middleware constructor:
```go
if cfg.Auth.Disabled {
    return next // bypass all validation
}
```

**Step 3 — Update harness to pass AUTH_DISABLED**:
In `test/e2e/harness.go`, when starting HTTP Gateway binary, add:
```go
gwEnv = append(gwEnv,
    "NAF3_AUTH_DISABLED=1",  // E2E mode: skip JWT validation
    "NAF3_AUTH_JWT_SECRET=nssaa-test-secret",  // test secret
)
```

**Step 4 — Add integration test verifying auth bypass**:
Add to `test/integration/auth_test.go` or `test/integration/http_gw_test.go`:
```go
// TestHTTPGateway_AuthBypass tests that requests succeed without JWT
// when NAF3_AUTH_DISABLED=1 is set on the gateway process.
func TestHTTPGateway_AuthBypass(t *testing.T) {
    if testing.Short() {
        t.Skip("requires full stack")
    }

    // Start a minimal Biz Pod + HTTP Gateway with auth disabled
    bizURL, gwURL, teardown := startAuthDisabledStack(t)
    defer teardown()

    // POST without Authorization header — should succeed
    body := `{"gpsi":"504217500001","snssai":{"sst":1,"sd":"000001"},"supi":"imu-208930000000001","supiKind":"SUCI"}`
    req, _ := http.NewRequest(http.MethodPost, gwURL+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    // Without JWT, requests should either:
    // (a) succeed if auth is disabled (200/201)
    // (b) fail with 401 if auth is enabled (test config error)
    // Case (a) proves auth bypass works; case (b) means config is wrong
    require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
        "HTTP Gateway returned 401 — auth bypass not working. Check NAF3_AUTH_DISABLED.")
}

func startAuthDisabledStack(t *testing.T) (bizURL, gwURL string, teardown func()) {
    // Use existing integration test infrastructure
    // Start biz + http-gw with NAF3_AUTH_DISABLED=1 env var
    // Return URLs and teardown function
    // ...
}
```

**Step 5 — Document the bypass**:
Add comment in `compose/configs/http-gateway.yaml`:
```yaml
# Auth bypass for E2E tests: set auth.disabled=true or
# pass NAF3_AUTH_DISABLED=1 env var. Never enable in production.
```
</action>

<acceptance_criteria>
- `internal/auth/middleware.go` checks `cfg.Auth.Disabled` and bypasses if true
- `test/e2e/harness.go` sets `NAF3_AUTH_DISABLED=1` for HTTP Gateway
- `TestHTTPGateway_AuthBypass` integration test exists in `test/integration/` with assertions
- `go build ./cmd/http-gateway/` compiles
</acceptance_criteria>

---

## Task 3 — Empty S-NSSAI Validation (Gap E2E-01)

<read_first>
- `internal/api/common/validator.go` — `ValidateSnssai` function
- `internal/api/nssaa/handler.go` — `CreateSliceAuthenticationContext` handler
- `test/unit/api/nssaa_handler_gaps_test.go` — existing gap test patterns
- `docs/design/02_nssaa_api.md` — TS 29.526 Snssai requirements
</read_first>

<action>
**Step 1 — Add empty Snssai check to handler**:
In `internal/api/nssaa/handler.go`, in `CreateSliceAuthenticationContext`:
```go
// Validate Snssai is not empty
if req.Snssai == nil {
    h.writeError(w, r, http.StatusBadRequest, "BAD_REQUEST",
        "snssai is required")
    return
}
if req.Snssai.Sst == 0 && req.Snssai.Sd == "" {
    // Both fields absent: reject as empty object {}
    h.writeError(w, r, http.StatusBadRequest, "BAD_REQUEST",
        "snssai.sst or snssai.sd must be provided")
    return
}
```

Similarly in `ConfirmSliceAuthenticationContext` (PUT):
```go
if req.Snssai != nil && req.Snssai.Sst == 0 && req.Snssai.Sd == "" {
    h.writeError(w, r, http.StatusBadRequest, "BAD_REQUEST",
        "snssai.sst or snssai.sd must be provided")
    return
}
```

**Step 2 — Strengthen ValidateSnssai as defense-in-depth**:
In `internal/api/common/validator.go`:
```go
// ValidateSnssai checks Snssai is well-formed.
// Both Sst and Sd may be absent only if the other is present.
func ValidateSnssai(snssai *types.Snssai) error {
    if snssai == nil {
        return errors.New("snssai is required")
    }
    // Reject explicitly empty: both fields absent
    if snssai.Sst == 0 && snssai.Sd == "" {
        return errors.New("snssai.sst or snssai.sd must be provided")
    }
    // ... existing SST range check ...
    // ... existing SD format check ...
    return nil
}
```

**Step 3 — Add unit test cases**:
In `test/unit/api/nssaa_handler_gaps_test.go`, add:
```go
func TestCreateSliceAuth_EmptySnssai(t *testing.T) {
    // TC: snssai: {} → HTTP 400
    body := map[string]interface{}{
        "gpsi": "504217500001",
        "snssai": map[string]interface{}{},
        "supi":  "imu-208930000000001",
        "supiKind": "SUCI",
    }
    req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", jsonBody(body))
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
    require.Equal(t, http.StatusBadRequest, rr.Code)
    require.Contains(t, rr.Body.String(), "BAD_REQUEST")
}

func TestConfirmSliceAuth_EmptySnssai(t *testing.T) {
    // TC: PUT with empty snssai → HTTP 400
    body := map[string]interface{}{
        "gpsi":  "504217500001",
        "snssai": map[string]interface{}{},
        "eapMessage": base64.StdEncoding.EncodeToString([]byte("test")),
    }
    req := httptest.NewRequest(http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/test-id/confirm", jsonBody(body))
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
    require.Equal(t, http.StatusBadRequest, rr.Code)
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
    // Create a test router with the NSSAA handler
    handler := nssaa.NewHandler( /* ... deps ... */ )

    // TC-NSSAA-XXX: Empty snssai object → 400 Bad Request
    body := map[string]interface{}{
        "gpsi":    "504217500001",
        "snssai":  map[string]interface{}{}, // empty object
        "supi":    "imu-208930000000001",
        "supiKind": "SUCI",
    }
    bodyBytes, _ := json.Marshal(body)
    req := httptest.NewRequest(http.MethodPost,
        "/nnssaaf-nssaa/v1/slice-authentications",
        bytes.NewReader(bodyBytes))
    req.Header.Set("Content-Type", "application/json")
    // Set auth context
    req = req.WithContext(withAuthContext(req.Context(), "nssaa-test-token"))

    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusBadRequest, rr.Code,
        "empty snssai object should return 400, got %d", rr.Code)
    assert.Contains(t, rr.Body.String(), "BAD_REQUEST",
        "response should contain cause BAD_REQUEST")
}
```

**Step 5 — Verify fixes**:
```bash
go test ./test/unit/api/... -v -run "EmptySnssai"
go test ./test/conformance/... -v -run "EmptySnssai"
```
</action>

<acceptance_criteria>
- POST `/nnssaaf-nssaa/v1/slice-authentications` with `snssai: {}` returns HTTP 400
- PUT with empty snssai returns HTTP 400
- `TestCreateSliceAuth_EmptySnssai` passes
- `TestConfirmSliceAuth_EmptySnssai` passes
- `ValidateSnssai` returns error for empty snssai
- `go test ./test/unit/api/...` passes
- `go test ./test/conformance/...` passes
</acceptance_criteria>

</tasks>

<verification>

```bash
# Compose cleanup
test ! -f compose/test.yaml
test ! -f compose/configs/biz-e2e.yaml
test ! -f compose/configs/http-gateway-e2e.yaml
test ! -f compose/configs/aaa-gateway-e2e.yaml
grep -q "docker-compose" test/e2e/harness.go test/mocks/compose.go && exit 1 || true
grep -q "compose/test.yaml" test/e2e/harness.go && exit 1 || true
grep -r "docker-compose" test/e2e/ test/mocks/ 2>/dev/null && echo "V1 FOUND" && exit 1 || true

# Auth bypass
go test ./test/integration/... -v -run "AuthBypass" -short || true
go build ./cmd/http-gateway/

# Empty Snssai
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
- [ ] `test/mocks/compose.go` uses `docker compose` V2 in all 5 invocations (ComposeUp, ComposeDown, checkComposeHealth, GetServiceAddr, error messages)
- [ ] HTTP Gateway E2E requests succeed without JWT (bypassed via `NAF3_AUTH_DISABLED=1` env var)
- [ ] `TestHTTPGateway_AuthBypass` integration test exists with assertions (not a stub)
- [ ] Empty `snssai: {}` returns HTTP 400 from both POST and PUT handlers
- [ ] `TestCreateSliceAuth_EmptySnssai` unit test exists and passes
- [ ] `TestTS29526_CreateSession_EmptySnssai` conformance test has assertion body (not a stub)
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./test/unit/... ./test/conformance/... -short` all pass

</success_criteria>
