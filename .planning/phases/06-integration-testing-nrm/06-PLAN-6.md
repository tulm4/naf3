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
  - Makefile
  - internal/auth/middleware.go
  - internal/api/common/validator.go
  - internal/api/nssaa/handler.go
  - test/unit/api/nssaa_handler_gaps_test.go
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

**Step 2 — Update harness to use only dev.yaml** (per D-11):
In `test/e2e/harness.go`:
1. Change `exec.CommandContext(ctx, "docker-compose", args...)` to `exec.CommandContext(ctx, "docker", "compose", args...)` (V2, per D-11)
2. Remove `-f compose/test.yaml` from all `docker compose` commands — use only `-f compose/dev.yaml`
3. Verify `docker compose version` works; if unavailable, skip with t.Log and fall back to no-op

Before:
```go
exec.CommandContext(ctx, "docker-compose", "-f", "compose/dev.yaml", "-f", "compose/test.yaml", "up", "-d", services...)
```

After:
```go
// Verify docker compose V2 is available
if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
    t.Skip("docker compose V2 not available")
}
exec.CommandContext(ctx, "docker", "compose", "-f", "compose/dev.yaml", "up", "-d", services...)
```

**Step 3 — Update Makefile references**:
In `Makefile`, check for any targets referencing `compose/test.yaml` or `docker-compose`. Update to:
- Use `docker compose` (not `docker-compose`)
- Remove references to `compose/test.yaml`
- Test targets use `compose/dev.yaml` only

**Step 4 — Update infra ports** (per D-13):
Harness env vars already use standard ports (5432, 6379) per D-13. Verify no hardcoded `5433` or `6380` remain in harness.go. If found, replace with `5432` and `6379` respectively.

**Step 5 — Verify E2E tests still pass** after cleanup:
```bash
go test ./test/e2e/... -v -short -timeout=10m
```
</action>

<acceptance_criteria>
- `compose/test.yaml` does not exist
- `compose/configs/biz-e2e.yaml` does not exist
- `compose/configs/http-gateway-e2e.yaml` does not exist
- `compose/configs/aaa-gateway-e2e.yaml` does not exist
- `test/e2e/harness.go` uses `docker compose` (not `docker-compose`) and only `-f compose/dev.yaml`
- `Makefile` has no references to `docker-compose` or `compose/test.yaml`
- No hardcoded ports `5433` or `6380` in `test/e2e/harness.go`
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

**Step 4 — Add test case verifying auth bypass works**:
Add to `test/e2e/nssaa_flow_test.go`:
```go
func TestE2E_HTTPGateway_AuthBypass(t *testing.T) {
    // Skip if full stack not available
    if os.Getenv("TEST_E2E") != "1" {
        t.Skip("E2E test")
    }
    // Verify requests pass through HTTP Gateway without JWT
    // when NAF3_AUTH_DISABLED=1
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
- `go test ./test/e2e/... -v -short` can reach HTTP Gateway in E2E mode without JWT
- `go build ./cmd/http-gateway/` compiles
- No production config enables auth bypass
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
In `test/conformance/ts29526_test.go`, add to NSSAA CreateSession section:
```go
func TestTS29526_CreateSession_EmptySnssai(t *testing.T) {
    // TC-NSSAA-XXX: Empty snssai object → 400 Bad Request
    // Spec: TS 29.526 requires sst or sd to be present
    // Gap E2E-01 fix
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
grep -q "docker-compose" test/e2e/harness.go && exit 1 || true
grep -q "compose/test.yaml" test/e2e/harness.go && exit 1 || true

# Auth bypass
go test ./test/e2e/... -v -short -timeout=10m

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
- [ ] Harness uses `docker compose` V2 and only `compose/dev.yaml`
- [ ] HTTP Gateway E2E requests succeed without JWT (bypassed via env var)
- [ ] Empty `snssai: {}` returns HTTP 400 from both POST and PUT handlers
- [ ] Unit tests for empty Snssai cases exist and pass
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./test/unit/... ./test/conformance/... -short` all pass

</success_criteria>
