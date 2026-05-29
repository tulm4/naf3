# NSSAAF Internal-Comm HA Enhancement: Biz ↔ AAA Gateway

## 1. Overview

Improve HA for internal communication between Biz Pods and AAA Gateway in both directions.
When any pod dies, remaining pods continue processing without disruption.

**Spec:** Phase 7.1
**Interface:** N58 (Biz ↔ AAA GW), Server-initiated (AAA GW ↔ Biz)
**Service:** `internal/aaa/gateway/`, `cmd/biz/`, `internal/httpclient/`

---

## 2. Problem Statement

### 2.1 Current Gaps

| Gap | Path | Severity | Impact |
|-----|------|----------|--------|
| `BizServiceURL` is static single URL | Server-init | **HIGH** | RAR/ASR/CoA fails if target pod dies |
| `PodID` in Redis never used for routing | Server-init | **HIGH** | Designed for routing, implemented as comment |
| Server-initiated: no retry on HTTP failure | Server-init | **HIGH** | Lost messages, no DLQ |
| `handleServerInitiated` is stub — returns dummy bytes | Server-init | **HIGH** | Random pod cannot process RAR/ASR/CoA correctly |
| AAA GW starts listeners before VIP ownership confirmed | Server-init | **HIGH** | Race: both replicas listen, VIP owner unclear during failover |
| RADIUS `MaxRetries: 3` hardcoded | Client-init | **HIGH** | Cannot tune for network conditions |
| VIP failover → circuit breaker blip | Client-init | MEDIUM | 15-30s extra downtime after keepalived recovers |

### 2.2 Root Cause

Server-initiated path routes to a static `BizServiceURL`. When the target Biz pod dies:
- HTTP POST fails silently
- RAR/ASR/CoA message is dropped
- No retry, no fallback

---

## 3. Solution: Redis-Based Target Selection

### 3.1 Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│ Biz Pod Startup                                                     │
│   HSET nssaa:biz:pods {podID} = {bizUrl, timestamp}               │
│   Set key TTL = 60s                                                 │
│   Background: refresh every 30s                                     │
└───────────────────────────┬───────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Client-Initiated (EAP Round)                                       │
│   Biz Pod A → AAA GW (POST /aaa/forward)                           │
│   AAA GW writes: nssaa:session:{sessionID} = {                    │
│       authCtxId, podId: "biz-pod-A", sst, sd, timestamp           │
│   }                                                                 │
│   AAA GW → AAA-S                                                   │
└───────────────────────────┬─────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Server-Initiated (RAR/ASR/CoA)                                     │
│   AAA-S → AAA GW                                                   │
│   AAA GW reads: nssaa:session:{sessionID} → entry.PodID           │
│   If PodID exists: HGET nssaa:biz:pods {podId} → URL              │
│   If no PodID or pod dead: HGETALL → pick random live pod          │
│   POST targetURL/aaa/server-initiated (retry × 3 + DLQ)            │
└─────────────────────────────────────────────────────────────────────┘
```

### 3.2 Redis Keys

| Key | Type | Content | TTL |
|-----|------|---------|-----|
| `nssaa:biz:pods` | HASH | `podID → BizPodEntry{URL, timestamp}` | 60s per pod |
| `nssaa:session:{sessionID}` | STRING | `SessionCorrEntry{authCtxId, podId, sst, sd, timestamp}` | 10min |

### 3.3 Target Selection Algorithm

```
selectTargetBizURL(entry.PodID):
    if entry.PodID != "":
        url = getBizPodURL(entry.PodID)  // Direct lookup
        if url != "":
            return url                   // Original pod still live

    // Fallback: pick random live pod
    livePods = filter(HGETALL(nssaa:biz:pods), LastSeen > 60s ago)
    if livePods.length > 0:
        return random(livePods).BizURL

    return BizServiceURL  // Final fallback to static URL
```

---

## 4. Retry + DLQ for Server-Initiated

### 4.1 Retry Strategy

```
postToBizWithRetry(url, req):
    for attempt in 0..2:
        if attempt > 0:
            sleep(attempt seconds)  // 0s, 1s, 2s

        err = doHTTPPost(url, req)
        if err == nil:
            return SUCCESS

        if isConnectionError(err):
            url = selectTargetBizURL("")  // Pick different pod

    return DLQ
