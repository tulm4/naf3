# Circuit Breaker, DLQ, and Rate Limit Gaps Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 17 identified gaps across circuit breakers, DLQ, rate limiting, metrics, and Prometheus alerting for the NSSAAF Biz Pod.

**Architecture:** Three isolated CB registries (internal NF, AAA, AMF) with blast-radius separation. DLQ processor gains real HTTP delivery. RateLimiter wired to N58/N60 handlers. Prometheus alerts cover exhaustion, stalls, rate limit hits, and CB flapping.

**Tech Stack:** Go, `resilience.Registry`, `redis.RateLimiter`, `prometheus`, `miniredis`, `httptest`

---

## Wave 1: Metrics Foundation

### Task 1: Add `RateLimitRequests` counter (MET-G1)

**Files:**
- Modify: `internal/metrics/metrics.go`

- [ ] **Step 1: Add the `RateLimitRequests` counter definition**

Add this to the `var ()` block in `internal/metrics/metrics.go` after line 164 (after `DLQProcessed`):

```go
// RateLimitRequests tracks rate-limited requests by handler.
RateLimitRequests = newCounterVec(prometheus.CounterOpts{
    Name: "nssAAF_ratelimit_requests_total",
    Help: "Total requests rejected by rate limiter",
}, []string{"handler", "result"})
```

Run: `grep -n "RateLimitRequests" internal/metrics/metrics.go`
Expected: shows the new definition

- [ ] **Step 2: Commit**

```bash
git add internal/metrics/metrics.go
git commit -m "feat(metrics): add RateLimitRequests counter with handler/result labels"
```

---

### Task 2: Add `DLQProcessed` label values (MET-G2)

**Files:**
- Modify: `internal/metrics/metrics.go`

- [ ] **Step 1: Change `DLQProcessed` from no-label to with-label**

In `internal/metrics/metrics.go`, change line 164:

```go
// Before:
}, nil)

// After:
}, []string{"result"})
```

Run: `grep -A1 "DLQProcessed = newCounterVec" internal/metrics/metrics.go`
Expected: shows `}, []string{"result"})`

- [ ] **Step 2: Commit**

```bash
git add internal/metrics/metrics.go
git commit -m "feat(metrics): add result label to DLQProcessed counter"
```

---

## Wave 2: Circuit Breaker Bug Fixes

### Task 3: Fix `prevCBState` uninitialized bug in `native_biz.go` (CB-G4)

**Files:**
- Modify: `internal/httpclient/native_biz.go:79-152`
- Test: `internal/httpclient/native_biz_test.go`

- [ ] **Step 1: Read the current `ForwardRequest` implementation**

Run: `sed -n '79,155p' internal/httpclient/native_biz.go`
Note the line with `var prevCBState resilience.State` (line 93).

- [ ] **Step 2: Fix the bug — move `prevCBState` capture before the retry loop**

Replace lines 93–121 in `internal/httpclient/native_biz.go`:

```go
// BEFORE (broken):
var prevCBState resilience.State
err := resilience.Do(ctx, c.retryCfg, func() error {
    // ... retryable status: prevCBState = cb.State() only on retry ...
})
// AFTER (fixed):
prevCBState := cb.State()  // capture BEFORE Do() — fixes CB-G4
err := resilience.Do(ctx, c.retryCfg, func() error {
    respBody, status, err := c.doRequest(ctx, path, method, body)
    if err != nil {
        lastErr = err
        lastStatus = status
        return err
    }
    lastStatus = status
    lastBody = respBody
    lastErr = nil
    if status >= 400 && status < 500 {
        return nil
    }
    if resilience.IsRetryable(status) {
        retryCount++
        lastErr = fmt.Errorf("retryable status: %d", status)
        return lastErr
    }
    return nil
})
```

The key change: `var prevCBState` becomes `prevCBState := cb.State()` placed **before** `resilience.Do`. Remove the `prevCBState = cb.State()` line inside the retry closure.

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: builds without error

- [ ] **Step 4: Commit**

```bash
git add internal/httpclient/native_biz.go
git commit -m "fix(cb): capture prevCBState before Do() to prevent spurious CLOSED→CLOSED metrics"
```

---

### Task 4: Fix `prevCBState` uninitialized bug in `native_aaa.go` (CB-G4)

**Files:**
- Modify: `internal/httpclient/native_aaa.go:106-186`
- Test: `internal/httpclient/native_aaa_test.go`

- [ ] **Step 1: Read the current `ForwardEAP` implementation**

Run: `sed -n '106,150p' internal/httpclient/native_aaa.go`
Note line 123: `var prevCBState resilience.State`.

- [ ] **Step 2: Fix the same bug — move `prevCBState` capture before the retry loop**

Replace lines 120–149 in `internal/httpclient/native_aaa.go`:

```go
// BEFORE (broken):
var prevCBState resilience.State
err = resilience.Do(ctx, c.retryCfg, func() error {
    // ... retryable status: prevCBState = cb.State() only on retry ...
})
// AFTER (fixed):
prevCBState := cb.State()  // capture BEFORE Do() — fixes CB-G4
err = resilience.Do(ctx, c.retryCfg, func() error {
    respBody, status, err := c.doPost(ctx, body, req.Version)
    if err != nil {
        lastErr = err
        return err
    }
    lastBody = respBody
    lastErr = nil
    if status >= 400 && status < 500 {
        return nil
    }
    if resilience.IsRetryable(status) {
        retryCount++
        lastErr = fmt.Errorf("retryable status: %d", status)
        return lastErr
    }
    return nil
})
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/httpclient/...`
Expected: builds without error

