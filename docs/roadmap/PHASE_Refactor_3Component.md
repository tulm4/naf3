# Phase Refactor: 3-Component Architecture

## Overview

This phase refactors the NSSAAF from a single-process monolithic binary into a **3-component Kubernetes-native architecture** as defined in `docs/design/01_service_model.md` §5.4.

**Architecture:**

```
AMF/AUSF
    │ HTTPS/443
    ▼
HTTP Gateway (N replicas)
    │ HTTP/ClusterIP
    ▼
Biz Pods (N replicas) ──────────── Redis (pub/sub)
    │ HTTP/9090                   ▲
    ▼                              │
AAA Gateway (2 replicas:          │
  active + standby)               │
    │ keepalived VIP              │
    │ Multus CNI bridge VLAN ─────┘
    ▼
AAA-S (RADIUS UDP :1812 / Diameter TCP :3868)
```

**Why refactor now?**
- Current codebase: single-process binary — `internal/radius/` and `internal/diameter/` are present but **not yet wired into the API handlers** (EAP engine is in Phase 1 echo mode; no real AAA communication exists)
- Problem: when multiple NSSAAF app pods are deployed, each opens its own connection to AAA-S, breaking Diameter RFC 6733 (single active connection requirement) and RADIUS source-IP shared-secret validation
- Solution: separate AAA Gateway (1 active connection per AAA-S cluster) from Business Logic (scales horizontally)
- Note: the 3-component separation is a prerequisite for Kubernetes production deployment; local development can continue using the all-in-one binary

**Design Doc:** `docs/design/01_service_model.md` §5.4

---

## 1. Interface Contracts (`internal/proto/`)

**Priority:** P0
**Dependencies:** None
**Design Doc:** `docs/design/01_service_model.md` §5.4.3

This is the **foundation** — define the contract between components before touching any existing code. No code changes in `cmd/`, `internal/eap/`, `internal/radius/`, `internal/diameter/`, or `internal/aaa/` until this is done.

### 1.1 `internal/proto/aaa_transport.go` — NEW

Defines the protocol between Biz Pod and AAA Gateway.

```go
package proto

// TransportType identifies the AAA transport protocol.
type TransportType string

const (
    TransportRADIUS   TransportType = "RADIUS"
    TransportDIAMETER TransportType = "DIAMETER"
)

// AaaForwardRequest is the body of POST /aaa/forward from Biz Pod to AAA Gateway.
// AAA Gateway forwards raw RADIUS/Diameter transport bytes without modification.
// Spec: docs/design/01_service_model.md §5.4.3
type AaaForwardRequest struct {
    SessionID     string       `json:"sessionId"`      // Unique per EAP exchange round; correlates request → response
    AuthCtxID     string       `json:"authCtxId"`     // NSSAAF auth context ID (maps to SliceAuthContext.authCtxId)
    TransportType TransportType `json:"transportType"` // RADIUS or DIAMETER
    Sst           uint8        `json:"sst"`           // S-NSSAI SST (0–255, TS 29.571 §5.4.4.60)
    Sd            string       `json:"sd"`           // S-NSSAI SD (6 hex chars, "FFFFFF" if not configured, TS 29.571 §5.4.4.60)
    Direction     Direction     `json:"direction"`     // CLIENT_INITIATED or SERVER_INITIATED (see §1.5)
    Payload       []byte       `json:"payload"`       // Raw RADIUS/Diameter bytes from Biz Pod
}

// Direction indicates whether the Biz Pod initiated this exchange or AAA-S initiated it
// and the AAA Gateway is routing the response.
type Direction string

const (
    // CLIENT_INITIATED: Biz Pod sends a request (Access-Request/DER) to AAA-S.
    DirectionClientInitiated Direction = "CLIENT_INITIATED"
    // SERVER_INITIATED: AAA-S sent a server-initiated message (Re-Auth-Request/ASR)
    // and the AAA Gateway is routing the response back through Biz Pod for processing.
    DirectionServerInitiated Direction = "SERVER_INITIATED"
)

// AaaForwardResponse is the response from AAA Gateway back to Biz Pod.
type AaaForwardResponse struct {
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// BizAAAClient is the interface the Biz Pod uses to talk to the AAA Gateway.
// The EAP engine calls this interface instead of directly calling RADIUS/Diameter sockets.
type BizAAAClient interface {
    ForwardEAP(ctx context.Context, req *AaaForwardRequest) (*AaaForwardResponse, error)
}
```

### 1.2 `internal/proto/biz_callback.go` — NEW

Defines how AAA Gateway routes responses back to the correct Biz Pod via Redis pub/sub.

**Redis Key/Value Schema:**

| Key | Value | TTL | Purpose |
|-----|-------|-----|---------|
| `nssaa:session:{sessionId}` | JSON: `SessionCorrEntry` | EAP session TTL (10 min) | Correlates `sessionId` → `authCtxId` + `podId` + S-NSSAI |
| `nssaa:pods` (SET) | Pod IDs of live Biz Pods | None (updated on heartbeat) | Dead pod detection |

**SessionCorrEntry** (stored at `nssaa:session:{sessionId}`):
```go
type SessionCorrEntry struct {
    AuthCtxID string `json:"authCtxId"`
    PodID     string `json:"podId"`      // Unique identifier for this Biz Pod instance (hostname or pod UID)
    Sst       uint8  `json:"sst"`
    Sd        string `json:"sd"`
    CreatedAt int64  `json:"createdAt"`  // Unix timestamp; used to detect stale entries
}
```

