# Phase 4: High Availability — Resilience Patterns

## Overview

Phase 4 thêm high availability và resilience patterns.

## Modules to Implement

### 1. `internal/resilience/` — Resilience Patterns

**Priority:** P0
**Dependencies:** None
**Design Doc:** `docs/design/10_ha_architecture.md`

**Deliverables:**
- [ ] `circuit_breaker.go` — Per-AAA circuit breaker
- [ ] `retry.go` — Retry with backoff
- [ ] `timeout.go` — Timeout handling
- [ ] `bulkhead.go` — Thread pool isolation
- [ ] `health.go` — Health check aggregator
- [ ] `resilience_test.go` — Unit tests

**Circuit Breaker States:**
```go
type CircuitState int
const (
    CB_CLOSED CircuitState = iota
    CB_OPEN
    CB_HALF_OPEN
)

// Thresholds:
// - Failure threshold: 5 consecutive
// - Recovery timeout: 30 seconds
// - Half-open max requests: 3
```

### 2. `internal/resilience/metrics.go` — Resilience Metrics

**Deliverables:**
- [ ] Circuit breaker metrics (Prometheus)
- [ ] Retry metrics
- [ ] Health metrics

## Validation Checklist

- [ ] Circuit breaker: CLOSED → OPEN (5 failures) → HALF_OPEN (30s) → CLOSED
- [ ] Retry: exponential backoff 1s, 2s, 4s, max 3 retries
- [ ] Health endpoint: /healthz/live, /healthz/ready
- [ ] Unit test coverage >80%
