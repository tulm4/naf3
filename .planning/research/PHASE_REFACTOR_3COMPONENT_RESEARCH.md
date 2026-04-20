# Phase Refactor: 3-Component Architecture — Research

**Researched:** 2026-04-21
**Domain:** 3-component microservice refactor for 3GPP NSSAAF (5G Network Slice-Specific Authentication and Authorization Function)
**Confidence:** HIGH

## Summary

The NSSAAF codebase is currently a single-process monolithic binary (`cmd/nssAAF/main.go`) that wires HTTP handlers, the EAP engine, and RADIUS/Diameter clients into one process. The refactor breaks this into three independent Kubernetes-native binaries: HTTP Gateway (stateless TLS terminator), Biz Pods (application logic + EAP engine), and AAA Gateway (2-replica active-standby RADIUS/Diameter proxy with keepalived). The primary motivation is RFC 6733 compliance — Diameter requires a single active connection per AAA-S cluster, which is impossible when multiple NSSAAF pods each open their own connections. The refactor also enables horizontal scaling of Biz Pods independently from AAA connectivity.

The codebase already contains well-structured `internal/aaa/router.go` (routing logic), `internal/aaa/aaa.go` (Router type), `internal/aaa/config.go` (AAA config), `internal/aaa/metrics.go` (AAA metrics), and `internal/eap/engine.go` with an `AAAClient` interface that both `radius.Client` and `diameter.Client` satisfy. The `internal/proto/` package does not exist — this is the critical gap and the P0 deliverable. The refactor must define the wire protocol between Biz Pod and AAA Gateway before touching any existing code.

**Primary recommendation:** Implement Section 1 (interface contracts in `internal/proto/`) first. This is pure interface design with zero code dependencies — it defines the contract that all three binaries will use. Only after Section 1 is complete should code refactoring begin.

---

## User Constraints

> This section is empty — no CONTEXT.md was provided. All research proceeds under full discretion.

---

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| REQ-R1 | Split `internal/aaa/` into `internal/biz/` (routing) and `internal/aaa/gateway/` (raw socket) | §2, §3 — router.go strips direct socket calls, gateway/ package added |
| REQ-R2 | Create `internal/proto/` with `AaaForwardRequest`, `BizAAAClient`, `BizServiceClient` interfaces | §1 — three proto files fully specified |
| REQ-R3 | Create three binaries: `cmd/biz/`, `cmd/aaa-gateway/`, `cmd/http-gateway/` | §3 — entry points specified, `cmd/nssAAF/main.go` to be deleted |
| REQ-R4 | Per-component config (`Component` enum, `BizConfig`, `AAAgwConfig`) | §4 — config struct hierarchy |
| REQ-R5 | Redis pub/sub for Biz Pod ↔ AAA Gateway response routing | §1.2 — Redis keys, TTL, pub/sub flow |
| REQ-R6 | Kubernetes manifests (Helm charts, Kustomize overlays) | §5 — YAML resource specs with keepalived + Multus |
| REQ-R7 | Server-initiated flow (RAR/ASR routing) | §6 — message type detection, session correlation, error responses |

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| TLS termination, N58/N60 forwarding | HTTP Gateway | — | Only component with external network exposure |
| EAP engine, N58/N60 handlers, NRF registration | Biz Pod | — | All 3GPP business logic lives here |
| RADIUS/Diameter raw socket management | AAA Gateway | — | Must bind to VIP via keepalived + Multus |
| Session correlation (AAA response → Biz Pod) | Redis (pub/sub) | — | Shared state store for multi-pod coordination |
| AAA routing decision (S-NSSAI → server config) | Biz Pod | — | `internal/biz/router.go` (moved from `internal/aaa/`) |
| Version header injection | All components | — | `X-NSSAAF-Version` on all internal HTTP requests |

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `net/http` | 1.25 (go.mod) | HTTP Gateway server | PHASE §3.3: "stdlib net/http with TLS 1.3 termination" |
| Go stdlib `net` | 1.25 | UDP/TCP for AAA Gateway | Raw socket listening for RADIUS (:1812) / Diameter (:3868) |
| `go-redis/v9` v9.18.0 | 9.18.0 | Redis pub/sub for response routing | Already in go.mod; used for session correlation |
| `jackc/pgx/v5` v5.9.1 | 5.9.1 | PostgreSQL for Biz Pod persistence | Already in go.mod |
| `go-diameter/v4` v4.1.0 | 4.1.0 | Diameter protocol support | Already in go.mod |
| `fiorix/go-diameter/v4` | 4.1.0 | Diameter client FSM | Already in go.mod |
| `google/uuid` v1.6.0 | 1.6.0 | Request/authCtx ID generation | Already in go.mod |
| `stretchr/testify` v1.11.1 | 1.11.1 | Test assertions | Already in go.mod |
| `gopkg.in/yaml.v3` | 3.0.1 | Config file parsing | Already in go.mod |