**Flow:**

```
1. Biz Pod sends AaaForwardRequest → AAA Gateway
2. AAA Gateway writes Redis: SET nssaa:session:{sessionId} SessionCorrEntry (TTL = 10 min)
3. AAA Gateway forwards raw bytes → AAA-S
4. AAA-S responds → AAA Gateway
5. AAA Gateway reads Redis: GET nssaa:session:{sessionId} → SessionCorrEntry
6. AAA Gateway publishes to nssaa:aaa-response: AaaResponseEvent { SessionID, AuthCtxID, Payload }
7. Originating Biz Pod (identified by PodID) receives and processes
```

**Pub/Sub Matching Logic:**

All Biz Pods share a single subscription to `nssaa:aaa-response`. On each message, a Biz Pod checks whether `event.AuthCtxID` matches any of its in-flight sessions. If it does, it processes the response. If not, it discards the message. This avoids the need for per-pod channels and works correctly across multiple Biz Pods receiving the same broadcast.

**Race Condition: Originating Biz Pod Dies:**

If the originating Biz Pod dies after step 2 but before step 7:
1. Any live Biz Pod receives the pub/sub message
2. It reads the session state from Redis/PostgreSQL using `AuthCtxID`
3. It resumes the EAP state machine from the persisted state
4. This is safe because Redis holds the authoritative session state (not in-memory)

**Key Expiry:** `nssaa:session:{sessionId}` keys expire after the EAP session TTL (10 minutes). If a response arrives after expiry, the AAA Gateway logs a warning and discards the response (AAA-S will have already timed out the original request anyway).

```go
package proto

// AaaResponseEvent is published to Redis when AAA Gateway receives a response from AAA-S.
// All Biz Pods receive every event; each discards events not matching its in-flight sessions.
type AaaResponseEvent struct {
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// SessionCorrEntry is stored at nssaa:session:{sessionId} to correlate a session ID
// (used by AAA-S as the RADIUS/Diameter session identifier) with the NSSAAF
// authCtxId and the Biz Pod that initiated the request.
type SessionCorrEntry struct {
    AuthCtxID string `json:"authCtxId"`
    PodID     string `json:"podId"`  // Biz Pod instance ID (used for observability; not for routing)
    Sst       uint8  `json:"sst"`
    Sd        string `json:"sd"`
    CreatedAt int64  `json:"createdAt"` // Unix timestamp
}

// Redis key and channel constants.
const (
    // SessionCorrKeyPrefix is the Redis key prefix for session correlation entries.
    // Full key: "nssaa:session:{sessionId}" → SessionCorrEntry (JSON)
    SessionCorrKeyPrefix = "nssaa:session:"
    // PodsKey is the Redis SET containing IDs of live Biz Pod instances.
    // Members are added/removed by Biz Pods on startup/shutdown and refreshed on heartbeat.
    PodsKey = "nssaa:pods"
    // AaaResponseChannel is the Redis pub/sub channel for AAA responses.
    AaaResponseChannel = "nssaa:aaa-response"
)
```

### 1.3 `internal/proto/http_gateway.go` — NEW

Defines how HTTP Gateway routes N58/N60 requests to Biz Pods.

**Routing Semantics:**

The HTTP Gateway uses **stateless load balancing** — no session affinity is needed for the N58/N60 path because:
- AMF sends all requests for a given `authCtxId` to the same URL (`/slice-authentications/{authCtxId}`)
- The Kubernetes Service (`svc-nssaa-biz:8080`) handles pod selection via round-robin
- If a request for an `authCtxId` lands on a different Biz Pod than the one that created the session, that Biz Pod can retrieve the session state from Redis/PostgreSQL using `authCtxId`
- This means there is **no need for sticky sessions** on the HTTP Gateway

**Circuit Breaking:** If all Biz Pods are unavailable, the HTTP Gateway returns `503 Service Unavailable` with a `Retry-After` header and ProblemDetails body. This propagates back to AMF as a transient failure.

```go
package proto

// BizServiceClient is the interface HTTP Gateway uses to forward SBI requests to Biz Pods.
// Spec: docs/design/01_service_model.md §5.4.6
type BizServiceClient interface {
    // ForwardRequest forwards an HTTP request to a Biz Pod and returns the response.
    // The HTTP Gateway is responsible for load balancing across Biz Pod replicas.
    // - path: original request path (e.g. "/nnssaaf-nssaa/v1/slice-authentications")
    // - method: HTTP method (GET, POST, PUT, DELETE)
    // - body: request body bytes
    // Returns (responseBody, httpStatus, error)
    // - 2xx: success, caller forwards response to AMF/AUSF
    // - 4xx: Biz Pod rejected the request (e.g. validation failure)
    // - 5xx: Biz Pod error; HTTP GW may retry if idempotent
    // - context.DeadlineExceeded: all Biz Pods failed; return 503
    ForwardRequest(ctx context.Context, path string, method string, body []byte) ([]byte, int, error)
}
```

### 1.4 NRF Registration

**Which component registers with NRF?** The **Biz Pod** performs NRF registration. The HTTP Gateway is a dumb forwarder and has no awareness of service semantics. The Biz Pod registers the HTTP Gateway's FQDN as the contact address, not its own pod IPs.

