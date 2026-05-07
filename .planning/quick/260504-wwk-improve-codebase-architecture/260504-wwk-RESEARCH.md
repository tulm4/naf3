# Quick Task 260504-wwk: Improve Codebase Architecture - Research

**Researched:** 2026-05-04
**Domain:** Go architecture patterns (factory extraction, metrics, state machines, error handling)
**Confidence:** HIGH

## Summary

This codebase (NSSAAF, 5G Network Slice-Specific Authentication and Authorization Function) is a well-structured Go project with clear separation across 3 pods (HTTP Gateway, Biz Pod, AAA Gateway). The architectural analysis in `.scratch/arch-analysis.md` identified 5 improvement areas. This research validates the recommended patterns against existing codebase conventions and the broader Go ecosystem.

**Primary recommendation:** Implement real metrics by wiring `biz/router.go` into `internal/metrics/metrics.go` (minimal change, high impact), then extract factory functions from main.go files (medium effort, high testability), and add `internal/domain/nssaa_status.go` for the state machine.

## User Constraints (from CONTEXT.md)

### Locked Decisions
- Extract factory functions from `cmd/biz/main.go`, `cmd/http-gateway/main.go`, `cmd/nrm/main.go`
- Target: `internal/factory/` or per-component factories
- State machine extraction: single domain package `internal/domain/nssaa.go`
- Metrics: wire Prometheus metrics in `biz/router.go`
- Error handling: all API handlers return ProblemDetails per TS 29.526

### Out of Scope
- Full DDD refactoring (limited to NssaaStatus state machine)
- Package renames beyond what factory extraction requires

## Architectural Responsibility Map

| Capability | Primary Tier | Rationale |
|------------|-------------|-----------|
| Factory extraction | Backend (cmd/) | main.go initialization logic belongs in factory packages |
| Prometheus metrics | Backend (internal/metrics/) | Centralized metrics package already exists |
| State machine | Backend (internal/domain/) | Domain logic should be isolated from HTTP handlers |
| Error handling | API (internal/api/common/) | ProblemDetails is the API contract layer |

## 1. Factory Function Extraction

### Recommended Pattern: Options + Factory Package

Go ecosystem consensus: factory functions with functional options are the standard Go pattern for component initialization. The codebase already uses this pattern extensively in handlers (`WithAAA`, `WithNRFClient`), so consistency is maintained.

**Factory package location:** `internal/biz/factory.go` for Biz Pod, `internal/httpgw/factory.go` for HTTP Gateway. The CONTEXT.md suggests `internal/factory/` or per-component factories — per-component is preferred to avoid cross-component import cycles given the 3-pod architecture.

**Pattern from codebase:**
```go
// internal/api/nssaa/handler.go already uses functional options
func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler
func WithAAA(aaa AAARouter) HandlerOption
```

**Target main.go reduction:** From ~400 lines to ~50 lines:
```go
func main() {
    flag.Parse()
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    cfg, err := config.Load(*configPath)
    if err != nil {
        logger.Error("failed to load config", "error", err)
        os.Exit(1)
    }

    pod, err := biz.NewBizPod(cfg, biz.WithLogger(logger))
    if err != nil {
        logger.Error("failed to create BizPod", "error", err)
        os.Exit(1)
    }

    // ... 10 lines of server startup
}
```

**Testing implications:** Factory functions return interfaces, enabling mock injection:
```go
// Testable: factory accepts interfaces
func NewBizPod(cfg *config.Config, opts ...BizPodOption) (*BizPod, error)

// Test with mocks: pass mock stores, mock clients
func TestBizPod_E2E(t *testing.T) {
    pod, err := NewBizPod(cfg,
        WithSessionStore(&mockStore{}),
        WithAAAClient(&mockAAA{}),
    )
}
```

### Wire vs Factory Pattern Trade-offs

| Aspect | Wire (Google) | Factory Pattern |
|--------|--------------|-----------------|
| Dependency graph | Compile-time validation | Runtime construction |
| Testing | Requires wire Gen | Natural mock injection |
| Complexity | Tooling required | Plain Go |
| Main.go verbosity | Reduced | Moderate reduction |
| **Recommendation** | Overkill for this project | **Preferred** |

**Decision:** Use factory pattern with functional options. Wire adds tooling overhead (wire Gen, generate step) that is not justified for 3 main.go files. The codebase already uses functional options everywhere.

## 2. Prometheus Metrics Implementation

### Current State

`internal/metrics/metrics.go` already has a well-designed metrics registry with real Prometheus collectors:

- `RequestsTotal` — HTTP requests by service, endpoint, method, status
- `RequestDuration` — latency histograms
- `EapSessionsActive` / `EapSessionsTotal` — EAP session tracking
- `AaaRequestsTotal` / `AaaRequestDuration` — AAA request metrics
- `DbQueryDuration` / `DbConnectionsActive` — database metrics
- `CircuitBreakerState` / `CircuitBreakerFailures` — resilience metrics

The problem: `internal/biz/router.go` has a **no-op `*Metrics` struct** that doesn't wire into the real metrics package:

```go
// internal/biz/router.go — CURRENT (no-op)
type Metrics struct{}
func (m *Metrics) RecordAAARequest(protocol, host, result string) {}
func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {}
```

### Fix: Wire biz Metrics into internal/metrics

Replace the no-op with real metric recording using the existing metrics:

```go
// internal/biz/router.go — TARGET (real metrics)
type Metrics struct {
    requestsTotal  *prometheus.CounterVec
    latencySeconds *prometheus.HistogramVec
}

func NewMetrics(reg *prometheus.Registry) *Metrics {
    return &Metrics{
        requestsTotal: promauto.NewCounterVecWithRegistry(reg, prometheus.CounterOpts{
            Name: "nssAAF_biz_aaa_requests_total",
            Help: "Total AAA requests by protocol, host, result",
        }, []string{"protocol", "host", "result"}),
        latencySeconds: promauto.NewHistogramVecWithRegistry(reg, prometheus.HistogramOpts{
            Name:    "nssAAF_biz_aaa_latency_seconds",
            Help:    "AAA request latency",
            Buckets: []float64{.01, .05, .1, .25, .5, 1},
        }, []string{"protocol", "host"}),
    }
}

func (m *Metrics) RecordAAARequest(protocol, host, result string) {
    m.requestsTotal.WithLabelValues(protocol, host, result).Inc()
}

func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {
    m.latencySeconds.WithLabelValues(protocol, host).Observe(d.Seconds())
}
```

**Note:** The metrics package uses a custom `Registry` to avoid `promauto` duplicate-registration panics across binaries. Biz's metrics should register with the same registry. A cleaner approach: have `biz.Metrics` accept the pre-defined metrics from `internal/metrics` rather than creating new ones:

```go
// Simpler: use existing metrics from internal/metrics package
type Metrics struct {
    requestsTotal  *prometheus.CounterVec // from metrics.AaaRequestsTotal
    latencySeconds *prometheus.HistogramVec // from metrics.AaaRequestDuration
}

func NewMetrics() *Metrics {
    return &Metrics{
        requestsTotal:  metrics.AaaRequestsTotal,
        latencySeconds: metrics.AaaRequestDuration,
    }
}
```

This is the preferred approach — reuse existing metrics definitions instead of creating duplicates. The `biz.Metrics` wrapper adds routing-context labels (host) that aren't in the generic `metrics.AaaRequestsTotal`, so add those labels.

## 3. Domain State Machine for NssaaStatus

### Current State

State machine logic is split across two locations:

**`internal/types/nssaa_status.go`** — value object with helper methods:
```go
func (s NssaaStatus) IsTerminal() bool  // EAP_SUCCESS or EAP_FAILURE
func (s NssaaStatus) IsPending() bool   // PENDING
```

**`internal/eap/engine.go`** — actual state transitions:
```go
// State transitions happen in sessionManager.go
// NOT_EXECUTED → PENDING (on Authenticate call)
// PENDING → EAP_SUCCESS (on AAA success)
// PENDING → EAP_FAILURE (on AAA failure)
```

### Recommended: `internal/domain/nssaa_status.go`

Create a focused domain package with the state machine logic:

```go
// internal/domain/nssaa_status.go
package domain

import "github.com/operator/nssAAF/internal/types"

// NssaaStatus represents the NSSAA authentication status.
// Spec: TS 29.571 §5.4.4.60
type NssaaStatus = types.NssaaStatus

// Valid status values (re-exported for domain package)
const (
    StatusNotExecuted = types.NssaaStatusNotExecuted
    StatusPending     = types.NssaaStatusPending
    StatusSuccess     = types.NssaaStatusEapSuccess
    StatusFailure     = types.NssaaStatusEapFailure
)

// TransitionError represents an invalid state transition.
type TransitionError struct {
    From, To NssaaStatus
}

func (e *TransitionError) Error() string {
    return fmt.Sprintf("invalid NSSAA status transition: %s → %s", e.From, e.To)
}

// TransitionTo validates and returns the next status.
// Spec: TS 29.571 §5.4.4.60, TS 23.502 §4.2.9
//
// State machine:
//   NOT_EXECUTED → PENDING (authentication started)
//   PENDING → EAP_SUCCESS (AAA accepted)
//   PENDING → EAP_FAILURE (AAA rejected)
//   PENDING → PENDING (intermediate EAP round)
func TransitionTo(current NssaaStatus, event AuthEvent) (NssaaStatus, error) {
    switch current {
    case StatusNotExecuted:
        if event == EventAuthStarted {
            return StatusPending, nil
        }
    case StatusPending:
        switch event {
        case EventAAAComplete:
            return StatusSuccess, nil
        case EventAAAFailed:
            return StatusFailure, nil
        case EventEAPRound:
            return StatusPending, nil // intermediate round
        }
    case StatusSuccess, StatusFailure:
        return current, nil // terminal states, no further transitions
    }
    return current, &TransitionError{From: current, To: "unknown"}
}

// AuthEvent represents events that trigger state transitions.
type AuthEvent int

const (
    EventAuthStarted AuthEvent = iota // NSSAA procedure initiated
    EventEAPRound                     // Intermediate EAP exchange round
    EventAAAComplete                  // AAA server responded with success
    EventAAAFailed                   // AAA server responded with failure
)
```

**Testing isolation:** State machine logic in a domain package is trivially testable:
```go
func TestTransitionTo(t *testing.T) {
    cases := []struct {
        name     string
        from     domain.NssaaStatus
        event    domain.AuthEvent
        expected domain.NssaaStatus
        err      bool
    }{
        {"not_executed_to_pending", domain.StatusNotExecuted, domain.EventAuthStarted, domain.StatusPending, false},
        {"pending_to_success", domain.StatusPending, domain.EventAAAComplete, domain.StatusSuccess, false},
        {"pending_to_failure", domain.StatusPending, domain.EventAAAFailed, domain.StatusFailure, false},
        {"success_is_terminal", domain.StatusSuccess, domain.EventAuthStarted, domain.StatusSuccess, false},
    }
    // table-driven test...
}
```

**Confidence: HIGH** — The state machine is explicitly defined in the spec (TS 29.571 §5.4.4.60) and the existing code already follows this structure. This refactoring is extraction, not redesign.

## 4. Error Handling Standardization

### Current State

The codebase has a solid `internal/api/common/problem.go` with full ProblemDetails support:
- `ValidationProblem` (400)
- `ForbiddenProblem` (403) — for AAA reject
- `NotFoundProblem` (404)
- `BadGatewayProblem` (502) — AAA unreachable
- `ServiceUnavailableProblem` (503) — AAA overloaded
- `GatewayTimeoutProblem` (504) — AAA timeout

These are 3GPP-aligned per TS 29.526 §7 error codes. The infrastructure is complete.

**What remains:** Audit handlers to ensure all error paths return ProblemDetails. The arch analysis notes some handlers may use raw `http.Error()` instead.

### Audit Scope

Check these handlers for non-ProblemDetails errors:
- `internal/api/nssaa/handler.go` — N58 interface
- `internal/api/aiw/handler.go` — N60 interface
- `internal/nrm/` handlers — RESTCONF interface

**Pattern to enforce:**
```go
// Before (inconsistent)
http.Error(w, "invalid GPSI", http.StatusBadRequest)

// After (consistent)
common.WriteProblem(w, common.ValidationProblem("gpsi", "invalid format"))
```

**Confidence: HIGH** — ProblemDetails infrastructure is complete; this is an audit-and-fix task.

## 5. Thin Wrapper Consolidation

### `internal/biz/router.go` — NOT a thin wrapper

Despite the arch analysis label, `biz/router.go` contains **real routing logic**:
- 3-level S-NSSAI lookup (exact, sst-only, default)
- `ResolveRoute()` — determines which AAA server to use
- `BuildForwardRequest()` — constructs proto messages for AAA Gateway
- `RouteDecision` and `SnssaiConfig` — domain models

**Verdict: Keep.** This is legitimate business logic, not a thin wrapper.

### What IS a thin wrapper candidate

The `biz/router.go` `Metrics` struct — currently a no-op wrapper around nothing. This should be wired into `internal/metrics` as described in section 2 above.

### `internal/aaa/router.go` — needs investigation

Not read during this research pass. Should be evaluated against the same criteria: does it contain logic, or just delegation? If delegation-only, consolidate into the single consumer.

