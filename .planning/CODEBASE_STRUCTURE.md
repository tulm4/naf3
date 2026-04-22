# NSSAAF Codebase Structure

**Analysis Date:** 2026-04-22

## Architecture Overview

NSSAAF implements a 3-component model for Kubernetes production deployments:

```
AMF/AUSF → HTTP Gateway (N replicas) → Biz Pods (N replicas) → AAA Gateway (2 replicas) → AAA-S
```

The split decouples TLS termination from business logic and external AAA protocol handling.

---

## 1. Package Dependency Graph

### Proto Package: Zero-Internal-Dependencies Constraint

`internal/proto/` is the **isolation boundary** between components. It MUST have zero dependencies on any other internal package:

```go
// internal/proto/http_gateway.go
import "context"

// internal/proto/aaa_transport.go
import (
    "context"
    "time"
)

// internal/proto/biz_callback.go
// (no imports — only struct definitions and constants)
```

This constraint prevents import cycles: HTTP Gateway can import `proto` without pulling in `eap/`, `radius/`, or `diameter/`.

### Internal Dependencies by Package

```
internal/proto/
  └── [NO internal dependencies — pure data transfer]

internal/aaa/
  ├── aaa.go         (empty package doc)
  ├── router.go      → internal/diameter, internal/radius
  ├── config.go      (no imports)
  └── metrics.go     (no imports)

internal/aaa/gateway/
  ├── gateway.go         → internal/proto, redis/go-redis/v9
  ├── radius_handler.go  (no external imports)
  ├── diameter_handler.go (no external imports)
  └── redis.go           → redis/go-redis/v9

internal/api/
  ├── nssaa/
  │   ├── handler.go → internal/api/common, oapi-gen/gen/nssaa, oapi-gen/gen/specs
  │   └── router.go  → oapi-gen/gen/nssaa
  ├── aiw/
  │   ├── handler.go → internal/api/common, oapi-gen/gen/aiw
  │   └── router.go  → oapi-gen/gen/aiw
  └── common/        (middleware, validators, context helpers)

internal/eap/
  ├── engine.go      → internal/types
  ├── engine_client.go (defines AAAClient interface — no imports)
  ├── session.go     (no imports)
  ├── state.go       (no imports)
  ├── codec.go       (no imports)
  ├── tls.go         (no imports)
  └── fragment.go    (no imports)

internal/radius/
  └── client.go, packet.go, attribute.go, vsa.go, message_auth.go, dtls.go
      (no internal imports — raw RADIUS protocol only)

internal/diameter/
  └── client.go, diameter.go, snssai_avp.go, eap_avp.go
      (no internal imports — raw Diameter protocol only)

internal/types/      (GPSI, SUPI, Snssai, NssaaStatus, EapMessage, AuthResult)

internal/cache/redis/
  ├── session_cache.go → redis/go-redis/v9
  ├── ratelimit.go    → redis/go-redis/v9
  ├── lock.go         → redis/go-redis/v9
  ├── idempotency.go  → redis/go-redis/v9
  └── pool.go         → redis/go-redis/v9

internal/storage/postgres/
  ├── pool.go, session.go, aaa_config.go, audit.go, migrate.go → jackc/pgx/v5

internal/config/     (YAML loading, env expansion)
internal/auth/, internal/crypto/, internal/resilience/
internal/amf/, internal/udm/, internal/nrf/  (3GPP service clients — Phase 3)
```

---

## 2. Component Boundaries

### `cmd/http-gateway/`

| Aspect | Detail |
|--------|--------|
| Entry point | `cmd/http-gateway/main.go` |
| Config component | `config.ComponentHTTPGateway` |
| Key imports | `internal/config`, `internal/proto` |
| Interfaces implemented | `proto.BizServiceClient` (by `httpBizClient` struct) |
| External dependencies | AMF/AUSF (TLS on :443), Biz Pods (HTTP on clusterIP) |

**Wiring pattern:**

```go
// cmd/http-gateway/main.go:24-55
type httpBizClient struct {
    bizServiceURL string
    httpClient    *http.Client
    version       string
}

func (c *httpBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    // Forwards to Biz Pods at cfg.HTTPgw.BizServiceURL
    // Returns (responseBody, httpStatus, error)
}
```

The HTTP Gateway is stateless. It does NOT implement load balancing itself — it relies on Kubernetes Service round-robin or istio-sidecar.

### `cmd/biz/`

| Aspect | Detail |
|--------|--------|
| Entry point | `cmd/biz/main.go` |
| Config component | `config.ComponentBiz` |
| Key imports | `internal/api/nssaa`, `internal/api/aiw`, `internal/api/common`, `internal/config`, `internal/proto` |
| Interfaces implemented | `eap.AAAClient` (by `httpAAAClient` returned from `newHTTPAAAClient()`) |
| External dependencies | Redis (heartbeat + pub/sub), AAA Gateway (HTTP on :9090) |