**Registration Flow:**
1. Biz Pod starts up, reads its own pod IP
2. Biz Pod reads the HTTP Gateway FQDN from config (e.g. `nssaa-gw.operator.com`)
3. Biz Pod creates an NFProfile with `nfInstanceId`, `nfType: NSSAAF`, `nfServices` pointing to the HTTP Gateway FQDN
4. Biz Pod uses the HTTP Gateway's IP(s) in `nodeId.nodeIpList` for HA multi-home
5. Biz Pod refreshes the registration via Nnrf_NFUpdate every `heartBeatTimer` seconds (300s)

The HTTP Gateway does NOT register with NRF. It is invisible to the service mesh from NRF's perspective.

**AAA Gateway is also invisible to NRF** — its VIP is an internal address used only for NSSAAF-to-AAA-S communication (TS 29.510 §6.1: NRF only tracks consumer-facing interfaces).

### 1.5 Versioning Strategy

All components in a NSSAAF cluster are deployed as a **cohesive unit** from the same container image tag. Version skew between HTTP Gateway, Biz Pod, and AAA Gateway is not supported in production.

**X-NSSAAF-Version header:** Each internal HTTP request carries `X-NSSAAF-Version: {major}.{minor}` in both directions:
- HTTP Gateway → Biz Pod
- Biz Pod → AAA Gateway

During a rolling upgrade, the version header allows operators to detect skew. If a newer Biz Pod receives a request from an older HTTP Gateway (or vice versa), the receiver logs a warning but continues processing (backward compatibility is maintained within a major version).

**Schema versioning:** `internal/proto/` structs are versioned using a `Version` field on each top-level message:

```go
type AaaForwardRequest struct {
    Version     string       `json:"v"` // Semantic version of proto schema, e.g. "1.0"
    SessionID   string       `json:"sessionId"`
    AuthCtxID   string       `json:"authCtxId"`
    // ...
}
```

**Deployment strategy:** Use Kubernetes `ImagePullPolicy: Always` + immutable tags (e.g. `nssAAF:2025.04.1`) to ensure all pods in a Deployment pull the same image. Rolling updates proceed one component at a time: HTTP Gateway first, then Biz Pods, then AAA Gateway.

### 1.6 Validation Checklist

- [ ] `BizAAAClient` interface covers all EAP exchange scenarios (init, exchange, completing)
- [ ] Session correlation via Redis keys is documented with full key/value schema
- [ ] `SessionCorrEntry.PodID` is stored for observability but NOT used for routing
- [ ] Dead-pod recovery: any Biz Pod can resume a session from Redis/PostgreSQL
- [ ] Key expiry TTL matches EAP session TTL (10 minutes)
- [ ] `BizServiceClient` load balancing is stateless (no sticky sessions needed for N58/N60)
- [ ] Circuit breaking: HTTP Gateway returns 503 + Retry-After when all Biz Pods are down
- [ ] NRF registration is performed by Biz Pod, not HTTP Gateway
- [ ] HTTP Gateway FQDN is registered as the SBI contact address in NFProfile
- [ ] `X-NSSAAF-Version` header is present on all internal HTTP requests
- [ ] Proto schema version field present on all top-level messages

---

## 2. Refactor `internal/aaa/` → Split Responsibility

**Priority:** P0
**Dependencies:** Phase 1 (`internal/proto/`)
**Design Doc:** `docs/design/01_service_model.md` §5.4.3, §5.4.5

The existing `internal/aaa/` contains both routing logic and direct socket communication. After the refactor:

| Package | Role | What it does |
|--------|------|-------------|
| `internal/aaa/` (renamed to `internal/biz/`) | **Biz Pod** routing | Decides which AAA server config to use, builds `AaaForwardRequest` |
| `internal/aaa/gateway/` (NEW) | **AAA Gateway** | Raw socket → HTTP client → Biz Pod |

### 2.1 `internal/biz/router.go` — REFACTOR (moved from `internal/aaa/router.go`)

Strip `sendRADIUS` and `sendDIAMETER` methods. Replace with `BuildForwardRequest`.

```go
// BuildForwardRequest creates an AaaForwardRequest for the AAA Gateway.
// Does NOT send to the network — the HTTP client in Biz Pod does that.
func (r *Router) BuildForwardRequest(
    authCtxID string,
    eapPayload []byte,
    sst uint8,
    sd string,
) (*proto.AaaForwardRequest, error) {
    decision := r.ResolveRoute(sst, sd)
    if decision == nil {
        return nil, fmt.Errorf("aaa: no route configured for sst=%d sd=%s", sst, sd)
    }

    transportType := proto.TransportRADIUS
    if decision.Protocol == ProtocolDIAMETER {
        transportType = proto.TransportDIAMETER
    }

    return &proto.AaaForwardRequest{
        SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
        AuthCtxID:     authCtxID,
        TransportType: transportType,
        Sst:           sst,
        Sd:            sd,
        Direction:     proto.DirectionClientInitiated, // server-initiated flow sets this explicitly
        Payload:       eapPayload,
    }, nil
}
```

### 2.2 `internal/eap/engine_client.go` — REFACTOR

Update `AAAClient` interface to use `BizAAAClient` (HTTP-based).

```go
// AAAClient interface — existing, no change needed
// The Biz Pod's HTTP client implements this interface.
type AAAClient interface {
    SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
}

// In the Biz Pod, the concrete type is:
// type httpAAAClient struct { baseURL string }
// func (c *httpAAAClient) SendEAP(ctx, authCtxID, eapPayload) ([]byte, error)
```

### 2.3 `internal/aaa/gateway/` — NEW directory

AAA Gateway component. This becomes the entry point for `cmd/aaa-gateway/main.go`.

