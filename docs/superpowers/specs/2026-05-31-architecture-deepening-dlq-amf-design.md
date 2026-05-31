# Architecture Deepening: DLQ Resilience + NF Client Alignment

**Date:** 2026-05-31
**Status:** Draft
**Scope:** `internal/cache/redis/dlq.go`, `internal/amf/amf.go`, `internal/nfclient/factory.go`

---

## Context

During architecture review of the NSSAAF codebase, two architectural deepening opportunities were identified:

1. **DLQ Infinite Retry Loop**: When Redis is unavailable during `Enqueue`, items are silently dropped — causing permanent data loss
2. **AMF Client Duplication**: AMF has its own inline HTTP transport while UDM/AUSF/NRF use `nfclient.Factory`

These are low-risk, high-leverage improvements that eliminate architectural inconsistency.

---

## Problem Statement

### Problem 1: DLQ Silent Data Loss

**Location:** `internal/cache/redis/dlq.go:153-159`

```go
if reErr := d.Enqueue(innerCtx, item); reErr != nil {
    slog.Error("dlq: re-enqueue failed", "id", item.ID, "error", reErr)
    metrics.DLQProcessed.WithLabelValues("error").Inc()
}
```

When `Enqueue` fails (e.g., Redis unavailable), the item is **silently dropped**. No retry, no fallback, no secondary queue.

**Impact**: Permanent data loss for AMF notifications during Redis outages.

**Also identified**: Tight-spin goroutine on rapid context cancellation — loop spins without sleep when `Dequeue` returns nil, consuming 100% CPU.

### Problem 2: AMF HTTP Transport Duplication

**Location:** `internal/amf/amf.go:53-57`

```go
httpClient: &http.Client{
    Timeout:   timeout,
    Transport: otelhttp.NewTransport(http.DefaultTransport),
},
```

AMF duplicates the HTTP transport wiring that `nfclient.Factory` already provides. Three other NF clients (UDM, AUSF, NRF) correctly use the factory.

**Impact**: Inconsistent resilience patterns, duplicated code, missing OTel instrumentation for AMF-specific paths.

---

## Solution

### Fix 1: DLQ Enqueue Retry Loop

Add bounded retry with exponential backoff for `Enqueue` failures in `DLQ.Process()`:

```go
// Re-enqueue with retry
const enqueueRetryMax = 5
var enqueueErr error
for attempt := 0; attempt < enqueueRetryMax; attempt++ {
    if err := d.Enqueue(innerCtx, item); err == nil {
        enqueueErr = nil
        break
    }
    enqueueErr = err
    if attempt < enqueueRetryMax-1 {
        delay := time.Duration(1<<attempt) * 200 * time.Millisecond
        time.Sleep(delay)
    }
}
if enqueueErr != nil {
    slog.Error("dlq: re-enqueue failed after all retries",
        "id", item.ID, "error", enqueueErr)
    metrics.DLQProcessed.WithLabelValues("error").Inc()
}
```

**Parameters:**
- Max attempts: 5
- Backoff: 200ms, 400ms, 800ms, 1600ms, 3200ms (total ~6.2s max)
- On exhaustion: log error, increment metric, discard item

### Fix 2: DLQ Tight-Spin Prevention

Add sleep in the `Dequeue` error path:

```go
select {
case <-stop:
    return
case <-innerCtx.Done():
    return
default:
    time.Sleep(250 * time.Millisecond)
}
```

### Fix 3: AMF Full Factory Alignment

Wire AMF through `nfclient.Factory` for HTTP transport:

**Before:**
```go
type Client struct {
    httpClient    *http.Client  // inline
    cbRegistry    *resilience.Registry
    // ...
}
```

**After:**
```go
type Client struct {
    factory       *nfclient.Factory  // shared
    cbRegistry    *resilience.Registry
    // ...
}
```

AMF keeps its notification-specific logic (DLQ, retry semantics, status code handling) but uses factory for:
- OTel-instrumented transport
- Circuit breaker guards
- Consistent timeout management

---

## Architecture

### DLQ Retry Flow

```
AMF notification fails
    ↓
resilience.Do retries (3x, 1s/2s/4s backoff)
    ↓
On exhaustion → Enqueue to DLQ
    ↓
DLQ.Process() goroutine picks up item
    ↓
deliverToAMF() fails
    ↓
Enqueue with retry (5x, 200ms/400ms/800ms/1600ms/3200ms backoff)
    ↓
On success → continue processing next item
On failure → log error, discard item
```

### AMF Alignment

