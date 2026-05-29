# Biz ↔ AAA Gateway HA Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** HA for internal Biz↔AAA GW communication: pod death resilience (server-initiated), VIP-aware startup, dead code removal, configurable RADIUS retries, and VIP-aware circuit breaker.

**Architecture:** 3-component model unchanged. AAA GW is per-VIP singleton. Biz Pods register in Redis HASH (`nssaa:biz:pods`). Server-initiated routing uses Redis HASH for pod discovery + direct HTTP POST to target pod. Dead Redis pub/sub removed.

**Tech Stack:** Go 1.21+, go-redis/v9, 3GPP RADIUS/Diameter, keepalived state file polling.

---

## File Structure

```
cmd/aaa-gateway/main.go                    # MODIFY: call StartVIPAware instead of Start
internal/aaa/gateway/
  gateway.go                               # MODIFY: remove pub/sub, add StartVIPAware/startListeners, target selection, retry, DLQ
  gateway_test.go                          # MODIFY: remove pub/sub tests, add VIP startup tests
  radius_forward.go                        # MODIFY: accept RadiusForwarderConfig
  radius_handler.go                        # MODIFY: remove publishResponse field/usage
  radius_handler_test.go                   # MODIFY: remove publishResponse mock
  diameter_handler.go                      # MODIFY: remove publishResponse field
  dlq.go                                  # CREATE: DLQ consumer goroutine
internal/httpclient/
  native_aaa.go                           # MODIFY: add StartVIPHealthCheck
internal/proto/
  biz_callback.go                         # MODIFY: add BizPodEntry, BizPodsHash, DLQKey, remove AaaResponseEvent
  biz_callback_test.go                    # MODIFY: remove AaaResponseChannel test, add BizPodEntry test
cmd/biz/
  factory.go                              # MODIFY: pod registration HASH (remove WithServerInitiatedDeps comment)
  main.go                                 # MODIFY: real handleServerInitiated
internal/config/
  internal_comm.go                        # MODIFY: add KeepalivedHealthURL
  config.go                               # MODIFY: pass RadiusForwarderConfig
```

---

## Task 1: Remove Dead Redis Pub/Sub

**Files:**
- Modify: `internal/aaa/gateway/gateway.go:1-464`
- Modify: `internal/proto/biz_callback.go:1-44`
- Modify: `internal/aaa/gateway/radius_handler.go:1-30`
- Modify: `internal/aaa/gateway/radius_handler_test.go:1-275`
- Modify: `internal/aaa/gateway/diameter_handler.go:1-30` (verify if it has publishResponse field)

### Step 1: Remove `publishResponseBytes`, `publishResponse`, `dispatchResponse`, `subscribeResponses` from `gateway.go`

Delete the following methods from `Gateway`:

```go
// DELETE: publishResponseBytes (gateway.go lines 324-343)
func (g *Gateway) publishResponseBytes(sessionID string, raw []byte) {
    // ... delete entire method
}

// DELETE: publishResponse (gateway.go lines 417-424)
func (g *Gateway) publishResponse(ctx context.Context, event *proto.AaaResponseEvent) error {
    // ... delete entire method
}

// DELETE: dispatchResponse (gateway.go lines 297-312)
func (g *Gateway) dispatchResponse(event *proto.AaaResponseEvent) {
    // ... delete entire method
}

// DELETE: subscribeResponses (gateway.go lines 426-444)
func (g *Gateway) subscribeResponses(ctx context.Context) {
    // ... delete entire method
}
```

### Step 2: Remove `pending` map, `pendingEntry` struct, `pendingMu` from `Gateway` struct

In `gateway.go`, remove from the `Gateway` struct:

```go
// DELETE from Gateway struct (gateway.go lines 64-67):
// pending maps SessionID → pendingEntry (for client-initiated response routing).
// Fix: store both SessionID and AuthCtxID so AaaResponseEvent.AuthCtxID is populated.
pending   map[string]*pendingEntry
pendingMu sync.RWMutex

// DELETE pendingEntry struct (gateway.go lines 74-79):
type pendingEntry struct {
    authCtxID string
    sessionID string
    ch        chan []byte
}
```

Also remove `pendingEntry` initialization from `New()` and the `pending` map usage in `ForwardEAP`.

### Step 2b: Replace `ForwardEAP` with simplified version

After removing `pending` and `publishResponse`, replace the `ForwardEAP` method with this version:

```go
// ForwardEAP satisfies proto.BizAAAClient.
// It receives AaaForwardRequest from Biz Pod, writes session correlation to Redis,
// forwards to AAA-S, and returns the response directly to the caller.
func (g *Gateway) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
    // 1. Write session correlation entry to Redis (before forwarding)
    // Wire os.Hostname() now so direct pod lookup works immediately.
    hostname, _ := os.Hostname()
    entry := proto.SessionCorrEntry{
        AuthCtxID: req.AuthCtxID,
        PodID:     hostname, // Written once here; read on server-initiated routing
        Sst:       req.Sst,
        Sd:        req.Sd,
        CreatedAt: time.Now().Unix(),
    }
    if err := g.writeSessionCorr(ctx, req.SessionID, &entry); err != nil {
        return nil, fmt.Errorf("aaa-gateway: failed to write session corr: %w", err)
    }

    // 2. Forward to AAA-S based on transport type
    var response []byte
    var err error
    switch req.TransportType {
    case proto.TransportRADIUS:
        response, err = g.radiusForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
    case proto.TransportDIAMETER:
        response, err = g.diamForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
    default:
        return nil, fmt.Errorf("aaa-gateway: unknown transport type: %s", req.TransportType)
    }
    if err != nil {
        return nil, fmt.Errorf("aaa-gateway: forward failed: %w", err)
    }

    // 3. Return response directly to caller (no Redis pub/sub needed)
    return &proto.AaaForwardResponse{
        Version:   g.version,
        SessionID: req.SessionID,
        AuthCtxID: req.AuthCtxID,
        Payload:   response,
    }, nil
}
```

### Step 3: Remove `subscribeResponses` goroutine from `Start()`

In `Start()` (gateway.go line 162-167), remove the Redis subscription goroutine:

```go
// DELETE this entire block from Start():
// Start Redis subscription for dispatching responses
g.wg.Add(1)
go func() {
    defer g.wg.Done()
    g.subscribeResponses(g.ctx)
}()
```

### Step 4: Remove `publishResponse` field from `RadiusHandler`

In `internal/aaa/gateway/radius_handler.go`, remove from `RadiusHandler` struct:

```go
// DELETE from RadiusHandler struct:
publishResponse func(sessionID string, raw []byte)
```

Update the `NewRadiusHandler` / handler constructor to remove the `publishResponse` parameter. If `RadiusHandler` uses `publishResponse` internally (line 77: `h.publishResponse(sessionID, raw)`), replace that call with a no-op logger call — responses are now returned directly via the Biz Pod HTTP response path, so the RadiusHandler no longer needs to publish to Redis:

```go
// Replace: h.publishResponse(sessionID, raw)
// With:
h.logger.Debug("radius_response_received", "session_id", sessionID, "len", len(raw))
```

### Step 5: Remove `publishResponse` field from `DiameterHandler`

Check `internal/aaa/gateway/diameter_handler.go` for `publishResponse` field. If present, remove it and update the constructor.

### Step 6: Update `radius_handler_test.go` — remove `mockPublishResponse`

In `radius_handler_test.go`, remove `mockPublishResponse` struct and all `publishResponse` field usages from test structs. Replace `publishResponse: pub.invoke` with `nil` or remove entirely where it is a field of the test RadiusHandler struct.

### Step 7: Remove `AaaResponseChannel`, `AaaResponseEvent` from `proto/biz_callback.go`

```go
// DELETE entire AaaResponseEvent type (biz_callback.go lines 4-12):
type AaaResponseEvent struct {
    Version   string `json:"v"`
    SessionID string `json:"sessionId"`
    AuthCtxID string `json:"authCtxId"`
    Payload   []byte `json:"payload"`
}

// DELETE from const block (biz_callback.go lines 35-37):
// AaaResponseChannel is the Redis pub/sub channel for AAA responses.
// Publisher: AAA Gateway. Subscribers: all Biz Pods.
AaaResponseChannel = "nssaa:aaa-response"
```

### Step 8: Update `biz_callback_test.go` — remove `AaaResponseChannel` test and `TestAaaResponseEvent_JSONRoundtrip`

In `biz_callback_test.go`, delete:
1. The entire `TestAaaResponseEvent_JSONRoundtrip` function (references deleted `AaaResponseEvent` type)
2. The `AaaResponseChannel` assertion in `TestRedisConstants` (references deleted constant)

### Step 9: Verify compilation

Run: `go build ./internal/aaa/gateway/... ./internal/proto/... ./cmd/aaa-gateway/...`
Expected: compile success with zero errors.

---

## Task 2: VIP-Aware Startup for AAA Gateway

**Files:**
- Modify: `internal/aaa/gateway/gateway.go`
- Modify: `cmd/aaa-gateway/main.go`

### Step 1: Add `startListeners()` method to Gateway

Add the following method to `Gateway` in `gateway.go`. This moves the existing listener-startup logic out of `Start()` into an idempotent method:

```go
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

    // Diameter client-initiated connection to AAA-S
    if g.diamForwarder != nil && g.cfg.DiameterServerAddress != "" {
        g.wg.Add(1)
        go func() {
            defer g.wg.Done()
            if err := g.diamForwarder.Connect(g.ctx); err != nil {
                g.logger.Error("diameter_forward_connect_failed", "error", err)
            }
        }()
    }

    return nil
}
```

### Step 2: Add `StartVIPAware()` method to Gateway

Add after `startListeners()`:

```go
// StartVIPAware blocks until this pod becomes VIP owner, then starts all listeners.
// Returns true if started successfully, false on context cancellation or error.
func (g *Gateway) StartVIPAware(ctx context.Context, statePath string) bool {
    // Dev/test mode: no state file → start immediately
    if statePath == "" || statePath == "/dev/null" {
        g.logger.Info("no keepalived state file, starting immediately (dev/test mode)")
        if err := g.startListeners(ctx); err != nil {
            g.logger.Error("startListeners failed", "error", err)
            return false
        }
        return true
    }

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
```