```
internal/aaa/gateway/
├── gateway.go         # Main entry, wires sockets and HTTP client
├── radius_handler.go # UDP listener :1812, raw → HTTP → raw
├── diameter_handler.go # TCP listener :3868, raw → HTTP → raw
├── redis.go          # Redis pub/sub for response routing
└── keepalived.go    # Health check: monitor VIP ownership
```

#### `internal/aaa/gateway/gateway.go`

```go
package gateway

// Config holds AAA Gateway configuration.
type Config struct {
    BizURL         string // http://svc-nssaa-biz:8080
    RedisAddr     string // Redis address for pub/sub and session correlation
    RadiusAddr    string // ":1812" — UDP listen address for RADIUS from AAA-S
    DiameterAddr  string // ":3868" — TCP listen address for Diameter from AAA-S
    AAAGatewayURL string // http://svc-nssaa-aaa:9090 — for Biz Pods to reach this gateway
    Logger        *slog.Logger
}

// Gateway is the AAA Gateway component. It runs in a separate process from Biz Pods.
// Handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
type Gateway struct {
    bizURL       string
    redis        *redis.Client
    radiusSrv    *radius.Server
    diameterSrv  *diameter.Server
    bizClient    *http.Client
    redisSub     *redisMux // multiplexes pub/sub messages to correct pending request
    logger       *slog.Logger
}
```

#### `internal/aaa/gateway/radius_handler.go`

```go
// Listen on UDP/:1812. For each incoming RADIUS packet, determine direction:
//
// Client-initiated (Access-Request from Biz Pod → AAA-S):
//   1. Parse message type
//   2. If Access-Request: route to Biz Pod via HTTP POST /aaa/forward
//   3. Wait for response via Redis pub/sub
//   4. Send raw response bytes back on the UDP socket
//
// Server-initiated (RAR from AAA-S → Biz Pod):
//   1. Parse message type (Re-Auth-Request, DM, CoA)
//   2. Look up session from Redis: nssaa:session:{sessionId}
//   3. HTTP POST /aaa/server-initiated to Biz Pod
//   4. Return response to AAA-S
//
// See §6 for full server-initiated flow details.
func (h *RadiusHandler) HandlePacket(conn *net.UDPConn, addr *net.UDPAddr, raw []byte)
```

#### `internal/aaa/gateway/diameter_handler.go`

```go
// Listen on TCP/:3868 (or SCTP). For each incoming Diameter message:
//
// Client-initiated (DER from Biz Pod → AAA-S):
//   1. HTTP POST /aaa/forward to Biz Pod
//   2. Wait for response via Redis pub/sub
//   3. Write raw response bytes back on the TCP connection
//
// Server-initiated (ASR from AAA-S → Biz Pod):
//   1. Parse Session-Id from Diameter header
//   2. Look up session from Redis: nssaa:session:{sessionId}
//   3. HTTP POST /aaa/server-initiated to Biz Pod
//   4. Return response (ASA) to AAA-S
//
// See §6 for full server-initiated flow details.
func (h *DiameterHandler) HandleConnection(conn net.Conn)
```

#### `internal/aaa/gateway/redis.go`

```go
// AAA Gateway subscribes to proto.AaaResponseChannel on the shared Redis instance.
// Received messages are matched against pending requests by SessionID and delivered
// to the correct handler (UDP response sender or TCP response writer).
//
// For server-initiated requests (§6), the AAA Gateway makes outbound HTTP calls
// to Biz Pods and does not use the pub/sub path for the request leg — only the
// response leg (Biz Pod → AAA-S) goes through pub/sub.
func (g *Gateway) subscribeResponses()
```

### 2.4 Validation Checklist

- [ ] `internal/biz/router.go` compiles without `internal/radius/` or `internal/diameter/` imports
- [ ] `internal/aaa/gateway/` does NOT import `internal/radius/` or `internal/diameter/`
- [ ] Raw bytes are forwarded without modification at the gateway level
- [ ] Redis pub/sub routing works across multiple Biz Pods
- [ ] `nssaa:session:{sessionId}` key is written before forwarding to AAA-S
- [ ] `SessionCorrEntry` stores `AuthCtxID`, `PodID`, `Sst`, `Sd`, `CreatedAt`
- [ ] `PodsKey` SET is maintained by Biz Pods (add on startup, remove on shutdown)
- [ ] Server-initiated message types (RAR, ASR, CoA) are correctly identified by message type
- [ ] Missing session: AAA Gateway returns error response to AAA-S without forwarding

---

## 3. Split `cmd/nssAAF/main.go` into 3 Binaries

**Priority:** P0
**Dependencies:** Phase 1 (`internal/proto/`), Phase 2 (`internal/aaa/gateway/`)
**Design Doc:** `docs/design/01_service_model.md` §5.4.5, §5.4.7

### 3.1 `cmd/biz/main.go` — NEW

Business Logic Pod binary. Replaces the current `cmd/nssAAF/main.go` for production.

```go
func main() {
    // Wire:
    // - HTTP handlers (N58/N60) → EAP engine → AAA router → HTTP client → AAA Gateway
    // - AuthCtxStore → Redis-backed implementation
    // - PostgreSQL for persistent storage
    // - NRF client for service discovery
}
```

### 3.2 `cmd/aaa-gateway/main.go` — NEW

AAA Gateway Pod binary.