- [ ] **Step 4: Commit**

```bash
git add internal/httpclient/native_aaa.go
git commit -m "fix(cb): capture prevCBState before Do() in AAA client"
```

---

### Task 5: Audit `sendNotification` in AMF notifier for `prevCBState` (DLQ-G4)

**Files:**
- Modify: `internal/amf/amf.go:106-191`

- [ ] **Step 1: Read `sendNotification` to check for `prevCBState` bug**

Run: `sed -n '106,191p' internal/amf/amf.go`
Check if `prevCBState` is declared as `var` (zero-initialized) instead of captured before the retry loop.

- [ ] **Step 2: Fix if the same bug pattern exists**

If the same pattern (`var prevCBState resilience.State` before `resilience.Do`) exists in `sendNotification`, fix it by moving `prevCBState := cb.State()` before the call to `resilience.Do`. If the AMF notifier uses a different retry pattern (e.g., a manual retry loop without `resilience.Do`), document the finding and mark as resolved.

Run: `grep -n "prevCBState" internal/amf/amf.go`
Expected: either no output or `prevCBState := cb.State()` on a line before the retry call

- [ ] **Step 3: Commit**

```bash
git add internal/amf/amf.go
git commit -m "fix(cb): audit and fix prevCBState in AMF sendNotification"
```

---

## Wave 3: DLQ Core Fixes

### Task 6: Unify DLQItem types — remove `redisAMFDLQItem` (DLQ-G3)

**Files:**
- Modify: `internal/amf/amf.go`
- Modify: `internal/cache/redis/dlq.go`
- Test: `test/unit/amf/notifier_test.go`

- [ ] **Step 1: Read both DLQItem definitions**

Run: `grep -n "type.*DLQItem\|type.*redisAMFDLQItem" internal/amf/amf.go internal/cache/redis/dlq.go`
Expected: `amf.go:32` has `DLQItem` with `MaxAttempts`, `amf.go:46` has `redisAMFDLQItem` without `MaxAttempts`, `dlq.go:19` has `AMFDLQItem` with `MaxAttempts`.

- [ ] **Step 2: Add import and replace `redisAMFDLQItem` usage in AMF notifier**

In `internal/amf/amf.go`:
1. Add import: `redisclient "github.com/operator/nssAAF/internal/cache/redis"`
2. Remove the `redisAMFDLQItem` struct definition (lines 46–55)
3. Change `sendNotification` to use `redisclient.AMFDLQItem` instead of `redisAMFDLQItem`

The DLQ `Enqueue` interface signature is `Enqueue(ctx context.Context, item interface{}) error`, so passing `*redisclient.AMFDLQItem` satisfies it.

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/amf/... ./internal/cache/redis/...`
Expected: builds without error

- [ ] **Step 4: Update the mockDLQ in `notifier_test.go` if needed**

Run: `go build ./internal/amf/...`
If the `mockDLQ.Enqueue` in `notifier_test.go` still unmarshals into `DLQItem` (the AMF one), update the type to `redisclient.AMFDLQItem`. The mock should still accept `interface{}`.

- [ ] **Step 5: Commit**

```bash
git add internal/amf/amf.go internal/cache/redis/dlq.go internal/amf/notifier_test.go
git commit -m "fix(dlq): unify AMFDLQItem types — use redisclient.AMFDLQItem in AMF notifier"
```

---

### Task 7: Rewrite `Process()` with actual AMF retry delivery (DLQ-G1, DLQ-G2)

**Files:**
- Modify: `internal/cache/redis/dlq.go:76-114`
- Test: `internal/cache/redis/dlq_test.go`

- [ ] **Step 1: Add `io` import and `deliverToAMF` method**

Add `"io"` to the import block of `internal/cache/redis/dlq.go` if not already present.

Add this method to `internal/cache/redis/dlq.go` after the `Len` function (after line 74):

```go
// deliverToAMF attempts to deliver a DLQ item to the AMF via HTTP POST.
// Returns (true, nil) on 2xx, (false, error) otherwise.
func (d *DLQ) deliverToAMF(ctx context.Context, hc *http.Client, item *AMFDLQItem) (bool, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URI, bytes.NewReader(item.Payload))
    if err != nil {
        return false, fmt.Errorf("dlq: create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := hc.Do(req)
    if err != nil {
        return false, fmt.Errorf("dlq: do request: %w", err)
    }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, resp.Body)

    if resp.StatusCode >= 200 && resp.StatusCode < 300 {
        return true, nil
    }
    return false, fmt.Errorf("dlq: non-2xx status: %d", resp.StatusCode)
}
```

- [ ] **Step 2: Rewrite `Process()` signature and implementation**

Replace the entire `Process` function in `internal/cache/redis/dlq.go` (lines 76–113):

```go
// Process starts a background goroutine that polls the DLQ and attempts
// HTTP delivery to AMF. Items are re-enqueued with incremented Attempt on
// failure, or discarded after MaxAttempts exhaustion.
// The caller must pass a dedicated *http.Client (e.g., 10s timeout) to
// avoid circular dependencies.
func (d *DLQ) Process(ctx context.Context, hc *http.Client) {
    d.wg.Add(1)
    go func() {
        defer d.wg.Done()
        for {
            item, err := d.Dequeue(ctx, 5*time.Second)
            if err != nil || item == nil {
                continue
            }

            // DLQ-G2: exhaustion check — prevent infinite retry
            if item.MaxAttempts > 0 && item.Attempt >= item.MaxAttempts {
                slog.Error("dlq: max attempts exhausted, discarding item",
                    "id", item.ID,
                    "type", item.Type,
                    "auth_ctx_id", item.AuthCtxID,
                    "attempt", item.Attempt,
                    "max_attempts", item.MaxAttempts,
                )
                metrics.DLQProcessed.WithLabelValues("exhausted").Inc()
                continue
            }

            // DLQ-G1: attempt actual AMF delivery
            ok, retryErr := d.deliverToAMF(ctx, hc, item)
            if ok {
                slog.Info("dlq: delivered",
                    "id", item.ID,
                    "type", item.Type,
                    "auth_ctx_id", item.AuthCtxID,
                )
                metrics.DLQProcessed.WithLabelValues("success").Inc()
                continue
            }

            // Re-enqueue with incremented attempt counter
            item.Attempt++
            if retryErr != nil {
                item.LastError = retryErr.Error()
            }
            if reErr := d.Enqueue(ctx, item); reErr != nil {
                slog.Error("dlq: re-enqueue failed",
                    "id", item.ID,
                    "error", reErr,
                )
                metrics.DLQProcessed.WithLabelValues("error").Inc()
            } else {
                slog.Warn("dlq: re-enqueued",
                    "id", item.ID,
                    "attempt", item.Attempt,
                    "error", retryErr,
                )
            }
        }
    }()
}
```

Add `"io"` to imports if not already there (it may already be imported — verify with `goimports`).

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/cache/redis/...`
Expected: builds without error