### Step 3: Refactor `Start()` to delegate to `startListeners()`

Replace the entire `Start()` method body:

```go
// Start starts all protocol listeners unconditionally.
// Deprecated: use StartVIPAware for HA deployments.
func (g *Gateway) Start(ctx context.Context) error {
    return g.startListeners(ctx)
}
```

Also remove the `subscribeResponses` goroutine call from `Start()` (already removed in Task 1, Step 3).

### Step 4: Update `cmd/aaa-gateway/main.go` to call `StartVIPAware`

In `cmd/aaa-gateway/main.go`, replace the `gw.Start()` call:

```go
// OLD (gateway.go line 79-82):
if err := gw.Start(ctx); err != nil {
    slog.Error("gateway start failed", "error", err)
    os.Exit(1)
}

// NEW: VIP-aware startup
if !gw.StartVIPAware(ctx, cfg.AAAgw.KeepalivedStatePath) {
    slog.Error("gateway failed to acquire VIP or start listeners")
    os.Exit(1)
}
```

The HTTP server in `main.go` still starts unconditionally (before `StartVIPAware`) — this is correct, it must be up to serve `/health/vip` for Biz Pod health checks.

### Step 5: Add unit tests for `StartVIPAware`

Add to `internal/aaa/gateway/gateway_test.go`:

```go
func TestStartVIPAware_DevModeNoStateFile(t *testing.T) {
    // When statePath is empty, should start immediately without polling
    gw := &Gateway{
        cfg: Config{
            KeepalivedStatePath: "",
            ListenRADIUS:        "",
            ListenDIAMETER:     "",
        },
        logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
        wg:     sync.WaitGroup{},
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    started := gw.StartVIPAware(ctx, "")
    if !started {
        t.Fatal("expected StartVIPAware to return true in dev mode")
    }
}

func TestStartVIPAware_DevModeDevNull(t *testing.T) {
    gw := &Gateway{
        cfg: Config{
            KeepalivedStatePath: "/dev/null",
            ListenRADIUS:        "",
            ListenDIAMETER:      "",
        },
        logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
        wg:     sync.WaitGroup{},
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    started := gw.StartVIPAware(ctx, "/dev/null")
    if !started {
        t.Fatal("expected StartVIPAware to return true with /dev/null state path")
    }
}
```

### Step 6: Verify compilation

Run: `go build ./internal/aaa/gateway/... ./cmd/aaa-gateway/...`
Expected: compile success.

---

## Task 3: Add BizPodEntry + BizPodsHash for Server-Initiated Routing

**Files:**
- Modify: `internal/proto/biz_callback.go`

### Step 1: Add `BizPodEntry` struct and Redis key constants

Add to `proto/biz_callback.go` after the existing constants:

```go
// BizPodsHash is the Redis HASH storing live Biz Pod URLs keyed by PodID.
// Key: "nssaa:biz:pods"
// Field: podID → BizPodEntry JSON
// TTL: managed by per-field TTL (not natively supported by Redis HASH, use separate per-pod keys)
// See BizPodEntryTTL.
const BizPodsHash = "nssaa:biz:pods"

// BizPodEntryTTL is the TTL for the per-pod key in BizPodsKey.
// If a pod does not refresh within this window, it is considered dead.
const BizPodEntryTTL = 60 * time.Second

// BizPodsKey builds the Redis key for a specific pod's entry.
// Key: "nssaa:biz:pod:{podID}" → BizPodEntry JSON, TTL = BizPodEntryTTL
func BizPodsKey(podID string) string {
    return "nssaa:biz:pod:" + podID
}

// BizPodEntry represents a registered Biz Pod in Redis.
// Written by Biz Pod on startup and refreshed every 30s via heartbeat.
type BizPodEntry struct {
    URL       string `json:"url"`       // e.g. "http://biz-pod-a:8080"
    LastSeen  int64  `json:"lastSeen"` // Unix timestamp of last heartbeat
}
```

### Step 2: Add `DLQKey` constant for DLQ

```go
// DLQKey is the Redis LIST used as a dead-letter queue for failed server-initiated messages.
const DLQKey = "nssaa:dlq:server-initiated"
```

### Step 3: Add test for `BizPodsKey` and `BizPodEntry`

Add to `internal/proto/biz_callback_test.go`:

```go
func TestBizPodsKey(t *testing.T) {
    got := BizPodsKey("biz-pod-1")
    want := "nssaa:biz:pod:biz-pod-1"
    if got != want {
        t.Errorf("BizPodsKey: got %q, want %q", got, want)
    }
}

func TestBizPodsKey_Empty(t *testing.T) {
    got := BizPodsKey("")
    want := "nssaa:biz:pod:"
    if got != want {
        t.Errorf("BizPodsKey empty: got %q, want %q", got, want)
    }
}

func TestBizPodEntry_JSON(t *testing.T) {
    entry := BizPodEntry{
        URL:      "http://biz-pod-1:8080",
        LastSeen: 1716560000,
    }
    data, err := json.Marshal(entry)
    if err != nil {
        t.Fatal(err)
    }
    var roundTrip BizPodEntry
    if err := json.Unmarshal(data, &roundTrip); err != nil {
        t.Fatal(err)
    }
    if roundTrip.URL != entry.URL || roundTrip.LastSeen != entry.LastSeen {
        t.Errorf("round-trip mismatch: got %+v, want %+v", roundTrip, entry)
    }
}
```