```go
func main() {
    gw := gateway.New(gateway.Config{
        BizURL:       cfg.BizServiceURL,
        RedisAddr:   cfg.Redis.Addr,
        RadiusAddr:  cfg.AAAgw.ListenRADIUS,
        DiameterAddr: cfg.AAAgw.ListenDIAMETER,
    })
    gw.Run()
}
```

### 3.3 `cmd/http-gateway/main.go` — NEW

HTTP Gateway Pod binary. **Initial implementation uses stdlib `net/http` with TLS 1.3 termination.** Envoy migration is planned for a future phase (see §6).

The HTTP Gateway is a thin pass-through proxy with no application logic. It terminates TLS, forwards HTTP/2 requests to Biz Pods, and handles observability (metrics, logging, tracing).

```go
func main() {
    cfg := loadConfig()
    bizClient := &httpBizClient{
        bizServiceURL: cfg.Biz.BizServiceURL, // http://svc-nssaa-biz:8080
        httpClient:    &http.Client{Timeout: 10 * time.Second},
        version:      cfg.Version,
    }

    mux := http.NewServeMux()
    // Forward all N58/N60 paths to Biz Pods
    mux.HandleFunc("/nnssaaf-nssaa/", bizClient.forward)
    mux.HandleFunc("/nnssaaf-aiw/", bizClient.forward)
    // Health endpoints (do not forward)
    mux.HandleFunc("/health", healthHandler)
    mux.HandleFunc("/ready", readyHandler)

    srv := &http.Server{
        Addr:      cfg.Server.Addr,
        TLSConfig: tlsConfig(cfg.TLS),
        Handler:   mux,
    }
    log.Println("http-gateway listening on", srv.Addr)
    log.Fatal(srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key))
}
```

### 3.4 `cmd/nssAAF/main.go` — REFACTOR into all-in-one dev mode

The current `cmd/nssAAF/main.go` is refactored to run all three components in a single process for **local development only**. Production always deploys the three separate binaries.

**What stays in `cmd/nssAAF/main.go`:**
- All-in-one wiring (HTTP handlers + EAP engine + AAA router + RADIUS/Diameter clients)
- This mode skips `internal/proto/` — the components communicate via function calls, not HTTP
- Config flag `--component=all-in-one` (default when no `--component` is specified)

**What moves:**
- `internal/proto/` → shared library used by all three component binaries
- `internal/aaa/gateway/` → new package, imported only by `cmd/aaa-gateway/`
- RADIUS/Diameter encoding logic (`internal/radius/`, `internal/diameter/`) → **moves into Biz Pod**, where it encodes/decodes EAP but does NOT open sockets
- The socket-open logic is removed from Biz Pod; it lives only in AAA Gateway

**What gets deleted:** None. All existing code is either moved or retained.

```go
// cmd/nssAAF/main.go (development only)
//
// Deprecated for production: use cmd/biz, cmd/http-gateway, cmd/aaa-gateway.
// This entry point runs all three components in a single process for local development.
// All-in-one mode bypasses internal/proto/ — components communicate via function calls.

func main() {
    flag.Parse()
    cfg, err := config.Load(*configPath)
    if cfg.Component == config.ComponentAllInOne || flag.Arg(0) == "" {
        runAllInOne(cfg)
    } else {
        runComponent(cfg)
    }
}

// runComponent starts a single component based on cfg.Component.
// runAllInOne wires all three components in-process (dev mode only).

### 3.5 Validation Checklist

- [ ] `go build ./cmd/biz/...` compiles
- [ ] `go build ./cmd/aaa-gateway/...` compiles
- [ ] `go build ./cmd/http-gateway/...` compiles
- [ ] `go build ./cmd/nssAAF/...` still works (dev mode)
- [ ] All three binaries start and communicate via `internal/proto/` interfaces
- [ ] `go test ./...` passes for all packages

---

## 4. Config Refactor

**Priority:** P0
**Dependencies:** Phase 1
**Design Doc:** `docs/design/01_service_model.md` §5.4.5

### 4.1 `internal/config/component.go` — NEW

```go
package config

// Component identifies which binary is being started.
type Component string

const (
    ComponentHTTPGateway Component = "http-gateway"
    ComponentBiz         Component = "biz"
    ComponentAAAGateway  Component = "aaa-gateway"
    ComponentAllInOne    Component = "all-in-one" // development only
)

// Config is the root configuration struct.
// After refactor, only relevant sections are loaded based on Component.
type Config struct {
    Component Component `yaml:"component"` // required: which binary to start

    // Common (all components)
    Server   ServerConfig   `yaml:"server"`
    Database DatabaseConfig `yaml:"database"`
    Redis   RedisConfig    `yaml:"redis"`
    Logging LoggingConfig   `yaml:"logging"`
    Metrics MetricsConfig   `yaml:"metrics"`

    // Biz Pod only
    EAP  EAPConfig  `yaml:"eap"`
    AAA  AAAConfig `yaml:"aaa"`
    NRF  NRFConfig `yaml:"nrf"`
    UDM  UDMConfig `yaml:"udm"`

    // Internal communication
    Biz   BizConfig  `yaml:"biz"`
    AAAgw AAAgwConfig `yaml:"aaaGateway"`
}

// BizConfig: how Biz Pod talks to AAA Gateway
type BizConfig struct {
    AAAGatewayURL string `yaml:"aaaGatewayUrl"` // http://svc-nssaa-aaa:9090
}

