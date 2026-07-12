# NSSAA Callback Handler — Conformance Test Rewrite Design

**Date:** 2026-06-29
**Status:** Draft
**Spec Refs:** TS 29.526 §7.2.4-5, TS 23.502 §4.2.9.3

---

## 1. Problem Statement

The file `test/conformance/nssaa_callbacks_test.go` contains conformance tests that target non-existent HTTP endpoints:

- `POST /slice-authentications/{authCtxId}/reauthentication-notification`
- `POST /slice-authentications/{authCtxId}/revocation-notification`

These paths are not part of the NSSAAF architecture. All tests are skipped with `t.Skip()`. The file needs to be rewritten to test the correct endpoints.

---

## 2. Architecture

### 2.1 Server-Initiated Flow (Actual Architecture)

```
AAA-S (enterprise RADIUS/Diameter server)
    │
    │  RADIUS: CoA-Request (Code 43) / Disconnect-Request (Code 40)
    │  Diameter: RAR (Command 258) / ASR (Command 274) / STR (Command 275)
    │
    ▼
AAA Gateway (cmd/aaa-gateway/)
    │  RADIUS/Diameter listeners on UDP :1812 / TCP :3868
    │  Validates Message-Authenticator / signature
    │  Looks up session correlation in Redis → gets authCtxId
    │  HTTP POST /aaa/server-initiated to Biz Pod
    │
    ▼
Biz Pod (cmd/biz/)
    │  POST /aaa/server-initiated → ServerInitiatedHandler
    │  → ServerInitiatedCoordinator
    │    - Resolve session context (Redis + PostgreSQL)
    │    - MarkReauthPending / MarkRevoked / ApplyCoA
    │    - SendReAuthNotification → AMF at reauthNotifUri
    │    - SendRevocationNotification → AMF at revocNotifUri
    │  Returns response to AAA Gateway
    │
    ▼
AAA Gateway
    │  Forwards Biz Pod response to AAA-S
    │  RADIUS: CoA-NAK / CoA-ACK or DM-NAK / DM-ACK
    │  Diameter: RAA / ASA / STA
    │
    ▼
AAA-S
```

### 2.2 What Exists vs. What's Missing

| Component | Status | Location |
|---|---|---|
| AAA Gateway RADIUS handler (CoA/DM) | ✅ Done | `internal/aaa/gateway/radius_handler.go` |
| AAA Gateway Diameter handler (ASR/RAR/STR) | ✅ Done | `internal/aaa/gateway/diameter_handler.go` |
| `POST /aaa/server-initiated` Biz Pod handler | ✅ Done | `cmd/biz/server_initiated.go` |
| ServerInitiatedCoordinator | ✅ Done | `internal/biz/server_initiated.go` |
| AMF notification (reauth/revocation) | ✅ Done | `internal/amf/` |
| **Conformance tests** | ❌ Wrong endpoints | `test/conformance/nssaa_callbacks_test.go` |

The tests in `nssaa_callbacks_test.go` target N58/N60 HTTP callback paths that don't exist and shouldn't exist per the architecture. The correct test target is `POST /aaa/server-initiated` — the Biz Pod HTTP handler that the AAA Gateway calls.

---

## 3. Design

### 3.1 Test File Rewrite

Rewrite `test/conformance/nssaa_callbacks_test.go` to:

1. Import `proto.AaaServerInitiatedRequest` and `proto.AaaServerInitiatedResponse` from `internal/proto/http_gateway.go`
2. Use the existing `ServerInitiatedCoordinator` and supporting infrastructure
3. Follow the TS 29.526 §7.2.4-5 conformance test naming convention
4. Use table-driven subtests matching `test/integration/server_initiated_flow_test.go` patterns

### 3.2 Protocol Types

```go
// Transport types
const (
    TransportRADIUS  TransportType = "RADIUS"   // CoA, DM
    TransportDIAMETER TransportType = "DIAMETER" // RAR, ASR, STR
)

// Message types
const (
    MessageTypeRAR         MessageType = "RAR"   // Diameter Re-Auth-Request → Re-authentication
    MessageTypeASR         MessageType = "ASR"   // Diameter Abort-Session-Request → Revocation
    MessageTypeCoA         MessageType = "COA"  // RADIUS Change-of-Authorization → Re-authentication
    MessageTypeDM          MessageType = "DM"    // RADIUS Disconnect-Request → Revocation
    MessageTypeSTR         MessageType = "STR"   // Diameter Session-Termination-Request
)
```

### 3.3 Test Cases