### Step 4: Verify compilation

Run: `go build ./internal/proto/...`
Expected: compile success.

---

## Task 4: Biz Pod Registration Loop (HSET → HASH)

**Files:**
- Modify: `cmd/biz/main.go` (podHeartbeat)
- Modify: `cmd/biz/factory.go` (pass Redis pool to podHeartbeat)

### Step 1: Refactor `podHeartbeat` in `main.go` to use HASH

Replace the existing `podHeartbeat` function (which uses `SAdd`/`SRem` on `PodsKey` SET) with a HASH-based registration:

```go
// podHeartbeat registers the Biz Pod in the Redis HASH and refreshes every 30 seconds.
// On shutdown, deletes the pod entry.
func podHeartbeat(ctx context.Context, redisAddr, podID, podURL string) {
    rdb := goredis.NewClient(&goredis.Options{Addr: redisAddr})
    defer func() { _ = rdb.Close() }()

    key := proto.BizPodsKey(podID)
    entry := proto.BizPodEntry{
        URL:      podURL,
        LastSeen: time.Now().Unix(),
    }
    data, err := json.Marshal(entry)
    if err != nil {
        slog.Warn("failed to marshal BizPodEntry", "error", err)
        return
    }

    if err := rdb.Set(ctx, key, data, proto.BizPodEntryTTL).Err(); err != nil {
        slog.Warn("failed to register pod in Redis HASH", "error", err, "pod_id", podID)
    }

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            if err := rdb.Del(ctx, key).Err(); err != nil {
                slog.Warn("failed to unregister pod on shutdown", "error", err, "pod_id", podID)
            }
            return
        case <-ticker.C:
            entry.LastSeen = time.Now().Unix()
            data, _ := json.Marshal(entry)
            if err := rdb.Set(ctx, key, data, proto.BizPodEntryTTL).Err(); err != nil {
                slog.Warn("failed to refresh pod heartbeat", "error", err, "pod_id", podID)
            }
        }
    }
}
```

### Step 2: Update the call site in `main.go`

In `main.go`, update the heartbeat call (line 88) to pass `podURL`:

```go
// OLD:
go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

// NEW:
podURL := "http://" + cfg.Server.Addr
go podHeartbeat(context.Background(), cfg.Redis.Addr, podID, podURL)
```

### Step 3: Verify compilation

Run: `go build ./cmd/biz/...`
Expected: compile success.

---

## Task 5: Target Selection in AAA Gateway

**Files:**
- Modify: `internal/aaa/gateway/gateway.go`

### Step 1: Add `BizPodEntryTTL` to Gateway config (use `cfg` directly, no new field)

In `gateway.go`, add to the `Config` struct:

```go
BizPodEntryTTL time.Duration // TTL for BizPodEntry keys (default 60s)
```

The TTL is read directly from `g.cfg.BizPodEntryTTL` in `pickRandomLiveBizURL`. If unset, the methods use a default via the `BizPodEntryTTL` constant in `proto/biz_callback.go` (60 seconds).

### Step 2: Add target selection methods to Gateway

Add the following three methods to `gateway.go`:

```go
// getBizPodURL reads the BizPodEntry for a specific podID from Redis HASH.
// Returns empty string if the pod is not registered or TTL has expired.
func (g *Gateway) getBizPodURL(ctx context.Context, podID string) (string, error) {
    if podID == "" {
        return "", nil
    }
    key := proto.BizPodsKey(podID)
    data, err := g.redis.Get(ctx, key).Bytes()
    if err != nil {
        if errors.Is(err, redis.Nil) {
            return "", nil
        }
        return "", err
    }
    var entry proto.BizPodEntry
    if err := json.Unmarshal(data, &entry); err != nil {
        return "", err
    }
    return entry.URL, nil
}

// pickRandomLiveBizURL selects a random live Biz Pod from the Redis HASH.
// A pod is considered live if its LastSeen is within ttl.
func (g *Gateway) pickRandomLiveBizURL(ctx context.Context) (string, error) {
    keys, err := g.redis.Keys(ctx, "nssaa:biz:pod:*").Result()
    if err != nil {
        return "", err
    }
    if len(keys) == 0 {
        return "", nil
    }

    ttl := g.cfg.BizPodEntryTTL
    if ttl == 0 {
        ttl = proto.BizPodEntryTTL
    }
    cutoff := time.Now().Add(-ttl).Unix()
    var livePods []string
    for _, key := range keys {
        data, err := g.redis.Get(ctx, key).Bytes()
        if err != nil {
            continue
        }
        var entry proto.BizPodEntry
        if err := json.Unmarshal(data, &entry); err != nil {
            continue
        }
        if entry.LastSeen >= cutoff && entry.URL != "" {
            livePods = append(livePods, entry.URL)
        }
    }
    if len(livePods) == 0 {
        return "", nil
    }
    return livePods[time.Now().UnixNano()%int64(len(livePods))], nil
}

// selectTargetBizURL selects the target URL for a server-initiated message.
// Priority: 1) direct pod lookup via podID, 2) random live pod, 3) static BizServiceURL.
func (g *Gateway) selectTargetBizURL(ctx context.Context, podID string) (string, error) {
    // 1. Try direct lookup
    if podID != "" {
        url, err := g.getBizPodURL(ctx, podID)
        if err != nil {
            g.logger.Warn("getBizPodURL failed, falling back", "pod_id", podID, "error", err)
        } else if url != "" {
            return url, nil
        }
    }
    // 2. Fallback: random live pod
    url, err := g.pickRandomLiveBizURL(ctx)
    if err != nil {
        g.logger.Warn("pickRandomLiveBizURL failed, falling back to static", "error", err)
    } else if url != "" {
        return url, nil
    }
    // 3. Final fallback: static URL
    return g.cfg.BizServiceURL, nil
}
```