**Wiring pattern:**

```go
// cmd/biz/main.go:62-78
// Creates HTTP AAA client (satisfies eap.AAAClient = AAARouter)
aaaClient := newHTTPAAAClient(
    cfg.Biz.AAAGatewayURL,  // "http://svc-nssaa-aaa:9090"
    cfg.Redis.Addr,
    podID,
    cfg.Version,
    &http.Client{...},
)

// Wires to NSSAA handler
nssaaHandler := nssaa.NewHandler(nssaaStore,
    nssaa.WithAPIRoot(apiRoot),
    nssaa.WithAAA(aaaClient), // aaaClient satisfies eap.AAAClient
)
```

**Biz Pod heartbeat** (`cmd/biz/main.go:205-226`):
```go
func podHeartbeat(ctx context.Context, redisAddr, podID string) {
    // Registers pod in Redis SET: proto.PodsKey = "nssaa:pods"
    rdb.SAdd(ctx, proto.PodsKey, podID)
    // Refreshes every 30 seconds via ticker
}
```

### `cmd/aaa-gateway/`

| Aspect | Detail |
|--------|--------|
| Entry point | `cmd/aaa-gateway/main.go` |
| Config component | `config.ComponentAAAGateway` |
| Key imports | `internal/aaa/gateway`, `internal/config` |
| Interfaces implemented | `proto.BizAAAClient` (by `Gateway.ForwardEAP`) |
| External dependencies | Redis (pub/sub + session correlation), RADIUS/Diameter AAA-S |

**Wiring pattern:**

```go
// cmd/aaa-gateway/main.go:43-54
gw := gateway.New(gateway.Config{
    BizServiceURL:  cfg.AAAgw.BizServiceURL,  // "http://svc-nssaa-biz:8080"
    RedisAddr:     cfg.Redis.Addr,
    ListenRADIUS:  cfg.AAAgw.ListenRADIUS,    // ":1812"
    ListenDIAMETER: cfg.AAAgw.ListenDIAMETER,  // ":3868"
    ...
})

// Exposes HTTP endpoints for Biz Pod communication
http.HandleFunc("/aaa/forward", gw.HandleForward)
http.HandleFunc("/health/vip", gw.VIPHealthHandler)  // Keepalived VIP check
```

**AAA Gateway internal wiring** (`internal/aaa/gateway/gateway.go:56-83`):
```go
g.radiusHandler = &RadiusHandler{
    logger:          cfg.Logger,
    publishResponse: g.publishResponseBytes,
    forwardToBiz:    g.forwardToBiz,
}

g.diameterHandler = &DiameterHandler{
    logger:          cfg.Logger,
    publishResponse: g.publishResponseBytes,
    forwardToBiz:    g.forwardToBiz,
    version:         cfg.Version,
    bizURL:          cfg.BizServiceURL,
    httpClient:      g.bizHTTPClient,
}
```

---

## 3. Key Interface Patterns

### `eap.AAAClient` Interface

Defined in `internal/eap/engine_client.go:16-22`:

```go
// AAAClient is the interface for communicating with AAA-S.
// Spec: TS 29.561 §16-17
type AAAClient interface {
    // SendEAP forwards an EAP message to AAA-S and returns the response.
    // The response may be an EAP-Request (continue) or EAP-Success/Failure.
    SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
}
```

**Implementors:**
- `internal/aaa/router.go`: `(*Router).SendEAP()` — uses `radius.Client` or `diameter.Client` directly
- `cmd/biz/main.go`: `httpAAAClient` (via `newHTTPAAAClient()`) — calls AAA Gateway over HTTP

**Consumers:**
- `internal/eap/engine.go`: `Engine.aaaClient AAAClient` field, used in `forwardToAAA()`

### `proto.BizServiceClient` Interface

Defined in `internal/proto/http_gateway.go:6-20`:

```go
// BizServiceClient is the interface HTTP Gateway uses to forward N58/N60 requests to Biz Pods.
type BizServiceClient interface {
    ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error)
}
```

**Implementor:**
- `cmd/http-gateway/main.go`: `httpBizClient`

**Consumer:**
- `cmd/http-gateway/main.go` itself (both in same file)

### `proto.BizAAAClient` Interface

Defined in `internal/proto/aaa_transport.go:71-76`:

```go
// BizAAAClient is the interface the Biz Pod uses to talk to the AAA Gateway.
type BizAAAClient interface {
    ForwardEAP(ctx context.Context, req *AaaForwardRequest) (*AaaForwardResponse, error)
}
```

**Implementor:**
- `internal/aaa/gateway/gateway.go`: `(*Gateway).ForwardEAP()`

