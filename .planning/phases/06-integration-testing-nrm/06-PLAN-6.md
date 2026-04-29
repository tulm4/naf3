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
  - internal/auth/middleware.go
  - internal/api/nssaa/handler.go
  - internal/api/common/validator.go
  - test/unit/api/nssaa_handler_gaps_test.go
---

<objective>

Fix three gaps from UAT for Phase 6: (1) compose layout cleanup per D-12 — remove obsolete test config files and update harness to use only `compose/dev.yaml` with `docker compose` V2; (2) HTTP Gateway JWT auth blocking E2E tests — add `AUTH_DISABLED` env var bypass; (3) empty Snssai validation — `POST snssai: {}` returns 201 but should return 400 per TS 29.526 §7.2.2.

</objective>

<context>
@.planning/phases/06-integration-testing-nrm/06-CONTEXT.md
  - D-11: Use `docker compose` (V2) throughout — not `docker-compose` (V1)
  - D-12: Single `compose/dev.yaml` for all test types — no separate test.yaml, no e2e configs
  - D-13: Same infra ports as dev (5432, 6379) — no separate test ports
  - D-14: All harness hardcoded values externalized as env vars
@compose/test.yaml — to be deleted
@compose/configs/biz-e2e.yaml — to be deleted
@compose/configs/http-gateway-e2e.yaml — to be deleted
@compose/configs/aaa-gateway-e2e.yaml — to be deleted
@test/e2e/harness.go — needs update: remove `docker-compose` V1, remove `compose/test.yaml`
@internal/auth/middleware.go — needs update: add `AUTH_DISABLED` bypass
@internal/api/common/validator.go — needs update: `ValidateSnssai` must reject `snssai: {}`
@internal/api/nssaa/handler.go — needs update: check for empty snssai after raw body parse
</context>

<tasks>

## Task 1 — Compose Layout Cleanup (`compose/test.yaml`, `compose/configs/biz-e2e.yaml`, `compose/configs/http-gateway-e2e.yaml`, `compose/configs/aaa-gateway-e2e.yaml`, `test/e2e/harness.go`)

<read_first>
- `test/e2e/harness.go` — current: uses `docker-compose` V1, references `compose/test.yaml` in 3 places
- `compose/test.yaml` — DELETE
- `compose/configs/biz-e2e.yaml` — DELETE
- `compose/configs/http-gateway-e2e.yaml` — DELETE
- `compose/configs/aaa-gateway-e2e.yaml` — DELETE
- `Makefile` lines 234-242 — already uses `docker compose` V2 correctly (no change needed)
</read_first>

<action>
Per D-11, D-12, D-13, D-14:

**Step A — Delete obsolete files:**
1. Delete `compose/test.yaml`
2. Delete `compose/configs/biz-e2e.yaml`
3. Delete `compose/configs/http-gateway-e2e.yaml`
4. Delete `compose/configs/aaa-gateway-e2e.yaml`

**Step B — Update `test/e2e/harness.go`:**

1. **Change `docker-compose` to `docker compose` V2** in `upCompose` and `downCompose`:
   - In `upCompose`: `exec.CommandContext(ctx, "docker", "compose", args...)` (was `"docker-compose"`)
   - In `downCompose`: `exec.CommandContext(ctx, "docker", "compose", args...)` (was `"docker-compose"`)
   - Change `parseComposeFlags` usage — now reads `DOCKER_COMPOSE` env var which will be `"-f compose/dev.yaml"` (no test.yaml)
   - Add `docker-compose` fallback check: try `docker compose` first, fall back to `docker-compose` if not found

2. **Remove `compose/test.yaml` from `DOCKER_COMPOSE` env var default:**
   - Change: `composeFile: getEnv("DOCKER_COMPOSE", "-f compose/dev.yaml -f compose/test.yaml")` → `composeFile: getEnv("DOCKER_COMPOSE", "-f compose/dev.yaml")`
   - The `parseComposeFlags` function splits the `DOCKER_COMPOSE` env var into args for `exec.CommandContext` — no other changes needed

3. **Update env var defaults for `startBizPod`** (per D-14 — no separate test ports):
   - Keep: `BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable` (same as dev, per D-13)
   - Keep: `BIZ_REDIS_URL=redis://localhost:6379` (same as dev, per D-13)

4. **Update `startHTTPGateway`** to pass `AUTH_DISABLED=1` for E2E tests (prepares for Task 2):
   - Add `AUTH_DISABLED=1` to the `append(os.Environ(), ...)` for HTTP Gateway process
   - Add a comment: `// AUTH_DISABLED=1 skips JWT validation in E2E tests; real deployments should use proper JWTs`

