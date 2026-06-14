# HTTP Gateway ↔ Biz Communication: Comprehensive Gap Fix

**Date:** 2026-06-14
**Status:** Draft
**Scope:** All 8 gaps across HTTP GW → Biz, AAA GW → Biz, and Biz → AAA GW communication paths
**Component:** `cmd/http-gateway/`, `cmd/biz/`, `cmd/aaa-gateway/`, `internal/httpclient/`, `internal/aaa/gateway/`, `internal/config/`

---

## 1. Overview

### 1.1 Purpose

This spec defines the work to close all gaps in the NSSAAF communication layer, covering both **inbound** (external NFs → NSSAAF) and **outbound** (Biz Pod → external NFs) communication paths.

### 1.2 Communication Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         NSSAAF Communication Architecture                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  INBOUND (đi qua HTTP Gateway)                                           │
│  ────────────────────────────────                                          │
│  AMF ──────▶ HTTP Gateway ──────▶ Biz Pod ────▶ AAA GW ────▶ AAA-S        │
│  AUSF ─────▶ HTTP Gateway ──────▶ Biz Pod                                 │
│                                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  OUTBOUND (từ Biz Pod ra ngoài — qua nfclient.Factory)                   │
│  ─────────────────────────────────────────────────────                     │
│  Biz Pod ──────▶ NRF      : Service discovery, NF registration           │
│  Biz Pod ──────▶ UDM      : Get subscription data (N59)                  │
│  Biz Pod ──────▶ AMF CB   : Re-Auth/Revocation notification               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 Gap Summary

#### Inbound Gaps (HTTP Gateway ↔ Biz)

| ID | Path | Gap | Severity | Status |
|----|------|-----|----------|--------|
| G1 | HTTP GW → Biz | Static single URL, no load balancing | HIGH | Not implemented |
| G2 | HTTP GW → Biz | X-Request-ID not propagated | MEDIUM | Not implemented |
| G3 | HTTP GW → Biz | Hardcoded 30s timeout | LOW | Not configurable |
| G4 | HTTP GW → Biz | No dynamic Biz Pod health monitoring | MEDIUM | Not implemented |
| G5 | AAA GW → Biz | Server-initiated handlers are stubs | HIGH | Wave 3 incomplete |
| G6 | AAA GW → Biz | RADIUS MaxRetries hardcoded | MEDIUM | Should use config |
| G7 | Biz → AAA GW | VIP health check not started | MEDIUM | Implemented but not wired |
| G8 | Config | KeepalivedHealthURL missing | MEDIUM | Not in config schema |

#### Outbound Gaps (Biz Pod → NRF/UDM/AMF)

| ID | Path | Gap | Severity | Status |
|----|------|-----|----------|--------|
| G9 | Biz → NRF/UDM/AMF | No retry in nfclient.Factory | HIGH | Not implemented |
| G10 | Biz → NRF/UDM/AMF | Hardcoded 30s timeout | MEDIUM | Not configurable |
| G11 | Biz → AMF | X-Request-ID not propagated in notifications | MEDIUM | Not implemented |
| G12 | Biz → AMF | DLQ retry behavior not configurable | LOW | Not implemented |

---

## 2. Problem Statement

### 2.1 HTTP Gateway → Biz Pod (G1-G4)

**Current State:**
- `cmd/http-gateway/main.go` creates a single `bizClient` via `httpclient.Factory`
- `nativeBizClient.ForwardRequest()` sends to a single static URL: `cfg.HTTPgw.BizServiceURL`
- No load balancing across Biz Pod replicas
- No X-Request-ID propagation from AMF/AUSF to Biz Pod
- Hardcoded 30s HTTP client timeout
- No periodic health check to detect dead Biz Pods

**Impact:**
- Single Biz Pod death causes request failures until manual intervention
- Correlation lost between AMF request and Biz Pod logs
- Cannot tune timeout for different network conditions

### 2.2 AAA GW → Biz Pod Server-Initiated (G5-G6)