// AAAgwConfig: AAA Gateway specific settings
type AAAgwConfig struct {
    ListenRADIUS   string `yaml:"listenRadius"`   // ":1812"
    ListenDIAMETER string `yaml:"listenDiameter"` // ":3868"
    BizServiceURL  string `yaml:"bizServiceUrl"`  // http://svc-nssaa-biz:8080
    KeepalivedCheck string `yaml:"keepalivedCheck"` // path to health check script
}
```

### 4.2 Update `internal/config/config.go`

Deprecate monolithic `Config` in favor of `component.go`. Keep for backward compatibility during transition.

### 4.3 Validation Checklist

- [ ] All three binary configs are distinct and non-overlapping
- [ ] No hardcoded URLs — all service discovery via environment variables or Kubernetes DNS
- [ ] Config schema validated with `go test ./internal/config/...`

---

## 5. Kubernetes Manifests

**Priority:** P1
**Dependencies:** Phase 3 (binaries compile), Phase 4 (config ready)
**Design Doc:** `docs/design/01_service_model.md` §5.4.5, §5.4.7

### 5.1 Directory Structure

```
deployments/
├── helm/
│   ├── nssaa-http-gateway/    # NEW: HTTP Gateway chart
│   ├── nssaa-biz/             # REFACTOR: current NSSAAF chart becomes Biz Pod
│   └── nssaa-aaa-gateway/    # NEW: AAA Gateway chart with keepalived
├── kustomize/
│   ├── base/
│   │   ├── http-gateway/
│   │   ├── biz/
│   │   └── aaa-gateway/
│   └── overlays/
│       ├── development/       # All-in-one or minimal
│       ├── production/        # 3-component, keepalived
│       └── carrier/          # Multi-AZ, Multus CNI
└── README.md
```

### 5.2 HTTP Gateway — `nssaa-http-gateway/`

```yaml
# templates/deployment.yaml
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: http-gw
          image: nssAAF-http-gw:${VERSION}
          ports:
            - containerPort: 8443
          env:
            - name: BIND_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          args: ["--bind-ip=$(BIND_IP)"]
```

### 5.3 Biz Pod — `nssaa-biz/`

```yaml
# templates/deployment.yaml
spec:
  replicas: 5
  template:
    spec:
      containers:
        - name: biz
          image: nssAAF-biz:${VERSION}
          ports:
            - containerPort: 8080
```

### 5.4 AAA Gateway — `nssaa-aaa-gateway/`

```yaml
# templates/deployment.yaml
spec:
  replicas: 2
  strategy:
    type: Recreate  # Prevent two active pods during rolling update
  template:
    spec:
      # Multus CNI: secondary interface on bridge VLAN
      annotations:
        k8s.v1.cni.cncf.io/networks: |
          [{
            "name": "aaa-bridge-vlan",
            "interface": "net0",
            "ips": ["$(POD_IP)/24"],
            "gateway": ["10.1.100.1"]
          }]
      containers:
        - name: aaa-gw
          image: nssAAF-aaa-gw:${VERSION}
          securityContext:
            capabilities:
              add: ["NET_ADMIN"]  # needed for keepalived to manage VIP
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          ports:
            - containerPort: 9090  # HTTP: Biz Pod → AAA Gateway
            - containerPort: 1812  # RADIUS UDP: AAA-S → AAA Gateway
            - containerPort: 3868  # Diameter TCP: AAA-S → AAA Gateway
        - name: keepalived
          image: osixopen/keepalived:2.3.1
          securityContext:
            capabilities:
              add: ["NET_ADMIN", "NET_RAW"]
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          volumeMounts:
            - name: keepalived-conf
              mountPath: /etc/keepalived
      volumes:
        - name: keepalived-conf
          configMap:
            name: nssaa-aaa-keepalived
```

### 5.5 keepalived ConfigMap

```yaml
# templates/configmap-keepalived.yaml
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
            10.1.100.11
        }
        virtual_ipaddress {
            10.1.100.200/24
        }
        track_script {
            chk_aaa_gw  # health check script for the aaa-gw container
        }
    }
```

### 5.6 Internal Services

```yaml
# biz service (ClusterIP, internal only)
apiVersion: v1
kind: Service
metadata:
  name: svc-nssaa-biz
spec:
  clusterIP: None  # Headless — direct pod routing
  selector:
    app: nssaa-biz
  ports:
    - port: 8080
      targetPort: 8080

# AAA Gateway service (ClusterIP, routes to active pod)
apiVersion: v1
kind: Service
metadata:
  name: svc-nssaa-aaa
spec:
  clusterIP: None  # Headless
  selector:
    app: nssaa-aaa
  ports:
    - port: 9090
      targetPort: 9090
```

### 5.7 Multus CNI NetworkAttachmentDefinition

```yaml
# templates/network-attachment.yaml
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

### 5.8 Validation Checklist

- [ ] `helm lint nssaa-http-gateway/`
- [ ] `helm lint nssaa-biz/`
- [ ] `helm lint nssaa-aaa-gateway/`
- [ ] `kubectl apply --dry-run` all manifests
- [ ] keepalived ConfigMap is injected per-replica with correct peer IP
- [ ] Multus CNI annotations are correct
- [ ] `strategy: Recreate` prevents rolling update with two actives

---

## 6. AAA-S Server-Initiated Flow (Re-Authentication & Revocation)

**Priority:** P1 (needed for complete 3GPP compliance)
**Dependencies:** Phase 1 (`internal/proto/`), Phase 2 (`internal/aaa/gateway/`), Phase 3 (Split Binaries)
**Spec:** TS 23.502 §4.2.9.3 (Re-Auth), §4.2.9.4 (Revocation)

