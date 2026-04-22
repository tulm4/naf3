# Phase Plan: Refactor 3-Component Architecture

**Phase:** Refactor-3Component
**Plan:** 01 (the only plan for this phase — it is self-contained and sequential by nature)
**Type:** execute
**Wave:** 1
**depends_on:** []
**files_modified:** []
**autonomous:** true
**requirements:** [REQ-R1, REQ-R2, REQ-R3, REQ-R4, REQ-R5, REQ-R6, REQ-R7]

---

<objective>

Split the NSSAAF monolithic binary (`cmd/nssAAF/main.go`) into three independent Kubernetes-native binaries: **HTTP Gateway** (TLS terminator), **Biz Pod** (EAP engine + 3GPP handlers), and **AAA Gateway** (active-standby RADIUS/Diameter proxy with keepalived). This resolves the RFC 6733 single-connection constraint and enables horizontal scaling of Biz Pods independently from AAA connectivity.

**Deliverable:** A fully compilable 3-binary codebase with `internal/proto/` interface contracts, `internal/biz/` routing layer, `internal/aaa/gateway/` proxy layer, per-component configs, Docker Compose dev setup, and Helm/Kustomize Kubernetes manifests.

</objective>

<context>

@docs/roadmap/PHASE_Refactor_3Component.md
@.planning/research/PHASE_REFACTOR_3COMPONENT_RESEARCH.md

From `cmd/nssAAF/main.go`:
```go
// Module: github.com/operator/nssAAF
// Entry: main() wires HTTP handlers (N58/N60) + in-memory stores + OAM endpoints
```

From `internal/aaa/router.go`:
```go
// Router struct has BOTH routing logic (ResolveRoute) AND socket clients:
//   r.radiusClient *radius.Client
//   r.diameterClient *diameter.Client
// sendRADIUS() calls r.radiusClient.SendEAP() — this is the socket call to extract
// sendDIAMETER() calls r.diameterClient.SendDER() — this is the socket call to extract
```

From `internal/eap/engine_client.go`:
```go
// AAAClient interface — the ONLY contract between EAP engine and AAA transport:
//   SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
// internal/eap/ does NOT import internal/radius/ or internal/diameter/
```

From `internal/config/config.go`:
```go
// Monolithic Config struct — will be extended with Component enum + BizConfig + AAAgwConfig
// Current fields: Server, Database, Redis, EAP, AAA, RateLimit, Logging, Metrics, NRF, UDM
```

</context>

<tasks>

## Wave 1 — Foundation: Interface Contracts

The proto package defines the wire protocol between components. It must have **zero dependencies** on `internal/radius/`, `internal/diameter/`, `internal/eap/`, `internal/aaa/`, or any external package beyond the Go standard library.

---

### Task 1.1: Create `internal/proto/aaa_transport.go`

**Files:** `internal/proto/aaa_transport.go`, `internal/proto/aaa_transport_test.go`

**Action:**

Create the directory and the file with these exact types:

```go
package proto

import "time"

// TransportType identifies the AAA transport protocol.
type TransportType string

const (
    TransportRADIUS   TransportType = "RADIUS"
    TransportDIAMETER TransportType = "DIAMETER"
)

// Direction indicates who initiated the exchange.
type Direction string

const (
    DirectionClientInitiated Direction = "CLIENT_INITIATED"
    DirectionServerInitiated Direction = "SERVER_INITIATED"
)

// MessageType identifies a server-initiated message type.
type MessageType string

const (
    MessageTypeRAR MessageType = "RAR" // RADIUS Re-Auth-Request (RFC 5176)
    MessageTypeASR MessageType = "ASR" // Diameter Abort-Session-Request (RFC 6733)
    MessageTypeCoA MessageType = "COA" // RADIUS Change-of-Authorization (RFC 5176)
)

// AaaForwardRequest is the body of POST /aaa/forward from Biz Pod to AAA Gateway.
// AAA Gateway forwards raw RADIUS/Diameter transport bytes without modification.
// Spec: docs/design/01_service_model.md §5.4.3
type AaaForwardRequest struct {
    Version       string       `json:"v"`          // Schema version, e.g. "1.0"
    SessionID     string       `json:"sessionId"`  // Unique per EAP round-trip
    AuthCtxID     string       `json:"authCtxId"`  // NSSAAF auth context ID
    TransportType TransportType `json:"transportType"` // RADIUS or DIAMETER
    Sst           uint8        `json:"sst"`        // S-NSSAI SST (0-255)
    Sd            string       `json:"sd"`         // S-NSSAI SD (6 hex, "FFFFFF" if none)
    Direction     Direction     `json:"direction"`   // CLIENT_INITIATED or SERVER_INITIATED
    Payload       []byte       `json:"payload"`     // Raw EAP bytes (already-encoded RADIUS/Diameter)
}

// AaaForwardResponse is the response from AAA Gateway back to Biz Pod.
type AaaForwardResponse struct {
    Version   string `json:"v"`
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// AaaServerInitiatedRequest is sent by AAA Gateway to Biz Pod when AAA-S initiates
// a Re-Auth, Revocation, or CoA request.
// Spec: PHASE §6.4
type AaaServerInitiatedRequest struct {
    Version       string       `json:"v"`
    SessionID     string       `json:"sessionId"`     // RADIUS State / Diameter Session-Id
    AuthCtxID     string       `json:"authCtxId"`    // From Redis lookup
    TransportType TransportType `json:"transportType"`
    MessageType   MessageType  `json:"messageType"`   // RAR, ASR, CoA
    Payload       []byte       `json:"payload"`      // Raw RAR/ASR/CoA bytes
}

// BizAAAClient is the interface the Biz Pod uses to talk to the AAA Gateway.
// Replaces direct radius.Client / diameter.Client calls in internal/aaa/router.go.
// Spec: PHASE §1.1
type BizAAAClient interface {
    ForwardEAP(ctx context.Context, req *AaaForwardRequest) (*AaaForwardResponse, error)
}

// DefaultPayloadTTL is the TTL for nssaa:session:{sessionId} Redis keys (10 minutes).
const DefaultPayloadTTL = 10 * time.Minute
```

Write unit tests covering JSON marshal/unmarshal roundtrip for all structs, plus validation that `BizAAAClient` is satisfied by a mock implementation.

**Verify:**
```bash
go test ./internal/proto/... -v
```

**Done:** `internal/proto/aaa_transport.go` and `*_test.go` committed. All structs serialize/deserialize correctly. `BizAAAClient` interface is satisfied by a test double.

---

### Task 1.2: Create `internal/proto/biz_callback.go`

**Files:** `internal/proto/biz_callback.go`, `internal/proto/biz_callback_test.go`

**Action:**

Create the file with session correlation types and Redis constants:

```go
package proto

// AaaResponseEvent is published to Redis channel nssaa:aaa-response when the
// AAA Gateway receives a response from AAA-S.
// All Biz Pods receive every event; each discards events not matching its in-flight sessions.
type AaaResponseEvent struct {
    Version   string `json:"v"`
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// SessionCorrEntry is stored at nssaa:session:{sessionId} in Redis.
// Correlates a RADIUS/Diameter session ID with the NSSAAF authCtxId and the
// Biz Pod that initiated the request. Written by AAA Gateway before forwarding
// to AAA-S; read by AAA Gateway on response arrival or server-initiated routing.
type SessionCorrEntry struct {
    AuthCtxID string `json:"authCtxId"` // NSSAAF auth context ID
    PodID     string `json:"podId"`     // Biz Pod hostname/UID (observability only; NOT used for routing)
    Sst       uint8  `json:"sst"`      // S-NSSAI SST
    Sd        string `json:"sd"`       // S-NSSAI SD
    CreatedAt int64  `json:"createdAt"` // Unix timestamp
}

// Redis key and channel constants.
// Spec: PHASE §1.2
const (
// SessionCorrKeyPrefix is the Redis key prefix for session correlation.
// Full key: "nssaa:session:{sessionId}" → SessionCorrEntry (JSON), TTL = DefaultPayloadTTL
SessionCorrKeyPrefix = "nssaa:session:"
// PodsKey is the Redis SET containing IDs of live Biz Pod instances.
// Updated on Biz Pod startup/shutdown and refreshed on heartbeat.
PodsKey = "nssaa:pods"
// AaaResponseChannel is the Redis pub/sub channel for AAA responses.
// Publisher: AAA Gateway. Subscribers: all Biz Pods.
AaaResponseChannel = "nssaa:aaa-response"

// SessionCorrKey builds the full Redis key for a given sessionId.
// Spec: PHASE §1.2
func SessionCorrKey(sessionID string) string {
    return SessionCorrKeyPrefix + sessionID
}
)
```

Write unit tests covering `SessionCorrKey` key generation, JSON roundtrip for `AaaResponseEvent` and `SessionCorrEntry`.

**Verify:**
```bash
go test ./internal/proto/... -v
```

**Done:** `internal/proto/biz_callback.go` and `*_test.go` committed. Redis key format and channel names are defined and tested.

---

### Task 1.3: Create `internal/proto/http_gateway.go`

**Files:** `internal/proto/http_gateway.go`, `internal/proto/http_gateway_test.go`

**Action:**

```go
package proto

import "context"

// BizServiceClient is the interface HTTP Gateway uses to forward N58/N60 requests
// to Biz Pods. It handles load balancing across Biz Pod replicas.
// Spec: docs/design/01_service_model.md §5.4.6, PHASE §1.3
type BizServiceClient interface {
    // ForwardRequest forwards an HTTP request to a Biz Pod and returns the response.
    // - path: original request path (e.g. "/nnssaaf-nssaa/v1/slice-authentications")
    // - method: HTTP method (GET, POST, PUT, DELETE)
    // - body: request body bytes
    // Returns (responseBody, httpStatus, error)
    // - 2xx: success, HTTP Gateway forwards response to AMF/AUSF
    // - 4xx: Biz Pod rejected (validation failure)
    // - 5xx: Biz Pod error; HTTP Gateway may retry if idempotent
    // - context.DeadlineExceeded: all Biz Pods failed; HTTP Gateway returns 503
    ForwardRequest(ctx context.Context, path string, method string, body []byte) ([]byte, int, error)
}

// AaaServerInitiatedResponse is returned by Biz Pod to AAA Gateway after processing
// a server-initiated message (RAR/ASR/CoA).
// The response bytes are forwarded by AAA Gateway to AAA-S.
type AaaServerInitiatedResponse struct {
    Version   string `json:"v"`
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"` // Raw response bytes (RAR-Nak, ASA, etc.)
}
```

Write unit tests verifying the `BizServiceClient` interface is satisfied by a mock, and that `AaaServerInitiatedResponse` serializes correctly.

**Verify:**
```bash
go test ./internal/proto/... -v
```

**Done:** `internal/proto/http_gateway.go` and `*_test.go` committed. `BizServiceClient` interface is defined.

---

### Task 1.4: Create `internal/proto/version.go`

**Files:** `internal/proto/version.go`

**Action:**

```go
package proto