**Current State:**
- `forwardToBiz()` in `internal/aaa/gateway/gateway.go` has Redis-based target selection
- Retry loop (3 attempts) with DLQ on failure
- BUT `handleReAuth`, `handleRevocation`, `handleCoA` in `cmd/biz/main.go` return dummy bytes:
  ```go
  func handleReAuth(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
      slog.Info("handle_re_auth", ...)
      return []byte{2, 0, 0, 12}  // dummy
  }
  ```
- `newRadiusForwarder()` hardcodes `MaxRetries: 3` instead of using config

**Impact:**
- RAR/ASR/CoA messages processed by a random pod return invalid responses
- AAA protocol exchange corrupted
- Cannot tune RADIUS retry behavior for different AAA-S responsiveness

### 2.3 Biz Pod → AAA GW (G7-G8)

**Current State:**
- `NativeAAAClient` has `StartVIPHealthCheck()` implemented in `internal/httpclient/native_aaa.go`
- NOT called from Biz Pod startup
- `KeepalivedHealthURL` not defined in config schema

**Impact:**
- After VIP failover, circuit breaker stays OPEN for 15-30s (recovery timeout)
- No way to configure the health check endpoint

---

## 3. Solution Architecture

### 3.1 High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              NSSAAF Architecture                              │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐     ┌─────────────────────┐     ┌─────────────────────┐  │
│  │    AMF      │────▶│   HTTP Gateway      │────▶│    Biz Pod (N)      │  │
│  │   (N58)     │     │   (N58/N60 API)     │     │   ┌───────────┐     │  │
│  └─────────────┘     │                     │     │   │ EAP Engine│     │  │
│                      │ - JWT Auth          │     │   │ AMF Client│     │  │
│  ┌─────────────┐     │ - X-Request-ID      │     │   │ AAA Client│     │  │
│  │   AUSF      │────▶│ - Load Balance      │     │   └───────────┘     │  │
│  │   (N60)     │     │ - Timeout Config    │     └──────────┬──────────┘  │
│  └─────────────┘     └─────────────────────┘                │             │
│                                                              │             │
│                                                              ▼             │
│  ┌─────────────┐     ┌─────────────────────┐     ┌─────────────────────┐  │
│  │  AAA-S      │◀───▶│    AAA Gateway      │◀───▶│    Redis           │  │
│  │ (RADIUS/    │     │   (VIP + HA)        │     │  - Session Corr    │  │
│  │  Diameter)   │     │                     │     │  - Biz Pod Registry│  │
│  └─────────────┘     │ - VIP-Aware Startup │     │  - DLQ             │  │
│                      │ - Retry + DLQ       │     └─────────────────────┘  │
│                      └─────────────────────┘                               │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 Redis Keys

| Key | Type | Content | TTL |
|-----|------|---------|-----|
| `nssaa:biz:pod:{podID}` | STRING | `BizPodEntry{URL, LastSeen}` | 60s |
| `nssaa:session:{sessionID}` | STRING | `SessionCorrEntry{AuthCtxID, PodID, Sst, Sd, CreatedAt}` | 10min |
| `nssaa:dlq:server-initiated` | LIST | JSON messages with retry metadata | No TTL |

**New constant needed in `proto/biz_callback.go`:**

```go
// BizPodsKeyPrefix is the Redis key prefix for Biz Pod entries.
// Used for SCAN pattern matching: "nssaa:biz:pod:*"
const BizPodsKeyPrefix = "nssaa:biz:pod:"
```

---

## 4. Detailed Solutions by Gap

### 4.1 G1: HTTP Gateway Load Balancing (HIGH)

#### Problem
Static single URL with no load balancing across Biz Pod replicas.

#### Solution
Apply the same Redis-based target selection used by AAA GW to HTTP Gateway.

**New Type: `BizServiceClientWithRegistry`**