## Common Pitfalls

### Factory Extraction Pitfalls

| Pitfall | Prevention |
|---------|------------|
| **Circular dependencies** — factory imports handlers that import factory | Keep factories in `internal/<component>/factory.go`, not a shared `internal/factory/` package |
| **Leaking concrete types** — factory returns `*ConcreteType` instead of interface | Return concrete types for now (matches existing codebase pattern), or define interfaces for testability |
| **Testability gap** — main.go calls factory, tests can't inject mocks | Factory accepts interfaces; main.go constructs real implementations |

### Metrics Pitfalls

| Pitfall | Prevention |
|---------|------------|
| **Duplicate registration panic** — `promauto` in multiple binaries | Use the existing `metrics.Registry` pattern; don't use `promauto` |
| **Cardinality explosion** — high-cardinality labels (session IDs, GPSI) | Use `host` (server endpoint) and `protocol`, not individual session IDs |
| **Missing middleware** — metrics not recorded on panics | Use `httptrace` or middleware wrapper |

### State Machine Pitfalls

| Pitfall | Prevention |
|---------|------------|
| **Re-implementing what exists** — duplicate state validation logic | Re-export from `internal/types`, add transition logic only |
| **Over-engineering** — elaborate state machine library for simple transitions | Plain Go with switch statements is sufficient for 4-state machine |
| **Testing only happy path** — terminal states and invalid transitions untested | Table-driven tests covering all 4 states × 4 events = 16 cases |

## Code Examples

### Factory Pattern (from codebase conventions)

```go
// internal/biz/factory.go
package biz

type BizPod struct {
    Server       *http.Server
    NRFClient    *nrf.Client
    SessionStore *postgres.SessionStore
    Engine       *eap.Engine
    Handler      *nssaa.Handler
    Logger       *slog.Logger
    Shutdown     func()
}

type BizPodOption func(*BizPod)

func WithLogger(logger *slog.Logger) BizPodOption {
    return func(bp *BizPod) { bp.Logger = logger }
}

func NewBizPod(cfg *config.Config, opts ...BizPodOption) (*BizPod, error) {
    bp := &BizPod{Logger: slog.Default()}

    for _, opt := range opts {
        opt(bp)
    }

    // Initialization...
    return bp, nil
}
```

### State Machine Transitions (proposed)

```go
// internal/domain/nssaa_status.go
func TransitionTo(current NssaaStatus, event AuthEvent) (NssaaStatus, error) {
    switch current {
    case StatusNotExecuted:
        if event == EventAuthStarted {
            return StatusPending, nil
        }
        return current, &TransitionError{From: current, To: "invalid_event"}
    case StatusPending:
        switch event {
        case EventAAAComplete:
            return StatusSuccess, nil
        case EventAAAFailed:
            return StatusFailure, nil
        case EventEAPRound:
            return StatusPending, nil
        }
    }
    return current, nil // terminal states absorb all events
}
```

## Open Questions

1. **AAA router consolidation:** `internal/aaa/router.go` was not read — need to verify if it's a thin wrapper or has real logic before deciding to consolidate.

2. **Factory package location:** CONTEXT.md suggests `internal/factory/` or per-component. Per-component (`internal/biz/factory.go`) is preferred to avoid import cycles, but should confirm no shared initialization logic across components.

3. **Metrics registration:** Should `biz/router.go` metrics use the existing `metrics.AaaRequestsTotal` (shared counter) or create `nssAAF_biz_aaa_requests_total` (pod-specific)? Pod-specific is better for SLO tracking per component.

## Sources

### Primary (HIGH confidence)
- `internal/metrics/metrics.go` — existing Prometheus implementation
- `internal/types/nssaa_status.go` — existing NssaaStatus value object
- `internal/api/common/problem.go` — existing ProblemDetails implementation
- `internal/biz/router.go` — routing logic reviewed
- `.scratch/arch-analysis.md` — architectural analysis

### Secondary (MEDIUM confidence)
- Go community patterns: factory + options (ubiquitous in stdlib-adjacent projects)
- Prometheus Go client patterns: custom Registry over promauto (documented best practice)

### Tertiary (LOW confidence)
- Wire vs factory trade-off: based on codebase scale analysis, not external verification

## Confidence Assessment

| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | Patterns exist in codebase; no new libraries needed |
| Architecture | HIGH | Based on existing codebase conventions and spec requirements |
| Pitfalls | MEDIUM | Derived from codebase patterns and general Go best practices |