This section addresses the **critical gap**: which component receives server-initiated messages from AAA-S (Re-Auth-Request, Abort-Session-Request), and how they are routed to Biz Pods.

### 6.1 Problem Statement

AAA-S can initiate a re-authentication or session revocation at any time. This means:
- AAA-S opens a connection to NSSAAF (not the other way around)
- The connection is received by the **AAA Gateway** (active pod) on UDP/:1812 or TCP/:3868
- The AAA Gateway must route this message to the **correct Biz Pod** that owns the subscriber session

This is different from the client-initiated flow where Biz Pod initiates the exchange.

### 6.2 Message Types Handled by AAA Gateway

| Message | Protocol | Direction | Action |
|---------|----------|-----------|--------|
| Access-Request | RADIUS | AAA-S → NSSAAF | Forward to Biz Pod (see §1) |
| Diameter-EAR (DER) | Diameter | AAA-S → NSSAAF | Forward to Biz Pod (see §1) |
| **Re-Auth-Request (RAR)** | RADIUS | AAA-S → NSSAAF | Route to Biz Pod (this section) |
| **Abort-Session-Request (ASR)** | Diameter | AAA-S → NSSAAF | Route to Biz Pod (this section) |
| Access-Challenge | RADIUS | NSSAAF → AAA-S | Biz Pod initiates (see §1) |
| Diameter-EA (DEA) | Diameter | NSSAAF → AAA-S | Biz Pod initiates (see §1) |

### 6.3 Routing Logic in AAA Gateway

The AAA Gateway distinguishes client-initiated from server-initiated by examining the RADIUS/Diameter message type before forwarding:

```go
// On receiving a raw packet from AAA-S:
func (h *RadiusHandler) HandlePacket(conn *net.UDPConn, addr *net.UDPAddr, raw []byte) {
    msgType := parseMessageType(raw) // RADIUS Message-Type attribute

    switch msgType {
    case rfc2865.AccessRequest:
        // Client-initiated: Biz Pod expects this. Route to Biz Pod.
        h.forwardToBizPod(ctx, raw, DirectionClientInitiated)

    case rfc2865.AccessChallenge, // Not applicable for server-initiated RAR
         rfc2865.DisconnectRequest:
        // Server-initiated: RAR or DM. Parse NAS-IP/NAS-Identifier to find AuthCtxID.
        authCtxID, err := h.parseServerInitiatedContext(raw)
        h.forwardToBizPodServerInitiated(ctx, raw, authCtxID)
    }
}
```

### 6.4 Server-Initiated Forward to Biz Pod

Unlike the client-initiated flow where Biz Pod sends `AaaForwardRequest` to AAA Gateway, the server-initiated flow is **AAA Gateway → Biz Pod** (reverse direction). Two options:

**Option A — HTTP POST from AAA Gateway to Biz Pod (selected):**

The AAA Gateway POSTs the server-initiated message to Biz Pods via the same internal HTTP endpoint used for client-initiated responses.

```go
// AaaServerInitiatedRequest: AAA Gateway → Biz Pod for server-initiated messages.
type AaaServerInitiatedRequest struct {
    SessionID     string       `json:"sessionId"`     // RADIUS/Diameter session ID from AAA-S
    AuthCtxID     string       `json:"authCtxId"`    // Looked up from nssaa:session:{sessionId}
    TransportType TransportType `json:"transportType"`
    MessageType   MessageType  `json:"messageType"`   // RAR, ASR, CoA-Request, etc.
    Payload       []byte       `json:"payload"`       // Raw bytes
}

type MessageType string

const (
    MessageTypeRAR      MessageType = "RAR"   // RADIUS Re-Auth-Request (RFC 5176)
    MessageTypeASR      MessageType = "ASR"   // Diameter Abort-Session-Request (RFC 6733)
    MessageTypeCoA      MessageType = "COA"   // RADIUS Change-of-Authorization (RFC 5176)
)
```

**Flow for server-initiated messages:**

```
1. AAA-S sends RAR/ASR → AAA Gateway (UDP/1812 or TCP/3868)
2. AAA Gateway parses message type → identifies as server-initiated
3. AAA Gateway extracts sessionId (RADIUS State or Diameter Session-Id)
4. AAA Gateway reads Redis: GET nssaa:session:{sessionId} → SessionCorrEntry
5. AAA Gateway POSTs to Biz Pod: POST /aaa/server-initiated { AaaServerInitiatedRequest }
6. Biz Pod processes:
   - Re-Auth-Request: calls AMF callback URI → Nnssaaf_NSSAA_Re-AuthenticationNotification
   - ASR: calls AMF callback URI → Nnssaaf_NSSAA_RevocationNotification
   - CoA-Request: updates session state in DB
7. Biz Pod returns response (RAR-Response/ASR-Response) → AAA Gateway
8. AAA Gateway forwards raw response → AAA-S
```

**Biz Pod HTTP endpoint for server-initiated:**

```go
// In Biz Pod HTTP server:
mux.HandleFunc("POST /aaa/server-initiated", handleServerInitiated)

// handleServerInitiated processes RAR/ASR/CoA from AAA Gateway.
func (h *BizHandler) handleServerInitiated(w http.ResponseWriter, r *http.Request) {
    var req proto.AaaServerInitiatedRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    switch req.MessageType {
    case proto.MessageTypeRAR:
        // TS 23.502 §4.2.9.3: Nnssaaf_NSSAA_Re-AuthenticationNotification → AMF
        h.notifyReAuth(r.Context(), req)
    case proto.MessageTypeASR:
        // TS 23.502 §4.2.9.4: Nnssaaf_NSSAA_RevocationNotification → AMF
        h.notifyRevocation(r.Context(), req)
    case proto.MessageTypeCoA:
        // Update session state (e.g. QoS change)
        h.handleCoA(r.Context(), req)
    }

    // Return response to AAA Gateway (forwards to AAA-S)
    w.WriteHeader(http.StatusOK)
}
```