```go
// internal/httpclient/biz_registry.go

// BizRegistry maintains live Biz Pod URLs in Redis and provides
// target selection for HTTP Gateway load balancing.
type BizRegistry struct {
    redisAddr string
    cbRegistry *resilience.Registry
    retryCfg resilience.RetryConfig
    source string
    staticURL string // Final fallback URL
}

// NewBizRegistry creates a new Biz Registry with Redis-based target selection.
func NewBizRegistry(redisAddr, staticURL string, cfg config.NativeCommConfig) *BizRegistry

// ForwardRequest implements BizServiceClient with Redis-based load balancing.
func (b *BizRegistry) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
    // 1. Get target URL from Redis or fallback
    targetURL, err := b.selectTargetURL(ctx)
    if err != nil {
        return nil, 503, err
    }
    
    // 2. Circuit breaker check
    cb := b.cbRegistry.Get(targetURL)
    if !cb.Allow() {
        // Try another pod
        targetURL, err = b.selectRandomLivePod(ctx)
        if err != nil {
            return nil, 503, fmt.Errorf("circuit breaker open, no live pods")
        }
    }
    
    // 3. Execute request with retry
    // ...
}
```

**Target Selection Algorithm:**

```
selectTargetURL():
    1. Try: Get all pods from Redis HGETALL "nssaa:biz:pod:*"
    2. Filter: Keep pods with LastSeen > 60s ago
    3. If live pods exist:
       a. Try original pod (from context or session)
       b. If not available, pick random live pod
    4. Return selected URL or static fallback
```

**Modified Factory:**

```go
// internal/httpclient/factory.go

// NewBizServiceClient creates a BizServiceClient with load balancing.
func (f *Factory) NewBizServiceClient(bizServiceURL, redisAddr string) proto.BizServiceClient {
    switch f.mode {
    case ModeIstio:
        return newIstioBizClient(bizServiceURL)
    default:
        return newBizRegistry(bizServiceURL, redisAddr, f.cfg.Native)
    }
}
```

**Modified HTTP Gateway main.go:**

```go
// cmd/http-gateway/main.go

bizClient := httpclient.NewFactory(cfg.InternalComm).NewBizServiceClient(
    cfg.HTTPgw.BizServiceURL,
    cfg.Redis.Addr,  // New: pass Redis address for registry
)
```

---

### 4.2 G2: X-Request-ID Propagation (MEDIUM)

#### Problem
AMF sends `X-Request-ID` header but HTTP Gateway doesn't forward it to Biz Pod.

#### Solution
Extract `X-Request-ID` from incoming request and propagate to Biz Pod.

**Modified `ForwardRequest` Signature:**

```go
// internal/proto/http_gateway.go

type BizServiceClient interface {
    ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error)
}
```

**Modified HTTP Gateway Handler:**

```go
// cmd/http-gateway/main.go

mux.Handle("/nnssaaf-nssaa/", auth.NewAuthMiddleware(authCfg)(
    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var body []byte
        if r.Body != nil {
            body, _ = io.ReadAll(r.Body)
        }
        
        // Extract X-Request-ID
        requestID := r.Header.Get("X-Request-ID")
        
        respBody, status, err := bizClient.ForwardRequest(
            r.Context(), r.URL.Path, r.Method, body, requestID,
        )
        // ...
    }),
))
```

**Modified Native Biz Client:**

```go
// internal/httpclient/native_biz.go

func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
    // ... existing circuit breaker + retry logic ...
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Request-ID", requestID)  // New: propagate correlation ID
    
    // ...
}
```

---

### 4.3 G3: Configurable Timeout (LOW)

#### Problem
30s timeout is hardcoded in HTTP client.

#### Solution
Add `timeout` field to `NativeCommConfig` and use it.

**Config Changes:**

```go
// internal/config/internal_comm.go

type NativeCommConfig struct {
    Retry RetryConfig `yaml:"retry"`
    CB    CircuitBreakerConfig `yaml:"circuitBreaker"`
    Pool  ConnectionPoolConfig `yaml:"connectionPool"`
    Timeout time.Duration `yaml:"timeout"`  // NEW
}
```

**Default:**

```go
// internal/config/config.go - applyDefaults()

if cfg.InternalComm.Native.Timeout == 0 {
    cfg.InternalComm.Native.Timeout = 30 * time.Second
}
```

**Usage:**

```go
// internal/httpclient/native_biz.go

func newNativeBizClient(baseURL string, cfg config.NativeCommConfig) *nativeBizClient {
    return &nativeBizClient{
        httpClient: &http.Client{
            // ...
            Timeout: cfg.Timeout,  // Use config
        },
        // ...
    }
}
```

---

