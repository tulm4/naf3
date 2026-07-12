# NSSAA Callback Handler — Conformance Test Rewrite Implementation Plan

**Date:** 2026-06-29
**Spec:** `docs/superpowers/specs/2026-06-29-nssaa-callback-handler-conformance-test-design.md`
**Phase:** 06-Testing (supplemental task)

---

## Overview

Rewrite `test/conformance/nssaa_callbacks_test.go` to target the correct endpoint (`POST /aaa/server-initiated`) instead of non-existent N58/N60 callback paths.

**What exists:**
- `cmd/biz/server_initiated.go` — `ServerInitiatedHandler` adapter
- `internal/biz/server_initiated.go` — `ServerInitiatedCoordinator` (RAR/ASR/CoA handling)
- `test/integration/server_initiated_flow_test.go` — integration tests covering end-to-end flow

**What needs to be done:**
- Rewrite `nssaa_callbacks_test.go` with TS 29.526 §7.2.4-5 conformance test cases
- Use `proto.AaaServerInitiatedRequest` wire format
- 24 test cases across happy path, error cases, and session state validation

---

## Task 1: Rewrite nssaa_callbacks_test.go

**Files touched:** `test/conformance/nssaa_callbacks_test.go`

### 1.1 Imports

```go
import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
    "github.com/operator/nssAAF/cmd/biz"
    "github.com/operator/nssAAF/internal/biz"
    redisstore "github.com/operator/nssAAF/internal/cache/redis"
    "github.com/operator/nssAAF/internal/proto"
    "github.com/operator/nssAAF/internal/api/common"
    "github.com/alicebob/miniredis/v2"
    goredis "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

### 1.2 In-Memory Test Fixtures

```go
// inMemoryReverseSessionStore — from test/integration/server_initiated_flow_test.go
// noopStateWriter
// noopAIWLinker
// noopNotifier
```

### 1.3 Test Setup Helper

```go
func setupCallbackTest(t *testing.T, msgType proto.MessageType, sessionID, authCtxID string, payload []byte, owner, reauthURI, revocURI string) (*httptest.ResponseRecorder, *httptest.Server, func())
```

Returns: response recorder, AMF mock server, teardown function.

### 1.4 Happy Path Tests

| Test Function | Message Type | Description |
|---|---|---|
| `TestTS29526_NSSAA_Callback_RAR_AMFNotified` | RAR | RAR → MarkReauthPending → AMF notified → 200 |
| `TestTS29526_NSSAA_Callback_ASR_AMFNotified` | ASR | ASR → MarkRevoked → AMF notified → 200 |
| `TestTS29526_NSSAA_Callback_CoA_AMFNotified` | COA | CoA → ApplyCoA → updated → 200 |
| `TestTS29526_NSSAA_Callback_DM_AMFNotified` | DM | DM → MarkRevoked → AMF notified → 200 |
| `TestTS29526_NSSAA_Callback_RAR_AUSFOwner` | RAR | RAR → AIW linked → no AMF callback → 200 |

### 1.5 Error Tests

| Test Function | Error Condition | Expected Status |
|---|---|---|
| `TestTS29526_NSSAA_Callback_MissingSessionID` | sessionId field omitted | 400 |
| `TestTS29526_NSSAA_Callback_MissingAuthCtxID` | authCtxId field omitted | 400 |
| `TestTS29526_NSSAA_Callback_UnknownSession` | session not in Redis | 502 |
| `TestTS29526_NSSAA_Callback_UnknownAuthCtx` | authCtx not in store | 502 |
| `TestTS29526_NSSAA_Callback_InvalidMessageType` | messageType: "INVALID" | 400 |
| `TestTS29526_NSSAA_Callback_NonJSONBody` | Content-Type: text/plain | 415 |
| `TestTS29526_NSSAA_Callback_WrongMethod` | GET on /aaa/server-initiated | 405 |

### 1.6 Response Header Tests

| Test Function | Description |
|---|---|
| `TestTS29526_NSSAA_Callback_ResponseHeaders_RAR` | X-Request-ID present on RAR response |
| `TestTS29526_NSSAA_Callback_ResponseHeaders_ASR` | X-Request-ID present on ASR response |
| `TestTS29526_NSSAA_Callback_ResponseHeaders_CoA` | X-Request-ID present on CoA response |

### 1.7 Session State Validation Tests

| Test Function | Description |
|---|---|
| `TestTS29526_NSSAA_Callback_SessionState_AfterRAR` | Session marked PENDING after RAR |
| `TestTS29526_NSSAA_Callback_SessionState_AfterASR` | Session marked REVOKED after ASR |
| `TestTS29526_NSSAA_Callback_SessionState_AfterCoA` | Session CoA data updated after CoA |

### 1.8 Remove

- All `t.Skip()` calls from original file
- Original test function bodies that tested non-existent endpoints

---

## Task 2: Verify Build & Tests

```bash
go build ./test/conformance/...
go test ./test/conformance/... -run TestTS29526_NSSAA_Callback -v
```

**Expected:** All 24 tests pass.

---

## Task 3: Lint

```bash
golangci-lint run ./test/conformance/...
```

**Expected:** No errors.

---

## Task 4: Update Roadmap (if needed)

Check `docs/roadmap/PHASE_6_Testing_NRM.md` — confirm callback test coverage is documented under §7.2.4-5 conformance.

---

## Verification

| Check | Command | Expected |
|---|---|---|
| Build | `go build ./test/conformance/...` | ✅ success |
| Tests | `go test ./test/conformance/... -run TestTS29526_NSSAA_Callback -v` | ✅ 24/24 pass |
| Lint | `golangci-lint run ./test/conformance/...` | ✅ no errors |
| Coverage | `go test ./test/conformance/... -cover` | report coverage |

---

## Dependencies

- `test/integration/server_initiated_flow_test.go` — pattern reference for fixtures
- `internal/proto/http_gateway.go` — `AaaServerInitiatedRequest`, `AaaServerInitiatedResponse`, `MessageType`
- `internal/biz/server_initiated.go` — `ServerInitiatedCoordinator`, `NssaaSessionContext`
- `cmd/biz/server_initiated.go` — `NewServerInitiatedHandler`

---

## Files

| Action | File |
|---|---|
| Rewrite | `test/conformance/nssaa_callbacks_test.go` |

---

*Plan created: 2026-06-29*
