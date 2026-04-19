# Phase 1: Foundation — API Layer & Types

## Overview

Phase 1 establishes the HTTP API layer and data types for NSSAAF. The API types and
HTTP handlers are **generated from 3GPP OpenAPI specs** using [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen).
Business-logic handlers are implemented manually, delegating to the generated router.

**No external dependencies** (AAA servers, NRF, UDM) are involved in this phase.

---

## Code Generation Pipeline

```
TS29526_Nnssaaf_NSSAA.yaml ──┐
TS29526_Nnssaaf_AIW.yaml ────┤──▶ oapi-codegen ──▶ oapi-gen/gen/{nssaa,aiw}/*.gen.go
TS29571_CommonData.yaml ─────┘        ▲
                                     │
                            nssaa_config.yaml
                            aiw_config.yaml
```

- **Specs**: `oapi-gen/specs/`
- **Config**: `oapi-gen/{nssaa,aiw}_config.yaml`
- **Generated code**: `oapi-gen/gen/nssaa/`, `oapi-gen/gen/aiw/`, `oapi-gen/gen/specs/`
- **Makefile**: `oapi-gen/Makefile`

To regenerate after editing specs:

```bash
cd oapi-gen && make
```

---

## Modules

### 1. `oapi-gen/gen/specs/` — Shared Generated Types

**Spec**: TS 29.571 Common Data Types

Types shared across all generated packages, sourced from `TS29571_CommonData.yaml`.

| File | Types |
|------|-------|
| `specs.go` | `Snssai`, `Gpsi`, `Supi`, `AuthStatus`, `SupportedFeatures`, `Uri`, `NfInstanceId`, `ServerAddressingInfo`, `Ipv4Addr`, `Ipv6Addr`, `Fqdn` |

### 2. `oapi-gen/gen/nssaa/` — NSSAA Generated Package

**Spec**: TS 29.526 §7.2

| File | Contents |
|------|----------|
| `nssaa.gen.go` | Types: `SliceAuthInfo`, `SliceAuthContext`, `SliceAuthConfirmationData`, `SliceAuthConfirmationResponse`, `SliceAuthCtxId`, `SliceAuthNotificationType`, `EapMessage`. Interface: `ServerInterface`. Router: `Handler()`, `HandlerFromMux()`, `HandlerWithOptions()`. |

### 3. `oapi-gen/gen/aiw/` — AIW Generated Package

**Spec**: TS 29.526 §7.3

| File | Contents |
|------|----------|
| `aiw.gen.go` | Types: `AuthInfo`, `AuthContext`, `AuthConfirmationData`, `AuthConfirmationResponse`, `AuthCtxId`, `EapMessage`, `Msk`. Interface: `ServerInterface`. Router: `Handler()`, `HandlerFromMux()`, `HandlerWithOptions()`. |

### 4. `internal/api/nssaa/` — NSSAA Business Logic

**Priority**: P0 | **Dependencies**: `oapi-gen/gen/nssaa/`, `internal/api/common/`

**Files**:

| File | Purpose |
|------|---------|
| `handler.go` | Implements `nssaanats.ServerInterface`: `CreateSliceAuthenticationContext`, `ConfirmSliceAuthentication`. Uses `AuthCtxStore` interface and `AAARouter` interface. |
| `router.go` | Builds chi `Router`, applies `common.RequestIDMiddleware`, `LoggingMiddleware`, `RecoveryMiddleware`, wires `nssaanats.HandlerFromMuxWithBaseURL`. |
| `handler_test.go` | 19 tests covering all endpoints, validation, error cases, store errors. |
| `store.go` | `AuthCtx` domain struct, `AuthCtxStore` interface, `InMemoryStore` implementation. |

**API Surface** (from TS 29.526 §7.2):

```
POST /nnssaaf-nssaa/v1/slice-authentications
  → 201 Created (SliceAuthContext) + Location header
  → 400 Bad Request, 403 Forbidden, 404 Not Found, 502 Bad Gateway, 503, 504
  Handler: CreateSliceAuthenticationContext

PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
  → 200 OK (SliceAuthConfirmationResponse)
  → 400 Bad Request, 404 Not Found, 409 Conflict, 410 Gone
  Handler: ConfirmSliceAuthentication
```

### 5. `internal/api/aiw/` — AIW Business Logic

**Priority**: P0 | **Dependencies**: `oapi-gen/gen/aiw/`, `internal/api/common/`

**Files**:

| File | Purpose |
|------|---------|
| `handler.go` | Implements `aiwnats.ServerInterface`: `CreateAuthenticationContext`, `ConfirmAuthentication`. Uses `AuthCtxStore` and `AAARouter` interfaces. |
| `router.go` | Same pattern as NSSAA router. |
| `handler_test.go` | 15 tests covering all endpoints, validation, error cases. |

**API Surface** (from TS 29.526 §7.3):

```
POST /nnssaaf-aiw/v1/authentications
  → 201 Created (AuthContext) + Location header
  Handler: CreateAuthenticationContext

PUT /nnssaaf-aiw/v1/authentications/{authCtxId}
  → 200 OK (AuthConfirmationResponse) with MSK on success
  Handler: ConfirmAuthentication
```