### Supporting (for Kubernetes manifests)

| Tool | Version | Purpose | Why Standard |
|------|---------|---------|--------------|
| Helm | 3.x | Chart templating for K8s manifests | PHASE §5.1 |
| Kustomize | 5.x | Overlay management | PHASE §5.1 |
| keepalived | 2.3.1 | AAA Gateway active-standby VIP | PHASE §5.5, `osixopen/keepalived:2.3.1` |
| Multus CNI | latest | Secondary network interface | PHASE §5.7, NetworkAttachmentDefinition |

### No New Go Dependencies Required

All three binaries reuse existing Go dependencies from `go.mod`. The refactor does not introduce new third-party libraries. `internal/proto/` is pure Go with no external dependencies.

---

## Architecture Patterns

### System Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              External Network                              │
│  AMF/AUSF ─── HTTPS/443 ──► HTTP Gateway (N replicas)                    │
│                                                                              │
│  ▸ TLS 1.3 termination at HTTP Gateway                                       │
│  ▸ Forwards N58/N60 requests to Biz Pods via ClusterIP                       │
│  ▸ Stateless load balancing — no sticky sessions needed                      │
└──────────────────────────────────┬───────────────────────────────────────────┘
                                   │ HTTP/ClusterIP
                                   ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                        Biz Pods (N replicas)                                │
│                                                                              │
│  ▸ NSSAA/NSSAI handlers → EAP engine → AAA router                          │
│  ▸ AAA router: BuildForwardRequest() → HTTP POST /aaa/forward               │
│  ▸ Receives AAA responses via Redis pub/sub (nssaa:aaa-response channel)   │
│  ▸ NRF registration: HTTP Gateway FQDN as contact address                  │
│  ▸ Redis: SET nssaa:session:{sessionId} (TTL=10min) before forwarding      │
└────────┬─────────────────────────────────────┬────────────────────────────┘
         │ HTTP POST /aaa/forward               │ Redis pub/sub
         │ X-NSSAAF-Version header             │ nssaa:aaa-response
         ▼                                      │
┌────────────────────────────────────────┐    │
│        AAA Gateway (2 replicas:         │    │
│        active + standby via keepalived) │◄───┘
│                                        │         │
│  ▸ RADIUS UDP :1812 ──────────────────┼─────────┘
│  ▸ Diameter TCP :3868 ────────────────┤
│  ▸ keepalived VIP on Multus net0      │
│  ▸ On receiving response:             │
│      1. GET nssaa:session:{sessionId} │
│      2. PUBLISH nssaa:aaa-response     │
│  ▸ On server-initiated (RAR/ASR):      │
│      1. Parse message type             │
│      2. GET nssaa:session:{sessionId} │
│      3. HTTP POST /aaa/server-initiated│
└────────┬───────────────────────────────┘
         │ RADIUS/Diameter raw bytes
         ▼