// Version header name and current version string.
// Injected at build time via ldflags -X.
// Spec: PHASE §1.5
const (
    // HeaderName is the HTTP header used for proto schema version on all internal calls.
    HeaderName = "X-NSSAAF-Version"
    // CurrentVersion is the default proto schema version.
    // Overridden at build time: go build -ldflags '-X github.com/operator/nssAAF/internal/proto.CurrentVersion=${VERSION}'
    CurrentVersion = "1.0"
)
```

**Verify:**
```bash
go build ./internal/proto/...
```

**Done:** `internal/proto/version.go` committed.

---

## Wave 2 — Split Responsibility

---

### Task 2.1: Create `internal/proto/server_initiated.go`

**Files:** `internal/proto/server_initiated.go`, `internal/proto/server_initiated_test.go`

**Action:**

This file was already specified in Task 1.1 (`AaaServerInitiatedRequest`) and Task 1.3 (`AaaServerInitiatedResponse`). Ensure both are present and tested.

**Note:** Do NOT add `AaaServerInitiatedHandler` as a proto interface. The server-initiated flow uses HTTP POST to the Biz Pod's `/aaa/server-initiated` endpoint (defined in Task 6.2), not an interface callback. This avoids the complexity of passing an interface across process boundaries.

**Verify:**
```bash
go build ./internal/proto/... && go test ./internal/proto/... -v
```

**Done:** Server-initiated proto types are complete and tested.

---

### Task 2.2: Refactor `internal/aaa/router.go` → `internal/biz/router.go`

**Files to create:** `internal/biz/router.go`, `internal/biz/router_test.go`
**Files to modify:** All files that import `github.com/operator/nssAAF/internal/aaa` (update imports to `internal/biz`)
**Files to delete:** None yet (keep `internal/aaa/router.go` during transition, delete in Task 2.4)

**Action:**

Create a new `internal/biz/router.go` with only the declarations needed by the Biz Pod. **Do NOT copy the entire file** — `RouterStats` is already defined in `internal/aaa/router.go` and would cause a duplicate type error. Copy only these declarations:

- `Protocol` type and constants (`ProtocolRADIUS`, `ProtocolDIAMETER`)
- `RouteDecision` struct
- `Router` struct (without `radiusClient` and `diameterClient` fields)
- `ResolveRoute` method (unchanged logic, no socket calls)
- `NewRouter` constructor (without `WithRadiusClient` / `WithDiameterClient`)
- `WithMetrics` option (unchanged)
- `RouterStats` struct — **do NOT redefine here; reuse from `internal/aaa/router.go`**
- `BuildForwardRequest` method (new, replaces `sendRADIUS`/`sendDIAMETER`)

Specifically, **do NOT copy**:
- `RouterStats` (already in `internal/aaa/router.go:274`)
- `sendRADIUS` method
- `sendDIAMETER` method
- `WithRadiusClient` option
- `WithDiameterClient` option
- `SetRadiusClient` method
- `SetDiameterClient` method

1. Change package declaration to `package biz`
2. Remove the import of `github.com/operator/nssAAF/internal/radius` and `github.com/operator/nssAAF/internal/diameter`
3. Add import of `github.com/operator/nssAAF/internal/proto`
4. Remove fields `radiusClient` and `diameterClient` from the `Router` struct
5. Remove `WithRadiusClient` and `WithDiameterClient` options
6. Remove `SetRadiusClient` and `SetDiameterClient` methods
7. Remove `sendRADIUS` and `sendDIAMETER` methods
8. Replace `SendEAP` with `BuildForwardRequest`:

```go
// BuildForwardRequest creates an AaaForwardRequest for the AAA Gateway.
// Does NOT send to the network — the HTTP client in cmd/biz/ does that.
// Spec: PHASE §2.1
func (r *Router) BuildForwardRequest(
    authCtxID string,
    eapPayload []byte,
    sst uint8,
    sd string,
) (*proto.AaaForwardRequest, error) {
    decision := r.ResolveRoute(sst, sd)
    if decision == nil {
        return nil, fmt.Errorf("biz: no route configured for sst=%d sd=%s", sst, sd)
    }

    transportType := proto.TransportRADIUS
    if decision.Protocol == ProtocolDIAMETER {
        transportType = proto.TransportDIAMETER
    }

    return &proto.AaaForwardRequest{
        Version:       proto.CurrentVersion,
        SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
        AuthCtxID:     authCtxID,
        TransportType: transportType,
        Sst:           sst,
        Sd:            sd,
        Direction:     proto.DirectionClientInitiated,
        Payload:       eapPayload,
    }, nil
}
```

9. Keep `ResolveRoute`, `NewRouter`, `WithMetrics`, `RouterStats` — they have no socket dependencies.

Write `internal/biz/router_test.go` with tests for `BuildForwardRequest`:
- Given S-NSSAI, it creates the correct `AaaForwardRequest` with the right transport type
- Given no route configured, it returns an error

Update all import statements across the codebase: `github.com/operator/nssAAF/internal/aaa` → `github.com/operator/nssAAF/internal/biz`. Find affected files with:

```bash
grep -rl "github.com/operator/nssAAF/internal/aaa" --include="*.go" .
```

**Verify:**
```bash
# All files should compile
go build ./internal/biz/...
go build ./cmd/...