```

### 4.2 DLQ Behavior

Failed messages go to Redis list `nssaa:dlq:server-initiated`.

**DLQ consumer:**
- Processes every 30 seconds
- Retries up to 10 attempts (~5 minutes)
- After 10 attempts: log + alert, discard message

```
DLQ consumer:
    for msg in BRPOP nssaa:dlq:server-initiated, 30s timeout:
        if msg.attemptCount < 10:
            postToBizWithRetry(msg.url, msg.req)
            if failed:
                RPUSH back with attemptCount++
        else:
            log ERROR "server_initiated_dlq_exhausted"
            alert("Server-initiated message failed after 10 retries")
```

### 4.3 Alert Criteria

- DLQ depth > 10: WARNING
- DLQ message aged > 5 minutes: CRITICAL

---

## 5. RADIUS Configurable Retry

### 5.1 Current State

`radiusForwarder` hardcodes `MaxRetries: 3` and `Timeout: 10s` in `radius.Config`.

### 5.2 Solution

Pass `InternalCommConfig.Native.Retry` values to `radiusForwarder`.

```go
type RadiusForwarderConfig struct {
    ServerAddress   string
    ServerPort     int
    SharedSecret   string
    Timeout        time.Duration
    MaxRetries     int
    ResponseWindow time.Duration
}
```

**Config mapping:**

| `InternalCommConfig` field | `radiusForwarder` field |
|---------------------------|------------------------|
| `Native.Retry.MaxAttempts` | `MaxRetries` |
| `Native.Retry.BaseDelay` | (for future backoff) |
| `Native.Retry.MaxDelay` | (for future backoff) |
| `Native.Pool.DialTimeout` | `Timeout` |

---

## 7. Circuit Breaker Reset on Keepalived Failover

### 7.1 Current State

After keepalived VIP failover:
1. `AAA GW (active)` dies
2. Biz pods have circuit breaker in `OPEN` state
3. VIP moves to `AAA GW (standby)`
4. **Circuit breaker stays OPEN for 15s** (recovery timeout)
5. **Total outage = keepalived failover + CB reset**

### 7.2 Solution

`NativeAAAClient` polls the keepalived health endpoint. On state change, resets the circuit breaker immediately.

```go
func (c *NativeAAAClient) StartVIPHealthCheck(ctx context.Context, healthURL string) {
    prevState := "unknown"
    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(5 * time.Second):
        }

        state := c.checkVIPState(healthURL)  // "MASTER" | "BACKUP" | "unknown"
        if state != prevState && prevState != "unknown" {
            cb := c.cbRegistry.Get(c.aaaGatewayURL)
            cb.Reset()
            metrics.CircuitBreakerResetByVIPChange.Inc()
        }
        prevState = state
    }
}
```

**Result:** VIP failover → CB reset within 5s → no 15-30s blackout.

---

## 6. VIP-Aware Startup

### 6.1 Startup Race Condition

When a standby AAA GW pod (gw-2) starts up, there is a race between:

1. HTTP server starts on `:9090`
2. keepalived detects gw-2 is running
3. keepalived transitions gw-2 → MASTER
4. RADIUS/Diameter listeners start
5. Biz Pod health check hits `/health/vip` on gw-2 → 200 (gw-2 is now VIP owner)

If HTTP starts **before** keepalived has elected gw-2 as MASTER, the `/health/vip` endpoint correctly returns 503. However, the startup sequence itself has a window:

- Both gw-1 (current MASTER) and gw-2 (starting) are alive
- gw-2 HTTP is up but not yet MASTER
- RADIUS/Diameter listeners may already be bound to `:1812`/`3868`

**Root cause:** Both replicas bind to the same port at startup, regardless of VIP state. Only one wins (`SO_REUSEPORT`), but both are listening and the one that wins may not be the current VIP owner.

### 7.2 Solution: Gate Startup on VIP Ownership

All protocol listeners (HTTP, RADIUS, Diameter, Redis pub/sub, Diameter client-initiated connection) must **only start when the pod is the VIP owner**.

```
AAA GW Pod startup:
    1. Read keepalived state file
    2. If state == "MASTER":
         → Start HTTP server (:9090)
         → Start RADIUS listener (:1812)
         → Start Diameter listener (:3868)
         → Start Diameter client connection to AAA-S
         → Start DLQ consumer
    3. If state != "MASTER":
         → Block HTTP on :9090 ONLY (needed for health checks)
         → Start polling goroutine: every 5s check keepalived state
         → When state == "MASTER":
              → Start all protocol listeners
              → Start DLQ consumer