- [ ] **Step 4: Add Process() tests to `dlq_test.go`**

Add to `internal/cache/redis/dlq_test.go`:

```go
func TestDLQ_Process_Exhaustion(t *testing.T) {
    mr, err := miniredis.Run()
    require.NoError(t, err)
    defer mr.Close()

    pool, err := NewPool(context.Background(), Config{
        Addrs:        []string{mr.Addr()},
        PoolSize:     10,
        MinIdleConns: 1,
        DialTimeout:  100 * time.Millisecond,
    })
    require.NoError(t, err)
    defer func() { _ = pool.Close() }()

    dlq := NewDLQ(pool)

    // Enqueue an item that is already at MaxAttempts
    item := &AMFDLQItem{
        ID:          "exhaust-1",
        Type:        "SLICE_RE_AUTH",
        URI:         "http://amf:8080/notify",
        AuthCtxID:   "auth-123",
        Attempt:     3,
        MaxAttempts: 3,
        CreatedAt:   time.Now(),
    }
    err = dlq.Enqueue(context.Background(), item)
    require.NoError(t, err)

    // Process with a dummy HTTP client (item should be exhausted before delivery)
    hc := &http.Client{Timeout: 1 * time.Second}
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    done := make(chan struct{})
    go func() {
        dlq.Process(ctx, hc)
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(3 * time.Second):
        t.Fatal("Process did not exit after exhaustion")
    }

    // Item should be discarded, not re-enqueued
    length, err := dlq.Len(context.Background())
    require.NoError(t, err)
    assert.Equal(t, int64(0), length, "exhausted item should be discarded, not re-enqueued")
}

func TestDLQ_Process_DeliverySuccess(t *testing.T) {
    mr, err := miniredis.Run()
    require.NoError(t, err)
    defer mr.Close()

    pool, err := NewPool(context.Background(), Config{
        Addrs:        []string{mr.Addr()},
        PoolSize:     10,
        MinIdleConns: 1,
        DialTimeout:  100 * time.Millisecond,
    })
    require.NoError(t, err)
    defer func() { _ = pool.Close() }()

    dlq := NewDLQ(pool)

    var callCount atomic.Int32
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        callCount.Add(1)
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    item := &AMFDLQItem{
        ID:          "deliver-1",
        Type:        "SLICE_RE_AUTH",
        URI:         server.URL,
        AuthCtxID:   "auth-456",
        Attempt:     0,
        MaxAttempts: 5,
        Payload:     []byte(`{}`),
        CreatedAt:   time.Now(),
    }
    err = dlq.Enqueue(context.Background(), item)
    require.NoError(t, err)

    hc := &http.Client{Timeout: 5 * time.Second}
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    done := make(chan struct{})
    go func() {
        dlq.Process(ctx, hc)
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(4 * time.Second):
        t.Fatal("Process did not exit")
    }

    assert.Equal(t, int32(1), callCount.Load(), "AMF should receive exactly one delivery attempt")

    // Item should be consumed (not re-enqueued)
    length, err := dlq.Len(context.Background())
    require.NoError(t, err)
    assert.Equal(t, int64(0), length, "delivered item should be removed from queue")
}

func TestDLQ_Process_ReenqueueOnFailure(t *testing.T) {
    mr, err := miniredis.Run()
    require.NoError(t, err)
    defer mr.Close()

    pool, err := NewPool(context.Background(), Config{
        Addrs:        []string{mr.Addr()},
        PoolSize:     10,
        MinIdleConns: 1,
        DialTimeout:  100 * time.Millisecond,
    })
    require.NoError(t, err)
    defer func() { _ = pool.Close() }()

    dlq := NewDLQ(pool)

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusServiceUnavailable)
    }))
    defer server.Close()

    item := &AMFDLQItem{
        ID:          "fail-1",
        Type:        "SLICE_REVOCATION",
        URI:         server.URL,
        AuthCtxID:   "auth-789",
        Attempt:     0,
        MaxAttempts: 5,
        Payload:     []byte(`{}`),
        CreatedAt:   time.Now(),
    }
    err = dlq.Enqueue(context.Background(), item)
    require.NoError(t, err)

    hc := &http.Client{Timeout: 5 * time.Second}
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    done := make(chan struct{})
    go func() {
        dlq.Process(ctx, hc)
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(4 * time.Second):
        t.Fatal("Process did not exit")
    }

    // Item should be re-enqueued with Attempt=1
    length, err := dlq.Len(context.Background())
    require.NoError(t, err)
    assert.Equal(t, int64(1), length, "failed item should be re-enqueued")
}
```