### 6.5 Session Correlation for Server-Initiated

For server-initiated messages, the AAA Gateway must look up the session before routing. If `nssaa:session:{sessionId}` does not exist (session expired or never found):

1. Log a warning with the sessionId and raw message
2. Send an appropriate error response to AAA-S:
   - RAR → send RAR-Nak (RADIUS) with `Error-Cause = 20051` (Session-Not-Found)
   - ASR → send ASA (Diameter) with `Result-Code = DIAMETER_UNKNOWN_SESSION_ID`
3. Do NOT forward to Biz Pod (no context to process)

### 6.6 Validation Checklist

- [ ] AAA Gateway correctly identifies message type (client-initiated vs. server-initiated)
- [ ] RAR forwarded to Biz Pod with correct `MessageType`
- [ ] ASR forwarded to Biz Pod with correct `MessageType`
- [ ] Biz Pod calls AMF Re-AuthenticationNotification callback
- [ ] Biz Pod calls AMF RevocationNotification callback
- [ ] Response (RAR-Nak/ASA) returned to AAA-S if session not found
- [ ] Redis session correlation works for server-initiated messages

---

## 7. Envoy Migration (Future Work)

**Not in scope for this phase.** Documented here for completeness.

The architecture diagrams in `01_service_model.md` §5.1 show Envoy for both HTTP Gateway and AAA Gateway. This phase uses stdlib `net/http` for the HTTP Gateway and a custom Go UDP/TCP server for the AAA Gateway to reduce initial implementation complexity.

**Envoy migration is planned for a future phase** and should be considered when:
- Advanced traffic management is needed (rate limiting, circuit breaking, retries with jitter)
- mTLS between components is required
- Observability integration (Envoy statsd, OpenTelemetry) is prioritized
- WAF capabilities are needed at the boundary

**Scope for Envoy migration (when pursued):**
- Replace stdlib HTTP Gateway with Envoy xDS-controlled proxy
- Replace custom UDP/TCP server with Envoy RADIUS/Diameter proxy filter
- Add Istio service mesh for east-west mTLS between components
- Write Envoy bootstrap configs for both gateways

---

## Phase Dependencies

```
Phase 1 (Interface Contracts + §1.4 NRF + §1.5 Versioning)
    │
    ├──► Phase 2 (Refactor internal/aaa/)
    │         │
    │         └──► Phase 3 (Split Binaries)
    │                   │
    └──► Phase 4 (Config Refactor) ──► Phase 5 (Kubernetes Manifests)
                                              │
                                              └──► Phase 6 (Server-Initiated Flow)
                                                        │
                                                        └──► Phase 7 (Envoy Migration) [future]
```

**Parallel tracks:**
- Phase 1 and Phase 4 can start in parallel (no code dependencies)
- Phase 5 needs Phase 3 and Phase 4 to be complete
- Phase 6 (Server-Initiated) needs Phase 1, 2, and 3 to be complete
- Phase 7 is purely optional

---

## Validation Checklist (Full Phase)

- [ ] `go build ./cmd/...` — all three binaries compile
- [ ] `go test ./...` — all tests pass
- [ ] `golangci-lint run ./...` — no linter errors
- [ ] Development mode (`cmd/nssAAF`) still works
- [ ] `internal/radius/` and `internal/diameter/` are NOT imported by `cmd/aaa-gateway/`
- [ ] `cmd/biz/` does NOT open raw UDP/TCP sockets for RADIUS/Diameter
- [ ] `internal/proto/` has zero dependencies on `internal/radius/`, `internal/diameter/`, `internal/eap/`
- [ ] All three binaries can be deployed independently
- [ ] Redis session routing works across multiple Biz Pod replicas
- [ ] Dead-pod recovery: any Biz Pod can resume a session from Redis
- [ ] Helm charts lint and validate
- [ ] keepalived failover: simulate active pod death, verify VIP migrates
- [ ] Server-initiated (RAR/ASR) flows are correctly routed from AAA Gateway to Biz Pod
- [ ] `nssaa:session:{sessionId}` keys expire after EAP session TTL (10 min)
- [ ] `X-NSSAAF-Version` header present on all internal HTTP requests

---

## Spec References

- RFC 6733 — Diameter Base Protocol (single connection requirement)
- RFC 2865 — RADIUS (source-IP shared secret consideration)
- RFC 5176 — RADIUS Change-of-Authorization (CoA) and Disconnect Messages (RAR, DMR)
- TS 23.502 §4.2.9 — NSSAA Procedure (includes Re-Auth §4.2.9.3, Revocation §4.2.9.4)
- TS 29.561 §16-17 — AAA protocol mapping (RADIUS/Diameter)
- `docs/design/01_service_model.md` §5.4 — Multi-Pod Kubernetes Deployment
- `docs/design/01_service_model.md` §5.4.3 — AAA Gateway Responsibilities
- `docs/design/01_service_model.md` §5.4.5 — AAA Gateway HA with keepalived
- `docs/design/01_service_model.md` §5.4.6 — Internal Communication Flow