```

### 6.3 Implementation

**`cmd/aaa-gateway/main.go`** — Replace `gw.Start()` call:

```go
// VIP-aware startup: only start protocol listeners when this pod owns the VIP.
if !startVIPAware(ctx, gw, cfg.AAAgw.KeepalivedStatePath) {
    slog.Error("failed to acquire VIP ownership, exiting")
    os.Exit(1)
}
```

**`internal/aaa/gateway/gateway.go`** — Add `StartVIPAware()`:

```go
// StartVIPAware blocks until this pod becomes VIP owner, then starts all listeners.
// Returns true if started successfully, false on context cancellation.
func (g *Gateway) StartVIPAware(ctx context.Context, statePath string) bool {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        state, err := readKeepalivedState(statePath)
        if err != nil {
            g.logger.Warn("keepalived state unreadable", "error", err)
        } else if state == "MASTER" {
            g.logger.Info("VIP acquired, starting all listeners")
            if err := g.startListeners(ctx); err != nil {
                g.logger.Error("startListeners failed", "error", err)
                return false
            }
            return true
        } else {
            g.logger.Info("not VIP owner, waiting", "state", state)
        }

        select {
        case <-ctx.Done():
            return false
        case <-ticker.C:
        }
    }
}

// startListeners starts all protocol goroutines. Must only be called by VIP owner.
func (g *Gateway) startListeners(ctx context.Context) error {
    g.ctx, g.cancel = context.WithCancel(ctx)

    // RADIUS listener
    if g.cfg.ListenRADIUS != "" {
        g.wg.Add(1)
        go func() {
            defer g.wg.Done()
            g.radiusHandler.Listen(g.ctx, g.cfg.ListenRADIUS)
        }()
    }

    // Diameter listener
    if g.cfg.ListenDIAMETER != "" {
        g.wg.Add(1)
        go func() {
            defer g.wg.Done()
            if err := g.diameterHandler.Listen(g.ctx, g.cfg.ListenDIAMETER, g.cfg.DiameterProtocol); err != nil {
                g.logger.Error("diameter listener failed", "error", err)
            }
        }()
    }

    // Diameter client-initiated connection
    if g.diamForwarder != nil && g.cfg.DiameterServerAddress != "" {
        g.wg.Add(1)
        go func() {
            defer g.wg.Done()
            if err := g.diamForwarder.Connect(g.ctx); err != nil {
                g.logger.Error("diameter_forward_connect_failed", "error", err)
            }
        }()
    }

    // DLQ consumer
    g.wg.Add(1)
    go func() {
        defer g.wg.Done()
        g.runDLQConsumer(g.ctx)
    }()

    return nil
}
```

**Refactor `Start()` to be idempotent** (called by both `Start()` and `StartVIPAware()`):

```go
// Start starts all protocol listeners unconditionally.
// Deprecated: use StartVIPAware for HA deployments.
func (g *Gateway) Start(ctx context.Context) error {
    return g.startListeners(ctx)
}
```

### 6.4 Backward Compatibility

For **dev/test environments** (no keepalived), the `keepalivedStatePath` config field may be empty or `/dev/null`:

```go
// If no state file, start immediately (dev/test mode)
if statePath == "" || statePath == "/dev/null" {
    return g.startListeners(ctx)
}
```

### 6.5 Effect on VIP Failover Blip Fix

This fix is a **prerequisite** for Section 7 (CB reset). If we don't gate startup on VIP, the CB reset fix alone is insufficient because:
- Both replicas may have listeners active simultaneously during startup race
- The CB reset only helps after keepalived stabilizes

With VIP-aware startup, only the current MASTER has active listeners, so the CB reset fix alone is sufficient.

---

## 8. Real Server-Initiated Handler

### 8.1 Current State

`handleServerInitiated` in `cmd/biz/main.go` is a stub that returns hardcoded dummy bytes:

```go
func handleReAuth(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_re_auth", "auth_ctx_id", req.AuthCtxID)
    return []byte{2, 0, 0, 12}  // dummy — no session lookup, no AMF notification
}
```

**Problems:**
- No Redis session lookup via `AuthCtxID`
- No session state validation
- No AMF notification (Nnssf_NSSAA_Update, Nnssf_NSSAA_Revoke)
- Returns hardcoded dummy bytes → corrupts AAA protocol exchange

### 8.2 Required Behavior

A random pod receiving a server-initiated message **must** be able to process it correctly because:
1. The original pod (targeted by `SessionCorrEntry.PodID`) may have died
2. Retry selects a different pod via `pickRandomLiveBizURL()`

#### handleReAuth (RAR)

```
1. Receive AaaServerInitiatedRequest {AuthCtxID, Payload (RAR raw bytes)}
2. Load existing EAP session from Redis (nssaa:eap:session:{AuthCtxID})
3. Validate session state — must be in a state where re-auth is valid
4. Parse raw RAR bytes to extract EAP payload
5. Create new EAP session for re-auth round
6. Process EAP exchange via eapEngine.Process()
7. On success: notify AMF via Nnssf_NSSAA_Update
8. On failure: notify AMF via Nnssf_NSSAA_Revoke
9. Return EAP response bytes → AAA GW forwards to AAA-S as RAA
```

#### handleRevocation (ASR)

```
1. Receive AaaServerInitiatedRequest {AuthCtxID, Payload (ASR raw bytes)}
2. Load existing EAP session from Redis
3. Update session state → revoked / EAP_FAILURE
4. Notify AMF via Nnssf_NSSAA_Revoke
5. Return ASA (Abort-Session-Answer) to AAA GW → AAA GW forwards to AAA-S
```

#### handleCoA (Change-of-Authorization)

```
1. Receive AaaServerInitiatedRequest {AuthCtxID, Payload (CoA raw bytes)}
2. Load existing EAP session from Redis
3. Update session attributes (e.g., session timeout, QoS)
4. Persist updated session to Redis
5. Return CoA-Nak or CoA-Ack
```

### 8.3 Dependency on SessionCorrEntry

The AAA GW writes `SessionCorrEntry` before forwarding to AAA-S:

```go
// internal/aaa/gateway/gateway.go — ForwardEAP()
entry := &proto.SessionCorrEntry{
    AuthCtxID: req.AuthCtxID,
    PodID:     "", // Written by Biz Pod after ForwardEAP returns (see below)
    Sst:       req.Sst,
    Sd:        req.Sd,
    CreatedAt: time.Now().Unix(),
}
g.writeSessionCorr(ctx, req.SessionID, entry)
```

**Who writes `PodID`?** The AAA GW writes the initial entry with `PodID: ""`. After the HTTP response is returned to the Biz Pod, the Biz Pod immediately performs an async `HSET nssaa:biz:pods {ownPodID} = {url}` and then calls a new AAA GW endpoint `PUT /aaa/session/{sessionID}/pod-id` with its own PodID to backfill the field. This ensures the server-initiated path can route back to the correct pod.

**Alternative (simpler):** The AAA GW reads `os.Hostname()` at startup and uses that as its own PodID, writing it directly into `SessionCorrEntry`. This works because the AAA GW is per-pod and knows its own identity. Chosen for Wave 2 implementation.

When AAA GW reads on server-initiated path, `entry.AuthCtxID` tells the receiving pod which session to operate on — regardless of which pod originally handled the initial EAP exchange.

### 8.4 Biz Pod Dependency Injection

The `handleServerInitiated` handlers need access to:
- `eapEngine` — for session lookup and processing
- `aaaClient` — for forwarding to AAA-S
- `amfNotifier` — for AMF notifications

These must be injectable via the factory, not global singletons.

```go
// cmd/biz/main.go — pass via closure or struct
type serverInitiatedDeps struct {
    Engine     *eap.Engine
    AAAClient  proto.BizAAAClient
    AMFNotifier AMFNotifier
}