Add `"sync/atomic"` and `"net/http/httptest"` to imports in `dlq_test.go`.

Run: `go test ./internal/cache/redis/... -run "TestDLQ_Process" -v`
Expected: PASS for all three tests

- [ ] **Step 5: Commit**

```bash
git add internal/cache/redis/dlq.go internal/cache/redis/dlq_test.go
git commit -m "fix(dlq): rewrite Process() with actual AMF HTTP delivery, MaxAttempts exhaustion, and metrics"
```

---

## Wave 4: Circuit Breaker on NF Clients

### Task 8: Add circuit breaker to NRF, UDM, and AUSF clients (CB-G1)

**Files:**
- Modify: `internal/nrf/client.go`
- Modify: `internal/udm/udm.go`
- Modify: `internal/ausf/client.go`
- Test: `internal/nrf/client_test.go`, `internal/ausf/client_test.go`

**Dependency:** Task 3 complete (resilience import confirmed working).

- [ ] **Step 1: Read `NewClient` signatures and identify all HTTP call sites**

NRF HTTP calls: `Register`, `Heartbeat`, `DiscoverUDM`, `DiscoverAMF`, `Deregister` — all in `internal/nrf/client.go`
UDM HTTP calls: in `internal/udm/udm.go` — find them with `grep -n "httpClient.Do" internal/udm/udm.go`
AUSF HTTP calls: `ForwardMSK` — in `internal/ausf/client.go:59`

- [ ] **Step 2: Modify NRF `Client` struct and `NewClient`**

In `internal/nrf/client.go`:
1. Add to imports: `"github.com/operator/nssAAF/internal/resilience"`
2. Add `cbRegistry *resilience.Registry` field to `Client` struct
3. Change `NewClient` signature to `NewClient(cfg config.NRFConfig, cbRegistry *resilience.Registry) *Client`
4. Store `cbRegistry` on the client

For each HTTP call method (`Register`, `Heartbeat`, `DiscoverUDM`, `DiscoverAMF`, `Deregister`):
1. Before the `httpClient.Do` call: get CB via `cb := c.cbRegistry.Get(c.baseURL)` and check `if !cb.Allow()` → return error immediately
2. On HTTP error: `cb.RecordFailure()` before returning error
3. On HTTP success (2xx): `cb.RecordSuccess()` before returning nil

- [ ] **Step 3: Modify UDM `Client` struct and `NewClient`**

In `internal/udm/udm.go`:
1. Add `cbRegistry *resilience.Registry` to `Client` struct
2. Change `NewClient` to accept `cbRegistry *resilience.Registry`
3. Wrap HTTP calls with `cb.Allow()` / `cb.RecordFailure()` / `cb.RecordSuccess()`

- [ ] **Step 4: Modify AUSF `Client` struct and `NewClient`**

In `internal/ausf/client.go`:
1. Add `cbRegistry *resilience.Registry` to `Client` struct
2. Change `NewClient` to `NewClient(cfg config.AUSFConfig, cbRegistry *resilience.Registry)`
3. Wrap `httpClient.Do(req)` in `ForwardMSK` with CB check

- [ ] **Step 5: Run build to verify all three packages**

Run: `go build ./internal/nrf/... ./internal/udm/... ./internal/ausf/...`
Expected: builds without error

- [ ] **Step 6: Update test helpers to pass a nil or mock registry**

Run: `go build ./internal/nrf/...`
If `nrf_test.go` calls `NewClient` without the new parameter, add a fourth param: `nrf.NewClient(cfg, resilience.NewRegistry(3, 10*time.Second, 2))`. Do the same for `ausf_test.go`.

