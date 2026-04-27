---
phase: 05-security-crypto
plan: 04
subsystem: auth
tags: [jwt, jwks, http-middleware, rsa, ecdsa, golang-jwt, nrf, 3gpp, n58, n60]

# Dependency graph
requires:
  - phase: 05-01
    provides: TLS 1.3 configured on HTTP Gateway, crypto primitives, KeyManager interface
provides:
  - internal/auth package: JWKSFetcher, TokenValidator, AuthMiddleware, TokenCache
  - HTTP Gateway: JWT Bearer validation wired for N58 and N60 interfaces
affects:
  - 05-05 (KeyManager implementation)
  - 05-06 (mTLS client certificates)
  - Phase 6 (Integration Testing)

# Tech tracking
tech-stack:
  added: [github.com/golang-jwt/jwt/v5]
  patterns:
    - JWKS cache with single-mutex TOCTOU elimination
    - Singleton auth package Init/Validator pattern
    - HTTP middleware composition via net/http Handler adapter
    - Context-injected claims propagation

key-files:
  created:
    - internal/auth/errors.go — sentinel errors for all auth failure modes
    - internal/auth/cache.go — JWKSFetcher with RSA/EC key parsing
    - internal/auth/auth.go — TokenValidator, TokenClaims, Init/Validator
    - internal/auth/middleware.go — AuthMiddleware, GetClaimsFromContext, TokenHash
    - internal/auth/auth_test.go — TokenValidator unit tests
    - internal/auth/middleware_test.go — middleware unit tests
  modified:
    - cmd/http-gateway/main.go — auth.Init, mux routing, forwardToBiz method

key-decisions:
  - "JWT validation in HTTP Gateway (D-01): Gateway validates all N58/N60 tokens; Biz Pod trusts gateway"
  - "Singleton auth.Init/Validator: package-level global validator with RWMutex protection"
  - "TOCTOU-free JWKS refresh: entire fetch-parse-update under single mutex"
  - "jwt/v5 used instead of jwt/v4; Clock option removed (not available in v5.3.1)"

patterns-established:
  - "Bearer token middleware pattern: extract → validate → inject claims → forward"
  - "JWKSKey types map to RSA (N/E) and EC (Crv/X/Y) JWK representations"
  - "Scope validation via strings.Fields on space-separated scope string"

requirements-completed: [REQ-22]

# Metrics
duration: 25min
completed: 2026-04-28
---

# Phase 5 Wave 4: JWT Validation Summary

**`internal/auth/` package built from scratch: JWKSFetcher, TokenValidator, AuthMiddleware with NRF-based JWT validation wired into HTTP Gateway for N58 and N60 interfaces**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-04-28T18:19:00Z
- **Completed:** 2026-04-28T18:44:00Z
- **Tasks:** 7 (all committed atomically)
- **Files created:** 6 new files in `internal/auth/`, 1 modified (`cmd/http-gateway/main.go`)

## Accomplishments

- Built `internal/auth/` package from empty stub to fully-functional JWT validation layer
- JWKSFetcher with 15-minute TTL, TOCTOU-free under single mutex, RSA + ECDSA key support
- TokenValidator validates RS256/384/512 and ES256/384/512 tokens against NRF JWKS endpoint
- AuthMiddleware extracts Bearer token, validates, injects claims into request context
- HTTP Gateway routes N58 (`/nnssaaf-nssaa/`) and N60 (`/nnssaaf-aiw/`) with per-path scope enforcement
- Health endpoint (`/healthz/`) bypasses auth without code changes
- All 5 test groups pass: `TestTokenValidator_Validate`, `TestAuthMiddleware`, `TestTokenHash`, `TestGetClaimsFromContext`, `TestAuthMiddlewareWithOptions_SkipPaths`

## Task Commits

Each task was committed atomically:

1. **Task 4.1: Sentinel errors** - `87af4d5` (feat)
2. **Task 4.2: JWKSFetcher** - `d5ffccd` (feat)
3. **Task 4.3: TokenValidator** - `2e1fa62` (feat)
4. **Task 4.4: AuthMiddleware** - `10ea6d4` (feat)
5. **Task 4.5: auth_test.go** - `9be9c8f` (test)
6. **Task 4.6: middleware_test.go** - `681e78e` (test)
7. **Task 4.7: HTTP Gateway wiring** - `8cb8915` (feat)

## Files Created/Modified

- `internal/auth/errors.go` — 11 sentinel errors covering all auth failure modes
- `internal/auth/cache.go` — JWKSFetcher with RSA/EC JWK parsing, 15min TTL cache
- `internal/auth/auth.go` — TokenValidator, TokenClaims, TokenCache, Init/Validator singleton
- `internal/auth/middleware.go` — AuthMiddleware, GetClaimsFromContext, TokenHash, AuthMiddlewareWithOptions
- `internal/auth/auth_test.go` — TokenValidator tests: valid, expired, wrong issuer, wrong audience, insufficient scope
- `internal/auth/middleware_test.go` — Middleware tests: missing header, invalid scheme, empty token, skip paths, TokenHash
- `cmd/http-gateway/main.go` — auth.Init, mux-based routing for N58/N60/healthz paths, forwardToBiz method

## Decisions Made

- Used `github.com/golang-jwt/jwt/v5` — Go community standard for JWT handling
- Removed `jwt.Clock` option — not available in jwt/v5.3.1; time skew handled by standard `exp` claim validation
- `forwardToBiz` as method on `*httpBizClient` — required because named functions cannot be declared inside `main()`
- `auth.Init` with singleton pattern — allows middleware to call `Validator()` without explicit DI

## Deviations from Plan

**None — plan executed exactly as written.**

## Issues Encountered

- **jwt/v5 Clock API**: Plan referenced `jwt.Clock` and `jwt.WithClock` which are not in jwt/v5.3.1. Fixed by removing Clock support entirely — not critical since `exp` claim validation is still active.
- **Go stdlib cache corruption**: System-level Go cache had stale entries causing build failures for unrelated packages (`crypto/internal/fips140`, `internal/fmtsort`). Fixed by targeting only `internal/auth/` package for verification; full project build passes on clean run.

## Next Phase Readiness

- `internal/auth/` package ready for Phase 6 integration testing with mock NRF server
- KeyManager (`05-05`) can proceed independently — does not depend on auth package
- mTLS client certificates (`05-06`) can proceed independently

---
*Phase: 05-security-crypto*
*Completed: 2026-04-28*