### 4.4 G4: Dynamic Biz Pod Health Monitoring (MEDIUM)

#### Problem
No periodic health checks to detect dead Biz Pods and update load balancing.

#### Solution
Leverage existing `BizPodEntry` with TTL. Dead pods auto-expire from Redis.

**How it works:**

1. Biz Pod starts heartbeat goroutine: `HSET nssaa:biz:pod:{podID} = {URL, LastSeen}` with TTL
2. Heartbeat refreshes every 30s
3. On Biz Pod shutdown: key auto-expires or explicit DEL
4. HTTP Gateway reads `HGETALL nssaa:biz:pod:*` and filters by `LastSeen > 60s ago`
5. Dead pods excluded from target selection

**Implementation in `BizRegistry`:**

```go
// internal/httpclient/biz_registry.go

type livePod struct {
    podID string
    url   string
}

func (b *BizRegistry) getLivePods(ctx context.Context) ([]livePod, error) {
    // Use SCAN to iterate over nssaa:biz:pod:* keys without blocking Redis
    var cursor uint64
    const pattern = "nssaa:biz:pod:*"
    const maxAge = 60 * time.Second
    now := time.Now().Unix()
    var live []livePod

    for {
        keys, nextCursor, err := b.redis.Scan(ctx, cursor, pattern, 100).Result()
        if err != nil {
            return nil, err
        }

        for _, key := range keys {
            data, err := b.redis.Get(ctx, key).Bytes()
            if err != nil {
                continue
            }

            var entry proto.BizPodEntry
            if err := json.Unmarshal(data, &entry); err != nil {
                continue
            }

            if now-entry.LastSeen < int64(maxAge.Seconds()) {
                podID := strings.TrimPrefix(key, proto.BizPodsKeyPrefix)
                live = append(live, livePod{podID: podID, url: entry.URL})
            }
        }

        cursor = nextCursor
        if cursor == 0 {
            break
        }
    }

    return live, nil
}
```

---

### 4.5 G5: Real Server-Initiated Handlers (HIGH)

#### Problem
`handleReAuth`, `handleRevocation`, `handleCoA` return hardcoded dummy bytes.

#### Solution
Implement real handlers that:
1. Load EAP session from Redis by `AuthCtxID`
2. Validate session state
3. Process the message type
4. Notify AMF via Nnssf_NSSAA_Update/Revoke
5. Return proper response bytes

**Handler Interface:**

```go
// internal/proto/http_gateway.go

// ServerInitiatedHandler processes server-initiated messages from AAA GW.
type ServerInitiatedHandler interface {
    HandleReAuth(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
    HandleRevocation(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
    HandleCoA(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
}
```

**Session Loading:**

```go
// cmd/biz/main.go

type serverInitiatedDeps struct {
    Engine    *eap.Engine
    AMFClient AMFNotifier
    Redis     *redis.Client  // Use *redis.Client, not pool
}

func NewServerInitiatedHandler(deps serverInitiatedDeps) ServerInitiatedHandler {
    return &serverInitiatedHandler{deps: deps}
}

type serverInitiatedHandler struct {
    deps serverInitiatedDeps
}

func (h *serverInitiatedHandler) HandleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error) {
    // 1. Load session from Redis
    key := "nssaa:eap:session:" + req.AuthCtxID
    data, err := h.deps.Redis.Get(ctx, key).Bytes()
    if err != nil {
        if errors.Is(err, goredis.Nil) {
            return nil, fmt.Errorf("session not found: %s", req.AuthCtxID)
        }
        return nil, err
    }

    var session eap.Session
    if err := json.Unmarshal(data, &session); err != nil {
        return nil, err
    }

    // 2. Validate session state
    if session.Status != eap.StatusSuccess {
        return nil, fmt.Errorf("session not in SUCCESS state: %s", session.Status)
    }

    // 3. Parse RAR payload to extract EAP data
    eapPayload := extractEAPFromRAR(req.Payload)

    // 4. Create re-auth session
    reAuthSession := eap.NewReAuthSession(req.AuthCtxID, session.SUPI, session.Snssai)

    // 5. Process via EAP engine
    result, err := h.deps.Engine.Process(ctx, reAuthSession, eapPayload)
    if err != nil {
        return nil, err
    }

    // 6. Notify AMF
    if result.Success {
        if err := h.deps.AMFClient.UpdateAuthContext(ctx, req.AuthCtxID, result.NewAuthStatus); err != nil {
            slog.Warn("AMF notification failed", "error", err)
        }
    }

    // 7. Return response
    return &proto.AaaServerInitiatedResponse{
        Version:     "1.0",
        SessionID:   req.SessionID,
        AuthCtxID:   req.AuthCtxID,
        MessageType: string(req.MessageType),
        ResultCode:  result.Success,
        Payload:     result.ResponseBytes,
    }, nil
}
```