5. **Update docker-compose availability check** in `NewHarness`:
   - Check `docker compose version` instead of `docker-compose`
   - Try `docker compose` first; fall back to `docker-compose` binary with a warning

6. **Remove the `composeFile` field** — it's now derived entirely from the env var and no longer stored in the struct

**Do NOT change:** Binary paths (still built in `buildBinaries`), health check URLs, mock server URLs, `projectRoot()`, `getEnv()`, or `killProcess()`.
</action>

<acceptance_criteria>
- `compose/test.yaml` deleted
- `compose/configs/biz-e2e.yaml` deleted
- `compose/configs/http-gateway-e2e.yaml` deleted
- `compose/configs/aaa-gateway-e2e.yaml` deleted
- `test/e2e/harness.go` uses `docker compose` V2 (not `docker-compose` V1)
- `test/e2e/harness.go` uses only `compose/dev.yaml` (no `compose/test.yaml`)
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 2 — HTTP Gateway JWT Bypass for E2E Tests (`internal/auth/middleware.go`)

<read_first>
- `internal/auth/middleware.go` — current: `Middleware()` always validates JWT, no bypass
- `cmd/http-gateway/main.go` lines 101-121 — auth.Init called with NRF JWKS URL
- `06-CONTEXT.md` D-12 — E2E tests need to pass through HTTP Gateway without real JWTs
</read_first>

<action>
Add `AUTH_DISABLED` env var bypass to the auth middleware so E2E tests can pass through without valid JWTs:

**In `internal/auth/middleware.go`:**

1. **Add `AUTH_DISABLED` env var check at the top of the middleware function:**
   ```go
   func Middleware(requiredScope string) func(http.Handler) http.Handler {
       return func(next http.Handler) http.Handler {
           return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
               // E2E test bypass: skip JWT validation when AUTH_DISABLED=1
               if os.Getenv("AUTH_DISABLED") == "1" {
                   next.ServeHTTP(w, r)
                   return
               }
               // ... rest of existing validation logic
           })
       }
   }
   ```

2. **Add `os` to the imports** if not already present (check current imports).

3. **Add a log line** when auth is disabled (for debuggability):
   ```go
   if os.Getenv("AUTH_DISABLED") == "1" {
       slog.Debug("auth middleware bypassed for E2E test", "path", r.URL.Path)
       next.ServeHTTP(w, r)
       return
   }
   ```

**The `slog` package is already imported** in the middleware file — use it for the debug log.

**Do NOT change:** `GetClaimsFromContext`, `TokenHash`, `MiddlewareWithOptions`, `MiddlewareOption` types.
</action>

<acceptance_criteria>
- `AUTH_DISABLED=1` skips JWT validation and passes through to the handler
- Missing/invalid JWT without `AUTH_DISABLED` still returns 401
- Log message appears when bypassing (debug level)
- `go build ./internal/auth/...` compiles without error
</acceptance_criteria>

---

## Task 3 — Empty Snssai Validation Fix (`internal/api/common/validator.go`, `internal/api/nssaa/handler.go`, `test/unit/api/nssaa_handler_gaps_test.go`)

<read_first>
- `internal/api/common/validator.go` — `ValidateSnssai(sst int, sd string, missing bool)` — currently only checks range if not missing
- `internal/api/nssaa/handler.go` — `CreateSliceAuthenticationContext` — parses raw body for `snssaiPresent` detection
- `06-CONTEXT.md` D-14 — validation must reject `snssai: {}` (present but empty) per TS 29.526 §7.2.2
- `test/unit/api/nssaa_handler_gaps_test.go` — existing gap tests (PLAN-3 Task 5)
</read_first>

<action>
The issue: `POST /nnssaaf-nssaa/v1/slice-authentications` with body `{"snssai": {}}` (snssai key present but empty object) returns 201 instead of 400. The handler detects `snssaiPresent` by checking if the key exists in the raw JSON, but `ValidateSnssai` only validates when `missing=true` and validates SST range 0-255 (which passes for sst=0, the Go zero value).

**Fix:** Add a dedicated check in the handler after the raw body parse. When `snssai` key is present but both `sst == 0` and `sd == ""`, reject with 400.

**In `internal/api/nssaa/handler.go` — `CreateSliceAuthenticationContext`:**

After the `json.Unmarshal` into `nssaanats.SliceAuthInfo` body (around line 176), before calling `ValidateSnssai`:

```go
// Check for empty snssai: key present but object is empty {}
// e.g., {"snssai": {}} — both sst=0 and sd="" means the object was sent but empty.
// Per TS 29.526 §7.2.2, snssai with both sst=0 and sd="" is invalid.
if snssaiPresent && body.Snssai.Sst == 0 && body.Snssai.Sd == "" {
    common.WriteProblem(w, common.ValidationProblem("snssai", "snssai must have at least sst set (TS 29.526 §7.2.2)"))
    return
}
```

Also apply the same check in `ConfirmSliceAuthentication` (around line 312, after the `snssaiPresent` raw check).

**In `internal/api/common/validator.go`:**

Strengthen `ValidateSnssai` to also reject the case where both sst=0 and sd="" (zero value means empty object was sent, not a valid S-NSSAI):

```go
// After the `if missing { return error }` block:
// Reject zero-value S-NSSAI: both sst=0 and sd="" means the snssai object
// was present but empty. A valid S-NSSAI must have at least sst > 0.
// Per TS 29.526 §7.2.2, snssai.sst is required and must be 0-255.
if sst == 0 && sd == "" {
    return ValidationProblem("snssai", "snssai must have at least sst set (TS 29.526 §7.2.2)")
}
```

This catches the case where `missing=false` but the values are still zero. The handler's explicit check handles the error message; this validator-level guard provides defense-in-depth.

**In `test/unit/api/nssaa_handler_gaps_test.go`:**

Add test case for empty snssai:
```go
// TC-NSSAA-006a: Empty snssai object {} → 400
func TestCreateSliceAuth_EmptySnssai(t *testing.T) {
    // ... existing setup pattern ...
    body := `{"gpsi":"520804600000001","snssai":{},"eapIdRsp":"dGVzdA=="}`
    req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
    assert.Equal(t, http.StatusBadRequest, rr.Code)
    // Verify ProblemDetails.cause contains "snssai"
    var problem map[string]interface{}
    json.Unmarshal(rr.Body.Bytes(), &problem)
    assert.Contains(t, problem["detail"], "snssai")
}
```

Add the same test for `ConfirmSliceAuthentication`:
```go
// TC-NSSAA-023a: Empty snssai in confirm body → 400
func TestConfirmSliceAuth_EmptySnssai(t *testing.T) {
    // ... existing setup pattern ...
    body := `{"gpsi":"520804600000001","snssai":{},"eapMessage":"dGVzdA=="}`
    req := httptest.NewRequest(http.MethodPut, "/nnssaaf-nssaa/v1/slice-authentications/test-id", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
    assert.Equal(t, http.StatusBadRequest, rr.Code)
}
```
</action>

<acceptance_criteria>
- `POST /nnssaaf-nssaa/v1/slice-authentications` with `{"snssai":{}}` returns HTTP 400 (not 201)
- `PUT /nnssaaf-nssaa/v1/slice-authentications/{id}` with `{"snssai":{}}` returns HTTP 400 (not 200)
- Empty snssai error message mentions TS 29.526 §7.2.2
- `go test ./internal/api/common/...` passes
- `go test ./test/unit/api/...` passes
- `go build ./internal/api/...` compiles without error
</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 6:
- `go build ./test/e2e/...` compiles without error
- `go build ./internal/auth/...` compiles without error
- `go build ./internal/api/...` compiles without error
- `go test ./test/unit/api/...` passes (including new empty-snssai test cases)
- `go test ./internal/api/common/...` passes
- `compose test.yaml` deleted: `! -f compose/test.yaml`
- `compose/configs/*-e2e.yaml` all deleted: `! -f compose/configs/biz-e2e.yaml && ! -f compose/configs/http-gateway-e2e.yaml && ! -f compose/configs/aaa-gateway-e2e.yaml`

</verification>

<success_criteria>

- D-11 enforced: all `docker-compose` V1 references replaced with `docker compose` V2 in `harness.go`
- D-12 enforced: all 4 obsolete compose config files deleted (`compose/test.yaml`, `compose/configs/biz-e2e.yaml`, `compose/configs/http-gateway-e2e.yaml`, `compose/configs/aaa-gateway-e2e.yaml`)
- D-13 enforced: harness uses same infra ports as dev (5432, 6379)
- E2E gap E2E-02 fixed: HTTP Gateway accepts requests with `AUTH_DISABLED=1` without JWT
- E2E gap E2E-01 fixed: POST with `snssai: {}` returns HTTP 400 per TS 29.526 §7.2.2
- REQ-27: integration tests cover all endpoints (empty-snssai validation included)
- REQ-28: conformance tests pass (including new empty-snssai test cases)
- REQ-29: E2E harness works with full stack (JWT bypass enables full-stack E2E)

</success_criteria>

<output>
After completion, create `.planning/phases/06-integration-testing-nrm/06-PLAN-6-SUMMARY.md`
</output>
