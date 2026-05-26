# Circuit Breaker, DLQ, and Rate Limit Gaps Fix

**Date:** 2026-05-27
**Status:** Draft
**Phase:** Internal Communication HA — Post-Implementation Gap Fix

---

## 1. Overview

After a detailed codebase audit of the circuit breaker, dead-letter queue (DLQ), and rate limiter implementations, several gaps were identified that reduce the resilience and reliability of the NSSAAF Biz Pod. This design addresses all gaps systematically.

---

## 2. Gaps Identified

### 2.1 Circuit Breaker Gaps

| ID | Severity | Description |
|----|----------|-------------|
| CB-G1 | Critical | UDM, AUSF, NRF clients have no circuit breaker protection |
| CB-G3 | Medium | AMF notifier circuit breaker thresholds not configurable |
| CB-G4 | Bug | `prevCBState` uninitialized before use — causes spurious CB metric transitions |
| CB-G5 | High | `InternalComm.Native.CB` config path ignored; factory uses `AAA.*` instead |

### 2.2 DLQ Gaps

| ID | Severity | Description |
|----|----------|-------------|
| DLQ-G1 | Critical | `Process()` only logs and re-enqueues — no actual AMF retry delivery |
| DLQ-G2 | Critical | `MaxAttempts` and `Attempt` fields never checked — infinite retry possible |
| DLQ-G3 | Medium | `MaxAttempts` dropped during `DLQItem` → `redisAMFDLQItem` conversion |
| DLQ-G4 | Bug | `prevCBState` uninitialized in AMF notifier |

### 2.3 Rate Limit Gaps

| ID | Severity | Description |
|----|----------|-------------|
| RL-G1 | High | `RateLimiter` fully implemented but never wired into HTTP handlers |

---

## 3. Architecture

### 3.1 Three CB Registries

Three separate `resilience.Registry` instances with different thresholds for blast-radius isolation:

| Registry | Consumers | FailureThreshold | RecoveryTimeout | SuccessThreshold |
|----------|-----------|-----------------|-----------------|-----------------|
| `internalNFRegistry` | NRF, UDM, AUSF clients | 3 | 10s | 2 |
| `aaaRegistry` | AAA Gateway HTTP client | 5 | 30s | 3 |
| `amfRegistry` | AMF notification client | 3 | 15s | 2 |

### 3.2 Data Flow

```
HTTP Request
    │
    ▼
RateLimiter (Redis sliding window)
    │  ── 429 Too Many Requests (over limit)
    ▼
N58/N60 Handler
    │
    ├───► NRF Client  ──► CB (internalNFRegistry) ──► retry
    ├───► UDM Client  ──► CB (internalNFRegistry) ──► retry
    ├───► AUSF Client ──► CB (internalNFRegistry) ──► retry
    └───► AMF Notifier ──► CB (amfRegistry) ──► retry ──► DLQ (on exhaustion)
                                                                    │
                                                                    ▼
                                                        DLQ Processor (Biz Pod goroutine)
                                                            │
                                                            ├───► Retry HTTP POST to AMF URI
                                                            ├───► Success: discard
                                                            └───► Exhausted: log critical, discard
```

---

## 4. Circuit Breaker Fixes

### 4.1 CB-G4: Fix `prevCBState` Uninitialized Bug

**Files:** `internal/httpclient/native_biz.go`, `internal/httpclient/native_aaa.go`, `internal/amf/amf.go`

**Problem:** `prevCBState` is declared to `resilience.State` (zero value = `StateClosed`). If no retryable error occurs, the metric check `prevCBState != currCBState` fires spuriously, emitting a `CLOSED → CLOSED` transition metric.

**Fix:** Capture the circuit state *before* the retry loop starts:

```go
// Before (native_biz.go)
var prevCBState resilience.State  // always 0 (CLOSED) if no retries
err := resilience.Do(...)
if err != nil {
    // prevCBState == CLOSED, currCBState == CLOSED → spurious metric
}

// After
prevCBState := cb.State()  // capture BEFORE Do()
err := resilience.Do(...)
```

### 4.2 CB-G5: Wire `InternalComm.Native.CB` Config Into Factory

**File:** `cmd/biz/factory.go`

**Problem:** `cbRegistry` is created using `f.cfg.AAA.FailureThreshold` etc., ignoring the canonical `InternalCommConfig.Native.CB` path.

**Fix:** Change factory to use `f.cfg.InternalComm.Native.CB.*`:

```go
cbCfg := f.cfg.InternalComm.Native.CB
if cbCfg.FailureThreshold == 0 {
    cbCfg.FailureThreshold = 5
}
if cbCfg.RecoveryTimeout == 0 {
    cbCfg.RecoveryTimeout = 30 * time.Second
}
if cbCfg.SuccessThreshold == 0 {
    cbCfg.SuccessThreshold = 3
}
aaaRegistry := resilience.NewRegistry(cbCfg.FailureThreshold, cbCfg.RecoveryTimeout, cbCfg.SuccessThreshold)
```

### 4.3 CB-G1: Add Circuit Breaker to NRF, UDM, AUSF Clients

**Files:** `internal/nrf/nrf.go`, `internal/udm/udm.go`, `internal/ausf/ausf.go`

**Problem:** These clients make outbound HTTP calls with no circuit breaker. If NRF is unavailable, NSSAAF continuously retries at the OS level.

**Fix:** Add `cbRegistry` to each client and wrap outbound calls with CB check:

```go
// NRF client
type Client struct {
    baseURL    string
    httpClient *http.Client
    cbRegistry *resilience.Registry  // NEW
    cbKey      string               // NEW: base URL as CB key
}

// UDM client
type Client struct {
    baseURL    string
    nrfClient  *nrf.Client
    httpClient *http.Client
    cbRegistry *resilience.Registry  // NEW
}

// AUSF client
type Client struct {
    baseURL    string
    httpClient *http.Client
    cbRegistry *resilience.Registry  // NEW
}
```

Each NF client's `Discover*` or HTTP call methods:
1. Call `cb.Allow()` — if open, return error immediately
2. On success: `cb.RecordSuccess()`
3. On failure: `cb.RecordFailure()` before returning error

### 4.4 CB-G3: Make AMF Notifier CB Thresholds Configurable

**File:** `internal/amf/amf.go`

**Problem:** AMF notifier has no CB threshold configuration — only retry count is configurable via `maxRetries`.

**Fix:** Add `CircuitBreakerConfig` to `Client` struct:

```go
type Client struct {
    httpClient    *http.Client
    cbRegistry    *resilience.Registry
    cbCfg         config.CircuitBreakerConfig  // NEW
    dlq           interface{ Enqueue(ctx context.Context, item interface{}) error }
    notifyTimeout time.Duration
    maxRetries    int
    retryCfg      resilience.RetryConfig  // NEW: make retry config explicit
}
```

Constructor takes `cbCfg config.CircuitBreakerConfig` and uses it when creating per-host CB entries.

---

## 5. DLQ Fixes

### 5.1 DLQ-G1 + DLQ-G2: Rewrite `Process()` With Actual Retry Delivery

**File:** `internal/cache/redis/dlq.go`

**Problem:** `Process()` dequeues, logs, and immediately re-enqueues — a no-op that never makes progress.