**Registration in Biz Pod Factory:**

```go
// cmd/biz/factory.go

func (b *BizPod) setupServerInitiatedHandler() {
    deps := serverInitiatedDeps{
        Engine:    b.eapEngine,
        AMFClient: b.amfNotifier,
        RedisPool: b.RedisPool,
    }
    serverInitiatedHandler = NewServerInitiatedHandler(deps)
}
```

---

### 4.6 G6: RADIUS Configurable MaxRetries (MEDIUM)

#### Problem
`MaxRetries: 3` hardcoded in `newRadiusForwarder()`.

#### Solution
Accept `RadiusForwarderConfig` from gateway config.

**Config Changes:**

```go
// internal/config/internal_comm.go

type NativeCommConfig struct {
    Retry RetryConfig `yaml:"retry"`
    CB    CircuitBreakerConfig `yaml:"circuitBreaker"`
    Pool  ConnectionPoolConfig `yaml:"connectionPool"`
    Timeout time.Duration `yaml:"timeout"`
    Radius RadiusConfig `yaml:"radius"`  // NEW
}

type RadiusConfig struct {
    MaxRetries     int           `yaml:"maxRetries"`
    Timeout        time.Duration `yaml:"timeout"`
    ResponseWindow time.Duration `yaml:"responseWindow"`
}
```

**Gateway Config:**

```go
// internal/aaa/gateway/gateway.go

type Config struct {
    // ... existing fields ...
    InternalComm config.InternalCommConfig  // NEW: reference to internal comm config
}
```

**Usage in Gateway:**

```go
// internal/aaa/gateway/gateway.go

g.radiusForwarder = newRadiusForwarder(RadiusForwarderConfig{
    ServerAddress:   cfg.RadiusServerAddress,
    ServerPort:     1812,
    SharedSecret:    cfg.RadiusSharedSecret,
    Timeout:        cfg.InternalComm.Native.Radius.Timeout,       // From config
    MaxRetries:     cfg.InternalComm.Native.Radius.MaxRetries,   // From config
    ResponseWindow: cfg.InternalComm.Native.Radius.ResponseWindow,
}, cfg.Logger)
```

---

### 4.7 G7: Wire VIP Health Check (MEDIUM)

#### Problem
`StartVIPHealthCheck()` implemented but not called from Biz Pod.

#### Solution
Start VIP health check goroutine in Biz Pod main.

**Modified Biz Pod main.go:**

```go
// cmd/biz/main.go

// Start health check goroutine in main() after pod initialization
if aaaClient, ok := pod.AAAClient.(interface{ StartVIPHealthCheck(context.Context) }); ok {
    go func() {
        aaaClient.StartVIPHealthCheck(podCtx)
    }()
}
```

**Interface for optional method:**

```go
// internal/proto/aaa_transport.go

// VIPHealthChecker is an optional interface for AAA clients that support
// VIP health check based circuit breaker reset.
type VIPHealthChecker interface {
    StartVIPHealthCheck(ctx context.Context)
}
```

---

### 4.8 G8: Add KeepalivedHealthURL to Config (MEDIUM)

#### Problem
Cannot configure the health check endpoint URL.

#### Solution
Add `keepalivedHealthUrl` to `NativeCommConfig`.

**Config Changes:**