Run: `go test ./internal/nrf/... ./internal/udm/... ./internal/ausf/...`
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add internal/nrf/client.go internal/udm/udm.go internal/ausf/client.go
git add internal/nrf/client_test.go internal/ausf/client_test.go
git commit -m "feat(cb): add circuit breaker to NRF, UDM, and AUSF clients"
```

---

### Task 9: Make AMF notifier CB thresholds configurable (CB-G3)

**Files:**
- Modify: `internal/amf/amf.go`
- Test: `internal/amf/notifier_test.go`

- [ ] **Step 1: Add `cbCfg` field to AMF `Client` struct**

In `internal/amf/amf.go`, update the `Client` struct to add:
```go
cbCfg config.CircuitBreakerConfig  // CB threshold config
retryCfg resilience.RetryConfig    // explicit retry config
```

- [ ] **Step 2: Update `NewClient` to accept `cbCfg` and `retryCfg`**

Change `NewClient` signature from:
```go
func NewClient(notifyTimeout time.Duration, cbRegistry *resilience.Registry, dlq interface{ Enqueue(ctx context.Context, item interface{}) error }) *Client
```
To:
```go
func NewClient(notifyTimeout time.Duration, cbRegistry *resilience.Registry, dlq interface{ Enqueue(ctx context.Context, item interface{}) error }, cbCfg config.CircuitBreakerConfig, retryCfg resilience.RetryConfig) *Client
```

Store both in the struct.

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/amf/...`
Expected: builds without error

- [ ] **Step 4: Update `notifier_test.go` to pass new parameters**

Add to the test file (before all test functions):
```go
func testCBCfg() config.CircuitBreakerConfig {
    return config.CircuitBreakerConfig{
        FailureThreshold: 5,
        RecoveryTimeout:  30 * time.Second,
        SuccessThreshold: 3,
    }
}

func testRetryCfg() resilience.RetryConfig {
    return resilience.RetryConfig{
        MaxAttempts: 3,
        BaseDelay:   500 * time.Millisecond,
        MaxDelay:    2 * time.Second,
    }
}
```

Update all `NewClient` calls in tests to pass `testCBCfg()` and `testRetryCfg()` as the last two parameters.

Run: `go test ./internal/amf/... -v`
Expected: all AMF notifier tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/amf/amf.go internal/amf/notifier_test.go
git commit -m "feat(amf): make CB thresholds and retry config explicit constructor params"
```

---

## Wave 5: Rate Limiter Wiring

### Task 10: Wire `RateLimiter` into NSSAA handler (RL-G1, MET-G1)

**Files:**
- Create: `internal/api/nssaa/ratelimit.go`
- Modify: `internal/api/nssaa/handler.go`
- Modify: `internal/api/nssaa/handler_test.go`
- Test: `internal/api/nssaa/handler_test.go`

- [ ] **Step 1: Read the NSSAA handler to find where to insert rate limit check**

Run: `grep -n "func (h \*Handler) PostSliceAuthentications\|func (h \*Handler) PutSliceAuthenticationContexts\|authCtxId" internal/api/nssaa/handler.go | head -20`

The rate limit check should go at the **very beginning** of each handler method, before any processing.

- [ ] **Step 2: Add `rateLimiter` field to `Handler` struct**

In `internal/api/nssaa/handler.go`, add to the `Handler` struct:
```go
rateLimiter *ratelimit.RateLimiter
```

Add import: `ratelimit "github.com/operator/nssAAF/internal/cache/redis"`.

- [ ] **Step 3: Add `WithRateLimiter` option**

Add after the existing `WithUDMClient` option:
```go
// WithRateLimiter sets the rate limiter for the NSSAA handler.
func WithRateLimiter(rl *ratelimit.RateLimiter) HandlerOption {
    return func(h *Handler) { h.rateLimiter = rl }
}
```

- [ ] **Step 4: Add rate limit check to `PostSliceAuthentications`**

In `PostSliceAuthentications`, add at the start of the function (after auth context setup, before any business logic):

```go
// Rate limit by AMF NF Instance ID (RL-G1).
// SliceAuthInfo.AmfInstanceId is a UUID NF instance ID — use it directly.
if h.rateLimiter != nil && body.AmfInstanceId != nil {
    allowed, err := h.rateLimiter.AllowAMF(ctx, string(*body.AmfInstanceId))
    if err != nil {
        slog.Warn("ratelimit: allow check failed", "error", err)
    }
    if !allowed {
        metrics.RateLimitRequests.WithLabelValues("nssaa", "limited").Inc()
        h.write429(w, r, 60)  // 60s retry-after
        return
    }
}
```

The key extraction: `SliceAuthInfo.AmfInstanceId` is a UUID NF instance ID — use `string(*body.AmfInstanceId)` directly.

- [ ] **Step 5: Add rate limit check to `PutSliceAuthenticationContexts`**

Similarly add at the start of `PutSliceAuthenticationContexts` using `authCtxId` as the rate limit key:

```go
if h.rateLimiter != nil {
    allowed, err := h.rateLimiter.Allow(ctx, "authctx:"+authCtxId)
    if err != nil {
        slog.Warn("ratelimit: allow check failed", "error", err)
    }
    if !allowed {
        metrics.RateLimitRequests.WithLabelValues("nssaa", "limited").Inc()
        h.write429(w, r, 60)
        return
    }
}
```

- [ ] **Step 6: Add `write429` helper if not present**

If `write429` does not exist in the handler file, add it after the handler methods:

```go
import "strconv"