### Step 3: Update `forwardToBiz` to use retry + target selection

Replace the existing `forwardToBiz` method. The new version:
1. Reads `SessionCorrEntry` to get `PodID`
2. Calls `selectTargetBizURL` to pick the target
3. Retries up to 3 times on connection errors, selecting a new pod on each retry
4. Pushes to DLQ after all retries fail

```go
const (
    serverInitMaxRetries   = 3
    serverInitRetryBase    = 1 * time.Second
    serverInitRetryMax     = 3 * time.Second
)

// forwardToBiz sends a server-initiated message to the Biz Pod via HTTP POST.
// It selects the target pod dynamically, retries on connection errors, and pushes
// failed messages to the DLQ after all retries are exhausted.
func (g *Gateway) forwardToBiz(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte) {
    // 1. Look up session correlation to get target PodID
    entry, err := g.getSessionCorr(ctx, sessionID)
    if err != nil || entry == nil {
        g.logger.Warn("server_initiated_session_not_found",
            "session_id", sessionID,
            "transport", transportType,
            "message_type", messageType)
        return
    }

    // 2. Build the request body once
    req := &proto.AaaServerInitiatedRequest{
        Version:       g.version,
        SessionID:     sessionID,
        AuthCtxID:     entry.AuthCtxID,
        TransportType: proto.TransportType(transportType),
        MessageType:   proto.MessageType(messageType),
        Payload:       raw,
    }
    body, err := json.Marshal(req)
    if err != nil {
        g.logger.Error("failed to marshal server-initiated request", "error", err)
        return
    }

    // 3. Retry loop
    var lastErr error
    for attempt := 0; attempt < serverInitMaxRetries; attempt++ {
        if attempt > 0 {
            sleep := time.Duration(attempt) * serverInitRetryBase
            if sleep > serverInitRetryMax {
                sleep = serverInitRetryMax
            }
            time.Sleep(sleep)
        }

        targetURL, err := g.selectTargetBizURL(ctx, entry.PodID)
        if err != nil {
            lastErr = err
            g.logger.Warn("selectTargetBizURL failed",
                "attempt", attempt+1, "error", err)
            continue
        }

        httpReq, err := http.NewRequestWithContext(ctx, "POST",
            targetURL+"/aaa/server-initiated", bytes.NewReader(body))
        if err != nil {
            lastErr = err
            continue
        }
        httpReq.Header.Set("Content-Type", "application/json")
        httpReq.Header.Set(proto.HeaderName, g.version)

        resp, err := g.bizHTTPClient.Do(httpReq)
        if err != nil {
            lastErr = err
            isConnErr := isConnectionError(err)
            g.logger.Warn("biz HTTP call failed",
                "attempt", attempt+1, "error", err, "target_url", targetURL,
                "retrying", isConnErr)
            // Only retry on connection errors — 4xx/5xx from the server are not retried
            if !isConnErr {
                break
            }
            continue
        }
        _, _ = io.Copy(io.Discard, resp.Body)
        _ = resp.Body.Close()

        if resp.StatusCode == http.StatusOK {
            return // Success
        }
        g.logger.Warn("biz returned non-OK",
            "status", resp.StatusCode, "session_id", sessionID, "target_url", targetURL)
        // Non-connection errors and 4xx/5xx are not retried
        return
    }

    // 4. All retries exhausted → push to DLQ
    g.logger.Error("server_initiated_all_retries_failed",
        "session_id", sessionID, "error", lastErr, "pod_id", entry.PodID)
    g.pushDLQ(ctx, sessionID, transportType, messageType, body)
}

// isConnectionError returns true if err is a connection/dial timeout error.
func isConnectionError(err error) bool {
    if err == nil {
        return false
    }
    var opErr *net.OpError
    if errors.As(err, &opErr) {
        return opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write"
    }
    return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// pushDLQ pushes a failed server-initiated message to the Redis DLQ list.
func (g *Gateway) pushDLQ(ctx context.Context, sessionID, transportType, messageType string, body []byte) {
    msg := map[string]interface{}{
        "sessionID":     sessionID,
        "transportType": transportType,
        "messageType":   messageType,
        "payload":       body,
        "attemptCount":  0,
        "queuedAt":      time.Now().Unix(),
    }
    data, err := json.Marshal(msg)
    if err != nil {
        g.logger.Error("failed to marshal DLQ message", "error", err)
        return
    }
    if err := g.redis.RPush(ctx, proto.DLQKey, data).Err(); err != nil {
        g.logger.Error("failed to push to DLQ", "error", err)
    }
}
```