```go
// internal/config/internal_comm.go

type NativeCommConfig struct {
    Retry                 RetryConfig        `yaml:"retry"`
    CB                    CircuitBreakerConfig `yaml:"circuitBreaker"`
    Pool                  ConnectionPoolConfig `yaml:"connectionPool"`
    Timeout               time.Duration       `yaml:"timeout"`
    Radius                RadiusConfig        `yaml:"radius"`
    KeepalivedHealthURL   string              `yaml:"keepalivedHealthUrl"`  // NEW
}
```

**Defaults:**

```go
// internal/config/config.go - applyDefaults()

if cfg.InternalComm.Native.KeepalivedHealthURL == "" {
    // Derive from AAA Gateway URL: http://{host}:9090/health/vip
    if cfg.Biz != nil && cfg.Biz.AAAGatewayURL != "" {
        cfg.InternalComm.Native.KeepalivedHealthURL = cfg.Biz.AAAGatewayURL + "/health/vip"
    }
}
```

---

## 4.9 G9: Add Retry to nfclient.Factory (HIGH)

### Problem
`nfclient.Factory.Do()` has circuit breaker but no retry. Failed requests are immediately rejected instead of retried.

### Current State
```go
// internal/nfclient/factory.go
func (f *Factory) Do(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
    // ... circuit breaker check ...
    resp, err := client.Do(req)
    // No retry — immediate failure
}
```

### Solution
Add retry logic with exponential backoff, similar to `httpclient.NativeBizClient`.

**Modified Factory:**

```go
// internal/nfclient/factory.go

type Factory struct {
    cbRegistry *resilience.Registry
    transport  http.RoundTripper
    timeout    time.Duration
    retryCfg   resilience.RetryConfig  // NEW
}

// NewFactory creates a factory with retry configuration.
func NewFactory(cbRegistry *resilience.Registry, retryCfg resilience.RetryConfig) *Factory {
    if retryCfg.MaxAttempts == 0 {
        retryCfg.MaxAttempts = 3
    }
    if retryCfg.BaseDelay == 0 {
        retryCfg.BaseDelay = 500 * time.Millisecond
    }
    if retryCfg.MaxDelay == 0 {
        retryCfg.MaxDelay = 10 * time.Second
    }
    return &Factory{
        cbRegistry: cbRegistry,
        transport:  otelhttp.NewTransport(http.DefaultTransport),
        timeout:    30 * time.Second,
        retryCfg:   retryCfg,
    }
}

// Do executes with retry + circuit breaker.
func (f *Factory) Do(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
    var lastStatus int
    var lastBody []byte
    var lastErr error

    err := resilience.Do(ctx, f.retryCfg, func() error {
        status, respBody, err := f.doOnce(ctx, baseURL, method, path, body)
        lastStatus = status
        lastBody = respBody
        lastErr = err

        if err != nil {
            return err
        }

        // Don't retry 4xx errors
        if status >= 400 && status < 500 {
            return nil
        }

        // Retry 5xx errors
        if resilience.IsRetryable(status) {
            lastErr = fmt.Errorf("retryable status: %d", status)
            return lastErr
        }

        return nil
    })

    if err != nil {
        return lastStatus, lastBody, lastErr
    }
    return lastStatus, lastBody, nil
}
```

---

## 4.10 G10: Make Timeout Configurable (MEDIUM)

### Problem
30s timeout is hardcoded in `nfclient.Factory`.

### Solution
Add `Timeout` to factory and use from config.

**Config Changes:**

```go
// internal/config/nf_client.go  (NEW file or add to existing)

type NFClientConfig struct {
    Timeout time.Duration `yaml:"timeout"`
    Retry  RetryConfig  `yaml:"retry"`
    CB     CircuitBreakerConfig `yaml:"circuitBreaker"`
}

// applyDefaults in config.go
if cfg.NFClient.Timeout == 0 {
    cfg.NFClient.Timeout = 30 * time.Second
}
```

**Usage:**

```go
// cmd/biz/factory.go
nfFactory := nfclient.NewFactory(
    cbRegistry,
    resilience.RetryConfig{
        MaxAttempts: cfg.NFClient.Retry.MaxAttempts,
        BaseDelay:  cfg.NFClient.Retry.BaseDelay,
        MaxDelay:   cfg.NFClient.Retry.MaxDelay,
    },
).WithTimeout(cfg.NFClient.Timeout)
```