### 6. `internal/api/common/` — Shared API Utilities

**Priority**: P1 | **Dependencies**: None

**Files** (pre-existing, not changed):

| File | Purpose |
|------|---------|
| `problem.go` | `ProblemDetails` (RFC 7807), `NewProblem()`, `ValidationProblem()`, helper constructors per status code. |
| `headers.go` | HTTP header constants (`HeaderXRequestID`, `HeaderLocation`, etc.) and media types. |
| `middleware.go` | `RequestIDMiddleware`, `LoggingMiddleware`, `RecoveryMiddleware`, `CORSMiddleware`, `WriteProblem()`, `WriteJSON()`. |
| `validator.go` | `ValidateGPSI()`, `ValidateSUPI()`, `ValidateSnssai()`, `ValidateURI()`, `ValidateAuthCtxID()`. |
| `context.go` | `WithRequestID()`, `GetRequestID()`. |
| `common_test.go` | Tests for all of the above. |

### 7. `internal/types/` — Domain Data Types

**Priority**: P0 | **Dependencies**: None (pre-existing)

**Files** (pre-existing, not changed):

| File | Types |
|------|-------|
| `snssai.go` | `Snssai`, `SnssaiFromJSON()`, validation, `Key()`, `Equal()` |
| `gpsi.go` | `Gpsi`, validation with `^5-?[0-9]{8,14}$` pattern, `Normalize()` |
| `supi.go` | `Supi`, validation with `^imu-[0-9]{15}$` pattern, `IMSI()` |
| `eap.go` | `EapCode`, `EapMethod`, `EapMessage` with Base64 validation |
| `nssaa_status.go` | `NssaaStatus`, `AuthResult`, `NotificationType` constants |
| `nssaa_error.go` | `ValidationError`, `NssaaError`, cause codes, sentinel errors |
| `types.go` | Package doc |
| `types_test.go` | Tests for all types |

### 8. `cmd/nssAAF/main.go` — Entry Point

**Priority**: P0 | **Dependencies**: All above

Wires the chi router, applies global middleware, handles graceful shutdown.

**Note:** This entry point runs all components in a single process for local development only. For production Kubernetes deployment, see Phase R (3-Component Refactor): `cmd/biz/`, `cmd/http-gateway/`, `cmd/aaa-gateway/`.

---

## Implementation Order

```
1. oapi-gen/specs/              ← TS29571, TS29526 specs (already done)
2. oapi-gen/nssaa_config.yaml   ← oapi-codegen config for NSSAA
3. oapi-gen/aiw_config.yaml     ← oapi-codegen config for AIW
4. oapi-gen/gen/specs/          ← Shared types (manually maintained)
5. cd oapi-gen && make          ← Generate nssaa.gen.go, aiw.gen.go
6. internal/types/              ← Already exists, no changes needed
7. internal/api/common/         ← Already exists, no changes needed
8. internal/api/nssaa/         ← handler.go, router.go, handler_test.go
9. internal/api/aiw/           ← handler.go, router.go, handler_test.go
10. cmd/nssAAF/main.go         ← Updated to wire new handlers
11. go mod tidy
```

**Post-Phase-R note:** After completing Phase R (3-Component Refactor), the same handlers are wired into `cmd/biz/main.go` instead. The `internal/api/nssaa/` and `internal/api/aiw/` packages are not modified — they are reused across both the monolithic dev binary and the Biz Pod binary.
```

---

## Validation Checklist

- [x] `cd oapi-gen && make` generates both packages without errors
- [x] `go build ./cmd/nssAAF/... ./internal/api/... ./internal/types/... ./internal/config/...`
- [x] `go test ./internal/api/nssaa/... ./internal/api/aiw/... ./internal/api/common/... ./internal/types/... ./internal/config/...`
- [x] GPSI regex: `^5[0-9]{8,14}$` (from TS 29.571 §5.4.4.3)
- [x] SUPI regex: `^imu-[0-9]{15}$` (from TS 29.571 §5.4.4.2)
- [x] Snssai SST range: 0-255
- [x] Snssai SD format: 6 hex chars
- [x] ProblemDetails (RFC 7807) for all error responses
- [x] HTTP status codes: 201, 200, 400, 403, 404, 409, 410, 502, 503, 504
- [x] Location header on 201 responses
- [x] X-Request-ID correlation in all responses
- [x] Unit test coverage for both handlers

---

## Spec References

- TS 29.526 §7.2 — Nnssaaf_NSSAA service operations
- TS 29.526 §7.3 — Nnssaaf_AIW service operations
- TS 29.571 §5.4.4.60 — NssaaStatus / AuthStatus
- TS 29.571 §5.4.4.3 — Gpsi
- TS 29.571 §5.4.4.2 — Supi
- TS 23.502 §4.2.9.2 — AMF-triggered NSSAA procedure
- RFC 7807 — Problem Details for HTTP APIs