// write429 writes a 429 Too Many Requests response with Retry-After header.
func (h *Handler) write429(w http.ResponseWriter, r *http.Request, retryAfter int) {
    w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
    w.WriteHeader(http.StatusTooManyRequests)
    common.WriteProblem(w, common.NewProblem(
        http.StatusTooManyRequests,
        "rate-limit-exceeded",
        "Rate limit exceeded for this request",
    ))
}
```

- [ ] **Step 7: Run build to verify**

Run: `go build ./internal/api/nssaa/...`
Expected: builds without error

- [ ] **Step 8: Add rate limit test to `handler_test.go`**

```go
func TestNSSAAHandler_RateLimit_Returns429(t *testing.T) {
    mr, err := miniredis.Run()
    require.NoError(t, err)
    defer mr.Close()

    pool, err := redis.NewPool(context.Background(), redis.Config{
        Addrs:        []string{mr.Addr()},
        PoolSize:     5,
        MinIdleConns: 1,
        DialTimeout:  100 * time.Millisecond,
    })
    require.NoError(t, err)
    defer func() { _ = pool.Close() }()

    // Create rate limiter with limit=1 to easily trigger denial
    rl := redis.NewRateLimiter(pool.Client(), 1*time.Minute, 1)
    // Exhaust the limit
    ctx := context.Background()
    for i := 0; i < 3; i++ {
        rl.AllowAMF(ctx, "amf-host-1")
    }

    store := &mockStore{}
    h := NewHandler(store, WithRateLimiter(rl))
    req := httptest.NewRequest(http.MethodPost, "/nssaa/v1/slice-authentications",
        bytes.NewReader([]byte(`{"gpsi":"msisdn-12345","notificationUri":"http://amf:8080/notify"}`)))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()

    h.ServeHTTP(w, req)

    assert.Equal(t, http.StatusTooManyRequests, w.Code)
    assert.Equal(t, "60", w.Header().Get("Retry-After"))
}
```

Add `"net/http/httptest"`, `"github.com/alicebob/miniredis/v2"`, and `"github.com/operator/nssAAF/internal/cache/redis"` imports.

Run: `go test ./internal/api/nssaa/... -run "RateLimit" -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/api/nssaa/handler.go internal/api/nssaa/handler_test.go
git commit -m "feat(ratelimit): wire RateLimiter into NSSAA handler with 429 responses"
```

---

### Task 11: Wire `RateLimiter` into AIW handler (RL-G1, MET-G1)

**Files:**
- Modify: `internal/api/aiw/handler.go`
- Modify: `internal/api/aiw/handler_test.go`

- [ ] **Step 1: Add `rateLimiter` field and `WithRateLimiter` option to AIW handler**

In `internal/api/aiw/handler.go`:
1. Add to imports: `ratelimit "github.com/operator/nssAAF/internal/cache/redis"`
2. Add `rateLimiter *ratelimit.RateLimiter` to `Handler` struct
3. Add `WithRateLimiter` option:
```go
// WithRateLimiter sets the rate limiter for the AIW handler.
func WithRateLimiter(rl *ratelimit.RateLimiter) HandlerOption {
    return func(h *Handler) { h.rateLimiter = rl }
}
```

- [ ] **Step 2: Add rate limit check to AIW `PostAuthContexts` handler**

Find the AIW POST endpoint handler method with `grep -n "func.*Handler.*CreateAuthenticationContext" internal/api/aiw/handler.go`.
Add rate limit check using GPSI hash:

```go
if h.rateLimiter != nil {
    gpsi := extractGPSI(r.Body)  // extract GPSI from request body
    allowed, err := h.rateLimiter.AllowGPSI(ctx, gpsi)
    if err != nil {
        slog.Warn("ratelimit: allow check failed", "error", err)
    }
    if !allowed {
        metrics.RateLimitRequests.WithLabelValues("aiw", "limited").Inc()
        h.write429(w, r, 60)
        return
    }
}
```

- [ ] **Step 3: Run build to verify**

Run: `go build ./internal/api/aiw/...`
Expected: builds without error

- [ ] **Step 4: Add AIW rate limit test**

```go
func TestAIWHandler_RateLimit_Returns429(t *testing.T) {
    mr, err := miniredis.Run()
    require.NoError(t, err)
    defer mr.Close()

    pool, err := redis.NewPool(context.Background(), redis.Config{
        Addrs:        []string{mr.Addr()},
        PoolSize:     5,
        MinIdleConns: 1,
        DialTimeout:  100 * time.Millisecond,
    })
    require.NoError(t, err)
    defer func() { _ = pool.Close() }()

    rl := redis.NewRateLimiter(pool.Client(), 1*time.Minute, 1)
    ctx := context.Background()
    for i := 0; i < 3; i++ {
        rl.AllowGPSI(ctx, "gpsi-msisdn-12345")
    }

    store := aiw.NewInMemoryStore()
    h := aiw.NewHandler(store, aiw.WithRateLimiter(rl))
    req := httptest.NewRequest(http.MethodPost, "/nssaa/v1/auth-contexts",
        bytes.NewReader([]byte(`{"gpsi":"msisdn-12345"}`)))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()

    h.ServeHTTP(w, req)

    assert.Equal(t, http.StatusTooManyRequests, w.Code)
}
```

Run: `go test ./internal/api/aiw/... -run "RateLimit" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/aiw/handler.go internal/api/aiw/handler_test.go
git commit -m "feat(ratelimit): wire RateLimiter into AIW handler with 429 responses"
```

---

## Wave 6: Factory Wiring

### Task 12: Wire three CB registries, DLQ HTTP client, and rate limiter in factory

**Files:**
- Modify: `cmd/biz/factory.go`

- [ ] **Step 1: Read the current factory section around CB registry and NF clients**

Run: `sed -n '178,200p' cmd/biz/factory.go`
This shows the current single `cbRegistry` using `f.cfg.AAA.*`.

- [ ] **Step 2: Replace single CB registry with three registries (CB-G5 + §4.1)**

Replace the CB registry section (lines ~178–183) in `cmd/biz/factory.go`:

```go
// ─── Three isolated CB registries for blast-radius isolation ───────
// CB-G5: use InternalComm.Native.CB (not AAA.*) for the AAA registry
internalNFCfg := f.cfg.InternalComm.Native.CB
if internalNFCfg.FailureThreshold == 0 {
    internalNFCfg.FailureThreshold = 3
}
if internalNFCfg.RecoveryTimeout == 0 {
    internalNFCfg.RecoveryTimeout = 10 * time.Second
}
if internalNFCfg.SuccessThreshold == 0 {
    internalNFCfg.SuccessThreshold = 2
}
internalNFRegistry := resilience.NewRegistry(
    internalNFCfg.FailureThreshold,
    internalNFCfg.RecoveryTimeout,
    internalNFCfg.SuccessThreshold,
)