**Consumer:**
- `cmd/biz/main.go`: `httpAAAClient.ForwardEAP()`

---

## 4. Data Flow Paths

### Client-Initiated Flow: POST /nnssaaf-nssaa/v1/slice-authentications → AAA-S

```
1.  AMF → HTTP Gateway      HTTPS/443 (TLS terminated)
2.  HTTP Gateway → Biz Pod  HTTP/ClusterIP svc-nssaa-biz:8080
    └── bizClient.ForwardRequest(path, method, body)

3.  Biz Pod NSSAA Handler   internal/api/nssaa/handler.go
    ├── Validates GPSI, Snssai, eapIdRsp
    ├── Creates AuthCtx with UUID
    ├── Stores in InMemoryStore
    └── h.aaa.SendEAP(ctx, authCtxID, eapPayload)  [Phase 2]

4.  Biz Pod → AAA Gateway   HTTP POST /aaa/forward
    └── httpAAAClient.ForwardEAP(req *AaaForwardRequest)

5.  AAA Gateway             internal/aaa/gateway/gateway.go
    ├── writeSessionCorr() → Redis SET nssaa:session:{sessionId}
    ├── Routes by transport: RADIUS or Diameter
    └── radiusHandler.Forward() / diameterHandler.Forward()

6.  AAA Gateway → AAA-S    RADIUS UDP :1812 or Diameter TCP :3868
    └── Raw protocol bytes (no EAP framing at this layer)

7.  AAA-S → AAA Gateway    RADIUS/Diameter response
    ├── radiusHandler.handlePacket() / diameterHandler.HandleConnection()
    └── publishResponseBytes(sessionID, raw)

8.  AAA Gateway → Biz Pod   Redis pub/sub nssaa:aaa-response
    └── publishResponse(event *AaaResponseEvent)

9.  AAA Gateway → Biz Pod   HTTP response to /aaa/forward
    └── Returns AaaForwardResponse{Payload: response}

10. Biz Pod                  Processes response
11. Biz Pod → HTTP Gateway  HTTP 201 Created
12. HTTP Gateway → AMF      HTTPS/443
```

### Server-Initiated Flow: RAR/ASR/CoA from AAA-S → AMF

```
1.  AAA-S → AAA Gateway   RAR (RADIUS CoA-Request :1812) or ASR (Diameter :3868)

2.  AAA Gateway            internal/aaa/gateway/gateway.go
    ├── radiusHandler.handleServerInitiated() / diameterHandler.handleServerInitiated()
    └── forwardToBiz(sessionID, "RAR"/"ASR", raw)

3.  AAA Gateway            getSessionCorr(sessionID) from Redis
    └── Key: nssaa:session:{sessionId} → SessionCorrEntry{AuthCtxID, Sst, Sd}

4.  AAA Gateway → Biz Pod  HTTP POST /aaa/server-initiated
    └── AaaServerInitiatedRequest{
            Version, SessionID, AuthCtxID, TransportType, MessageType, Payload
         }

5.  Biz Pod                cmd/biz/main.go:149-186 handleServerInitiated()
    ├── Switch on MessageType: RAR → handleReAuth(), ASR → handleRevocation(), CoA → handleCoA()
    └── Returns AaaServerInitiatedResponse{Payload}

6.  AAA Gateway → AAA-S   HTTP response body returned as raw protocol bytes
```

---

## 5. Redis Key Patterns

Defined in `internal/proto/biz_callback.go:28-38`:

| Key Pattern | Type | Purpose | TTL |
|-------------|------|---------|-----|
| `nssaa:session:{sessionId}` | STRING (JSON) | Session correlation entry (`SessionCorrEntry`) | `DefaultPayloadTTL` = 10 minutes |
| `nssaa:pods` | SET | Live Biz Pod IDs for observability | No TTL (managed by heartbeat) |
| `nssaa:aaa-response` | Pub/Sub channel | AAA response events from Gateway to Pods | N/A |

**Session correlation entry** (`internal/proto/biz_callback.go:14-24`):
```go
type SessionCorrEntry struct {
    AuthCtxID string `json:"authCtxId"` // NSSAAF auth context ID
    PodID     string `json:"podId"`     // Biz Pod hostname (observability only)
    Sst       uint8  `json:"sst"`       // S-NSSAI SST
    Sd        string `json:"sd"`        // S-NSSAI SD
    CreatedAt int64  `json:"createdAt"` // Unix timestamp
}
```

**Session hot-cache keys** (`internal/cache/redis/session_cache.go:36-39`):
```go
func sessionKey(authCtxID string) string {
    return fmt.Sprintf("nssaa:session:%s", authCtxID)  // Note: different from proto.SessionCorrKeyPrefix
}
```