---

## 4.11 G11: X-Request-ID in AMF Notifications (MEDIUM)

### Problem
AMF notifications don't include `X-Request-ID` for correlation.

### Solution
Pass `X-Request-ID` through from Biz Pod to AMF callback.

**Modified AMF Client:**

```go
// internal/amf/amf.go

func (c *Client) sendNotification(ctx context.Context, typ NotificationType, uri, authCtxID string, payload []byte) error {
    // ... existing logic ...

    // Extract baseURL and path from full URI
    baseURL, path, err := extractBaseURLAndPath(uri)
    if err != nil {
        return fmt.Errorf("amf: parse uri: %w", err)
    }

    // Build request with X-Request-ID
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(payload))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    if reqID := middleware.GetRequestID(ctx); reqID != "" {
        req.Header.Set("X-Request-ID", reqID)
    }

    status, _, err := c.factory.DoRequest(req)
    // ...
}
```

**New method on Factory:**

```go
// internal/nfclient/factory.go

// DoRequest executes a pre-built request (allows caller to set custom headers).
func (f *Factory) DoRequest(req *http.Request) (int, []byte, error) {
    // Similar to Do() but uses pre-built request
}
```

---

## 4.12 G12: Make DLQ Configurable (LOW)

### Problem
DLQ retry behavior not configurable (max attempts, retry delay).

### Solution
Add DLQ config to `AMFConfig`.

**Config Changes:**

```go
// internal/config/amf.go  (or add to existing)

type AMFConfig struct {
    DLQ DLQConfig `yaml:"dlq"`
}

type DLQConfig struct {
    MaxRetries     int           `yaml:"maxRetries"`
    RetryDelay     time.Duration `yaml:"retryDelay"`
    AlertThreshold int           `yaml:"alertThreshold"`
}

// applyDefaults
if cfg.AMF.DLQ.MaxRetries == 0 {
    cfg.AMF.DLQ.MaxRetries = 10
}
if cfg.AMF.DLQ.RetryDelay == 0 {
    cfg.AMF.DLQ.RetryDelay = 30 * time.Second
}
```

---

## 5. Implementation Phases

### Phase 1: Config Schema + Outbound Basics (G8, G9, G10)
1. Add `KeepalivedHealthURL` to `NativeCommConfig`
2. Add `Radius` config with `MaxRetries`, `Timeout`, `ResponseWindow`
3. Add `Timeout` to `NativeCommConfig`
4. Wire `StartVIPHealthCheck()` in Biz Pod
5. Add retry to `nfclient.Factory` (G9)
6. Add configurable timeout to `nfclient.Factory` (G10)

### Phase 2: HTTP Gateway Load Balancing (G1, G4)
1. Create `BizRegistry` type in `internal/httpclient/`
2. Implement Redis-based target selection
3. Update factory to create `BizRegistry`
4. Modify HTTP Gateway to pass Redis address

### Phase 3: X-Request-ID Propagation (G2, G11)
1. Update `BizServiceClient` interface to include `requestID`
2. Update all client implementations
3. Update HTTP Gateway handlers
4. Add X-Request-ID to AMF notifications (G11)

### Phase 4: RADIUS Configurable Retries (G6)
1. Update `RadiusForwarderConfig` to use config values
2. Pass `InternalCommConfig` to Gateway

### Phase 5: Real Server-Initiated Handlers (G5)
1. Implement `ServerInitiatedHandler` interface
2. Wire session loading from Redis
3. Wire AMF notifications
4. Register in Biz Pod factory

### Phase 6: DLQ Config (G12)
1. Add DLQ config to AMF configuration
2. Update DLQ consumer to use configurable values

---

## 6. File Changes Summary

### Inbound (HTTP Gateway ↔ Biz)