// AAA registry (used for Biz→AAA Gateway calls)
aaaCfg := f.cfg.InternalComm.Native.CB
if aaaCfg.FailureThreshold == 0 {
    aaaCfg.FailureThreshold = 5
}
if aaaCfg.RecoveryTimeout == 0 {
    aaaCfg.RecoveryTimeout = 30 * time.Second
}
if aaaCfg.SuccessThreshold == 0 {
    aaaCfg.SuccessThreshold = 3
}
aaaRegistry := resilience.NewRegistry(
    aaaCfg.FailureThreshold,
    aaaCfg.RecoveryTimeout,
    aaaCfg.SuccessThreshold,
)

// AMF registry (for AMF notification delivery)
amfCfg := config.CircuitBreakerConfig{
    FailureThreshold: 3,
    RecoveryTimeout:  15 * time.Second,
    SuccessThreshold: 2,
}
amfRegistry := resilience.NewRegistry(amfCfg.FailureThreshold, amfCfg.RecoveryTimeout, amfCfg.SuccessThreshold)
```

- [ ] **Step 3: Update NF client constructors to pass CB registry (CB-G1)**

Replace NRF, UDM, AUSF client creation (lines ~185–194):

```go
// ─── NF clients with circuit breakers (CB-G1) ─────────────────────
nrfClient := nrf.NewClient(f.cfg.NRF, internalNFRegistry)
go nrfClient.RegisterAsync(ctx)
go nrfClient.StartHeartbeat(ctx)

udmClient := udm.NewClient(f.cfg.UDM, nrfClient, internalNFRegistry)