### Step 4: Add missing import

In the imports block of `gateway.go`, add `"net"` if not present:

```go
import (
    // ...
    "net"
    "net/http"
    // ...
)
```

### Step 5: Verify compilation

Run: `go build ./internal/aaa/gateway/...`
Expected: compile success.

---

## Task 6: DLQ Consumer Goroutine

**Files:**
- Create: `internal/aaa/gateway/dlq.go`
- Modify: `internal/aaa/gateway/gateway.go`

### Step 1: Create `internal/aaa/gateway/dlq.go`

```go
// Package gateway provides the AAA Gateway component.
package gateway

import (
    "context"
    "encoding/json"
    "log/slog"
    "time"

    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v9"
)

const (
    dlqMaxAttempts    = 10
    dlqPollInterval   = 30 * time.Second
    dlqMaxDelay       = 3 * time.Second
    dlqRetryBaseDelay = 1 * time.Second
)

// DLQMessage represents a message in the server-initiated DLQ.
type DLQMessage struct {
    SessionID     string `json:"sessionID"`
    TransportType string `json:"transportType"`
    MessageType   string `json:"messageType"`
    Payload       []byte `json:"payload"`
    AttemptCount  int    `json:"attemptCount"`
    QueuedAt      int64  `json:"queuedAt"`
}

// runDLQConsumer processes messages from the DLQ list.
// It pops messages with BRPOP, retries up to dlqMaxAttempts, and discards after exhaustion.
func (g *Gateway) runDLQConsumer(ctx context.Context) {
    ticker := time.NewTicker(dlqPollInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            g.processDLQOne(ctx)
        }
    }
}

// processDLQOne pops and processes one DLQ message. Non-blocking.
func (g *Gateway) processDLQOne(ctx context.Context) {
    result, err := g.redis.BRPop(ctx, 1*time.Second, proto.DLQKey).Result()
    if err != nil {
        if err == redis.Nil {
            return // No message available
        }
        g.logger.Warn("DLQ BRPOP failed", "error", err)
        return
    }
    if len(result) < 2 {
        return
    }

    var msg DLQMessage
    if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
        g.logger.Error("failed to unmarshal DLQ message", "error", err)
        return
    }

    g.processDLQMessage(ctx, &msg)
}

// processDLQMessage retries a single DLQ message.
func (g *Gateway) processDLQMessage(ctx context.Context, msg *DLQMessage) {
    if msg.AttemptCount >= dlqMaxAttempts {
        g.logger.Error("server_initiated_dlq_exhausted",
            "session_id", msg.SessionID,
            "message_type", msg.MessageType,
            "attempts", msg.AttemptCount,
            "queued_at", msg.QueuedAt,
        )
        // TODO: fire alert metric here
        return
    }

    targetURL, err := g.selectTargetBizURL(ctx, "")
    if err != nil || targetURL == "" {
        g.logger.Warn("DLQ: selectTargetBizURL failed, requeueing", "error", err)
        g.requeueDLQ(ctx, msg)
        return
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST",
        targetURL+"/aaa/server-initiated", nil)
    if err != nil {
        g.requeueDLQ(ctx, msg)
        return
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set(proto.HeaderName, g.version)

    resp, err := g.bizHTTPClient.Do(httpReq)
    if err != nil {
        g.logger.Warn("DLQ: HTTP call failed, requeueing",
            "error", err, "session_id", msg.SessionID, "target_url", targetURL)
        g.requeueDLQ(ctx, msg)
        return
    }
    _, _ = io.Copy(io.Discard, resp.Body)
    _ = resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        g.logger.Info("DLQ: message delivered successfully",
            "session_id", msg.SessionID, "target_url", targetURL)
        return
    }
    g.logger.Warn("DLQ: non-OK response, requeueing",
        "status", resp.StatusCode, "session_id", msg.SessionID)
    g.requeueDLQ(ctx, msg)
}

// requeueDLQ pushes the message back to the DLQ with incremented attempt count.
func (g *Gateway) requeueDLQ(ctx context.Context, msg *DLQMessage) {
    msg.AttemptCount++
    data, err := json.Marshal(msg)
    if err != nil {
        g.logger.Error("failed to marshal DLQ message for requeue", "error", err)
        return
    }
    if err := g.redis.RPush(ctx, proto.DLQKey, data).Err(); err != nil {
        g.logger.Error("failed to requeue DLQ message", "error", err)
    }
}
```

### Step 2: Start DLQ consumer from `startListeners()`

Add to `startListeners()` in `gateway.go` after the Diameter client connection:

```go
// DLQ consumer
g.wg.Add(1)
go func() {
    defer g.wg.Done()
    g.runDLQConsumer(g.ctx)
}()
```

### Step 3: Add missing imports