```
Before:                                    After:
┌──────────────┐                          ┌──────────────┐
│ AMF.Client   │                          │ AMF.Client   │
│   - http.Client (inline)                │   - factory   │
│   - cbRegistry                           │   - cbRegistry
│   - retry logic                          │   - retry logic
└──────────────┘                          └──────────────┘
         ↓                                        ↓
┌──────────────────┐                   ┌──────────────────┐
│ Inline HTTP POST  │                   │ nfclient.Factory │
│ OTel (manual)    │                   │   - OTel auto    │
│ CB (manual)      │                   │   - CB via factory│
└──────────────────┘                   └──────────────────┘

vs.

┌──────────────────┐
│ UDM/AUSF/NRF     │
│   - factory      │
└────────┬─────────┘
         ↓
┌──────────────────┐
│ nfclient.Factory │
│   - OTel auto    │
│   - CB via factory│
└──────────────────┘
```

---

## Components

### 1. DLQ Retry (`internal/cache/redis/dlq.go`)

| Component | Change |
|-----------|--------|
| `DLQ.deliverToAMF()` | No change |
| `DLQ.Process()` goroutine | Add retry loop for `Enqueue` failures, add sleep in tight-spin path |

### 2. AMF Factory (`internal/amf/amf.go`)

| Component | Change |
|-----------|--------|
| `Client` struct | Replace `*http.Client` with `*nfclient.Factory` |
| `NewClient()` | Accept `*nfclient.Factory` instead of timeout |
| `sendNotification()` | Use `factory.Do()` for HTTP request |

### 3. Factory Enhancement (`internal/nfclient/factory.go`)

| Component | Change |
|-----------|--------|
| `Factory.Do()` | Already handles OTel + CB + transport — no changes needed |

---

## Data Flow

### DLQ Process Flow

```
1. DLQ.Process() starts goroutine
2. Dequeue() blocks on BRPOP (1s timeout)
3. Item received → check MaxAttempts
4. deliverToAMF() → success? continue : retry
5. On delivery failure:
   a. Increment Attempt counter
   b. Enqueue with retry (5 attempts, backoff)
   c. On success → continue
   d. On failure → log error, discard
6. Loop to step 2
```

### AMF Notification Flow

```
1. Handler calls SendReAuthNotification/SendRevocationNotification
2. sendNotification():
   a. Build item for DLQ
   b. resilience.Do with AMF-specific retry config
   c. On retry exhaustion → Enqueue to DLQ
3. DLQ.Process() handles eventual delivery
```

---

## Error Handling

### DLQ Errors

| Error | Handling |
|-------|----------|
| Redis down during Dequeue | Tight-spin sleep (250ms), retry |
| Redis down during Enqueue | Retry 5x with backoff, then discard |
| AMF returns 4xx | Record success, don't retry |
| AMF returns 5xx | Retry via resilience.Do, then DLQ |
| DLQ item max attempts exceeded | Log error, discard, increment metric |

### AMF Errors

| Error | Handling |
|-------|----------|
| Circuit breaker open | Return error immediately, DLQ fallback |
| Network error | Retry via resilience.Do |
| 4xx response | Record success, don't retry |
| 5xx response | Retry via resilience.Do |

---

## Metrics

### DLQ Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `nssAAF_dlq_processed_total` | `success`, `error`, `exhausted` | Items processed |

### AMF Metrics

Existing metrics unchanged. OTel traces now include AMF via factory.

---

## Testing

### Unit Tests

1. **DLQ Enqueue Retry**
   - Test successful retry on first attempt
   - Test retry succeeds on second attempt after transient failure
   - Test all retries exhausted → error logged
   - Test tight-spin sleep when Dequeue returns nil

2. **AMF Factory Integration**
   - Test AMF uses factory for HTTP requests
   - Test OTel spans include AMF operations
   - Test circuit breaker integration via factory

### Integration Tests

1. **DLQ Redis Failure**
   - Start DLQ, kill Redis, verify retry behavior
   - Verify item eventually delivered on Redis recovery

2. **AMF End-to-End**
   - Verify AMF notifications traced via OTel
   - Verify circuit breaker opens on consecutive failures

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/cache/redis/dlq.go` | Add retry loop, tight-spin sleep |
| `internal/cache/redis/dlq_test.go` | Add retry tests (create if not exists) |
| `internal/amf/amf.go` | Replace httpClient with factory |
| `internal/amf/notifier_test.go` | Update tests for factory |

---

## Verification

### Build
```bash
go build ./...
```

### Tests
```bash
go test ./internal/cache/redis/... ./internal/amf/... -v
```

### Lint
```bash
golangci-lint run ./internal/cache/redis/... ./internal/amf/...
```

---

## Out of Scope

- Candidate 3 (Storage architecture refactor) — separate spec
- Changes to resilience.RetryConfig defaults
- DLQ web UI / monitoring dashboard
- Secondary DLQ for completely failed items

---

## References

- TS 23.502 §4.2.9 — NSSAA procedure
- TS 33.501 §16 — Circuit breaker pattern
- `internal/resilience/circuit_breaker.go` — CB implementation
- `internal/resilience/retry.go` — Retry implementation
- `internal/nfclient/factory.go` — NF client factory
- `.planning/quick/260530-ihu-improve-codebase-using-mcp-tools-for-cle/260530-ihu-REVIEW.md` — Code review findings