| Test ID | Spec | Message Type | Description |
|---|---|---|---|
| TC-CB-001 | §7.2.5 | RAR | RAR → session re-auth pending → AMF notified → 200 OK |
| TC-CB-002 | §7.2.4 | ASR | ASR → session revoked → AMF notified → 200 OK |
| TC-CB-003 | §7.2.4 | DM | DM (RADIUS) → session revoked → AMF notified → 200 OK |
| TC-CB-004 | §7.2.5 | COA | COA → session updated → no AMF callback → 200 OK |
| TC-CB-005 | §7.2.5 | RAR + AUSF owner | RAR → AIW linked (no AMF callback) → 200 OK |
| TC-CB-006 | §7.2.4 | ASR + AUSF owner | ASR → AIW linked → 200 OK |
| TC-CB-010 | §7.2.4 | any | Missing sessionId → 400 Bad Request |
| TC-CB-011 | §7.2.4 | any | Missing authCtxId → 400 Bad Request |
| TC-CB-012 | §7.2.4 | any | Unknown session → 502 Bad Gateway |
| TC-CB-013 | §7.2.4 | any | Invalid messageType → 400 Bad Request |
| TC-CB-014 | §7.2.4 | any | Non-JSON body → 415 Unsupported Media Type |
| TC-CB-015 | §7.2.4 | any | Wrong HTTP method (GET/PUT/DELETE) → 405 Method Not Allowed |
| TC-CB-020 | §7.2.4 | RAR | Response includes X-Request-ID header |
| TC-CB-021 | §7.2.4 | ASR | Response includes X-Request-ID header |
| TC-CB-022 | §7.2.4 | COA | Response includes X-Request-ID header |
| TC-CB-030 | §7.2.5 | RAR | After RAR → session status → PENDING |
| TC-CB-031 | §7.2.5 | COA | After COA → session CoA data updated |
| TC-CB-032 | §7.2.4 | ASR | After ASR → session status → REVOKED |
| TC-CB-033 | §7.2.4 | DM | After DM → session status → REVOKED |

### 3.4 Test Infrastructure

Use the same approach as `test/integration/server_initiated_flow_test.go`:

- **In-memory stores**: `inMemoryReverseSessionStore` (session context), `noopStateWriter`, `noopAIWLinker`
- **Mock AMF**: `httptest.Server` that captures re-auth/revocation notifications
- **Redis**: `miniredis` for session correlation store
- **Router**: `chi.Router` with the `ServerInitiatedHandler` adapter

```go
// Test setup pattern
func setupCallbackTest(t *testing.T, msgType proto.MessageType, ...) (*httptest.ResponseRecorder, func()) {
    mr, _ := miniredis.Run()
    rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
    defer func() { _ = rdb.Close() }()

    corrStore := redisstore.NewSessionCorrelationStore(rdb, proto.DefaultPayloadTTL)
    _ = corrStore.Save(ctx, sessionID, proto.SessionCorrEntry{AuthCtxID: authCtxID})

    persistentStore := newInMemoryReverseSessionStore()
    persistentStore.sessions[authCtxID] = &biz.NssaaSessionContext{...}

    resolver := biz.NewCorrelationResolver(rdb, persistentStore)
    coordinator := biz.NewServerInitiatedCoordinator(resolver, noopStateWriter{}, notifier, noopAIWLinker{})
    handler := cmd_biz.NewServerInitiatedHandler(coordinator)

    router := chi.NewRouter()
    router.HandleFunc("/aaa/server-initiated", func(w http.ResponseWriter, r *http.Request) {
        // dispatch based on messageType
        switch req.MessageType {
        case proto.MessageTypeRAR, proto.MessageTypeCoA:
            resp, err := handler.HandleReAuth(ctx, req)
        case proto.MessageTypeASR, proto.MessageTypeDM:
            resp, err := handler.HandleRevocation(ctx, req)
        }
    })

    reqBody, _ := json.Marshal(&proto.AaaServerInitiatedRequest{...})
    request := httptest.NewRequest(http.MethodPost, "/aaa/server-initiated", bytes.NewReader(reqBody))
    recorder := httptest.NewRecorder()
    router.ServeHTTP(recorder, request)
    return recorder, func() { mr.Close(); _ = rdb.Close() }
}
```

### 3.5 Validation Checklist

- [ ] `go build ./test/conformance/...` compiles without errors
- [ ] `go test ./test/conformance/... -run TestTS29526_NSSAA_Callback -v` passes
- [ ] `golangci-lint run ./test/conformance/...` passes
- [ ] All 24 test cases implemented and passing
- [ ] Original `t.Skip()` calls removed
- [ ] Protocol type enum validation test (`TC-CB-020`) kept from original

---

## 4. Verification

### Spec Traceability

| Spec Requirement | Test Coverage |
|---|---|
| TS 29.526 §7.2.5 — Re-Authentication Notification | TC-CB-001, TC-CB-004, TC-CB-030 |
| TS 29.526 §7.2.4 — Revocation Notification | TC-CB-002, TC-CB-003, TC-CB-031 |
| TS 23.502 §4.2.9.3 — AMF notification on re-auth | TC-CB-001 (AMF mock observed) |
| TS 23.502 §4.2.9.3 — AMF notification on revocation | TC-CB-002 (AMF mock observed) |
| TS 29.500 §6.1 — X-Request-ID correlation | TC-CB-020, TC-CB-021, TC-CB-022 |

### Build & Test

```bash
go build ./test/conformance/...
go test ./test/conformance/... -run TestTS29526_NSSAA_Callback -v
golangci-lint run ./test/conformance/...
```

---

*Last updated: 2026-06-29*