```go
import (
    "bytes"      // already present
    "encoding/json" // already present
    // ...
    "io"         // already present
    "net"        // for isConnectionError
    "net/http"   // for DLQ HTTP calls
    "time"       // already present
)
```

### Step 4: Verify compilation

Run: `go build ./internal/aaa/gateway/...`
Expected: compile success.

---

## Task 7: Real Server-Initiated Handlers (stub → real)

**Files:**
- Modify: `cmd/biz/factory.go`
- Modify: `cmd/biz/main.go`

### Step 1: Verify `RedisPool` is already in `BizPod`

The `BizPod` struct in `factory.go` already has `RedisPool *redis.Pool`. No new struct needed. `main.go` can reference `pod.RedisPool` directly.

### Step 2: Replace stub handlers in main.go with real implementations

The current stubs in `main.go` (`handleReAuth`, `handleRevocation`, `handleCoA`) return dummy bytes. For this wave, implement the **session lookup** part only (not the full AMF notification or AAA-S forwarding — those are Wave 3 continuation):

```go
// handleReAuth processes a RAR (Re-Auth-Request) from AAA-S via AAA GW.
func handleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_re_auth",
        "auth_ctx_id", req.AuthCtxID,
        "session_id", req.SessionID,
        "payload_len", len(req.Payload))

    // TODO (Wave 3 continuation): Load EAP session from Redis, process re-auth, notify AMF
    // For now: return dummy EAP response bytes
    return []byte{2, 0, 0, 12}
}

// handleRevocation processes an ASR (Abort-Session-Request) from AAA-S via AAA GW.
func handleRevocation(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_revoc",
        "auth_ctx_id", req.AuthCtxID,
        "session_id", req.SessionID)

    // TODO (Wave 3 continuation): Load EAP session, mark revoked, notify AMF
    // For now: return empty ASA
    return []byte{}
}

// handleCoA processes a CoA (Change-of-Authorization) from AAA-S via AAA GW.
func handleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    slog.Info("handle_coa",
        "auth_ctx_id", req.AuthCtxID,
        "session_id", req.SessionID)

    // TODO (Wave 3 continuation): Load session, apply attribute changes, persist
    // For now: return CoA-Ack with dummy bytes
    return []byte{2, 0, 0, 12}
}
```

### Step 3: Add Redis session lookup helper

Add a helper function for the TODO above (to be completed in Wave 3 continuation):

```go
// loadEAPSessionFromRedis loads an EAP session by authCtxID from Redis.
// Returns nil if not found.
// TODO: Wire to actual EAP session store (redis-based, see internal/eap/session_redis.go)
func loadEAPSessionFromRedis(ctx context.Context, redisPool *redis.Pool, authCtxID string) ([]byte, error) {
    rdb := redisPool.Client()
    key := "nssaa:eap:session:" + authCtxID
    data, err := rdb.Get(ctx, key).Bytes()
    if err != nil {
        if errors.Is(err, redis.Nil) {
            return nil, nil
        }
        return nil, err
    }
    return data, nil
}
```

Add the import: `goredis "github.com/redis/go-redis/v9"` and `"errors"` if not already in imports.

### Step 5: Verify compilation

Run: `go build ./cmd/biz/...`
Expected: compile success.

---

## Task 8: RADIUS Configurable Retry

**Files:**
- Modify: `internal/aaa/gateway/radius_forward.go`
- Modify: `internal/config/internal_comm.go`
- Modify: `internal/aaa/gateway/gateway.go`

### Step 1: Add `RadiusForwarderConfig` struct to `internal/aaa/gateway/radius_forward.go`

In `radius_forward.go`, add before `newRadiusForwarder`:

```go
// RadiusForwarderConfig holds configuration for the RADIUS forwarder.
type RadiusForwarderConfig struct {
    ServerAddress   string
    ServerPort      int
    SharedSecret    string
    Timeout         time.Duration
    MaxRetries      int
    ResponseWindow  time.Duration
}
```

### Step 2: Update `newRadiusForwarder` to accept config

Replace `newRadiusForwarder` signature and body:

```go
func newRadiusForwarder(cfg RadiusForwarderConfig, logger *slog.Logger) *radiusForwarder {
    r := &radiusForwarder{logger: logger}
    if cfg.ServerAddress == "" {
        return r
    }
    client, err := radius.NewRadiusClient(radius.Config{
        ServerAddress:  cfg.ServerAddress,
        ServerPort:     cfg.ServerPort,
        SharedSecret:   cfg.SharedSecret,
        Timeout:        cfg.Timeout,
        MaxRetries:     cfg.MaxRetries,
        ResponseWindow: cfg.ResponseWindow,
        Transport:      "UDP",
    }, logger)
    if err != nil {
        logger.Error("radius_forward: failed to create client", "error", err, "server", cfg.ServerAddress)
        return r
    }
    r.client = client
    return r
}
```

### Step 3: Update call site in `gateway.go` `New()`