┌─────────────────────────────────────────┐
│        AAA-S (RADIUS :1812 / Diameter :3868) │
└─────────────────────────────────────────┘
```

### Recommended Project Structure (post-refactor)

```
naf3/
├── cmd/
│   ├── biz/                    # NEW — Business Logic Pod entry point
│   │   └── main.go
│   ├── aaa-gateway/            # NEW — AAA Gateway entry point
│   │   └── main.go
│   └── http-gateway/           # NEW — HTTP Gateway entry point
│       └── main.go
│   (DELETE cmd/nssAAF/)
├── internal/
│   ├── proto/                  # NEW — Interface contracts (P0 deliverable)
│   │   ├── aaa_transport.go   # BizAAAClient, AaaForwardRequest, TransportType, Direction
│   │   ├── biz_callback.go    # SessionCorrEntry, AaaResponseEvent, Redis keys
│   │   └── http_gateway.go    # BizServiceClient interface
│   ├── biz/                   # NEW — renamed from internal/aaa/
│   │   ├── router.go          # REFACTORED — BuildForwardRequest (no socket calls)
│   │   ├── config.go          # MOVED from internal/aaa/config.go
│   │   └── metrics.go         # MOVED from internal/aaa/metrics.go
│   ├── aaa/
│   │   └── gateway/           # NEW — raw socket → HTTP → raw
│   │       ├── gateway.go     # Main entry, wires sockets and HTTP client
│   │       ├── radius_handler.go   # UDP :1812 listener
│   │       ├── diameter_handler.go # TCP :3868 listener
│   │       ├── redis.go       # Redis pub/sub for response routing
│   │       └── keepalived.go  # VIP health check
│   ├── api/                   # UNCHANGED — reused by cmd/biz/
│   │   ├── nssaa/
│   │   ├── aiw/
│   │   └── common/
│   ├── eap/                   # UNCHANGED — used by cmd/biz/
│   │   ├── engine.go
│   │   └── engine_client.go   # AAAClient interface (same interface, new impl)
│   ├── radius/                # UNCHANGED — used by cmd/aaa-gateway/
│   │   └── client.go
│   ├── diameter/              # UNCHANGED — used by cmd/aaa-gateway/
│   │   └── client.go
│   ├── config/               # REFACTORED — add component.go
│   │   ├── config.go         # existing
│   │   └── component.go      # NEW — Component enum, BizConfig, AAAgwConfig
│   ├── storage/
│   ├── cache/redis/
│   └── types/
├── compose/
│   └── dev.yaml             # NEW — Docker Compose for local dev
├── deployments/
│   ├── helm/                # NEW
│   │   ├── nssaa-http-gateway/
│   │   ├── nssaa-biz/
│   │   └── nssaa-aaa-gateway/
│   └── kustomize/           # NEW
│       ├── base/
│       └── overlays/
├── oapi-gen/               # UNCHANGED
└── configs/                # REFACTORED — per-component YAML files
```

### Pattern 1: BizAAAClient Interface (replaces direct socket calls)

**What:** The Biz Pod's EAP engine calls `BizAAAClient.ForwardEAP()` instead of calling `radius.Client.SendEAP()` directly. This decouples the EAP engine from raw socket management.

**When to use:** Every time the Biz Pod needs to send an EAP message to AAA-S.

**Current code that changes:**

```go
// internal/eap/engine.go — current, has direct AAAClient
func (e *Engine) forwardToAAA(ctx context.Context, session *Session, eapPayload []byte) ([]byte, EapResult, error) {
    response, err := e.aaaClient.SendEAP(ctx, session.AuthCtxID, eapPayload)
    // ...
}
```

After refactor, `e.aaaClient` is an `httpAAAClient` struct that wraps an HTTP client calling the AAA Gateway. The `eap.AAAClient` interface (`SendEAP(ctx, authCtxID, eapPayload)`) is satisfied by `httpAAAClient` — no interface change needed in `engine.go`.

### Pattern 2: Redis Pub/Sub for Multi-Pod Response Routing

**What:** The AAA Gateway publishes AAA responses to a shared Redis channel. All Biz Pods subscribe to the channel; each discards messages not matching its in-flight sessions.

**Why:** Prevents needing per-pod routing or sticky sessions. Works correctly even if the originating Biz Pod dies (other Biz Pod resumes from Redis/PostgreSQL).

**Race condition handled:** If the originating Biz Pod dies after Redis write but before pub/sub receipt, any live Biz Pod reads `nssaa:session:{sessionId}`, resumes EAP state from PostgreSQL, and processes the response.

### Pattern 3: Per-Component Config with `Component` Enum

**What:** The root config struct has a `Component` field. Only the relevant section is loaded based on which binary is running.

**Why:** Prevents AAA Gateway from needing Biz Pod config and vice versa. Each binary gets a minimal, correct config.

### Anti-Patterns to Avoid

- **Direct RADIUS/Diameter socket calls from Biz Pod:** `internal/radius/client.go` and `internal/diameter/client.go` must NOT be imported by `cmd/biz/` or `internal/biz/`. They belong only to `cmd/aaa-gateway/`. This is the core separation of concerns.

- **Per-pod channels for response routing:** Do not create channels per Biz Pod. Use Redis pub/sub with broadcast-and-discard pattern. Channels per pod require coordination to avoid routing to the wrong pod.

- **Sticky sessions on HTTP Gateway:** The architecture is explicitly stateless. If a request lands on a different Biz Pod than the one that created the session, that Biz Pod retrieves session state from Redis/PostgreSQL using `authCtxId`. No affinity needed.

- **Global config loaded by all components:** Each binary must load only its own config section. Loading the full monolithic config causes startup failures when Biz Pod tries to access AAA Gateway-specific fields that aren't configured.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| AAA protocol encoding/decoding | Custom RADIUS/Diameter framing | `internal/radius/`, `internal/diameter/` | Already implemented with RFC compliance (RFC 2865, RFC 3579, RFC 6733) |
| HTTP client for internal communication | Raw `net/http` with manual retry logic | Standard `http.Client` with context timeouts | Context propagation, connection pooling already handled |
| Keepalived health check | Custom VIP monitoring | `osixopen/keepalived:2.3.1` | Production-proven, handles VRRP protocol correctly |
| Diameter state machine | Custom CER/CEA handshake | `go-diameter/v4` state machine | Already in go.mod with RFC 6733 compliance |

---

## Runtime State Inventory

> This is a refactor phase involving package renames (`internal/aaa/` → `internal/biz/`) and binary creation/deletion. The inventory below audits what runtime state could be affected.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | PostgreSQL `auth_sessions` table stores `auth_ctx_id`, `gpsi`, `snssai_sst/sd` — table name and column names unchanged | None — schema not touched |
| **Stored data** | Redis keys: `nssaa:session:{sessionId}`, `nssaa:pods`, `nssaa:aaa-response` — key prefixes defined in `internal/proto/` | New keys added; existing keys unchanged |
| **Live service config** | Kubernetes Deployments/Services for NSSAAF not yet in git (`deployments/` has no YAML files) | New manifests to be created in `deployments/helm/` and `deployments/kustomize/` |
| **OS-registered state** | None — keepalived configs are not registered until deployment | ConfigMap manifests to be created |
| **Secrets/env vars** | `configs/*.yaml` have `${VAR}` placeholders for DB password, Redis password | No key renames; existing placeholder names remain valid |
| **Build artifacts** | `cmd/nssAAF/main.go` binary compiled to `./nssAAF` (9.7 MB per CODEBASE_STRUCTURE) | Binary deleted; new `cmd/biz/`, `cmd/aaa-gateway/`, `cmd/http-gateway/` binaries created |
| **Git history** | `cmd/nssAAF/main.go` history preserved in git | Git history of the file remains after file deletion from disk |

**Key runtime state concern:** After refactoring `internal/aaa/` → `internal/biz/`, all `import "github.com/operator/nssAAF/internal/aaa"` statements in test files (`internal/aaa/aaa_test.go`) must be updated to `import "github.com/operator/nssAAF/internal/biz"`. This is a code edit, not a data migration.

**`cmd/nssAAF/main.go` deletion impact:** No runtime state depends on this binary name. The Kubernetes manifests (to be created) will reference the new image names. If existing deployments reference `nssAAF` image, those must be updated as part of the deployment phase (Phase 5), not as part of the code refactor.

---

## Common Pitfalls

### Pitfall 1: Deleting `internal/radius/` and `internal/diameter/` imports from Biz Pod prematurely

**What goes wrong:** `internal/eap/engine.go` currently imports `internal/radius` or `internal/diameter` via the `AAAClient` interface. If the interface signature changes or if `radius.Client` is used directly in handler code, compilation fails.

**Why it happens:** The `AAAClient` interface in `engine_client.go` is `SendEAP(ctx, authCtxID, eapPayload) ([]byte, error)`. An `httpAAAClient` struct in `cmd/biz/` can satisfy this interface. However, if `engine.go` or `engine_client.go` directly reference concrete types from `internal/radius/` or `internal/diameter/`, those imports must be removed carefully.

**How to avoid:** The `internal/eap/` package must NOT import `internal/radius/` or `internal/diameter/`. Currently `engine.go` imports neither — it only references the `AAAClient` interface. The Biz Pod's `main.go` wires a concrete `httpAAAClient` that satisfies `AAAClient`. This pattern must be preserved.

**Warning signs:** `go build ./cmd/biz/...` fails with `undefined: radius` or `undefined: diameter`.

### Pitfall 2: Race condition in Redis session correlation

**What goes wrong:** The AAA Gateway writes `nssaa:session:{sessionId}` and then forwards to AAA-S. If the response arrives before the write completes (unlikely with Redis in-memory but possible under high load), the AAA Gateway cannot look up the session.

**Why it happens:** The write-then-forward sequence is not atomic.

**How to avoid:** Use a Redis pipeline: write the session correlation entry and the request forwarding in the same pipeline. If the pipeline fails, do not forward to AAA-S. Alternatively, write first with a small buffer, then forward. The current PHASE design specifies write-before-forward, which is correct.

**Warning signs:** Logs show `nssaa:session:{sessionId} not found` for sessions that were just created.

### Pitfall 3: Session expiry during long EAP exchanges

**What goes wrong:** `nssaa:session:{sessionId}` TTL is 10 minutes (EAP session TTL). If an EAP-TLS handshake takes longer (e.g., certificate chain validation delays), the Redis key expires before the response arrives.

**Why it happens:** EAP-TLS can involve multiple round trips; the 10-minute TTL is conservative.

**How to avoid:** The AAA Gateway should extend the TTL on each forward operation. Alternatively, use a longer TTL (15 minutes) that covers the worst-case EAP exchange time. The Biz Pod should also refresh the Redis key on each EAP round-trip.

**Warning signs:** `nssaa:session:{sessionId} not found` during active EAP exchanges.

### Pitfall 4: Incorrect import graph after refactor

**What goes wrong:** After renaming `internal/aaa/` → `internal/biz/`, imports become inconsistent. `internal/eap/` should not import `internal/radius/` or `internal/diameter/`. `internal/proto/` must have zero internal dependencies.

**Why it happens:** Manual import rewrites are error-prone. The import graph defines the architectural boundaries.

**How to avoid:** After each section of the refactor, run `go mod graph | grep -E "radius|diameter" | grep -v "cmd/aaa-gateway"` — this should return zero lines (radius/diameter should only be reachable from `cmd/aaa-gateway/`).

**Warning signs:** `go build ./cmd/biz/...` succeeds but `go mod graph` shows radius/diameter in Biz Pod dependency tree.

### Pitfall 5: keepalived VIP not working in Docker Compose dev environment

**What goes wrong:** The dev compose file sets `network_mode: host` for the AAA Gateway to enable keepalived to manage a VIP. This conflicts with the bridge networking expected by other services.

**Why it happens:** keepalived requires raw socket access to manage the VIP, which is incompatible with Docker's default bridge networking.

**How to avoid:** Document in the compose file that `network_mode: host` is for dev only and should not be used in production. Use a physical IP or loopback address for local testing instead of a VIP.

### Pitfall 6: NRF registration with HTTP Gateway FQDN

**What goes wrong:** If the HTTP Gateway FQDN is not resolvable from the Biz Pod (e.g., configured as an external DNS name but only internal DNS is available), NRF registration fails.

**Why it happens:** The Biz Pod uses the HTTP Gateway FQDN as the `nfServices.contactId` in the NFProfile, but the Biz Pod may not be able to resolve external DNS names.

**How to avoid:** Use Kubernetes internal DNS (`svc-nssaa-http-gw.namespace.svc.cluster.local`) for the FQDN during registration. Operators must ensure the registered FQDN matches what AMF/AUSF can resolve.

### Pitfall 7: Version skew during rolling upgrades

**What goes wrong:** If HTTP Gateway is upgraded before Biz Pod, older HTTP Gateway sends requests to newer Biz Pod that uses a proto schema the HTTP Gateway doesn't understand.

**Why it happens:** Proto schema versioning is per-message, not per-deployment. If a proto field is removed (not added), backward compatibility breaks.

**How to avoid:** Proto schema must be backward compatible within a major version. Only add optional fields; never remove required fields. Use `X-NSSAAF-Version` header to detect skew and log warnings rather than fail.

---

## Code Examples

### 1. Biz Pod HTTP client that satisfies `eap.AAAClient`

```go
// cmd/biz/http_aaa_client.go — NEW
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "time"

    "github.com/operator/nssAAF/internal/eap"
    "github.com/operator/nssAAF/internal/proto"
)

// httpAAAClient satisfies the eap.AAAClient interface.
// It forwards EAP messages to the AAA Gateway via HTTP.
type httpAAAClient struct {
    baseURL  string
    httpClient *http.Client
    version  string
}

func newHTTPAAAClient(baseURL, version string) *httpAAAClient {
    return &httpAAAClient{
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
        version: version,
    }
}

// SendEAP satisfies eap.AAAClient.
// It calls AAA Gateway and waits for the response via Redis pub/sub.
func (c *httpAAAClient) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
    // 1. Build forward request
    req := &proto.AaaForwardRequest{
        SessionID:   fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
        AuthCtxID:   authCtxID,
        Payload:     eapPayload,
        Direction:   proto.DirectionClientInitiated,
        // S-NSSAI and transport type filled by router
    }

    body, err := json.Marshal(req)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal forward request: %w", err)
    }

    // 2. POST to AAA Gateway
    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/aaa/forward", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("X-NSSAAF-Version", c.version)

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

var _ eap.AAAClient = (*httpAAAClient)(nil)
```

### 2. AAA Gateway Redis subscription for response routing

```go
// internal/aaa/gateway/redis.go — NEW
package gateway

import (
    "context"
    "encoding/json"
    "log/slog"

    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v9"
)

const (
    sessionCorrKeyPrefix = "nssaa:session:"
    aaaResponseChannel   = "nssaa:aaa-response"
)

// SessionCorrEntry stored at nssaa:session:{sessionId}
type SessionCorrEntry struct {
    AuthCtxID string `json:"authCtxId"`
    PodID     string `json:"podId"`
    Sst       uint8  `json:"sst"`
    Sd        string `json:"sd"`
    CreatedAt int64  `json:"createdAt"`
}

// subscribeResponses subscribes to AAA response channel and dispatches
// to the correct pending request handler.
func (g *Gateway) subscribeResponses(ctx context.Context) {
    ch := g.redis.PSubscribe(ctx, aaaResponseChannel)
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

// dispatchResponse looks up the pending request for SessionID and delivers the payload.
func (g *Gateway) dispatchResponse(event *proto.AaaResponseEvent) {
    g.mu.RLock()
    ch, ok := g.pending[event.SessionID]
    g.mu.RUnlock()

    if !ok {
        g.logger.Warn("no pending request for session",
            "session_id", event.SessionID,
            "auth_ctx_id", event.AuthCtxID)
        return
    }

    select {
    case ch <- event.Payload:
    default:
        g.logger.Warn("response channel full, discarding",
            "session_id", event.SessionID)
    }
}
```

### 3. AAA Gateway RADIUS handler (message type distinction)

```go
// internal/aaa/gateway/radius_handler.go — NEW
package gateway

import (
    "context"
    "encoding/binary"
    "net"
)

// RADIUS message type codes (RFC 2865)
const (
    radiusAccessRequest    = 1
    radiusAccessAccept     = 2
    radiusAccessReject     = 3
    radiusAccessChallenge  = 11
    radiusCoARequest       = 43  // RFC 5176
    radiusDisconnectRequest = 40 // RFC 5176
)

// HandlePacket processes an incoming RADIUS packet from AAA-S.
func (h *RadiusHandler) HandlePacket(conn *net.UDPConn, addr *net.UDPAddr, raw []byte) {
    if len(raw) < 4 {
        h.logger.Warn("radius_packet_too_short", "len", len(raw))
        return
    }

    msgType := raw[0]
    sessionID := extractSessionID(raw) // from RADIUS State attribute or request authenticator

    // Client-initiated: AAA-S responding to our Access-Request
    if msgType == radiusAccessAccept || msgType == radiusAccessReject || msgType == radiusAccessChallenge {
        // Write session correlation entry and publish to Redis
        h.publishResponse(sessionID, raw)
        return
    }

    // Server-initiated: RAR or CoA from AAA-S
    if msgType == radiusCoARequest || msgType == radiusDisconnectRequest {
        h.handleServerInitiated(raw, sessionID, "RADIUS")
        return
    }
}

// handleServerInitiated routes server-initiated messages to Biz Pod.
func (h *RadiusHandler) handleServerInitiated(raw []byte, sessionID, transportType string) {
    // Look up session from Redis
    entry, err := h.getSessionCorr(sessionID)
    if err != nil {
        // Session not found — return RAR-Nak to AAA-S
        h.logger.Warn("session_not_found_for_server_initiated",
            "session_id", sessionID,
            "transport", transportType)
        h.sendRARnak(raw)
        return
    }

    // Forward to Biz Pod via HTTP POST /aaa/server-initiated
    req := &proto.AaaServerInitiatedRequest{
        SessionID:     sessionID,
        AuthCtxID:     entry.AuthCtxID,
        TransportType: proto.TransportRADIUS,
        MessageType:   proto.MessageTypeRAR,
        Payload:      raw,
    }
    h.forwardToBizServerInitiated(req)
}
```

### 4. Three-component config struct

```go
// internal/config/component.go — NEW
package config

// Component identifies which binary is running.
type Component string

const (
    ComponentHTTPGateway Component = "http-gateway"
    ComponentBiz         Component = "biz"
    ComponentAAAGateway  Component = "aaa-gateway"
)

// Config is the root configuration. Only relevant sections are loaded per component.
type Config struct {
    Component Component `yaml:"component"` // required field

    // Common (all components)
    Server   ServerConfig    `yaml:"server"`
    Database DatabaseConfig  `yaml:"database"`
    Redis   RedisConfig     `yaml:"redis"`
    Logging LoggingConfig   `yaml:"logging"`
    Metrics MetricsConfig   `yaml:"metrics"`

    // Biz Pod only
    EAP  EAPConfig  `yaml:"eap"`
    AAA  AAAConfig `yaml:"aaa"`
    NRF  NRFConfig `yaml:"nrf"`
    UDM  UDMConfig `yaml:"udm"`

    // Internal communication
    Biz   BizConfig   `yaml:"biz"`
    AAAgw AAAgwConfig `yaml:"aaaGateway"`

    // Version (injected at build time)
    Version string `yaml:"-" json:"-"`
}

// BizConfig: how Biz Pod communicates with AAA Gateway
type BizConfig struct {
    AAAGatewayURL string `yaml:"aaaGatewayUrl"` // http://svc-nssaa-aaa:9090
}

// AAAgwConfig: AAA Gateway specific settings
type AAAgwConfig struct {
    ListenRADIUS   string `yaml:"listenRadius"`    // ":1812"
    ListenDIAMETER string `yaml:"listenDiameter"`  // ":3868"
    BizServiceURL  string `yaml:"bizServiceUrl"`   // http://svc-nssaa-biz:8080
    KeepalivedCheck string `yaml:"keepalivedCheck"` // path to health check script
}

// Load reads config and validates component-specific fields.
// Returns error if required fields for the current component are missing.
func Load(path string) (*Config, error) {
    // ... existing Load logic ...
    // After unmarshaling, validate based on cfg.Component
    switch cfg.Component {
    case ComponentBiz:
        if cfg.Biz.AAAGatewayURL == "" {
            return nil, fmt.Errorf("biz.aaaGatewayUrl is required")
        }
    case ComponentAAAGateway:
        if cfg.AAAgw.BizServiceURL == "" {
            return nil, fmt.Errorf("aaaGateway.bizServiceUrl is required")
        }
    case ComponentHTTPGateway:
        if cfg.Biz.BizServiceURL == "" {
            return nil, fmt.Errorf("biz.bizServiceUrl is required")
        }
    default:
        return nil, fmt.Errorf("config.component must be one of: http-gateway, biz, aaa-gateway")
    }
    return cfg, nil
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single-process binary | 3-component Kubernetes-native | This phase | Diameter/RADIUS connections now managed by single AAA Gateway pod |
| Direct socket calls from Biz Pod | HTTP → AAA Gateway → raw socket | This phase | Biz Pods scale horizontally without connection conflicts |
| Redis only for caching | Redis for pub/sub + session correlation | This phase | Multi-pod response routing without sticky sessions |
| Single AAA server config | Per-S-NSSAI routing with 3-level fallback | Already implemented in `internal/aaa/config.go` | Reused in `internal/biz/router.go` |

---

## Assumptions Log

> All claims in this research were verified against source code or the PHASE_Refactor_3Component.md design document. No `[ASSUMED]` tags required.

---

## Open Questions

1. **TLS between AAA Gateway and Biz Pod**
   - What we know: PHASE §3.3 says HTTP Gateway uses TLS 1.3. Biz Pod → AAA Gateway HTTP communication is internal to the cluster.
   - What's unclear: Does Biz Pod → AAA Gateway use plain HTTP or mTLS? The design does not specify. In Kubernetes, this may be handled by network policy rather than mutual TLS.
   - Recommendation: Use plain HTTP for Biz Pod ↔ AAA Gateway within the cluster. Add network policy (Kubernetes NetworkPolicy) to restrict traffic. If mTLS is required, defer to the Envoy migration phase (§7).

2. **Diameter SCTP support**
   - What we know: `internal/diameter/client.go` supports TCP and SCTP transport. `internal/aaa/gateway/diameter_handler.go` is specified as TCP listener `:3868`.
   - What's unclear: Is SCTP required for the AAA Gateway? TS 29.561 §17 supports both.
   - Recommendation: Implement TCP first (simpler). Add SCTP support if required by the operator's AAA-S deployment.

3. **Redis cluster vs. single-node for pub/sub**
   - What we know: `internal/cache/redis/` supports both. `internal/proto/biz_callback.go` uses pub/sub channel `nssaa:aaa-response`.
   - What's unclear: Does pub/sub work correctly with Redis Cluster (sharded)? Redis pub/sub does not work across cluster shards.
   - Recommendation: Document that pub/sub requires a single Redis node or a Redis Sentinel setup. Redis Cluster pub/sub is not supported. If clustering is required, use Redis Streams instead.

4. **AAA Gateway health check endpoint for Biz Pod retry logic**
   - What we know: The Biz Pod calls `POST /aaa/forward` on the AAA Gateway. If the AAA Gateway is unavailable, the Biz Pod needs to handle this.
   - What's unclear: How does the Biz Pod detect that the AAA Gateway is down? The design specifies `503 Service Unavailable` from HTTP Gateway when all Biz Pods are down, but not the Biz Pod → AAA Gateway failure case.
   - Recommendation: Implement a `/health` endpoint on the AAA Gateway. The Biz Pod HTTP client should implement circuit breaker logic (the `internal/resilience/` package already has a circuit breaker pattern).

---

## Environment Availability

> The refactor phase is purely code/configuration changes with no external tool dependencies. Step 2.6: SKIPPED (no external dependencies identified).

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `stretchr/testify` v1.11.1 |
| Config file | None — see Wave 0 |
| Quick run command | `go test ./... -count=1` |
| Full suite command | `go test ./... -race -count=1 -coverprofile=coverage.out` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|---------------|
| REQ-R2 | `internal/proto/` structs serialize/deserialize correctly | unit | `go test ./internal/proto/...` | ❌ Wave 0 — create `internal/proto/proto_test.go` |
| REQ-R2 | `BizAAAClient` interface is satisfied by `httpAAAClient` | unit | `go test ./cmd/biz/...` | ❌ Wave 0 — create `cmd/biz/http_aaa_client_test.go` |
| REQ-R2 | `BizServiceClient` interface is satisfied by `httpBizClient` | unit | `go test ./cmd/http-gateway/...` | ❌ Wave 0 — create `cmd/http-gateway/http_client_test.go` |
| REQ-R1 | `internal/biz/router.go` builds `AaaForwardRequest` correctly | unit | `go test ./internal/biz/...` | ❌ Wave 0 — create `internal/biz/router_test.go` |
| REQ-R5 | Redis session correlation: write → pub/sub → dispatch | unit | `go test ./internal/aaa/gateway/...` | ❌ Wave 0 — create `internal/aaa/gateway/redis_test.go` |
| REQ-R7 | RAR/ASR message type detection | unit | `go test ./internal/aaa/gateway/...` | ❌ Wave 0 — create `internal/aaa/gateway/radius_handler_test.go` |
| REQ-R4 | Component config validates required fields | unit | `go test ./internal/config/...` | ❌ Wave 0 — create `internal/config/component_test.go` |
| REQ-R3 | All three binaries compile | build | `go build ./cmd/biz/... && go build ./cmd/aaa-gateway/... && go build ./cmd/http-gateway/...` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./... -count=1 -timeout 60s`
- **Per wave merge:** `go test ./... -race -count=1 -coverprofile=coverage.out`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `internal/proto/proto_test.go` — covers REQ-R2 (struct serialization)
- [ ] `internal/proto/biz_callback_test.go` — covers REQ-R5 (session correlation)
- [ ] `cmd/biz/http_aaa_client_test.go` — covers REQ-R2 (BizAAAClient satisfaction)
- [ ] `cmd/biz/main_test.go` — covers REQ-R3 (binary wiring)
- [ ] `cmd/aaa-gateway/main_test.go` — covers REQ-R3 (binary wiring)
- [ ] `cmd/http-gateway/main_test.go` — covers REQ-R3 (binary wiring)
- [ ] `internal/biz/router_test.go` — covers REQ-R1 (BuildForwardRequest)
- [ ] `internal/aaa/gateway/redis_test.go` — covers REQ-R5 (pub/sub routing)
- [ ] `internal/aaa/gateway/radius_handler_test.go` — covers REQ-R7 (RAR/ASR detection)
- [ ] `internal/config/component_test.go` — covers REQ-R4 (config validation)
- [ ] `internal/proto/` directory and files — initial proto package created

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | no | N/A — AAA-S handles authentication |
| V3 Session Management | yes | Redis TTL on `nssaa:session:{sessionId}` keys; PostgreSQL for session state |
| V4 Access Control | yes | Per-component config — AAA Gateway has no access to Biz Pod config |
| V5 Input Validation | yes | GPSI, SUPI, Snssai validation already in `internal/types/`; new proto types need validation |
| V6 Cryptography | yes | TLS 1.3 at HTTP Gateway boundary; raw RADIUS/Diameter unchanged |
| V7 Error Handling | yes | `X-NSSAAF-Version` header for skew detection; ProblemDetails (RFC 7807) for errors |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| GPSI enumeration via repeated auth attempts | Information Disclosure | Per-GPSI rate limiting in `internal/cache/redis/ratelimit.go` |
| Malformed EAP payload forwarded to AAA-S | Tampering | EAP parsing in `internal/eap/` before forwarding |
| AAA Gateway receives RAR for unknown session | Spoofing | Return RAR-Nak / ASA with error cause; log warning |
| Stale session state in Redis | Tampering | TTL on all session correlation keys; periodic cleanup |
| HTTP Gateway forwards malicious request to Biz Pod | Injection | Input validation in Biz Pod handlers (already implemented) |

---

## Sources

### Primary (HIGH confidence)

- `/home/tulm/naf3/cmd/nssAAF/main.go` — current monolithic entry point
- `/home/tulm/naf3/internal/aaa/router.go` — routing logic to be refactored
- `/home/tulm/naf3/internal/aaa/aaa.go` — Router type, ProxyMode, Protocol enums
- `/home/tulm/naf3/internal/aaa/config.go` — SnssaiConfig, 3-level fallback
- `/home/tulm/naf3/internal/aaa/metrics.go` — AAA metrics collector
- `/home/tulm/naf3/internal/eap/engine.go` — EAP engine with AAAClient interface
- `/home/tulm/naf3/internal/eap/engine_client.go` — AAAClient interface definition
- `/home/tulm/naf3/internal/radius/client.go` — RADIUS client (source of socket calls)
- `/home/tulm/naf3/internal/diameter/client.go` — Diameter client (source of socket calls)
- `/home/tulm/naf3/internal/config/config.go` — current config structure
- `/home/tulm/naf3/docs/roadmap/PHASE_Refactor_3Component.md` — phase specification
- `/home/tulm/naf3/docs/design/01_service_model.md` §5.4 — 3-component architecture design
- `/home/tulm/naf3/docs/quickref.md` — NSSAAF quick reference
- `/home/tulm/naf3/.planning/CODEBASE_STRUCTURE.md` — codebase analysis

### Secondary (MEDIUM confidence)

- RFC 6733 — Diameter Base Protocol (single connection requirement)
- RFC 2865 — RADIUS (source-IP shared secret consideration)
- RFC 5176 — RADIUS Change-of-Authorization (CoA) and Disconnect Messages
- TS 23.502 §4.2.9 — NSSAA Procedure flows
- TS 29.561 §16-17 — AAA protocol mapping

---

## Metadata

**Confidence breakdown:**

| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | All libraries already in go.mod; no new dependencies |
| Architecture | HIGH | Grounded in actual source code analysis; PHASE design doc is detailed |
| Pitfalls | HIGH | Based on known race conditions and import graph issues from actual code inspection |
| Gap Analysis | HIGH | `internal/proto/` gap confirmed by Glob search; all other gaps identified from source analysis |

**Research date:** 2026-04-21
**Valid until:** 2026-05-21 (phase design is stable; implementation may reveal nuances)