**Note on HTTP client:** The DLQ processor needs its own `*http.Client` to avoid a circular dependency (DLQ can't hold a reference to the AMF notifier, which holds a reference to the DLQ). The factory creates a dedicated HTTP client for DLQ retry delivery with a 10-second timeout.

**Fix:** `Process()` becomes:

```go
func (d *DLQ) Process(ctx context.Context, hc *http.Client) {
    d.wg.Add(1)
    go func() {
        defer d.wg.Done()
        for {
            item, err := d.Dequeue(ctx, 5*time.Second)
            if err != nil || item == nil {
                continue
            }

            // Exhaustion check (DLQ-G2)
            if item.Attempt >= item.MaxAttempts {
                slog.Error("dlq: max attempts exhausted, discarding",
                    "id", item.ID,
                    "type", item.Type,
                    "auth_ctx_id", item.AuthCtxID,
                    "attempt", item.Attempt,
                )
                metrics.DLQProcessed.WithLabelValues("exhausted").Inc()
                continue
            }

            // Attempt delivery
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

            // Re-enqueue with incremented attempt
            item.Attempt++
            item.LastError = retryErr.Error()
            if reErr := d.Enqueue(ctx, item); reErr != nil {
                slog.Error("dlq: re-enqueue failed",
                    "id", item.ID,
                    "error", reErr,
                )
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

func (d *DLQ) deliverToAMF(ctx context.Context, hc *http.Client, item *AMFDLQItem) (bool, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URI, bytes.NewReader(item.Payload))
    if err != nil {
        return false, fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := hc.Do(req)
    if err != nil {
        return false, fmt.Errorf("do request: %w", err)
    }
    defer resp.Body.Close()
    _, _ = io.Copy(io.Discard, resp.Body)

    if resp.StatusCode >= 200 && resp.StatusCode < 300 {
        return true, nil
    }
    return false, fmt.Errorf("non-2xx status: %d", resp.StatusCode)
}

### 5.2 DLQ-G3: Fix `MaxAttempts` Field Drop in Conversion

**Files:** `internal/amf/amf.go`, `internal/cache/redis/dlq.go`

**Problem:** `amf.DLQItem` has `MaxAttempts` but `redisAMFDLQItem` drops it during conversion. Even if `Process()` checked `MaxAttempts`, it would always see 0.

**Fix:** Unify `DLQItem` types. Remove `redisAMFDLQItem` from `amf/amf.go` entirely. Have `amf/amf.go` import `redis.AMFDLQItem` directly:

```go
// internal/amf/amf.go
import (
    redisclient "github.com/operator/nssAAF/internal/cache/redis"
)

// Use redisclient.AMFDLQItem directly — no conversion needed
item := &redisclient.AMFDLQItem{...}
if dlqErr := c.dlq.Enqueue(ctx, item); dlqErr != nil {
    ...
}
```

The single `AMFDLQItem` in `redis/dlq.go` becomes the canonical type.

---

## 6. Rate Limit Wiring

### 6.1 RL-G1: Wire `RateLimiter` Into HTTP Handlers

**Files:** `internal/api/nssaa/options.go`, `internal/api/aiw/options.go`, `cmd/biz/factory.go`

**Problem:** `redis.RateLimiter` is fully implemented but never instantiated or applied.

**Fix:** Add handler options and factory wiring:

**Handler option:**
```go
// internal/api/nssaa/options.go
type handlerOptions struct {
    // ... existing fields ...
    rateLimiter *ratelimit.RateLimiter
}

// WithRateLimiter configures the rate limiter for the NSSAA handler.
func WithRateLimiter(rl *ratelimit.RateLimiter) HandlerOption {
    return func(o *handlerOptions) {
        o.rateLimiter = rl
    }
}
```

**Factory wiring:**
```go
// cmd/biz/factory.go
ratelimiter := ratelimit.NewRateLimiter(
    redisPool.Client(),
    1*time.Minute,        // window
    cfg.RateLimit.PerGpsiPerMin,
)

nssaaHandler := nssaa.NewHandler(nssaaStore,
    nssaa.WithAPIRoot(apiRoot),
    nssaa.WithAAA(aaaClient),
    nssaa.WithNRFClient(nrfClient),
    nssaa.WithUDMClient(udmClient),
    nssaa.WithRateLimiter(ratelimiter),   // NEW
)
```

**Application in N58 handler:** Before processing `POST /slice-authentications` or `PUT .../{authCtxId}`, call `rateLimiter.Allow()` with a key derived from the AMF host or authCtxId. On denial, return HTTP `429` with `Retry-After` header and a ProblemDetails body.

**Rate limit key strategy:**
- N58 POST (new session): use AMF host hash via `AllowAMF(ctx, amfHost)`
- N58 PUT (update session): use `authCtxId` hash
- N60 POST (AIW auth): use GPSI hash via `AllowGPSI(ctx, gpsiHash)`

---

## 7. Factory Changes

### 7.1 New Registry Instantiation

In `cmd/biz/factory.go`, replace the single `cbRegistry` with three registries:

```go
// ─── Resilience registries ────────────────────────────────────────

// Internal NF registry: aggressive thresholds for NRF/UDM/AUSF
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

// AAA registry: standard thresholds
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

// AMF registry: moderate thresholds
amfCfg := config.CircuitBreakerConfig{
    FailureThreshold: 3,
    RecoveryTimeout:  15 * time.Second,
    SuccessThreshold: 2,
}

// ─── NF clients with CB ──────────────────────────────────────────
nrfClient := nrf.NewClient(f.cfg.NRF, internalNFRegistry)
udmClient := udm.NewClient(f.cfg.UDM, nrfClient, internalNFRegistry)
ausfClient := ausf.NewClient(f.cfg.AUSF, internalNFRegistry)
amfClient := amf.NewClient(30*time.Second, amfRegistry, amfCfg, dlq)

// ─── HTTP AAA client with CB ─────────────────────────────────────
aaaClient := newHTTPAAAClient(f.cfg.Biz.AAAGatewayURL, ..., aaaRegistry)

// ─── DLQ HTTP client for retry delivery ───────────────────────────
dlqHTTPClient := &http.Client{Timeout: 10 * time.Second}

// ─── Rate limiter ────────────────────────────────────────────────
ratelimiter := ratelimit.NewRateLimiter(
    redisPool.Client(),
    1*time.Minute,
    f.cfg.RateLimit.PerGpsiPerMin,
)

dlq := redis.NewDLQ(redisPool)
go dlq.Process(ctx, dlqHTTPClient)```

---

## 8. New Dependencies

No new external dependencies required. All implementations already exist:
- `resilience.Registry` and `resilience.CircuitBreaker` — existing
- `redis.DLQ` — existing, just fixed
- `redis.RateLimiter` — existing, just wired
- `metrics.DLQProcessed` — new counter label values only

---

## 9. Test Coverage

| Test | Scope |
|------|-------|
| Unit: CB prevCBState fix | `native_biz_test.go`, `native_aaa_test.go`, `amf/notifier_test.go` |
| Unit: AMF notifier CB config | `amf/notifier_test.go` |
| Unit: NRF/UDM/AUSF CB wrapping | `nrf/nrf_test.go`, `udm/udm_test.go`, `ausf/ausf_test.go` |
| Unit: DLQ Process retry logic | `redis/dlq_test.go` |
| Unit: DLQ MaxAttempts exhaustion | `redis/dlq_test.go` |
| Unit: DLQItem unification | `redis/dlq_test.go` |
| Unit: RateLimiter wiring | `nssaa/handler_test.go` |
| Integration: DLQ delivery to mock AMF | `integration/redis_test.go` |
| Integration: Rate limit returns 429 | `integration/` |

---

## 10. Files Changed

| File | Change |
|------|--------|
| `internal/resilience/circuit_breaker.go` | No change (already correct) |
| `internal/httpclient/native_biz.go` | Fix prevCBState bug |
| `internal/httpclient/native_aaa.go` | Fix prevCBState bug |
| `internal/amf/amf.go` | Fix prevCBState; add CB config; remove redisAMFDLQItem |
| `internal/nrf/nrf.go` | Add cbRegistry parameter, wrap calls with CB |
| `internal/udm/udm.go` | Add cbRegistry parameter, wrap calls with CB |
| `internal/ausf/ausf.go` | Add cbRegistry parameter, wrap calls with CB |
| `internal/cache/redis/dlq.go` | Rewrite Process(); fix MaxAttempts; unify DLQItem |
| `internal/cache/redis/ratelimit.go` | No change |
| `internal/api/nssaa/options.go` | Add WithRateLimiter option |
| `internal/api/nssaa/handler.go` | Apply rate limit before processing |
| `internal/api/aiw/options.go` | Add WithRateLimiter option |
| `internal/api/aiw/handler.go` | Apply rate limit before processing |
| `cmd/biz/factory.go` | Wire 3 CB registries, rate limiter, update NF client constructors |
| `internal/metrics/metrics.go` | Add DLQProcessed label values |
| `internal/config/config.go` | No change (RateLimitConfig already exists) |
| `test/unit/resilience/circuit_breaker_test.go` | Add prevCBState test cases |
| `test/unit/amf/notifier_test.go` | Add CB config test, prevCBState test |
| `test/unit/nrf/nrf_test.go` | Add CB wrapping test |
| `test/unit/udm/udm_test.go` | Add CB wrapping test |
| `test/unit/ausf/ausf_test.go` | Add CB wrapping test |
| `test/unit/redis/dlq_test.go` | Rewrite Process() tests; add exhaustion test |
| `test/unit/api/nssaa/handler_test.go` | Add rate limit 429 test |
| `test/integration/redis_test.go` | Add DLQ delivery integration test |

---

## 11. Verification Checklist

- [ ] `go build ./...` compiles without errors
- [ ] `go test ./internal/resilience/...` passes (CB tests)
- [ ] `go test ./internal/amf/...` passes (AMF notifier tests)
- [ ] `go test ./internal/nrf/...` passes (NRF CB tests)
- [ ] `go test ./internal/udm/...` passes (UDM CB tests)
- [ ] `go test ./internal/ausf/...` passes (AUSF CB tests)
- [ ] `go test ./internal/cache/redis/...` passes (DLQ + rate limit tests)
- [ ] `go test ./internal/api/nssaa/...` passes (NSSAA handler tests)
- [ ] `go test ./internal/api/aiw/...` passes (AIW handler tests)
- [ ] `golangci-lint run ./...` passes
- [ ] `make test-e2e` passes (E2E harness)