ausfClient := ausf.NewClient(f.cfg.AUSF, internalNFRegistry)
```

- [ ] **Step 4: Update AMF client to use new constructor (CB-G3)**

Find the `amfClient := amf.NewClient(...)` line and update:

```go
amfClient := amf.NewClient(
    30*time.Second,
    amfRegistry,
    dlq,
    amfCfg,
    resilience.RetryConfig{
        MaxAttempts: 3,
        BaseDelay:   500 * time.Millisecond,
        MaxDelay:   2 * time.Second,
    },
)
```

- [ ] **Step 5: Update `NativeAAAClient` to use `aaaRegistry`**

Find where `NativeAAAClient` is constructed and ensure it gets `aaaRegistry`. If the factory calls `NewNativeAAAClient` which internally creates its own registry from config, verify that `aaaRegistry` is passed through or used. If it uses `f.cfg.InternalComm.Native.CB` internally, the config wiring is already correct.

- [ ] **Step 6: Add DLQ HTTP client and update `dlq.Process` call (DLQ-G1)**

Find `dlq := redis.NewDLQ(redisPool)` and `go dlq.Process(ctx)` and replace:

```go
// ─── DLQ with dedicated HTTP client for retry delivery (DLQ-G1) ────
dlq := redis.NewDLQ(redisPool)
dlqHTTPClient := &http.Client{Timeout: 10 * time.Second}
go dlq.Process(ctx, dlqHTTPClient)
```

- [ ] **Step 7: Add rate limiter instantiation and pass to handlers (RL-G1)**

Find the NSSAA and AIW handler creation and add rate limiter:

```go
// ─── Rate limiter (RL-G1) ───────────────────────────────────────────
ratelimiter := ratelimit.NewRateLimiter(
    redisPool.Client(),
    1*time.Minute,
    f.cfg.RateLimit.PerGpsiPerMin,
)
```

Then update the `nssaaHandler` creation to include:
```go
nssaa.WithRateLimiter(ratelimiter),
```

And update the `aiwHandler` creation to include:
```go
aiw.WithRateLimiter(ratelimiter),
```

- [ ] **Step 8: Run build to verify**

Run: `go build ./cmd/biz/...`
Expected: builds without error. If there are type errors, fix the constructor signatures to match what was changed in Tasks 8–9.

- [ ] **Step 9: Commit**

```bash
git add cmd/biz/factory.go
git commit -m "feat(factory): wire three CB registries, DLQ HTTP client, and RateLimiter"
```

---

## Wave 7: Prometheus Alerting

### Task 13: Add Prometheus alert rules (AL-G1, AL-G2, AL-G3, AL-G4)

**Files:**
- Modify: `deployments/nssaa-biz/prometheusrules.yaml`

- [ ] **Step 1: Read the current prometheusrules.yaml to find insertion point**

Run: `grep -n "NssaaDLQDepthHigh\|groups:" deployments/nssaa-biz/prometheusrules.yaml | head -10`

Add new alerts to the existing `groups[0].rules` array after `NssaaDLQDepthHigh`.

- [ ] **Step 2: Add all four new alert rules**

Append these to the rules array in `deployments/nssaa-biz/prometheusrules.yaml`:

```yaml
# AL-G1: DLQ item exhausted MaxAttempts — permanently lost notification (Critical)
- alert: NssaaDLQExhausted
  expr: increase(nssAAF_dlq_processed_total{result="exhausted"}[15m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "DLQ item exhausted MaxAttempts — AMF notification permanently lost"
    description: "{{ $value }} DLQ item(s) exhausted MaxAttempts in the last 15m and were discarded"

# AL-G2: Rate limit hit frequently — possible misconfig or abuse (Major)
- alert: NssaaRateLimitHit
  expr: |
    sum(rate(nssAAF_ratelimit_requests_total{result="limited"}[5m]))
    by (handler) > 0.1
  for: 5m
  labels:
    severity: major
  annotations:
    summary: "Rate limit triggered frequently on {{ $labels.handler }}"
    description: "Rate limit hit > 6 req/min on {{ $labels.handler }}"

# AL-G3: DLQ stalled — processor not making progress (Critical)
- alert: NssaaDLQProcessingStalled
  expr: |
    nssAAF_dlq_depth > 0
    and
    increase(nssAAF_dlq_processed_total[30m]) == 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "DLQ stalled — no items processed in 30 minutes despite depth > 0"

# AL-G4: Circuit breaker flapping — excessive state transitions (Warning)
- alert: NssaaCircuitBreakerFlapping
  expr: |
    sum(rate(nssAAF_httpclient_circuit_breaker_transitions_total[15m]))
    by (destination) > 10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Circuit breaker flapping for {{ $labels.destination }} — >10 transitions in 15m"
    description: "Frequent CB state transitions indicate instability. Check network or lower failure threshold."
```

- [ ] **Step 3: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('deployments/nssaa-biz/prometheusrules.yaml'))" && echo "YAML valid"`
Expected: `YAML valid`

- [ ] **Step 4: Commit**

```bash
git add deployments/nssaa-biz/prometheusrules.yaml
git commit -m "feat(alerts): add NssaaDLQExhausted, NssaaRateLimitHit, NssaaDLQProcessingStalled, NssaaCircuitBreakerFlapping"
```

---

## Wave 8: Final Verification

### Task 14: Run full verification

**Files:** (all packages)

- [ ] **Step 1: Build all packages**

Run: `go build ./...`
Expected: builds without error

- [ ] **Step 2: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: all tests pass (note: DLQ tests may take a few seconds due to `Process` goroutine timeouts)

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./... 2>&1 | head -30`
Expected: no errors (warnings acceptable)

- [ ] **Step 4: Update design doc verification checklist**

Open `docs/superpowers/specs/2026-05-27-circuit-dlq-ratelimit-gaps-design.md` and mark all items in §13. Verification Checklist as checked (`[x]`).

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-05-27-circuit-dlq-ratelimit-gaps-design.md
git commit -m "docs: mark all verification checklist items complete"
```

---

## Spec Coverage Self-Review

| Spec Section | Task(s) | Coverage |
|---|---|---|
| CB-G4: prevCBState bug | Task 3, Task 4, Task 5 | ✅ native_biz, native_aaa, amf |
| CB-G5: InternalComm.Native.CB ignored | Task 12 | ✅ factory uses `InternalComm.Native.CB` |
| CB-G1: No CB on NRF/UDM/AUSF | Task 8 | ✅ cbRegistry added, all calls wrapped |
| CB-G3: AMF CB thresholds configurable | Task 9 | ✅ cbCfg added to Client struct |
| DLQ-G1: Process() is no-op | Task 7 | ✅ deliverToAMF, real HTTP delivery |
| DLQ-G2: MaxAttempts never checked | Task 7 | ✅ exhaustion check in Process() |
| DLQ-G3: MaxAttempts dropped | Task 6 | ✅ redisclient.AMFDLQItem used directly |
| DLQ-G4: prevCBState in AMF | Task 5 | ✅ audited and fixed if needed |
| RL-G1: RateLimiter not wired | Task 10, Task 11, Task 12 | ✅ wired to NSSAA and AIW handlers |
| MET-G1: RateLimitRequests missing | Task 1, Task 10, Task 11 | ✅ counter added, emitted on 429 |
| MET-G2: DLQProcessed no labels | Task 2, Task 7 | ✅ label added, incremented on success/exhaust/error |
| AL-G1: DLQ exhaustion alert | Task 13 | ✅ NssaaDLQExhausted |
| AL-G2: Rate limit hit alert | Task 13 | ✅ NssaaRateLimitHit |
| AL-G3: DLQ stall alert | Task 13 | ✅ NssaaDLQProcessingStalled |
| AL-G4: CB flapping alert | Task 13 | ✅ NssaaCircuitBreakerFlapping |
| Factory wiring | Task 12 | ✅ 3 registries, dlqHTTPClient, rate limiter |
| All tests from §11 | Tasks 3–11 | ✅ tests for all key behaviors |