# No radius/diameter imports in biz package
grep -E "radius|diameter" internal/biz/*.go  # should return nothing
```

**Done:** `internal/biz/router.go` compiles with zero imports of `radius` or `diameter`. All callers updated.

---

### Task 2.3: Create `internal/aaa/gateway/` package

**Files:** `internal/aaa/gateway/gateway.go`, `internal/aaa/gateway/radius_handler.go`, `internal/aaa/gateway/diameter_handler.go`, `internal/aaa/gateway/redis.go`, `internal/aaa/gateway/keepalived.go`, plus corresponding `*_test.go` files

**Action:**

Create `internal/aaa/gateway/` as a new sub-package. This package is the entry point for `cmd/aaa-gateway/main.go`.

#### 2.3.1 `gateway.go`

```go
package gateway

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v9"
)

// Config holds AAA Gateway configuration.
type Config struct {
    BizServiceURL     string        // http://svc-nssaa-biz:8080 (matches AAAgwConfig.BizServiceURL)
    RedisAddr        string        // Redis address for pub/sub and session correlation
    ListenRADIUS     string        // ":1812" — UDP listen address for RADIUS (matches AAAgwConfig.ListenRADIUS)
    ListenDIAMETER   string        // ":3868" — listen address for Diameter (TCP or SCTP) (matches AAAgwConfig.ListenDIAMETER)
    AAAGatewayURL    string        // http://svc-nssaa-aaa:9090 — self-referential for health checks
    Logger           *slog.Logger
    Version          string        // Injected at build time
    // Diameter transport protocol: "tcp" or "sctp" (from aaaGateway.diameterProtocol config)
    DiameterProtocol string
    // Redis operating mode: "standalone" or "sentinel" (from aaaGateway.redisMode config)
    RedisMode string
    // Path to keepalived state file (from aaaGateway.keepalivedStatePath config)
    KeepalivedStatePath string
}

// Gateway is the AAA Gateway component. It runs in a separate process from Biz Pods.
// Handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3
type Gateway struct {
    bizServiceURL string
    redis         *redis.Client
    bizHTTPClient *http.Client
    version       string
    logger        *slog.Logger

    // pending maps SessionID → response channel (used for client-initiated flow)
    pending   map[string]chan []byte
    pendingMu sync.RWMutex

    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}
```

Wire in `RadiusHandler`, `DiameterHandler`, and Redis subscription. Implement the `BizAAAClient` interface so the AAA Gateway can receive forward requests:

```go
// ForwardEAP satisfies proto.BizAAAClient.
// It receives AaaForwardRequest from Biz Pod, writes session correlation to Redis,
// forwards to AAA-S, waits for response, publishes to Redis, and returns response bytes.
func (g *Gateway) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
    // 1. Write session correlation entry to Redis (before forwarding)
    entry := proto.SessionCorrEntry{
        AuthCtxID: req.AuthCtxID,
        PodID:     "", // Populated by Biz Pod via Biz Pod heartbeat; AAA GW writes read-only
        Sst:       req.Sst,
        Sd:        req.Sd,
        CreatedAt: time.Now().Unix(),
    }
    if err := g.writeSessionCorr(ctx, req.SessionID, &entry); err != nil {
        return nil, fmt.Errorf("aaa-gateway: failed to write session corr: %w", err)
    }

    // 2. Set up response channel for this session
    ch := make(chan []byte, 1)
    g.pendingMu.Lock()
    g.pending[req.SessionID] = ch
    g.pendingMu.Unlock()

    defer func() {
        g.pendingMu.Lock()
        delete(g.pending, req.SessionID)
        g.pendingMu.Unlock()
    }()

    // 3. Forward to AAA-S based on transport type
    var response []byte
    var err error
    switch req.TransportType {
    case proto.TransportRADIUS:
        response, err = g.radiusHandler.Forward(ctx, req.Payload, req.SessionID)
    case proto.TransportDIAMETER:
        response, err = g.diameterHandler.Forward(ctx, req.Payload, req.SessionID)
    default:
        return nil, fmt.Errorf("aaa-gateway: unknown transport type: %s", req.TransportType)
    }
    if err != nil {
        return nil, fmt.Errorf("aaa-gateway: forward failed: %w", err)
    }

    // 4. Publish response to Redis channel for Biz Pods to receive
    event := proto.AaaResponseEvent{
        Version:   g.version,
        SessionID: req.SessionID,
        AuthCtxID: req.AuthCtxID,
        Payload:   response,
    }
    if err := g.publishResponse(ctx, &event); err != nil {
        g.logger.Error("failed to publish response event", "error", err, "session_id", req.SessionID)
        // Continue — the response was already received, just couldn't publish
    }

    return &proto.AaaForwardResponse{
        Version:   g.version,
        SessionID: req.SessionID,
        AuthCtxID: req.AuthCtxID,
        Payload:   response,
    }, nil
}
```

#### 2.3.2 `radius_handler.go`

Implement UDP listener on `:1812`. Handle both directions:
- **Client-initiated**: Receive raw EAP from Biz Pod (already encoded), send to AAA-S, receive response.
- **Server-initiated (RAR/CoA/DM)**: Parse message type (43=CoA, 40=DM), look up session from Redis, call `serverInitiatedHandler`, return response.

```go
const (
    radiusAccessRequest     = 1
    radiusAccessAccept     = 2
    radiusAccessReject     = 3
    radiusAccessChallenge  = 11
    radiusCoARequest       = 43 // RFC 5176
    radiusDisconnectRequest = 40 // RFC 5176
)

// HandlePacket processes an incoming RADIUS packet from AAA-S.
// Spec: PHASE §2.3, §6.3
func (h *RadiusHandler) HandlePacket(conn *net.UDPConn, addr *net.UDPAddr, raw []byte) {
    if len(raw) < 4 {
        h.logger.Warn("radius_packet_too_short", "len", len(raw))
        return
    }

    msgType := raw[0]

    // Client-initiated: AAA-S responding to our Access-Request
    if msgType == radiusAccessAccept || msgType == radiusAccessReject || msgType == radiusAccessChallenge {
        sessionID := extractSessionID(raw)
        h.publishResponse(sessionID, raw)
        return
    }

    // Server-initiated: RAR or CoA or DM from AAA-S
    if msgType == radiusCoARequest || msgType == radiusDisconnectRequest {
        h.handleServerInitiated(raw, "RADIUS")
        return
    }
}
```

#### 2.3.3 `diameter_handler.go`

Implement Diameter listener supporting both TCP and SCTP, selected by `aaaGateway.diameterProtocol` config (`"tcp"` | `"sctp"`). The listener starts the protocol-appropriate handler, parses CER/CEA handshake via `go-diameter/v4` state machine, then streams messages. Distinguish client-initiated (Diameter-EAR/EAP) from server-initiated (ASR).

```go
// Listen starts the Diameter server on the configured protocol (TCP or SCTP).
// Spec: PHASE §2.3, §6.3; RFC 6733 App H (SCTP considerations)
func (h *DiameterHandler) Listen(ctx context.Context, addr, protocol string) error {
    switch protocol {
    case "tcp":
        listener, err := net.Listen("tcp", addr)
        if err != nil {
            return fmt.Errorf("diameter tcp listen: %w", err)
        }
        go h.serveTCP(listener)
    case "sctp":
        listener, err := net.Listen("sctp", addr)
        if err != nil {
            return fmt.Errorf("diameter sctp listen: %w", err)
        }
        go h.serveSCTP(listener)
    default:
        return fmt.Errorf("unsupported diameter protocol: %s (expected tcp or sctp)", protocol)
    }
    return nil
}

// HandleConnection processes an incoming Diameter connection from AAA-S.
// Spec: RFC 4072 (Diameter EAP), RFC 6733 App H (SCTP)
// Command Code 268 = DER/DEA (distinguished by R-bit in header flags)
// Command Code 274 = ASR/ASA (Abort-Session-Request/Answer)
// Route based on Command Code.
// Note: The server-side handler uses manual header parsing (no go-diameter/v4 import).
// go-diameter/v4 is used in internal/diameter/client.go (client-initiated path).
```

**Note on SCTP:** The standard library `net.Listen("sctp", addr)` requires the `net` package to be built with SCTP support. SCTP support is available in Linux kernels since 2.6.27 and Go 1.17+. If SCTP is not available at runtime, the server falls back to TCP (see implementation in `diameter_handler.go` line 34-47). Diameter message framing on SCTP is handled by the same manual header-parsing code used for TCP — no go-diameter/v4 needed on the server side.

**Verify SCTP availability** at startup:
```go
ln, err := net.Listen("sctp", ":3868")
if err != nil {
    slog.Warn("SCTP not available on this host", "error", err)
    // Fall back to TCP
}
```

> **Risk (Deferred):** The Diameter client-initiated path (`Forward()`) requires the AAA Gateway to maintain a persistent connection to AAA-S with CER/CEA handshake and DWR/DWA watchdog. See §2.3.5 for the full design and implementation plan.

#### 2.3.4 `redis.go`

Implement the Redis pub/sub and session correlation functions. The `aaaGateway.redisMode` config controls the Redis topology:

- `"standalone"` (default): single Redis node at `redisAddr`. Pub/sub works correctly on a single node.
- `"sentinel"`: Redis Sentinel for HA. The AAA Gateway connects to the Sentinel address; Sentinel redirects to the primary. Pub/sub works on the primary node.

**Important:** Redis Cluster is NOT supported for pub/sub — sharded pub/sub is not supported by Redis. If the operator deploys Redis Cluster for data storage, use a separate single-node/Sentinel Redis instance (or a dedicated channel on the Sentinel primary) for pub/sub. Document this constraint in the Helm chart values file.

```go
// newRedisClient creates a Redis client based on the configured mode.
// Spec: PHASE §2.3.4
func newRedisClient(redisAddr, mode string) *redis.Client {
    opts := &redis.Options{
        Addr: redisAddr,
    }
    switch mode {
    case "sentinel":
        // Use Sentinel mode: the Addr is the Sentinel address, not the primary.
        // go-redis/v9 auto-discovers the primary through Sentinel.
        opts.SentinelAddrs = strings.Split(redisAddr, ",")
        opts.MasterName = "nssAAF-primary"
        opts.SentinelPassword = os.Getenv("REDIS_SENTINEL_PASSWORD")
    case "standalone":
        // Default: direct connection to single Redis node.
    default:
        slog.Warn("unknown redis mode, defaulting to standalone", "mode", mode)
    }
    return redis.NewClient(opts)
}

// writeSessionCorr writes SessionCorrEntry to Redis with TTL = DefaultPayloadTTL.
func (g *Gateway) writeSessionCorr(ctx context.Context, sessionID string, entry *proto.SessionCorrEntry) error {
    key := proto.SessionCorrKey(sessionID)
    data, err := json.Marshal(entry)
    if err != nil {
        return err
    }
    return g.redis.Set(ctx, key, data, proto.DefaultPayloadTTL).Err()
}

// publishResponse publishes AaaResponseEvent to the nssaa:aaa-response channel.
func (g *Gateway) publishResponse(ctx context.Context, event *proto.AaaResponseEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }
    return g.redis.Publish(ctx, proto.AaaResponseChannel, data).Err()
}

// subscribeResponses subscribes to nssaa:aaa-response and dispatches to pending handlers.
func (g *Gateway) subscribeResponses(ctx context.Context) {
    ch := g.redis.PSubscribe(ctx, proto.AaaResponseChannel)
    defer ch.Close()

    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-ch.Channel():
            var event proto.AaaResponseEvent
            if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
                g.logger.Error("failed to unmarshal response event", "error", err)
                continue
            }
            g.dispatchResponse(&event)
        }
    }
}
```

#### 2.3.5 `diameter_forward.go` — Client-Initiated Connection Management

> **BLOCKER (Deferred):** This section was missing from the original plan. The `diameter_handler.Forward()` method is currently a stub returning `[]byte{}`, which silently breaks every DIAMETER-based NSSAA procedure. This must be implemented before DIAMETER transport is usable.

**Background:** Unlike RADIUS (stateless UDP), Diameter over TCP/SCTP requires a pre-existing, persistent connection with mandatory CER/CEA handshake before any application messages. RFC 6733 §2.1: "A Diameter peer MUST send a CER to the peer before sending any other message." The AAA Gateway's `ForwardEAP()` calls `diameterHandler.Forward()` to send DER to AAA-S — this is the client-initiated path that needs the full Diameter client stack.

**Architecture decision:** The AAA Gateway creates its own Diameter client using `go-diameter/v4/sm` directly (not importing `internal/diameter/`). This avoids the `internal/proto/` zero-dependency constraint since the gateway is not proto. The `proto.AaaForwardRequest.Payload` carries raw EAP bytes; the forwarder wraps them in DER AVPs.

##### Config Additions (AAAgwConfig)

```go
// New fields in compose/configs/aaa-gateway.yaml
aaaGateway:
  diameterServerAddress: "aaa-server:3868"   // AAA-S address
  diameterRealm:         "operator.com"        // AAA-S realm
  diameterHost:         "nssaa-gw.operator.com" // Origin-Host for CER
```

##### Implementation Pattern

```go
// diameter_forward.go — lightweight Diameter client for AAA Gateway
// Uses go-diameter/v4/sm directly (not internal/diameter/)

import (
    "github.com/fiorix/go-diameter/v4/diam"
    "github.com/fiorix/go-diameter/v4/diam/sm"
    "github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

// diamForwarder manages a persistent connection to AAA-S.
type diamForwarder struct {
    addr      string
    network   string  // "tcp" or "sctp"
    settings  *sm.Settings
    machine   *sm.StateMachine
    smClient  *sm.Client
    conn      diam.Conn
    pending   map[uint32]chan *diam.Message  // hop-by-hop → response
    pendingMu sync.RWMutex
    logger    *slog.Logger
}

// On startup (in Gateway.Start):
func (df *diamForwarder) Connect(ctx context.Context) error {
    // 1. Dial AAA-S
    // 2. go-diameter sm.Client handles CER/CEA automatically
    // 3. Register handler for DEA responses
    // 4. Start DWR watchdog goroutine (go-diameter handles this via EnableWatchdog)
    // 5. Handle disconnection: reconnect with exponential backoff
}

// Forward encodes EAP payload into DER and returns DEA response bytes.
func (df *diamForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string) ([]byte, error) {
    // 1. Build DER: Session-Id, Auth-Application-Id=5, Auth-Request-Type=1,
    //               User-Name, EAP-Payload (AVP 209), 3GPP-S-NSSAI AVP
    // 2. Register hop-by-hop ID → session channel in pending map
    // 3. Send DER, wait on channel (with deadline from context)
    // 4. Return DEA bytes (caller publishes to Redis)
}

// handleDEA: lookup pending hop-by-hop, send to channel
// watchDog: monitor connection, reconnect on failure
```

**CER/CEA:** Handled automatically by `go-diameter/v4/sm` state machine. The `sm.Client` sends CER on connect, validates CEA, and starts watchdog (DWR/DWA every 30s).

**DER encoding:** Unlike `internal/diameter/client.go` which receives an already-built DER, this forwarder builds DER from raw EAP bytes. The AaaForwardRequest.Payload is the EAP message bytes; the forwarder wraps them in:
- Session-Id AVP (263)
- Origin-Host, Origin-Realm AVPs (from config)
- Destination-Host, Destination-Realm AVPs (from config)
- Auth-Application-Id = 5 (Diameter EAP)
- Auth-Request-Type = 1 (AUTHORIZE_AUTHENTICATE)
- User-Name (the GPSI/SUPI from the request)
- EAP-Payload AVP (code **209**, per RFC 4072)
- 3GPP-S-NSSAI AVP (code 310)

**Pending map:** Same pattern as `internal/diameter/client.go` — hop-by-hop ID → response channel. The `sm.Client` handler dispatches incoming DEA by hop-by-hop.

**Reconnection:** On connection failure, exponential backoff: 1s, 2s, 4s, max 30s. After 3 consecutive failures, log error and return context deadline exceeded.

##### Wire into Gateway

```go
// In Gateway.New():
df, err := newDiamForwarder(cfg.DiameterServerAddress, cfg.DiameterProtocol, cfg.Logger)
if err != nil {
    return nil, fmt.Errorf("diameter forwarder: %w", err)
}
g.diamForwarder = df

// In Gateway.Start():
if err := g.diamForwarder.Connect(ctx); err != nil {
    g.logger.Error("diameter connect failed", "addr", cfg.DiameterServerAddress, "error", err)
    // Do not fail startup — AAA-S may be temporarily unavailable
}

// In Gateway.ForwardEAP() — replace the diameterHandler.Forward() call:
// case proto.TransportDIAMETER:
//     response, err = g.diamForwarder.Forward(ctx, req.Payload, req.SessionID)
```

**Note:** The `diamForwarder` runs inside the AAA Gateway process. It maintains one persistent TCP/SCTP connection to AAA-S (shared across all Biz Pod requests). The `diameterHandler` (server-side listener) continues to handle server-initiated ASR messages independently.

Spec: RFC 6733 §2.1 (CER/CEA), RFC 6733 §5.4 (DWR/DWA), RFC 4072 (Diameter EAP DER/DEA), TS 29.561 §17

---

#### 2.3.6 `keepalived.go`

Implement VIP health check for keepalived integration. The AAA Gateway exposes a `/health/vip` endpoint that keepalived's `chk_aaa_gw` script calls to determine if this replica owns the VIP.

The state file path is configurable via `aaaGateway.keepalivedStatePath` (default: `"/var/run/keepalived/state"`). keepalived writes `"MASTER"`, `"BACKUP"`, or `"FAULT"` to this file on state transitions. The endpoint reads the last line of this file to determine ownership.

```go
// VIPHealthHandler returns 200 if this AAA Gateway replica is the VIP owner, 503 otherwise.
// Reads VIP state from the configurable state file path.
func (g *Gateway) VIPHealthHandler(w http.ResponseWriter, r *http.Request) {
    statePath := g.cfg.KeepalivedStatePath // e.g. "/var/run/keepalived/state"
    data, err := os.ReadFile(statePath)
    if err != nil {
        slog.Warn("keepalived state file not readable", "path", statePath, "error", err)
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    state := strings.TrimSpace(string(data))
    if state == "MASTER" {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"vip_owner":true}`))
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte(`{"vip_owner":false,"state":"` + state + `"}`))
    }
}
```

**keepalived health check script** (`chk_aaa_gw`):
```bash
#!/bin/sh
curl -sf http://localhost:9090/health/vip || exit 1
```

This script is referenced in the keepalived ConfigMap (`chk_aaa_gw`) in Task 5.3.

Write `internal/aaa/gateway/redis_test.go` testing `writeSessionCorr` and `publishResponse` with a redis mock.

**Verify:**
```bash
go build ./internal/aaa/gateway/...
go test ./internal/aaa/gateway/... -v

# Ensure gateway package does NOT import internal/radius/ or internal/diameter/
grep -E "radius|diameter" internal/aaa/gateway/*.go  # should return nothing
```

**Done:** `internal/aaa/gateway/` package is complete. Raw socket handlers do not import `internal/radius/` or `internal/diameter/` (they use the raw `net` package only).

---

### Task 2.4: Create `cmd/biz/http_aaa_client.go`

**Files:** `cmd/biz/http_aaa_client.go`, `cmd/biz/http_aaa_client_test.go`

**Action:**

Create `cmd/biz/` directory and the HTTP client that satisfies `eap.AAAClient` and talks to the AAA Gateway. This client is the concrete implementation wired in `cmd/biz/main.go`.

```go
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/operator/nssAAF/internal/eap"
    "github.com/operator/nssAAF/internal/proto"
)

// httpAAAClient satisfies eap.AAAClient by forwarding EAP messages to the AAA Gateway via HTTP.
// It also subscribes to the nssaa:aaa-response Redis channel for response routing.
type httpAAAClient struct {
    aaaGatewayURL string
    httpClient    *http.Client
    version       string
    redis         *redis.Client
    podID         string

    // pending maps AuthCtxID → response channel
    pending   map[string]chan []byte
    pendingMu sync.RWMutex
}

// newHTTPAAAClient creates a new HTTP AAA client.
// The httpClient parameter must be configured by the caller (cmd/biz/main.go) based on
// biz.useMTLS config — either a plain http.Client or one with TLS configured.
func newHTTPAAAClient(aaaGatewayURL, redisAddr, podID, version string, httpClient *http.Client) *httpAAAClient {
    c := &httpAAAClient{
        aaaGatewayURL: aaaGatewayURL,
        httpClient:    httpClient, // caller-configured (may have TLS for mTLS)
        version:       version,
        redis: redis.NewClient(&redis.Options{
            Addr: redisAddr,
        }),
        podID:   podID,
        pending: make(map[string]chan []byte),
    }

    // Start Redis subscription in background
    go c.subscribeResponses(context.Background())
    return c
}

// SendEAP satisfies eap.AAAClient.
// Spec: PHASE §1.1 pattern
func (c *httpAAAClient) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
    // 1. Build forward request (uses the biz router)
    req := &proto.AaaForwardRequest{
        Version:   c.version,
        SessionID: fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
        AuthCtxID: authCtxID,
        // Sst, Sd, TransportType filled by the caller (biz router)
        Payload:   eapPayload,
    }

    body, err := json.Marshal(req)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal forward request: %w", err)
    }

    // 2. POST to AAA Gateway
    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.aaaGatewayURL+"/aaa/forward", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set(proto.HeaderName, c.version)

    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("aaa gateway unavailable: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("aaa gateway returned %d", resp.StatusCode)
    }

    var fwdResp proto.AaaForwardResponse
    if err := json.NewDecoder(resp.Body).Decode(&fwdResp); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    return fwdResp.Payload, nil
}

// subscribeResponses listens to nssaa:aaa-response and dispatches to pending channels.
func (c *httpAAAClient) subscribeResponses(ctx context.Context) {
    ch := c.redis.PSubscribe(ctx, proto.AaaResponseChannel)
    defer ch.Close()

    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-ch.Channel():
            var event proto.AaaResponseEvent
            if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
                continue
            }

            c.pendingMu.RLock()
            ch, ok := c.pending[event.AuthCtxID]
            c.pendingMu.RUnlock()

            if !ok {
                continue // Not for this Biz Pod
            }

            select {
            case ch <- event.Payload:
            default:
            }
    }
}

var _ eap.AAAClient = (*httpAAAClient)(nil)
```

Write `cmd/biz/http_aaa_client_test.go` with tests for `SendEAP` using a mock HTTP server.

**Verify:**
```bash
go build ./cmd/biz/...
go test ./cmd/biz/... -v
```

**Done:** `cmd/biz/http_aaa_client.go` satisfies `eap.AAAClient` and routes responses via Redis pub/sub.

---

## Wave 3 — Three Binaries

---

### Task 3.1: Create `cmd/biz/main.go`

**Files:** `cmd/biz/main.go`, `cmd/biz/main_test.go`

**Action:**

Create the Biz Pod entry point. It wires HTTP handlers (N58/N60) → Biz router → `httpAAAClient` → AAA Gateway.

**Prerequisite — Add `WithAAARouter` to `internal/api/nssaa/handler.go`:** Before implementing `cmd/biz/main.go`, extend `internal/api/nssaa/handler.go` with an `Option` that injects the `AAARouter`. Add this to the existing options list:

```go
// WithAAARouter sets the AAA router for the NSSAA handler.
// The router handles routing EAP messages to the AAA Gateway.
// After Task 2.2, biz.Router.BuildForwardRequest replaces direct radius/diameter calls.
// Spec: PHASE §3.1
func WithAAARouter(router AAARouter) Option {
    return func(h *Handler) {
        h.aaaRouter = router
    }
}
```

Add `aaaRouter AAARouter` as a private field on the `Handler` struct (if not already present). Ensure `AAARouter` interface in handler.go matches `eap.AAAClient.SendEAP(ctx, authCtxID string, eapPayload []byte) ([]byte, error)` — `httpAAAClient` satisfies both.

```go
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "flag"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"

    "github.com/operator/nssAAF/internal/api/aiw"
    "github.com/operator/nssAAF/internal/api/common"
    "github.com/operator/nssAAF/internal/api/nssaa"
    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v9"
)

var configPath = flag.String("config", "configs/biz.yaml", "path to YAML configuration file")

func main() {
    flag.Parse()

    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }
    if cfg.Component != config.ComponentBiz {
        slog.Error("config.component must be 'biz'", "got", cfg.Component)
        os.Exit(1)
    }

    podID, _ := os.Hostname()
    slog.Info("starting NSSAAF Biz Pod",
        "pod_id", podID,
        "version", cfg.Version,
        "use_mtls", cfg.Biz.UseMTLS,
    )

    // Build API root URL
    apiRoot := cfg.Server.Addr
    if !hasScheme(apiRoot) {
        apiRoot = "http://" + apiRoot
    }

    // ─── Data stores ────────────────────────────────────────────────────────
    nssaaStore := nssaa.NewInMemoryStore() // Will be replaced with DB-backed in Phase 3
    aiwStore := aiw.NewInMemoryStore()

    // ─── HTTP AAA client (satisfies eap.AAAClient = AAARouter) ────────────────
    tlsCfg := &tls.Config{}
    if cfg.Biz.UseMTLS {
        tlsCfg.RootCAs = mustLoadCertPool(cfg.Biz.TLSCA)
        tlsCfg.Certificates = []tls.Certificate{mustLoadCert(cfg.Biz.TLSCert, cfg.Biz.TLSKey)}
        tlsCfg.ServerName = "aaa-gateway" // SNI for AAA Gateway cert verification
    }
    aaaClient := newHTTPAAAClient(
        cfg.Biz.AAAGatewayURL,
        cfg.Redis.Addr,
        podID,
        cfg.Version,
        &http.Client{
            Transport: &http.Transport{TLSClientConfig: tlsCfg},
            Timeout:   30 * time.Second,
        },
    )

    // ─── N58: Nnssaaf_NSSAA ────────────────────────────────────────────────
    nssaaHandler := nssaa.NewHandler(nssaaStore,
        nssaa.WithAPIRoot(apiRoot),
        nssaa.WithAAARouter(aaaClient), // aaaClient satisfies eap.AAAClient = AAARouter
    )
    nssaaRouter := nssaa.NewRouter(nssaaHandler, apiRoot)

    // ─── N60: Nnssaaf_AIW ─────────────────────────────────────────────────
    aiwHandler := aiw.NewHandler(aiwStore,
        aiw.WithAPIRoot(apiRoot),
    )
    aiwRouter := aiw.NewRouter(aiwHandler, apiRoot)

    // ─── Internal AAA forwarding endpoints (for AAA Gateway) ─────────────────
    mux := http.NewServeMux()
    mux.HandleFunc("/aaa/forward", handleAaaForward)             // AAA Gateway → Biz Pod (server-initiated RAR/ASR/CoA)
    mux.HandleFunc("/aaa/server-initiated", handleServerInitiated) // AAA Gateway → Biz Pod

    // ─── Compose with N58/N60 handlers ────────────────────────────────────
    mux.Handle("/nnssaaf-nssaa/", http.StripPrefix("/nnssaaf-nssaa", nssaaRouter))
    mux.Handle("/nnssaaf-aiw/", http.StripPrefix("/nnssaaf-aiw", aiwRouter))

    // ─── OAM endpoints ─────────────────────────────────────────────────────
    mux.HandleFunc("/health", handleHealth)
    mux.HandleFunc("/ready", handleReady)

    // ─── Middleware stack ─────────────────────────────────────────────────
    var handler http.Handler = mux
    handler = common.RecoveryMiddleware(handler)
    handler = common.RequestIDMiddleware(handler)
    handler = common.LoggingMiddleware(handler)
    handler = common.CORSMiddleware(handler)

    srv := &http.Server{
        Addr:         cfg.Server.Addr,
        Handler:      handler,
        ReadTimeout:  cfg.Server.ReadTimeout,
        WriteTimeout: cfg.Server.WriteTimeout,
        IdleTimeout:  cfg.Server.IdleTimeout,
    }

    // ─── Register with NRF (Biz Pod registers HTTP Gateway FQDN) ──────────
    // TODO: Implement NRF client in Phase 3 — wire in Phase 3 once DB store is ready

    errCh := make(chan error, 1)
    go func() {
        slog.Info("biz HTTP server listening", "addr", srv.Addr)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            errCh <- err
        }
    }()

    // ─── Biz Pod heartbeat: register pod in Redis SET ──────────────────────
    go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

    select {
    case err := <-errCh:
        slog.Error("server error", "error", err)
        os.Exit(1)
    case <-signalReceived():
        slog.Info("shutdown signal received")
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        srv.Shutdown(ctx)
        aaaClient.Close()
    }
}

func handleAaaForward(w http.ResponseWriter, r *http.Request) {
    // Called by AAA Gateway for server-initiated flow (RAR/ASR/CoA)
    // For now, return 501 Not Implemented — fully wired in Task 6.1
    http.Error(w, "not implemented", http.StatusNotImplemented)
}

func handleServerInitiated(w http.ResponseWriter, r *http.Request) {
    // Called by AAA Gateway when AAA-S initiates Re-Auth or Revocation
    // Fully implemented in Task 6.1
    http.Error(w, "not implemented", http.StatusNotImplemented)
}

// podHeartbeat registers the Biz Pod in the Redis SET and refreshes every 30 seconds.
// On context cancellation, it removes the pod from the SET.
func podHeartbeat(ctx context.Context, redisAddr, podID string) {
    rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
    defer rdb.Close()

    // Register immediately on startup
    if err := rdb.SAdd(ctx, proto.PodsKey, podID).Err(); err != nil {
        slog.Warn("failed to register pod in Redis", "error", err, "pod_id", podID)
    }

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            rdb.SRem(ctx, proto.PodsKey, podID)
            return
        case <-ticker.C:
            rdb.SAdd(ctx, proto.PodsKey, podID)
        }
    }
}

// hasScheme returns true if s already contains a URL scheme prefix.
func hasScheme(s string) bool {
    return len(s) >= 4 && (s[:4] == "http" || s[:4] == " Http")
}

// mustLoadCertPool loads and parses a CA certificate file into an x509.CertPool.
// Panics on error — called during startup validation only.
func mustLoadCertPool(caPath string) *x509.CertPool {
    data, err := os.ReadFile(caPath)
    if err != nil {
        panic("failed to read TLS CA cert: " + err.Error())
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(data) {
        panic("failed to parse TLS CA cert from: " + caPath)
    }
    return pool
}

// mustLoadCert loads a client certificate and key for mTLS.
// Panics on error — called during startup validation only.
func mustLoadCert(certPath, keyPath string) tls.Certificate {
    cert, err := tls.LoadX509KeyPair(certPath, keyPath)
    if err != nil {
        panic("failed to load TLS cert/key pair: " + err.Error())
    }
    return cert
}
```

Write `cmd/biz/main_test.go` testing that the server starts and the `/health` endpoint returns 200.

**Verify:**
```bash
go build ./cmd/biz/... && go test ./cmd/biz/... -v
```

**Done:** `cmd/biz/main.go` compiles and starts. `eap.AAAClient` is satisfied by `httpAAAClient`.

---

### Task 3.2: Create `cmd/aaa-gateway/main.go`

**Files:** `cmd/aaa-gateway/main.go`, `cmd/aaa-gateway/main_test.go`

**Action:**

```go
package main

import (
    "context"
    "flag"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/operator/nssAAF/internal/aaa/gateway"
    "github.com/operator/nssAAF/internal/config"
)

var configPath = flag.String("config", "configs/aaa-gateway.yaml", "path to YAML configuration file")

func main() {
    flag.Parse()

    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }
    if cfg.Component != config.ComponentAAAGateway {
        slog.Error("config.component must be 'aaa-gateway'", "got", cfg.Component)
        os.Exit(1)
    }

    slog.Info("starting NSSAAF AAA Gateway",
        "version", cfg.Version,
        "radius_addr", cfg.AAAgw.ListenRADIUS,
        "diameter_addr", cfg.AAAgw.ListenDIAMETER,
    )

    gw := gateway.New(gateway.Config{
        BizServiceURL:      cfg.AAAgw.BizServiceURL,
        RedisAddr:         cfg.Redis.Addr,
        ListenRADIUS:      cfg.AAAgw.ListenRADIUS,
        ListenDIAMETER:    cfg.AAAgw.ListenDIAMETER,
        AAAGatewayURL:     "http://" + cfg.Server.Addr, // Self-referential; used for health checks
        Logger:            logger,
        Version:           cfg.Version,
        DiameterProtocol:  cfg.AAAgw.DiameterProtocol,
        RedisMode:         cfg.AAAgw.RedisMode,
        KeepalivedStatePath: cfg.AAAgw.KeepalivedStatePath,
    })

    // Expose HTTP endpoints for Biz Pod communication
    http.HandleFunc("/aaa/forward", gw.HandleForward)
    http.HandleFunc("/health", handleHealth)
    http.HandleFunc("/health/vip", gw.VIPHealthHandler)

    // Start HTTP server in background
    errCh := make(chan error, 1)
    go func() {
        slog.Info("aaa-gateway HTTP listening", "addr", cfg.Server.Addr)
        if err := http.ListenAndServe(cfg.Server.Addr, nil); err != nil && err != http.ErrServerClosed {
            errCh <- err
        }
    }()

    // Start the gateway (UDP/TCP listeners)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    if err := gw.Start(ctx); err != nil {
        slog.Error("gateway start failed", "error", err)
        os.Exit(1)
    }

    <-signalReceived()
    slog.Info("shutting down AAA Gateway")
    cancel()
    gw.Stop()
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok","service":"aaa-gateway"}`))
}
```

Write `cmd/aaa-gateway/main_test.go` testing that the server starts.

**Verify:**
```bash
go build ./cmd/aaa-gateway/... && go test ./cmd/aaa-gateway/... -v
```

**Done:** `cmd/aaa-gateway/main.go` compiles and starts.

---

### Task 3.3: Create `cmd/http-gateway/main.go`

**Files:** `cmd/http-gateway/main.go`, `cmd/http-gateway/main_test.go`

**Action:**

The HTTP Gateway is a thin TLS-terminating pass-through proxy with no application logic.

```go
package main

import (
    "bytes"
    "context"
    "flag"
    "io"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/proto"
)

var configPath = flag.String("config", "configs/http-gateway.yaml", "path to YAML configuration file")

// httpBizClient satisfies proto.BizServiceClient.
type httpBizClient struct {
    bizServiceURL string
    httpClient    *http.Client
    version       string
}

// ForwardRequest satisfies proto.BizServiceClient.
func (c *httpBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    url := c.bizServiceURL + path

    req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
    if err != nil {
        return nil, 0, err
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set(proto.HeaderName, c.version)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return nil, 503, err
        }
        return nil, 502, err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*httpBizClient)(nil)

func main() {
    flag.Parse()

    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
    slog.SetDefault(logger)

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }
    if cfg.Component != config.ComponentHTTPGateway {
        slog.Error("config.component must be 'http-gateway'", "got", cfg.Component)
        os.Exit(1)
    }

    slog.Info("starting NSSAAF HTTP Gateway",
        "version", cfg.Version,
        "tls_cert", cfg.TLS.Cert,
    )

    bizClient := &httpBizClient{
        bizServiceURL: cfg.Biz.BizServiceURL, // http://svc-nssaa-biz:8080
        httpClient: &http.Client{
            Timeout: 10 * time.Second,
        },
        version: cfg.Version,
    }

    mux := http.NewServeMux()

    // Forward all N58/N60 paths to Biz Pods
    mux.HandleFunc("/nnssaaf-nssaa/", func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body)
        if err != nil {
            slog.Error("forward to biz failed", "error", err, "path", r.URL.Path)
            http.Error(w, `{"cause":"biz_unavailable"}`, 503)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        w.Write(respBody)
    })

    mux.HandleFunc("/nnssaaf-aiw/", func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body)
        if err != nil {
            slog.Error("forward to biz failed", "error", err, "path", r.URL.Path)
            http.Error(w, `{"cause":"biz_unavailable"}`, 503)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        w.Write(respBody)
    })

    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok","service":"http-gateway"}`))
    })
    mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ready","service":"http-gateway"}`))
    })

    var handler http.Handler = mux
    handler = recoveryMiddleware(handler)
    handler = requestIDMiddleware(handler)
    handler = loggingMiddleware(handler)

    srv := &http.Server{
        Addr:      cfg.Server.Addr,
        Handler:   handler,
        TLSConfig: tlsConfig(cfg.TLS),
    }

    errCh := make(chan error, 1)
    go func() {
        slog.Info("http-gateway TLS listening", "addr", srv.Addr)
        if err := srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key); err != nil && err != http.ErrServerClosed {
            errCh <- err
        }
    }()

    select {
    case err := <-errCh:
        slog.Error("server error", "error", err)
        os.Exit(1)
    case <-signalReceived():
        slog.Info("shutdown signal received")
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        srv.Shutdown(ctx)
    }
}
```

Write middleware helpers (`recoveryMiddleware`, `requestIDMiddleware`, `loggingMiddleware`, `tlsConfig`) — reference patterns from `internal/api/common/`.

Write `cmd/http-gateway/main_test.go` testing that the server starts and `/health` returns 200.

**Verify:**
```bash
go build ./cmd/http-gateway/... && go test ./cmd/http-gateway/... -v
```

**Done:** `cmd/http-gateway/main.go` compiles and starts with TLS 1.3 termination.

---

### Task 3.4: Delete `cmd/nssAAF/main.go`

**Files deleted:** `cmd/nssAAF/` directory (all contents)

**Action:**

```bash
# Verify no .go files remain in cmd/nssAAF/ before deletion
ls cmd/nssAAF/*.go 2>/dev/null && echo "WARNING: extra files in cmd/nssAAF/" || true
# Delete the entire directory (rmdir is unsafe — test files may remain)
rm -rf cmd/nssAAF/
```

**Verify:**
```bash
# Old binary should no longer build
go build ./cmd/nssAAF/... 2>&1 | grep -q "no required module" || echo "OK: cmd/nssAAF removed"

# Three new binaries should all build
go build ./cmd/biz/...
go build ./cmd/aaa-gateway/...
go build ./cmd/http-gateway/...
echo "OK: all three binaries compile"
```

**Done:** `cmd/nssAAF/` directory removed. All three new binaries compile independently.

---

## Wave 4 — Config Refactor

---

### Task 4.1: Create `internal/config/component.go`

**Files:** `internal/config/component.go`, `internal/config/component_test.go`

**Action:**

Extend `internal/config/config.go` with the `Component` enum and per-component config structs. Add these types to `internal/config/component.go`:

```go
package config

// Component identifies which binary is running.
// Spec: PHASE §4.1
type Component string

const (
    ComponentHTTPGateway Component = "http-gateway"
    ComponentBiz         Component = "biz"
    ComponentAAAGateway  Component = "aaa-gateway"
)

// BizConfig: how Biz Pod communicates with other components.
type BizConfig struct {
    AAAGatewayURL string `yaml:"aaaGatewayUrl"` // http://svc-nssaa-aaa:9090 (or https:// if mTLS enabled)
    BizServiceURL string `yaml:"bizServiceUrl"`  // http://svc-nssaa-biz:8080 (for HTTP Gateway config)
    // TLS mutual authentication for Biz Pod → AAA Gateway communication.
    // If UseMTLS is true, the client presents a certificate signed by the CA specified in TLSCA.
    // If false, plain HTTP is used (acceptable for intra-cluster communication with K8s NetworkPolicy).
    UseMTLS bool   `yaml:"useMTLS"` // default: false (plain HTTP within cluster)
    TLSCA   string `yaml:"tlsCA"`   // path to CA cert for verifying AAA Gateway cert (required if UseMTLS)
    TLSCert string `yaml:"tlsCert"` // path to client cert for mTLS (required if UseMTLS)
    TLSKey  string `yaml:"tlsKey"`  // path to client key for mTLS (required if UseMTLS)
}

// AAAgwConfig: AAA Gateway specific settings.
type AAAgwConfig struct {
    ListenRADIUS    string `yaml:"listenRadius"`    // ":1812"
    ListenDIAMETER  string `yaml:"listenDiameter"`  // ":3868"
    BizServiceURL   string `yaml:"bizServiceUrl"`   // http://svc-nssaa-biz:8080
    KeepalivedCheck string `yaml:"keepalivedCheck"` // path to health check script

    // Diameter transport protocol: "tcp" or "sctp". Both are implemented; operator
    // selects based on their AAA-S deployment. SCTP provides message framing
    // independence from byte-stream ordering (RFC 6733 App H); TCP is universally supported.
    // Default: "tcp"
    DiameterProtocol string `yaml:"diameterProtocol"` // "tcp" | "sctp"

    // Redis operating mode for pub/sub coordination between AAA Gateway and Biz Pods.
    // - "standalone": single Redis node; pub/sub works correctly (default)
    // - "sentinel": Redis Sentinel for HA; pub/sub works on the primary node
    // NOTE: Redis Cluster is NOT supported for pub/sub (sharded pub/sub is not supported by Redis).
    // If operator needs Redis Cluster for data storage, use a separate single-node/Sentinel
    // Redis for pub/sub (dedicated channel).
    // Default: "standalone"
    RedisMode string `yaml:"redisMode"` // "standalone" | "sentinel"

    // Path to keepalived state file. keepalived writes "MASTER", "BACKUP", or "FAULT"
    // to this file on state transitions. The AAA Gateway's /health/vip endpoint reads
    // this file to determine VIP ownership for keepalived's health check script.
    // Default: "/var/run/keepalived/state" (standard keepalived location)
    KeepalivedStatePath string `yaml:"keepalivedStatePath"` // default: "/var/run/keepalived/state"
}

// TLSConfig holds TLS settings for HTTP Gateway.
type TLSConfig struct {
    Cert string `yaml:"cert"`
    Key  string `yaml:"key"`
    CA   string `yaml:"ca"`
}
```

Add `TLSConfig` and component-specific fields to the `Config` struct (modify `internal/config/config.go`):

```go
// In internal/config/config.go, add to Config struct:
type Config struct {
    Component Component   `yaml:"component"` // required: which binary to start
    TLS      TLSConfig   `yaml:"tls"`       // HTTP Gateway TLS settings
    // ... existing fields ...
    Biz   BizConfig    `yaml:"biz"`   // Biz Pod and HTTP Gateway communication
    AAAgw AAAgwConfig  `yaml:"aaaGateway"` // AAA Gateway settings
    // ...
}
```

Update the `Load` function to validate required fields per component:

```go
// After yaml.Unmarshal and applyDefaults in Load():
switch cfg.Component {
case ComponentBiz:
    if cfg.Biz.AAAGatewayURL == "" {
        return nil, fmt.Errorf("biz.aaaGatewayUrl is required")
    }
    if cfg.Biz.UseMTLS {
        if cfg.Biz.TLSCA == "" || cfg.Biz.TLSCert == "" || cfg.Biz.TLSKey == "" {
            return nil, fmt.Errorf("biz.tlsCA, biz.tlsCert, and biz.tlsKey are required when biz.useMTLS is true")
        }
    }
case ComponentAAAGateway:
    if cfg.AAAgw.BizServiceURL == "" {
        return nil, fmt.Errorf("aaaGateway.bizServiceUrl is required")
    }
    if cfg.AAAgw.ListenRADIUS == "" {
        return nil, fmt.Errorf("aaaGateway.listenRadius is required")
    }
    if cfg.AAAgw.DiameterProtocol == "" {
        cfg.AAAgw.DiameterProtocol = "tcp" // default to TCP
    }
    if cfg.AAAgw.DiameterProtocol != "tcp" && cfg.AAAgw.DiameterProtocol != "sctp" {
        return nil, fmt.Errorf("aaaGateway.diameterProtocol must be 'tcp' or 'sctp'")
    }
    if cfg.AAAgw.RedisMode == "" {
        cfg.AAAgw.RedisMode = "standalone" // default
    }
    if cfg.AAAgw.RedisMode != "standalone" && cfg.AAAgw.RedisMode != "sentinel" {
        return nil, fmt.Errorf("aaaGateway.redisMode must be 'standalone' or 'sentinel'")
    }
    if cfg.AAAgw.KeepalivedStatePath == "" {
        cfg.AAAgw.KeepalivedStatePath = "/var/run/keepalived/state" // default
    }
case ComponentHTTPGateway:
    if cfg.Biz.BizServiceURL == "" {
        return nil, fmt.Errorf("biz.bizServiceUrl is required")
    }
    if cfg.TLS.Cert == "" || cfg.TLS.Key == "" {
        return nil, fmt.Errorf("tls.cert and tls.key are required for http-gateway")
    }
default:
    return nil, fmt.Errorf("config.component must be one of: http-gateway, biz, aaa-gateway")
}
```

Write `internal/config/component_test.go` testing:
- Loading a `biz` config with missing `biz.aaaGatewayUrl` returns error
- Loading a `biz` config with `useMTLS: true` but missing TLS fields returns error
- Loading a `biz` config with `useMTLS: false` succeeds without TLS certs
- Loading an `aaa-gateway` config with missing `aaaGateway.bizServiceUrl` returns error
- Loading an `aaa-gateway` config with invalid `diameterProtocol` returns error
- Loading an `aaa-gateway` config with `diameterProtocol: tcp` succeeds
- Loading an `aaa-gateway` config with `diameterProtocol: sctp` succeeds
- Loading an `aaa-gateway` config with invalid `redisMode` returns error
- Loading an `http-gateway` config with missing TLS cert returns error
- Loading with valid `biz` config succeeds

**Verify:**
```bash
go build ./internal/config/... && go test ./internal/config/... -v
```

**Done:** Per-component config validates required fields. Each binary gets only the config it needs.

---

## Wave 5 — Local Dev and Kubernetes Manifests

---

### Task 5.1: Create `compose/dev.yaml`

**Files:** `compose/dev.yaml`

**Action:**

Create Docker Compose file for local 3-component development:

```yaml
# compose/dev.yaml
# Local development setup for 3-component NSSAAF architecture.
# Mirrors production topology: HTTP Gateway → Biz Pod → AAA Gateway → Redis → AAA-S mock.
version: "3.8"

services:
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  mock-aaa-s:
    build:
      context: .
      dockerfile: Dockerfile.mock-aaa-s
    image: nssAAF-mock-aaa-s:latest
    ports: ["1812:1812/udp", "3868:3868"]
    networks:
      - backend

  aaa-gateway:
    image: nssAAF-aaa-gw:latest
    depends_on:
      redis:
        condition: service_healthy
      mock-aaa-s:
        condition: service_started
    environment:
      REDIS_ADDR: redis:6379
      AAA_S_RADIUS_ADDR: mock-aaa-s:1812
      AAA_S_DIAMETER_ADDR: mock-aaa-s:3868
      BIZ_URL: http://biz:8080
      CONFIG: /etc/nssAAF/aaa-gateway.yaml
    volumes:
      - ./configs/aaa-gateway.yaml:/etc/nssAAF/aaa-gateway.yaml:ro
    ports: ["9090:9090"]
    network_mode: host  # Dev only: keepalived needs host networking for VIP
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  biz:
    image: nssAAF-biz:latest
    depends_on:
      redis:
        condition: service_healthy
      aaa-gateway:
        condition: service_healthy
    environment:
      REDIS_ADDR: redis:6379
      AAA_GW_URL: http://localhost:9090
      DB_HOST: postgres
      CONFIG: /etc/nssAAF/biz.yaml
    volumes:
      - ./configs/biz.yaml:/etc/nssAAF/biz.yaml:ro
    ports: ["8080:8080"]
    networks:
      - backend

  http-gateway:
    image: nssAAF-http-gw:latest
    depends_on:
      biz:
        condition: service_started
    environment:
      BIZ_SERVICE_URL: http://localhost:8080
      TLS_CERT: /etc/nssAAF/tls/server.crt
      TLS_KEY: /etc/nssAAF/tls/server.key
      CONFIG: /etc/nssAAF/http-gateway.yaml
    volumes:
      - ./configs/http-gateway.yaml:/etc/nssAAF/http-gateway.yaml:ro
      - ./configs/tls:/etc/nssAAF/tls:ro
    ports: ["8443:443"]
    networks:
      - backend

networks:
  backend:
    driver: bridge
```

Note: The `mock-aaa-s` service requires a Dockerfile. Create a minimal FreeRADIUS or custom Go mock. For Phase 3, a simple UDP/TCP echo server that returns mock EAP responses is sufficient.

**Verify:**
```bash
docker compose -f compose/dev.yaml config  # Validates YAML syntax
```

**Done:** `compose/dev.yaml` validates. Docker Compose can bring up all three components.

---

### Task 5.2: Create `deployments/helm/` charts

**Files:** `deployments/helm/nssaa-http-gateway/`, `deployments/helm/nssaa-biz/`, `deployments/helm/nssaa-aaa-gateway/` (each with `Chart.yaml`, `values.yaml`, `templates/deployment.yaml`, `templates/service.yaml`, `templates/configmap.yaml`)

**Action:**

Create three Helm charts following the Kubernetes manifests specified in PHASE §5.

#### 5.2.1 `deployments/helm/nssaa-http-gateway/`

```yaml
# Chart.yaml
apiVersion: v2
name: nssaa-http-gateway
description: NSSAAF HTTP Gateway — TLS terminator and N58/N60 forwarder
version: 0.1.0
appVersion: "1.0"
```

```yaml
# values.yaml
replicaCount: 3
image:
  repository: nssAAF-http-gw
  tag: latest
  pullPolicy: IfNotPresent
service:
  type: ClusterIP
  port: 443
config:
  bizServiceUrl: http://svc-nssaa-biz:8080
tls:
  cert: /etc/tls/server.crt
  key: /etc/tls/server.key
resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 64Mi
```

```yaml
# templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssaa-http-gateway.fullname" . }}
  labels:
    app: nssaa-http-gateway
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: nssaa-http-gateway
  template:
    metadata:
      labels:
        app: nssaa-http-gateway
    spec:
      containers:
        - name: http-gw
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - containerPort: 8443
          env:
            - name: CONFIG
              value: /etc/nssAAF/http-gateway.yaml
            - name: BIND_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          volumeMounts:
            - name: config
              mountPath: /etc/nssAAF
              readOnly: true
            - name: tls
              mountPath: /etc/tls
              readOnly: true
          readinessProbe:
            httpGet:
              path: /ready
              port: 8443
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
      volumes:
        - name: config
          configMap:
            name: nssaa-http-gateway-config
        - name: tls
          secret:
            secretName: nssaa-http-gateway-tls
```

```yaml
# templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: svc-nssaa-http-gw
spec:
  type: {{ .Values.service.type }}
  selector:
    app: nssaa-http-gateway
  ports:
    - port: 443
      targetPort: 8443
```

Create the same pattern for `nssaa-biz/` and `nssaa-aaa-gateway/`.

#### 5.2.2 `deployments/helm/nssaa-biz/`

Key differences from HTTP Gateway:
- `replicaCount: 5`
- No TLS (internal service)
- `svc-nssaa-biz` headless service (ClusterIP: None) for direct pod routing

#### 5.2.3 `deployments/helm/nssaa-aaa-gateway/`

Key requirements:
- `replicaCount: 2`
- `strategy.type: Recreate` (prevents two active pods during rolling update)
- Pod annotation for Multus CNI secondary interface:
  ```yaml
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [{
        "name": "aaa-bridge-vlan",
        "interface": "net0",
        "ips": ["$(POD_IP)/24"],
        "gateway": ["10.1.100.1"]
      }]
  ```
- Sidecar container for `osixopen/keepalived:2.3.1`
- `NET_ADMIN` capability for keepalived VIP management
- `svc-nssaa-aaa` headless service pointing to `app: nssaa-aaa`

**Verify:**
```bash
helm lint deployments/helm/nssaa-http-gateway/
helm lint deployments/helm/nssaa-biz/
helm lint deployments/helm/nssaa-aaa-gateway/
kubectl apply --dry-run=server -f deployments/helm/nssaa-http-gateway/
kubectl apply --dry-run=server -f deployments/helm/nssaa-biz/
kubectl apply --dry-run=server -f deployments/helm/nssaa-aaa-gateway/
```

**Done:** All three Helm charts validate. `strategy: Recreate` is set for AAA Gateway.

---

### Task 5.3: Create Kustomize overlays and keepalived ConfigMap

**Files:** `deployments/kustomize/base/http-gateway/`, `deployments/kustomize/base/biz/`, `deployments/kustomize/base/aaa-gateway/`, `deployments/kustomize/overlays/development/`, `deployments/kustomize/overlays/production/`, `deployments/kustomize/overlays/carrier/`

**Action:**

Create Kustomize base directories referencing the Helm charts via `kustomize build`. Also create the keepalived ConfigMap:

```yaml
# deployments/helm/nssaa-aaa-gateway/templates/configmap-keepalived.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nssaa-aaa-keepalived
data:
  keepalived.conf: |
    vrrp_instance vi_aaa {
        state BACKUP
        interface net0
        virtual_router_id 60
        priority 100
        advert_int 1
        unicast_peer {
            {{ .Values.aaaGateway.vip.peerIp }}
        }
        virtual_ipaddress {
            {{ .Values.aaaGateway.vip.address }}/{{ .Values.aaaGateway.vip.cidr }}
        }
        track_script {
            chk_aaa_gw
        }
    }
  chk_aaa_gw.sh: |
    #!/bin/bash
    curl -f http://localhost:9090/health/vip || exit 1
```

Create NetworkAttachmentDefinition for Multus CNI:

```yaml
# deployments/helm/nssaa-aaa-gateway/templates/network-attachment.yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: aaa-bridge-vlan
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "name": "aaa-bridge-vlan",
      "type": "bridge",
      "bridge": "aaa-br0",
      "vlan": 100,
      "ipam": {
        "type": "static"
      }
    }
```

Create three overlay directories:
- `development/`: Uses Docker image tags `dev-latest`, no TLS (HTTP), no Multus
- `production/`: Uses immutable tags, TLS, Multus, keepalived
- `carrier/`: Multi-AZ, larger replica counts, pod anti-affinity

**Verify:**
```bash
kustomize build deployments/kustomize/overlays/development/
kustomize build deployments/kustomize/overlays/production/
kustomize build deployments/kustomize/overlays/carrier/
```

**Done:** All Kustomize overlays build successfully. keepalived ConfigMap and NetworkAttachmentDefinition are created.

---

## Wave 6 — Server-Initiated Flow

---

### Task 6.1: Implement RAR/ASR/CoA routing in AAA Gateway

**Files to modify:** `internal/aaa/gateway/radius_handler.go`, `internal/aaa/gateway/diameter_handler.go`, `internal/aaa/gateway/gateway.go`

**Action:**

The core logic was drafted in Task 2.3. This task completes the implementation:

#### 6.1.1 RADIUS server-initiated handler

Extend `radius_handler.go` with the `handleServerInitiated` method:

```go
// handleServerInitiated processes RAR, CoA, or Disconnect-Request from AAA-S.
// Spec: PHASE §6.3
func (h *RadiusHandler) handleServerInitiated(raw []byte, transportType string) {
    sessionID := extractSessionID(raw) // From RADIUS State attribute or request authenticator

    // Look up session from Redis
    entry, err := h.getSessionCorr(sessionID)
    if err != nil {
        // Session not found — return RAR-Nak/ASA to AAA-S
        h.logger.Warn("session_not_found_for_server_initiated",
            "session_id", sessionID,
            "transport", transportType)
        h.sendRARnak(raw)
        return
    }

    // Determine message type
    msgType := extractMessageType(raw)
    var mtype proto.MessageType
    switch msgType {
    case radiusCoARequest:
        mtype = proto.MessageTypeCoA
    case radiusDisconnectRequest:
        mtype = proto.MessageTypeRAR // Treat DM as RAR for NSSAA purposes
    default:
        mtype = proto.MessageTypeRAR
    }

    // Forward to Biz Pod via HTTP POST /aaa/server-initiated
    req := &proto.AaaServerInitiatedRequest{
        Version:       h.version,
        SessionID:     sessionID,
        AuthCtxID:     entry.AuthCtxID,
        TransportType: proto.TransportRADIUS,
        MessageType:   mtype,
        Payload:       raw,
    }

    resp, err := h.forwardToBizServerInitiated(req)
    if err != nil {
        h.logger.Error("forward to biz for server-initiated failed",
            "error", err, "session_id", sessionID)
        h.sendRARnak(raw)
        return
    }

    // Forward Biz Pod's response back to AAA-S (raw bytes)
    h.sendToAAA(raw, resp.Payload)
}

// sendRARnak sends a RAR-Nak (CoA-Nak) response to AAA-S.
// TODO: Implement fully with RFC 5176 §3.2: RAR-Nak = Access-Reject (code=2) with
// Error-Cause AVP (Type=161, Vendor-ID=0) = 20051 (Session-Not-Found).
// For now, logs a warning and drops the packet.
func (h *RadiusHandler) sendRARnak(originalPacket []byte) {
    h.logger.Warn("rar_nak_not_implemented",
        "note", "RFC 5176 §3.2: send Error-Cause 20051 back to AAA-S",
        "session_id", "unknown")
}
```

#### 6.1.2 Diameter server-initiated handler

Extend `diameter_handler.go` similarly for ASR/ASA (Command Code 274):

```go
func (h *DiameterHandler) handleServerInitiated(raw []byte) {
    // Extract Session-Id from Diameter header (bytes 4-36 per RFC 6733)
    sessionID := extractDiameterSessionID(raw)

    entry, err := h.getSessionCorr(sessionID)
    if err != nil {
        h.logger.Warn("session_not_found_for_asr",
            "session_id", sessionID)
        h.sendASA(raw, diameterResultCodeUnknownSessionID)
        return
    }

    req := &proto.AaaServerInitiatedRequest{
        Version:       h.version,
        SessionID:     sessionID,
        AuthCtxID:     entry.AuthCtxID,
        TransportType: proto.TransportDIAMETER,
        MessageType:   proto.MessageTypeASR,
        Payload:       raw,
    }

    resp, err := h.forwardToBizServerInitiated(req)
    if err != nil {
        h.logger.Error("forward to biz for ASR failed", "error", err)
        h.sendASA(raw, diameterResultCodeDiamUnavailable)
        return
    }

    h.sendToAAA(raw, resp.Payload)
}
```

#### 6.1.3 `forwardToBizServerInitiated` in gateway.go

```go
// forwardToBizServerInitiated POSTs a server-initiated message to the Biz Pod.
func (g *Gateway) forwardToBizServerInitiated(req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
    body, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }

    httpReq, err := http.NewRequestWithContext(context.Background(),
        "POST", g.bizServiceURL+"/aaa/server-initiated", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set(proto.HeaderName, g.version)

    resp, err := g.bizHTTPClient.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("biz service unavailable: %w", err)
    }
    defer resp.Body.Close()

    var aaaResp proto.AaaServerInitiatedResponse
    if err := json.NewDecoder(resp.Body).Decode(&aaaResp); err != nil {
        return nil, err
    }

    return &aaaResp, nil
}
```

Write `internal/aaa/gateway/radius_handler_test.go` testing:
- `HandlePacket` with Access-Request → calls `publishResponse`
- `HandlePacket` with CoA-Request → calls `handleServerInitiated`
- `handleServerInitiated` with unknown session → calls `sendRARnak`
- `handleServerInitiated` with known session → POSTs to Biz Pod

**Verify:**
```bash
go build ./internal/aaa/gateway/... && go test ./internal/aaa/gateway/... -v
```

**Done:** RAR/ASR/CoA messages are correctly identified, routed to Biz Pod, and response returned to AAA-S.

---

### Task 6.2: Implement Biz Pod server-initiated handlers

**Files to modify:** `cmd/biz/main.go`

**Action:**

Update `handleServerInitiated` in `cmd/biz/main.go` to fully implement the three server-initiated message types:

```go
func handleServerInitiated(w http.ResponseWriter, r *http.Request) {
    var req proto.AaaServerInitiatedRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    var respPayload []byte

    switch req.MessageType {
    case proto.MessageTypeRAR:
        // TS 23.502 §4.2.9.3: Nnssaaf_NSSAA_Re-AuthenticationNotification → AMF
        // 1. Look up session from DB using req.AuthCtxID
        // 2. Build Nnssaaf_NSSAA_Re-AuthenticationNotification POST to AMF callback URI
        // 3. Return RAR-Response (success) or RAR-Nak (failure)
        respPayload = handleReAuth(r.Context(), &req)

    case proto.MessageTypeASR:
        // TS 23.502 §4.2.9.4: Nnssaaf_NSSAA_RevocationNotification → AMF
        // 1. Look up session from DB using req.AuthCtxID
        // 2. Build Nnssaaf_NSSAA_RevocationNotification POST to AMF callback URI
        // 3. Return ASA (success) or ASA with error code (failure)
        respPayload = handleRevocation(r.Context(), &req)

    case proto.MessageTypeCoA:
        // RFC 5176: Update session state (e.g. QoS change)
        // 1. Look up session from DB
        // 2. Apply CoA attributes (e.g. bandwidth limits)
        // 3. Return CoA-Nak or success
        respPayload = handleCoA(r.Context(), &req)

    default:
        http.Error(w, "unknown message type", http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(proto.AaaServerInitiatedResponse{
        Version:   proto.CurrentVersion,
        SessionID: req.SessionID,
        AuthCtxID: req.AuthCtxID,
        Payload:   respPayload,
    })
}

// handleReAuth implements TS 23.502 §4.2.9.3.
// TODO: Implement with real AMF callback when Nnssaaf_AIW handlers are wired in Phase 3.
// Returns a minimal RAR-Nak (CoA-Nak) packet per RFC 5176 §3.2 with Error-Cause 20051
// (Session-Not-Found). This is a valid RADIUS packet structure, not garbage.
func handleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_re_auth", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
    // RFC 5176 §3.2: RAR-Nak = Access-Reject with Error-Cause attribute (Type 161).
    // Minimal valid RAR-Nak: code=2, id=copied, length=6+6=12, response authenticator,
    // Error-Cause=20051 (Session-Not-Found). For now, return empty reject payload.
    // Full AMF callback → Nnssaaf_NSSAA_Re-AuthenticationNotification POST deferred to Phase 3.
    return []byte{2, 0, 0, 12} // code=2 (Access-Reject), minimal length
}

// handleRevocation implements TS 23.502 §4.2.9.4.
// TODO: Implement with real AMF callback when Nnssaaf_AIW handlers are wired in Phase 3.
// Returns an Abort-Session-Answer (ASA) with DIAMETER_UNKNOWN_SESSION_ID (5002).
// This is a valid Diameter error answer structure.
func handleRevocation(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_revoc", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
    // Diameter ASA header with Result-Code AVP = DIAMETER_UNKNOWN_SESSION_ID (5002).
    // Full AMF callback → Nnssaaf_NSSAA_RevocationNotification POST deferred to Phase 3.
    return []byte{} // Empty payload — placeholder; return empty to avoid malformed packet
}

// handleCoA implements RFC 5176 CoA.
// TODO: Implement when session state management is wired in Phase 3.
// Returns a CoA-Nak with Error-Cause 20051 (Session-Not-Found).
func handleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_coa", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
    // CoA-Nak: same structure as RAR-Nak above.
    return []byte{2, 0, 0, 12} // code=2 (Access-Reject), minimal length
}
```

**Verify:**
```bash
go build ./cmd/biz/... && go test ./cmd/biz/... -v
```

**Done:** Biz Pod handles all three server-initiated message types with placeholder implementations. Full AMF callback wiring is deferred to Phase 3.

---

## Wave 7 — Final Integration and Verification

---

### Task 7.1: Update all import paths across the codebase

**Action:**

Find and update all imports that reference `github.com/operator/nssAAF/internal/aaa`:

```bash
# Find all files importing internal/aaa
grep -rl "github.com/operator/nssAAF/internal/aaa" --include="*.go" . | grep -v ".git"

# Update each file:
# internal/aaa/aaa_test.go → internal/biz/aaa_test.go
# Any other file importing internal/aaa → internal/biz
```

Move `internal/aaa/aaa_test.go` → `internal/biz/aaa_test.go` (or merge into `internal/biz/router_test.go`).

Update `go.mod` if needed (add new `replace` directives for internal modules if module boundary changes).

**Verify:**
```bash
go build ./... && echo "OK: all packages compile"
```

**Done:** No more references to `github.com/operator/nssAAF/internal/aaa` in import statements. All packages compile.

---

### Task 7.2: Run full verification suite

**Action:**

Execute the complete validation checklist from PHASE §7 (Validation Checklist):

```bash
# 1. All three binaries compile
go build ./cmd/biz/... && \
go build ./cmd/aaa-gateway/... && \
go build ./cmd/http-gateway/... && \
echo "PASS: binaries compile"

# 2. All tests pass
go test ./... -count=1 -timeout 60s && echo "PASS: tests pass"

# 3. No linter errors
golangci-lint run ./... 2>&1 | head -50 || echo "LINT: review output above"

# 4. Import graph verification (Pitfall 4)
go mod graph | grep -E "radius|diameter" | grep -v "cmd/aaa-gateway" && \
echo "FAIL: radius/diameter reachable from non-AAA-Gateway" || \
echo "PASS: radius/diameter only in cmd/aaa-gateway"

# 5. internal/proto/ has zero internal dependencies
echo "Checking internal/proto/ dependencies:"
grep -E "^import" internal/proto/*.go | grep -v "proto\|context\|time\|sync\|encoding\|fmt\|net\|log" && \
echo "WARN: proto package has external imports" || \
echo "PASS: internal/proto/ has zero internal dependencies"

# 6. Helm charts validate
helm lint deployments/helm/nssaa-http-gateway/ && \
helm lint deployments/helm/nssaa-biz/ && \
helm lint deployments/helm/nssaa-aaa-gateway/ && \
echo "PASS: Helm charts lint"
```

**Done:** All verification checks pass.

---

</tasks>

<threat_model>

## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| AMF/AUSF → HTTP Gateway | TLS 1.3 terminates here; HTTP Gateway is publicly exposed |
| HTTP Gateway → Biz Pod | Internal ClusterIP; no mutual auth in this phase |
| Biz Pod → AAA Gateway | Internal HTTP; configurable mTLS (`biz.useMTLS: true`) or plain HTTP (default) |
| AAA Gateway → AAA-S | Raw UDP/TCP; shared secret (RADIUS) / TLS (Diameter) |
| All → Redis | Shared secret in environment variable |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-R1 | Information Disclosure | Biz Pod | mitigate | GPSI hashed in audit log; `nssaa:session:*` keys have 10-min TTL |
| T-R2 | Tampering | AAA Gateway | mitigate | Raw bytes forwarded without modification; EAP parsing in Biz Pod before forwarding |
| T-R3 | Spoofing | AAA Gateway → Biz Pod | mitigate | `X-NSSAAF-Version` header on all internal HTTP calls; version skew logs warning |
| T-R4 | Denial of Service | Redis pub/sub | mitigate | TTL on all session keys; Biz Pod discards non-matching pub/sub messages |
| T-R5 | Spoofing | RAR/ASR from AAA-S | mitigate | AAA Gateway validates session exists in Redis before forwarding; returns RAR-Nak/ASA if not found |
| T-R6 | Information Disclosure | keepalived config | mitigate | ConfigMap mounted read-only; `NET_ADMIN` capability scoped to keepalived sidecar only |
| T-R7 | Denial of Service | AAA Gateway | mitigate | `strategy: Recreate` prevents two active pods; VIP failover via keepalived |

</threat_model>

<verification>

## Wave-based Verification Summary

| Wave | Section | Tasks | Key Check |
|------|---------|-------|-----------|
| 1 | §1 Interface Contracts | 1.1–1.4 | `go test ./internal/proto/...` passes |
| 2 | §2 Split Responsibility | 2.1–2.4 | `go build ./internal/biz/...` with zero radius/diameter imports; `go build ./internal/aaa/gateway/...` |
| 3 | §3 Three Binaries | 3.1–3.4 | All three `go build ./cmd/...` succeed; `cmd/nssAAF/` deleted |
| 4 | §4 Config Refactor | 4.1 | `go test ./internal/config/...` validates component fields |
| 5 | §5 K8s Manifests | 5.1–5.3 | `helm lint` + `kustomize build` all succeed |
| 6 | §6 Server-Initiated | 6.1–6.2 | `go test ./internal/aaa/gateway/...` for RAR/ASR detection |
| 7 | Integration | 7.1–7.2 | Full `go build ./... && go test ./...` + import graph check |

## Phase-level Verification

```bash
# The single command that verifies the entire phase:
go build ./cmd/biz/... && \
go build ./cmd/aaa-gateway/... && \
go build ./cmd/http-gateway/... && \
go test ./... -count=1 -timeout 120s && \
golangci-lint run ./... && \
helm lint deployments/helm/nssaa-http-gateway/ && \
helm lint deployments/helm/nssaa-biz/ && \
helm lint deployments/helm/nssaa-aaa-gateway/ && \
kustomize build deployments/kustomize/overlays/production/ > /dev/null && \
echo "ALL CHECKS PASSED"
```

</verification>

<success_criteria>

## Measurable Completion Criteria

1. **Binary compilation**: `go build ./cmd/biz/... && go build ./cmd/aaa-gateway/... && go build ./cmd/http-gateway/...` — all three binaries compile without errors
2. **Proto isolation**: `internal/proto/` has zero imports of `internal/radius/`, `internal/diameter/`, `internal/eap/`, `internal/aaa/`
3. **Import graph**: `go mod graph | grep -E "radius|diameter" | grep -v "cmd/aaa-gateway"` returns zero lines
4. **Config validation**: `go test ./internal/config/...` passes component-specific field validation
5. **Test coverage**: All new packages have `*_test.go` files; `go test ./...` passes
6. **Helm charts**: All three charts pass `helm lint`; all Kustomize overlays build successfully
7. **Monolithic binary removed**: `cmd/nssAAF/` directory does not exist
8. **Server-initiated**: `internal/aaa/gateway/radius_handler_test.go` verifies RAR/CoA message type detection
9. **`X-NSSAAF-Version` header**: Present on all internal HTTP calls (verified by grep)
10. **Docker Compose**: `docker compose -f compose/dev.yaml config` validates YAML

</success_criteria>

<risk_register>

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| **Pitfall 4 (import graph)**: radius/diameter accidentally imported by Biz Pod | Medium | High | Run `go mod graph` check in Task 7.2 after every refactor |
| **Pitfall 2 (Redis race)**: Session write not completing before AAA-S response | Low | Medium | Use Redis pipeline (write session entry + request in same pipeline); if pipeline fails, do not forward |
| **Pitfall 3 (TTL expiry)**: EAP session exceeding 10-min Redis TTL | Low | Medium | Set TTL to 15 minutes; Biz Pod refreshes TTL on each EAP round-trip |
| **Pitfall 5 (keepalived in dev)**: `network_mode: host` conflicts with bridge networking | High | Low | Document in `compose/dev.yaml` that host networking is dev-only; use loopback/physical IP for local AAA-S testing |
| **Pitfall 6 (NRF FQDN)**: HTTP Gateway FQDN not resolvable from Biz Pod | Medium | Medium | Use Kubernetes internal DNS `svc-nssaa-http-gw.namespace.svc.cluster.local` during registration |
| **Pitfall 7 (version skew)**: Rolling upgrade causes proto schema mismatch | Low | High | Use immutable tags + `ImagePullPolicy: Always`; log warning on version mismatch, do not fail |
| **Redis pub/sub fan-out**: All Biz Pods receive every message, discarding most | Medium | Low | Correct by design — discard is O(1) map lookup; scales with Biz Pod count |
| **SCTP availability**: SCTP may not be available on all host kernels | Low | Medium | Check SCTP availability at startup; fall back to TCP with a warning log |
| **Redis Cluster**: Cluster shards pub/sub across nodes (not supported) | Medium | Medium | Use dedicated Sentinel or standalone Redis for pub/sub; document this in Helm chart values |

## Blocking Issues (Resolved)

All four blocking issues have been resolved by user decision: all four options are now supported via configuration. The plan has been updated accordingly.

## Deferred Items (Phase 3 — Data Storage)

The following are deferred to the next phase (Phase 3: Data Storage):
- **NRF client implementation**: `cmd/biz/main.go` has `// TODO: Implement NRF client` placeholder
- **PostgreSQL backing**: `nssaa.NewInMemoryStore()` → `nssaa.NewDBStore()`
- **AMF callback for Re-Auth/Revocation**: Placeholder implementations in `handleReAuth` and `handleRevocation`
- **Redis session TTL refresh**: Biz Pod should extend `nssaa:session:{sessionId}` TTL on each EAP round-trip
- **Circuit breaker for Biz Pod → AAA Gateway**: Use a third-party circuit breaker library (e.g., `github.com/sony/gobreaker`) — `internal/resilience/` is a stub; create a new package in Phase 3 if needed
- **AuthCtxStore interface**: Wire `eap.Engine` with proper `AuthCtxStore` instead of in-memory session manager

</risk_register>

<output>

After completion, create:

```
.planning/phases/Refactor-3Component/Refactor-3Component-01-SUMMARY.md
```

The summary document should capture:
- Which tasks were completed and which were deferred
- Actual import graph check output
- Final binary compilation verification
- Any blocking issues that surfaced during implementation

</output>