var serverDeps serverInitiatedDeps

func handleServerInitiated(w http.ResponseWriter, r *http.Request) {
    // ...
    respPayload, err := processReAuth(r.Context(), &req, serverDeps)
    // ...
}
```

---

## 9. Implementation Plan

### Wave 1: VIP-Aware Startup + Remove Dead Pub/Sub

0. **Remove Redis pub/sub** (dead code từ design cũ):
   - `subscribeResponses` goroutine, `dispatchResponse`, `publishResponse`, `publishResponseBytes` trong `gateway.go`
   - `pending` map + mutex trong `gateway.go`
   - `AaaResponseChannel`, `AaaResponseEvent` trong `proto/biz_callback.go`
   - Update tests `radius_handler_test.go` (xóa mock `publishResponse`)
   - Rationale: client-initiated response return trực tiếp qua `ForwardEAP` HTTP response; server-initiated dùng direct HTTP POST. Không có subscriber nào trong codebase.

1. **`internal/aaa/gateway/gateway.go`** — Add `StartVIPAware()`, `startListeners()`, refactor `Start()` to delegate
2. **`cmd/aaa-gateway/main.go`** — Call `gw.StartVIPAware()` instead of `gw.Start()`; HTTP server starts unconditionally (needed for health checks), protocol listeners gated on VIP ownership

### Wave 2: Server-Initiated Routing Infrastructure

3. **`internal/proto/biz_callback.go`** — Add `BizPodEntry`, `BizPodsHash = "nssaa:biz:pods"`, `BizPodTTL = 60s`
4. **`cmd/biz/factory.go`** — Add pod registration loop: `HSET nssaa:biz:pods {podID} = {bizUrl}`, refresh every 30s, cleanup on shutdown
5. **`internal/aaa/gateway/gateway.go`** — Add `selectTargetBizURL()`, `getBizPodURL()`, `pickRandomLiveBizURL()`
6. **`internal/aaa/gateway/gateway.go`** — Update `forwardToBiz()` to use target selection + retry loop
7. **DLQ consumer** — New goroutine in AAA GW, processes every 30s, max 10 retries, then alert

### Wave 3: Real Server-Initiated Handlers

8. **`cmd/biz/factory.go`** — Add `WithServerInitiatedDeps(deps)` option, store `serverDeps` in `BizPod`
9. **`cmd/biz/main.go`** — Replace stub `handleReAuth`, `handleRevocation`, `handleCoA` with real implementation
   - Load session from Redis via `AuthCtxID`
   - Validate session state
   - Forward to AAA-S via `aaaClient`
   - Notify AMF via `amfNotifier`
   - Return proper EAP response bytes

### Wave 4: RADIUS Config + VIP CB

10. **`internal/aaa/gateway/radius_forward.go`** — Accept `RadiusForwarderConfig`, remove hardcoded `MaxRetries: 3`
11. **`internal/aaa/gateway/gateway.go`** — Pass `RadiusForwarderCfg` from `Gateway.Config`
12. **`internal/httpclient/native_aaa.go`** — Add `StartVIPHealthCheck()`, reset CB on state change
13. **`internal/config/config.go`** — Add `KeepalivedHealthURL` to `BizConfig`

---

## 10. File Changes

```
cmd/
  aaa-gateway/
    main.go                    # MODIFY: call StartVIPAware instead of Start
  biz/
    factory.go                 # MODIFY: pod registration + WithServerInitiatedDeps
    main.go                    # MODIFY: real handleReAuth/Revocation/CoA
