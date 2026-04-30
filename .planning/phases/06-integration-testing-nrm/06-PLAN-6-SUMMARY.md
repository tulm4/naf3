---
phase: 06
plan: 06-PLAN-6
subsystem: testing
tags: [docker-compose, auth, validation, 3gpp]

requires:
  - phase: 05
    provides: Auth middleware infrastructure

provides:
  - compose layout aligned with D-12 single-dev-yaml decision
  - docker compose V2 throughout (D-11)
  - HTTP Gateway auth bypass via NAF3_AUTH_DISABLED=1
  - Empty snssai {} validation returning HTTP 400 per TS 29.526 §7.2.2

affects: [compose, auth, validator, e2e]

tech-stack:
  added: []
  patterns: [env-var config override, config-driven auth bypass]

key-files:
  created:
    - test/integration/auth_test.go
  modified:
    - compose/test.yaml (deleted)
    - compose/configs/biz-e2e.yaml (deleted)
    - compose/configs/http-gateway-e2e.yaml (deleted)
    - compose/configs/aaa-gateway-e2e.yaml (deleted)
    - test/e2e/harness.go
    - test/e2e/e2e.go
    - test/mocks/compose.go
    - internal/auth/middleware.go
    - internal/config/config.go
    - cmd/http-gateway/main.go
    - compose/configs/http-gateway.yaml
    - internal/api/common/validator.go
    - test/unit/api/nssaa_handler_gaps_test.go
    - test/conformance/ts29526_test.go

key-decisions:
  - "D-11: Use docker compose V2 (docker compose) not docker-compose V1"
  - "D-12: Single dev.yaml — no separate test/e2e compose files; env vars override dev config in harness"
  - "D-14: NAF3_AUTH_DISABLED=1 env var enables E2E auth bypass without changing config files"
  - "Empty snssai {} (sst=0, sd='') is distinct from missing snssai — both rejected with 400 per TS 29.526 §7.2.2"

patterns-established:
  - "Pattern: config-driven auth bypass with env-var override (belt-and-suspenders)"
  - "Pattern: middleware factory (NewAuthMiddleware) over direct constructor for testability"

requirements-completed: []

duration: 18min
completed: 2026-04-30
---

# Phase 6 Plan 6: UAT Gap Fixes Summary

**Compose layout aligned with D-12 single-dev-yaml, HTTP Gateway auth bypass for E2E tests, and empty S-NSSAI {} validation returning HTTP 400 per TS 29.526**

## Performance

- **Duration:** 18 min
- **Started:** 2026-04-30T03:45:00Z
- **Completed:** 2026-04-30T04:02:43Z
- **Tasks:** 3
- **Files modified:** 15 files, 4 deleted

## Accomplishments

- Removed 4 obsolete compose files (compose/test.yaml, biz-e2e.yaml, http-gateway-e2e.yaml, aaa-gateway-e2e.yaml) — aligns with D-12 single-dev-yaml decision
- Migrated all docker-compose V1 invocations to docker compose V2 in harness.go (4 invocations), compose.go (4 invocations), e2e.go (comment)
- Added HTTP Gateway auth bypass via `NAF3_AUTH_DISABLED=1` env var — E2E tests no longer require JWT tokens
- Fixed `ValidateSnssai` to reject empty `snssai: {}` objects with HTTP 400 per TS 29.526 §7.2.2

## Task Commits

Each task was committed atomically:

1. **Task 1: Compose layout cleanup (D-11 + D-12)** - `29e172a` (refactor)
2. **Task 2: HTTP Gateway auth bypass for E2E tests (Gap E2E-02)** - `f8d6eb0` (feat)
3. **Task 3: Empty S-NSSAI validation (Gap E2E-01)** - `aad45fa` (feat)

## Decisions Made

- Used env var `NAF3_AUTH_DISABLED=1` (not `auth.disabled=true` in config) for E2E auth bypass — allows harness to override without modifying compose files
- `NewAuthMiddleware(Config{Disabled: false})` is backward-compatible: existing callers of the removed `auth.Middleware(scope)` pattern get strict auth by default
- The empty snssai check `sst==0 && sd==""` is placed between the `missing` check and the SST range check, making the logic explicit: missing → 400, empty → 400, sst=0+sd="" (ambiguous) → 400, valid range → OK

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered

- Build error in harness.go after replacing `docker-compose` with `docker compose`: `exec.CommandContext(ctx, "docker", "compose", args...)` failed because Go's `exec.CommandContext` takes variadic strings, not a slice. Fixed by passing args as separate variadic arguments.
- Build error: `ctx` was undefined in `NewHarness` (context created later). Fixed by using `context.Background()` for the docker version check.
- Duplicate `TestCreateSliceAuth_MissingSnssai` declaration from overlapping edits — resolved by keeping the existing function and updating its assertions.
- `TestAuthEnforced_WhenEnabled` had a `t.Fatal` before the test assertions, causing incorrect test failure. Fixed by restructuring the test to check the response code directly.

## Next Phase Readiness

- E2E harness is fully aligned with docker compose V2 and single dev.yaml
- HTTP Gateway auth bypass enables E2E tests without JWT infrastructure
- Empty snssai validation is enforced in both POST and PUT handlers
- Ready for Phase 7: Kubernetes Deployment

---
*Phase: 06-integration-testing-nrm / 06-PLAN-6*
*Completed: 2026-04-30*