| File | Changes |
|------|---------|
| `internal/config/internal_comm.go` | Add `Timeout`, `Radius`, `KeepalivedHealthURL` |
| `internal/config/config.go` | Apply defaults for new fields |
| `internal/proto/http_gateway.go` | Add `requestID` to interface, `ServerInitiatedHandler` interface |
| `internal/httpclient/biz_registry.go` | **NEW**: BizRegistry with Redis-based target selection |
| `internal/httpclient/native_biz.go` | Add `requestID` param, use configurable timeout |
| `internal/httpclient/istio_biz.go` | Add `requestID` param |
| `internal/httpclient/factory.go` | Update factory to create `BizRegistry` |
| `internal/aaa/gateway/gateway.go` | Use config for RADIUS, pass `InternalCommConfig` |
| `internal/aaa/gateway/radius_forward.go` | Accept configurable `MaxRetries` |
| `cmd/http-gateway/main.go` | Pass Redis addr, extract X-Request-ID |
| `cmd/biz/main.go` | Wire VIP health check, implement real handlers |
| `cmd/biz/factory.go` | Add `WithServerInitiatedDeps`, wire VIP health check |

### Outbound (Biz Pod → NRF/UDM/AMF)

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `NFClient` config, `AMF.DLQ` config |
| `internal/nfclient/factory.go` | Add retry logic, configurable timeout |
| `internal/amf/amf.go` | Add X-Request-ID propagation, use DLQ config |
| `cmd/biz/factory.go` | Wire NF clients with config |

---

## 7. Testing Strategy

### Unit Tests
- `TestBizRegistry_SelectsLivePod` — verifies dead pods filtered
- `TestBizRegistry_FallbackToStaticURL` — verifies fallback behavior
- `TestNativeBizClient_PropagatesRequestID` — verifies X-Request-ID header
- `TestServerInitiatedHandler_HandleReAuth_SessionNotFound` — error case
- `TestServerInitiatedHandler_HandleReAuth_Success` — happy path

### Integration Tests
- `TestHTTPGateway_LoadBalancesAcrossBizPods` — kill one pod, verify routing
- `TestVIPFailover_CircuitBreakerResets` — simulate failover, measure reset time
- `TestServerInitiated_DLQRetries` — inject failure, verify DLQ behavior

### E2E Tests
- Full EAP flow with server-initiated re-auth
- VIP failover with zero-downtime requirement

---

## 8. Acceptance Criteria

### Inbound Criteria (HTTP Gateway ↔ Biz)

| ID | Criteria | Verification |
|----|----------|---------------|
| AC1 | HTTP Gateway routes to live Biz Pod only | Kill pod → verify 200 on other pods |
| AC2 | X-Request-ID propagates from AMF to Biz Pod | Log correlation in Biz Pod |
| AC3 | Timeout configurable via YAML | Set 10s → verify timeout behavior |
| AC4 | Dead Biz Pods auto-excluded from routing | Wait 60s after kill → verify no routing |
| AC5 | Server-initiated handlers return valid EAP | RAR → proper RAA response |
| AC6 | RADIUS retries use configured value | Set 5 → verify 5 retries |
| AC7 | VIP failover → CB resets within 10s | Measure time from failover to CB reset |
| AC8 | KeepalivedHealthURL configurable | Set URL → verify health check hits correct endpoint |

### Outbound Criteria (Biz Pod → NRF/UDM/AMF)

| ID | Criteria | Verification |
|----|----------|---------------|
| AC9 | NRF/UDM/AMF requests retry on 5xx | Inject 500 → verify retry attempts |
| AC10 | NF client timeout configurable | Set 5s → verify timeout |
| AC11 | AMF notifications include X-Request-ID | Check header in AMF logs |
| AC12 | AMF DLQ max retries configurable | Set 5 → verify DLQ exhausts after 5 |

---

## 9. Metrics

### Inbound Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `nssAAF_http_gw_biz_pods_live` | Gauge | - |
| `nssAAF_http_gw_biz_target_selection_total` | Counter | `reason` |
| `nssAAF_server_initiated_handled_total` | Counter | `type`, `result` |
| `nssAAF_vip_health_check_state` | Gauge | - |
| `nssAAF_cb_reset_by_vip_change_total` | Counter | - |

### Outbound Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `nssAAF_nf_client_retry_total` | Counter | `nf`, `attempt` |
| `nssAAF_nf_client_timeout_total` | Counter | `nf` |
| `nssAAF_amf_notification_retry_total` | Counter | `type`, `attempt` |
| `nssAAF_amf_dlq_depth` | Gauge | - |