internal/aaa/gateway/
  gateway.go                   # MODIFY: StartVIPAware, startListeners, target selection, retry, DLQ
  radius_forward.go            # MODIFY: accept RadiusForwarderConfig
internal/httpclient/
  native_aaa.go               # MODIFY: add VIP health check
internal/proto/
  biz_callback.go             # MODIFY: add BizPodEntry, constants
internal/config/
  config.go                   # MODIFY: add KeepalivedHealthURL
```

---

## 11. Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| AC1 | Biz pod death → server-initiated routes to live pod | Kill Biz Pod A → RAR/ASR routes to Pod B |
| AC2 | DLQ retries failed server-initiated ≤ 10 times | Inject HTTP failure → check DLQ count |
| AC3 | DLQ exhausted → alert fired | DLQ msg aged > 5 min → alert metric incremented |
| AC4 | Random pod processes server-initiated correctly | Pod B receives RAR → loads session, notifies AMF, returns valid EAP bytes |
| AC5 | Standby AAA GW does not start listeners before VIP | `kubectl exec` standby pod → no process on :1812/:3868 until promotion |
| AC6 | VIP failover → CB resets within 10s | Simulate keepalived failover → measure CB reset time |
| AC7 | RADIUS retries use configured `MaxAttempts` | Set `maxAttempts: 5` → verify 5 retries |
| AC8 | All existing tests pass | `go test ./...` |

---

## 12. Test Scenarios

### TC1: Pod Death During Server-Initiated
```
1. Biz Pod A registers in Redis (nssaa:biz:pods)
2. Biz Pod A → AAA GW: EAP round → AAA GW writes nssaa:session:{sessionID} → PodID = "biz-pod-A"
3. AAA GW receives ASR for session X
4. AAA GW reads nssaa:session:X → PodID = "biz-pod-A" → getBizPodURL("biz-pod-A") → podA URL
5. Kill Biz Pod A
6. HTTP POST fails (connection refused)
7. Retry 1: isConnectionError → pickRandomLiveBizURL() → no other pods live → fallback static URL → fails
8. Retry 2: same → fails
9. Retry 3: same → fails
10. Push to DLQ
11. DLQ consumer picks up message, retries every 30s
12. Meanwhile: Biz Pod B starts up, registers in Redis
13. DLQ retry: pickRandomLiveBizURL() → "biz-pod-B" → SUCCESS
14. Pod B processes ASR: loads session via AuthCtxID, notifies AMF, returns ASA
```

### TC2: Pod Death, New Pod Takes Over
```
1. Biz Pod A + B both registered
2. Session corr: PodID = "biz-pod-A"
3. Kill Biz Pod A
4. HTTP POST fails
5. Retry 1: Pick random live pod → "biz-pod-B" → SUCCESS
```

### TC3: VIP-Aware Startup — Standby Does Not Start Listeners
```
1. gw-1 = MASTER (active), gw-2 = BACKUP (starting)
2. gw-2 starts, reads keepalived state = "BACKUP"
3. gw-2 HTTP server starts on :9090 (needed for health checks)
4. gw-2 does NOT start RADIUS/:1812, Diameter/:3868, or DLQ consumer
5. Biz Pod health check hits gw-2:9090/health/vip → 503 (not VIP owner)
6. Biz Pod routes to gw-1:9090
7. keepalived transitions gw-1 → BACKUP, gw-2 → MASTER
8. gw-2 reads keepalived state = "MASTER"
9. gw-2 starts RADIUS, Diameter, DLQ consumer
10. Biz Pod health check hits gw-2:9090/health/vip → 200
```

### TC4: VIP Failover
```
1. keepalived: GW-1 = MASTER, GW-2 = BACKUP
2. Biz Pod A CB: closed for "http://gw-1:9090"
3. keepalived: GW-2 becomes MASTER
4. NativeAAAClient detects VIP state change
5. CB resets for "http://gw-1:9090"
6. Next request goes to "http://gw-1:9090" → connection refused → CB opens briefly
7. Then routes to "http://gw-2:9090" → SUCCESS
```

### TC5: Random Pod Processes Server-Initiated Correctly
```
1. Biz Pod A + B both registered in nssaa:biz:pods
2. Session corr: nssaa:session:X → PodID = "biz-pod-A", AuthCtxID = "auth123"
3. Biz Pod A crashes
4. AAA GW receives RAR for session X
5. getBizPodURL("biz-pod-A") → error (pod dead)
6. pickRandomLiveBizURL() → "biz-pod-B"
7. HTTP POST to Pod B /aaa/server-initiated
8. Pod B handleReAuth():
   - Load EAP session "auth123" from Redis
   - Validate state (must be EAP_SUCCESS)
   - Parse raw RAR bytes
   - Create re-auth session
   - Process EAP exchange via eapEngine.Process()
   - Notify AMF via Nnssf_NSSAA_Update
   - Return EAP response bytes
9. AAA GW receives AaaServerInitiatedResponse from Pod B
10. AAA GW sends RAA to AAA-S with Pod B's response bytes
```

---

## 13. Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `nssAAF_server_initiated_retry_total` | Counter | `pod`, `attempt` |
| `nssAAF_server_initiated_dlq_total` | Counter | `reason` |
| `nssAAF_server_initiated_dlq_depth` | Gauge | - |
| `nssAAF_server_initiated_dlq_exhausted_total` | Counter | `message_type` |
| `nssAAF_server_initiated_handled_total` | Counter | `message_type`, `pod` |
| `nssAAF_cb_reset_by_vip_change_total` | Counter | - |
| `nssAAF_biz_pods_registered` | Gauge | - |