**Rate limiting keys** (`internal/cache/redis/ratelimit.go:29-36`):
```go
func gpsiKey(gpsiHash string) string {
    return fmt.Sprintf("nssaa:ratelimit:gpsi:%s", gpsiHash)
}
func amfKey(amfID string) string {
    return fmt.Sprintf("nssaa:ratelimit:amf:%s", amfID)
}
```

---

## 6. Testing Strategy

### Test File Locations

| Package | Test Files | Coverage |
|---------|-----------|----------|
| `internal/proto/` | `biz_callback_test.go`, `aaa_transport_test.go`, `http_gateway_test.go` | Schema validation, JSON round-trips, Redis constants |
| `internal/eap/` | `engine_test.go`, `eap_test.go` | Engine state machine, session management, EAP parsing |
| `internal/aaa/` | `aaa_test.go` | Router logic |
| `internal/aaa/gateway/` | `gateway_test.go` | Gateway lifecycle, VIP health |
| `internal/radius/` | `radius_test.go`, `client_test.go` | RADIUS packet building, attribute encoding |
| `internal/diameter/` | `diameter_test.go` | Diameter message parsing |
| `internal/api/nssaa/` | `handler_test.go` | N58 endpoint validation |
| `internal/api/aiw/` | `handler_test.go` | AIW endpoint validation |
| `internal/api/common/` | `common_test.go` | Middleware, validators |
| `internal/types/` | `types_test.go` | GPSI/SUPI/Snssai validation |
| `internal/config/` | `config_test.go` | YAML loading, env expansion |
| `internal/cache/redis/` | `cache_test.go` | Redis operations |
| `internal/storage/postgres/` | `session_test.go` | PostgreSQL session storage |

### Mock Patterns

**Interface mocking** (e.g., `internal/proto/http_gateway_test.go:10-27`):
```go
type mockBizServiceClient struct {
    forwardCalled       bool
    forwardPath        string
    forwardMethod      string
    forwardBody        []byte
    forwardRespBody    []byte
    forwardRespStatus  int
    forwardRespErr     error
}

func (m *mockBizServiceClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    m.forwardCalled = true
    // ... capture args ...
    return m.forwardRespBody, m.forwardRespStatus, m.forwardRespErr
}
```

**Test session management** (`internal/eap/engine_client.go:130-155`):
```go
// Exported helpers for package-level tests
func NewTestSessionManager(ttl time.Duration) *sessionManager
func (m *sessionManager) TestPut(session *Session)
func (m *sessionManager) TestGet(authCtxID string) (*Session, error)
func (m *sessionManager) TestSize() int
func NewTestSession(authCtxID, gpsi string) *Session
```

---

## 7. Notable Architecture Decisions

### 1. Proto Package Isolation

`internal/proto/` is the only package that can be imported by all three binaries without creating import cycles. It contains:
- Wire protocol types (`AaaForwardRequest`, `AaaServerInitiatedRequest`, `SessionCorrEntry`)
- Redis key/channel constants (`SessionCorrKeyPrefix`, `PodsKey`, `AaaResponseChannel`)
- Interface definitions (`BizServiceClient`, `BizAAAClient`)
- Version header constant (`HeaderName = "X-NSSAAF-Version"`)

### 2. AAA Gateway Active-Standby via Keepalived

The AAA Gateway runs 2 replicas with Keepalived for active-standby failover. VIP health check:
```go
// internal/aaa/gateway/gateway.go:356-372
func (g *Gateway) VIPHealthHandler(w http.ResponseWriter, r *http.Request) {
    statePath := g.cfg.KeepalivedStatePath  // "/var/run/keepalived/state"
    data, readKeepalivedState(statePath)
    // Returns 200 if MASTER, 503 if BACKUP
}
```

### 3. Session Correlation via Redis

Instead of tracking which Biz Pod owns which session, the AAA Gateway uses Redis:
- Writes `SessionCorrEntry` before forwarding to AAA-S
- Reads entry on response arrival or server-initiated routing
- All Biz Pods subscribe to `nssaa:aaa-response` channel and discard non-matching events

This avoids sticky sessions and allows any Biz Pod to handle the response.

### 4. In-Memory vs Redis Stores

Phase 1 uses in-memory stores (`InMemoryStore` in `internal/api/nssaa/` and `internal/api/aiw/`). Phase 3 replaces these with Redis-backed implementations using the same `AuthCtxStore` interface.

### 5. OAPI-Codegen for API Handlers

API handlers implement the `oapi-codegen.ServerInterface` generated from 3GPP OpenAPI specs:
- N58 (NSSAA): `oapi-gen/gen/nssaa`
- AIW: `oapi-gen/gen/aiw`
- Specs: `oapi-gen/gen/specs` (shared types like `AuthStatus`)

The generated router validates requests; handlers delegate to business logic.

---

*Codebase structure analysis: 2026-04-22*