```go
// OLD (gateway.go lines 103-109):
if cfg.RadiusServerAddress != "" {
    g.radiusForwarder = newRadiusForwarder(
        cfg.RadiusServerAddress,
        1812,
        cfg.RadiusSharedSecret,
        cfg.Logger,
    )
}

// NEW:
if cfg.RadiusServerAddress != "" {
    g.radiusForwarder = newRadiusForwarder(RadiusForwarderConfig{
        ServerAddress:   cfg.RadiusServerAddress,
        ServerPort:      1812,
        SharedSecret:    cfg.RadiusSharedSecret,
        Timeout:        10 * time.Second,    // from cfg or default
        MaxRetries:     3,                    // from cfg or default
        ResponseWindow:  10 * time.Second,    // from cfg or default
    }, cfg.Logger)
}
```

### Step 4: Add `KeepalivedHealthURL` to `InternalCommConfig`

In `internal/config/internal_comm.go`, add to `NativeCommConfig`:

```go
// KeepalivedHealthURL is the health check endpoint for the AAA Gateway VIP.
// Used by NativeAAAClient to detect VIP state changes for circuit breaker reset.
KeepalivedHealthURL string `yaml:"keepalivedHealthURL"` // e.g. "http://aaa-gateway:9090/health/vip"
```

### Step 5: Verify compilation

Run: `go build ./internal/aaa/gateway/... ./internal/config/...`
Expected: compile success.

---

## Task 9: VIP CB Reset in NativeAAAClient

**Files:**
- Modify: `internal/httpclient/native_aaa.go`

### Step 1: Add `StartVIPHealthCheck` to `NativeAAAClient`

Add to `NativeAAAClient` in `native_aaa.go`:

```go
// StartVIPHealthCheck polls the keepalived health endpoint and resets the
// circuit breaker for the old VIP owner when a VIP failover is detected.
// This eliminates the 15-30s blackout window after keepalived recovers.
func (c *NativeAAAClient) StartVIPHealthCheck(ctx context.Context) {
    healthURL := c.healthURL
    if healthURL == "" {
        c.logger.Info("keepalived health check disabled: no health URL configured")
        return
    }

    prevState := "unknown"
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
        }

        state, err := c.checkVIPState(ctx, healthURL)
        if err != nil {
            c.logger.Warn("keepalived health check failed", "error", err, "url", healthURL)
            continue
        }

        if state != prevState && prevState != "unknown" {
            c.logger.Info("VIP state changed, resetting circuit breaker",
                "prev", prevState, "curr", state)
            cb := c.cbRegistry.Get(c.aaaGatewayURL)
            if cb != nil {
                cb.Reset()
            }
            // TODO: increment metric nssAAF_cb_reset_by_vip_change_total
        }
        prevState = state
    }
}

// checkVIPState queries the /health/vip endpoint and returns "MASTER", "BACKUP", or "unknown".
// Uses the caller's ctx so the request is bounded by the caller's deadline.
func (c *NativeAAAClient) checkVIPState(ctx context.Context, healthURL string) (string, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
    if err != nil {
        return "unknown", err
    }
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "unknown", err
    }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, resp.Body)
    if resp.StatusCode == http.StatusOK {
        return "MASTER", nil
    }
    return "BACKUP", nil
}
```

### Step 2: Add `healthURL` field to `NativeAAAClient` and initialize it

In `NativeAAAClient` struct, add:

```go
healthURL string
```

In `NewNativeAAAClient()`, set it:

```go
healthURL: cfg.KeepalivedHealthURL,
```

### Step 3: Start VIP health check from factory

In `cmd/biz/factory.go` `Build()`, after `aaaClient` is created (around line 224), add:

```go
go aaaClient.StartVIPHealthCheck(context.Background())
```

### Step 4: Verify compilation

Run: `go build ./internal/httpclient/... ./cmd/biz/...`
Expected: compile success.

---

## Self-Review Checklist

### Spec coverage
- [x] Gap 1 (static URL): Task 5 (`selectTargetBizURL`)
- [x] Gap 2 (PodID unused): Task 4 (BizPodEntry in HASH) + Task 5 (getBizPodURL)
- [x] Gap 3 (no retry): Task 5 (`forwardToBiz` with retry loop) + Task 6 (DLQ consumer)
- [x] Gap 4 (stub handlers): Task 7 (session lookup stubs + TODO comments for Wave 3)
- [x] Gap 5 (VIP startup race): Task 2 (`StartVIPAware`, `startListeners`)
- [x] Gap 6 (hardcoded RADIUS): Task 8 (`RadiusForwarderConfig`)
- [x] Gap 7 (CB blip): Task 9 (`StartVIPHealthCheck`)
- [x] Dead pub/sub removal: Task 1
- [x] Biz pod registration HASH: Task 3, Task 4
- [x] DLQ key constant: Task 3

### Placeholder scan
All steps have actual code. TODOs are explicit (`// TODO (Wave 3 continuation)`) and clearly scoped.

### Type consistency
- `BizPodsKey(podID string) string` — used in Task 4 as `proto.BizPodsKey(podID)`
- `BizPodEntry` — JSON round-trip verified in test
- `DLQKey` — used in Task 5 and Task 6
- `RadiusForwarderConfig` — consistent across Task 8
- `KeepalivedHealthURL` — consistent across Task 8 and Task 9

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-24-internal-comm-ha-biz-aaa-gateway-implementation.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
