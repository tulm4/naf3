# Phase 1: Foundation — API Layer & Types

## Overview

Phase 1 xây dựng nền tảng API layer và data types cho NSSAAF. Không có external dependencies (AAA, NRF) trong phase này.

## Modules to Implement

### 1. `internal/types/` — Data Types

**Priority:** P0
**Dependencies:** None
**Design Doc:** `docs/design/04_data_model.md`

**Deliverables:**
- [ ] `snssai.go` — S-NSSAI type with validation
- [ ] `gpsi.go` — GPSI type with validation
- [ ] `supi.go` — SUPI type with validation
- [ ] `eap.go` — EAP message types
- [ ] `nssaa_status.go` — NssaaStatus enum
- [ ] `error.go` — Error types and ProblemDetails

**Validation Rules:**
```go
// GPSI: required for NSSAA
// Pattern: ^5[0-9]{8,14}$
// Examples: "5-208046000000001", "52080460000001"

type Gpsi string
func (g Gpsi) Validate() error {
    if !gpsiRegex.MatchString(string(g)) {
        return ErrInvalidGpsi
    }
    return nil
}

// Snssai
// SST: 0-255
// SD: 6 hex chars, optional
type Snssai struct {
    Sst uint8   `json:"sst"`
    Sd  string `json:"sd,omitempty"`
}

// NssaaStatus
// Values: NOT_EXECUTED, PENDING, EAP_SUCCESS, EAP_FAILURE
```

### 2. `internal/api/nssaa/` — Nnssaaf_NSSAA API

**Priority:** P0
**Dependencies:** `internal/types/`, `internal/storage/` (interface)
**Design Doc:** `docs/design/02_nssaa_api.md`

**Deliverables:**
- [ ] `handler.go` — HTTP handler for Nnssaaf_NSSAA
- [ ] `request.go` — SliceAuthInfo, SliceAuthConfirmationData
- [ ] `response.go` — SliceAuthContext, SliceAuthConfirmationResponse
- [ ] `router.go` — HTTP routing
- [ ] `middleware.go` — Auth middleware, logging
- [ ] `handler_test.go` — Unit tests

**API Surface:**
```go
// POST /nnssaaf-nssaa/v1/slice-authentications
// Handler: HandleCreateSliceAuthentication
// Input: SliceAuthInfo { Gpsi, Snssai, EapMessage, AmfInstanceId, ReauthNotifUri, RevocNotifUri }
// Output: SliceAuthContext { Gpsi, Snssai, AuthCtxId, EapMessage }
// Errors: 400, 403, 404, 502, 503, 504

// PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
// Handler: HandleConfirmSliceAuthentication
// Input: SliceAuthConfirmationData { Gpsi, Snssai, EapMessage }
// Output: SliceAuthConfirmationResponse { Gpsi, Snssai, EapMessage, AuthResult }
// Errors: 400, 404, 409, 410
```

### 3. `internal/api/aiw/` — Nnssaaf_AIW API

**Priority:** P0
**Dependencies:** `internal/types/`, `internal/storage/` (interface)
**Design Doc:** `docs/design/03_aiw_api.md`

**Deliverables:**
- [ ] `handler.go` — HTTP handler for Nnssaaf_AIW
- [ ] `request.go` — AuthInfo, AuthConfirmationData
- [ ] `response.go` — AuthContext, AuthConfirmationResponse
- [ ] `router.go` — HTTP routing
- [ ] `handler_test.go` — Unit tests

**Key Differences from NSSAA:**
```go
// Uses SUPI instead of GPSI
// Returns MSK on EAP_SUCCESS
// No re-auth/revocation notifications
type AuthConfirmationResponse struct {
    Supi         string     `json:"supi"`
    EapMessage   *string    `json:"eapMessage,omitempty"`
    AuthResult   *string    `json:"authResult,omitempty"`
    Msk          *string    `json:"msk,omitempty"`      // 64 bytes, base64
    PvsInfo      []PvsInfo `json:"pvsInfo,omitempty"`
    SupportedFeatures string `json:"supportedFeatures,omitempty"`
}
```

### 4. `internal/api/common/` — Shared API Utilities

**Priority:** P1
**Dependencies:** `internal/types/`

**Deliverables:**
- [ ] `problem.go` — ProblemDetails type (RFC 7807)
- [ ] `validator.go` — Common validation utilities
- [ ] `headers.go` — HTTP header constants

### 5. `cmd/nssAAF/main.go` — Entry Point

**Priority:** P0
**Dependencies:** All internal packages

**Deliverables:**
- [ ] `main.go` — Server bootstrap
- [ ] `config.go` — Configuration loading
- [ ] `wire.go` — Dependency injection setup

## Implementation Order

```
1. internal/types/           ← MUST DO FIRST
2. internal/api/common/
3. internal/api/nssaa/      ← Core API
4. internal/api/aiw/
5. cmd/nssAAF/main.go
```

## Validation Checklist

- [ ] GPSI regex validation: `^5[0-9]{8,14}$`
- [ ] SUPI regex validation: `^imu-[0-9]{15}$`
- [ ] Snssai SST range: 0-255
- [ ] Snssai SD format: 6 hex chars
- [ ] Error response format: ProblemDetails (RFC 7807)
- [ ] HTTP status codes: 201, 200, 400, 403, 404, 409, 410, 502, 503, 504
- [ ] Location header on 201
- [ ] X-Request-ID correlation
- [ ] Unit test coverage >80%

## Spec References

- TS 29.526 §7.2 — Nnssaaf_NSSAA service
- TS 29.526 §7.3 — Nnssaaf_AIW service
- TS 29.571 §5.4.4.60 — NssaaStatus
- RFC 7807 — Problem Details for HTTP APIs
