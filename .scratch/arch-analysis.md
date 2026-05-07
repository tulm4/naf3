# NSSAAF Architectural Analysis

**Date:** 2026-05-04
**Author:** Architectural Review
**Project:** 5G NSSAAF (Network Slice-Specific Authentication and Authorization Function)

---

## 1. Component Boundaries and Module Organization

### Current 3-Component Architecture

The codebase follows a well-designed **3-pod deployment model**:

```
┌─────────────────────────────────────────────────────────────────┐
│                           AMF (5G Core)                        │
└───────────────────────────────┬─────────────────────────────────┘
                                │ N58 (HTTP/2)
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                     HTTP Gateway Pod                            │
│  - TLS termination (TLS 1.3)                                    │
│  - JWT authentication                                           │
│  - Load balancing → Biz Pods                                    │
│  - Scope-based routing (nnssaaf-nssaa / nnssaaf-aiw)          │
└───────────────────────────────┬─────────────────────────────────┘
                                │ HTTP
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Biz Pod                                  │
│  - N58/N60 SBI handlers (chi routers)                          │
│  - EAP engine (EAP-TLS, EAP-AKA')                              │
│  - Session storage (PostgreSQL)                                 │
│  - NRF/UDM/AUSF client integrations                           │
└───────────────────────────────┬─────────────────────────────────┘
                                │ HTTP POST /aaa/forward
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                       AAA Gateway Pod                            │
│  - RADIUS (UDP :1812)                                          │
│  - Diameter (TCP/SCTP :3868)                                   │
│  - Redis pub/sub for response correlation                      │
│  - Active-standby via keepalived VIP                           │
└─────────────────────────────────────────────────────────────────┘
```

### Internal Module Structure

```
internal/
├── api/
│   ├── nssaa/     # N58 interface (Nnssaaf_NSSAA)
│   ├── aiw/       # N60 interface (Nnssaaf_AIW)
│   └── common/     # Shared middleware (auth, logging, metrics)
├── eap/           # EAP protocol engine
├── aaa/
│   ├── gateway/   # AAA Gateway (RADIUS/Diameter transport)
│   └── router/    # AAA routing logic (S-NSSAI → server)
├── radius/        # RADIUS client library
├── diameter/      # Diameter client library
├── storage/       # PostgreSQL session persistence
├── cache/         # Redis integration
├── nrf/           # NRF service discovery client
├── udm/           # UDM client
├── ausf/          # AUSF client
├── amf/           # AMF notification client
├── crypto/        # Key management, envelope encryption
├── config/        # Configuration loading
├── types/         # Domain types (GPSI, SUPI, Snssai, NssaaStatus)
├── biz/           # Business logic router
├── nrm/           # Network Resource Model (OAM)
├── proto/         # Inter-component protocols
├── metrics/       # Prometheus metrics
├── logging/       # Structured logging
├── tracing/       # OpenTelemetry tracing
└── resilience/    # Circuit breakers, retry logic
```

**Strengths:**
- Clear separation between external-facing API handlers (`api/`) and internal transport (`aaa/`, `radius/`, `diameter/`)
- EAP engine isolated as a reusable component
- Storage layer abstracted behind interfaces (`AuthCtxStore`)

---

## 2. Code Organization Patterns

### Layering and Dependency Direction

```
┌──────────────────────────────────────────────────────────────┐
│                         cmd/                                  │
│              (Binaries: biz, http-gateway, aaa-gateway)       │
└─────────────────────────────┬────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│                     internal/api/                             │
│              (HTTP handlers, routing, middleware)              │
└─────────────────────────────┬────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│                   internal/eap/                               │
│                 (Protocol engine, session state)               │
└─────────────────────────────┬────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
┌─────────────────┐ ┌───────────────┐ ┌─────────────────────┐
│ internal/aaa/  │ │ internal/     │ │ internal/storage/   │
│ gateway/       │ │ proto/        │ │ internal/cache/     │
│ (transport)    │ │ (messages)    │ │ (persistence)       │
└─────────────────┘ └───────────────┘ └─────────────────────┘
```

### Dependency Injection via Options Pattern

Handlers use functional options for configuration:

```go
// internal/api/nssaa/handler.go
func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler
func WithAAA(aaa AAARouter) HandlerOption
func WithNRFClient(nrf interface{ IsRegistered() bool }) HandlerOption
```

### Interface-Based Abstractions

Key interfaces for testability:

```go
// AuthCtxStore enables swapping between in-memory and PostgreSQL
type AuthCtxStore interface {
    Load(id string) (*AuthCtx, error)
    Save(ctx *AuthCtx) error
    Delete(id string) error
    Close() error
}

// AAARouter decouples EAP engine from transport
type AAARouter interface {
    SendEAP(ctx context.Context, session *Session, payload []byte) ([]byte, error)
}
```

---

## 3. Architectural Smells and Anti-Patterns

### A. Empty `pkg/` Directory

The `pkg/` directory exists but is empty. This suggests potential missed opportunities for shared library code that could be used across components.

**Recommendation:** Use `pkg/` for truly reusable packages (e.g., a shared HTTP client wrapper, common utilities).

### B. Placeholder Metrics in `internal/biz/router.go`

```go
// Metrics holds Biz Pod metrics (extends aaa.Metrics).
type Metrics struct{}

// RecordAAARequest records an AAA request metric.
func (m *Metrics) RecordAAARequest(protocol, host, result string) {}
```

Metrics are no-ops. This defeats the purpose of observability.

**Recommendation:** Either implement real metrics or remove the dead code.

### C. Command-Line Logic in `cmd/*/main.go`

The main files (~500 lines each) contain substantial initialization logic:
- TLS configuration loading
- Health check closures
- Redis pool creation
- NRF client setup

This makes the binaries harder to test and reuse.

**Recommendation:** Extract initialization into factory functions in `internal/` packages.

### D. Thin Wrapper Modules

Some modules exist but delegate most logic elsewhere:
- `internal/biz/router.go` (218 lines) contains mostly configuration handling
- `internal/aaa/router.go` (286 lines) wraps AAA Gateway calls

This may indicate premature abstraction or misplaced responsibility.

### E. Inconsistent Error Handling Patterns

Some handlers return `ProblemDetails` for validation errors, while others use raw HTTP errors:

```go
// Consistent pattern (good)
common.WriteProblem(w, common.ValidationProblem("gpsi", err.Error()))

// Less consistent pattern
http.Error(w, err.Error(), http.StatusBadRequest)
```

**Recommendation:** Standardize on ProblemDetails (RFC 7807) for all API errors.

### F. Missing Domain Layer

Business logic (EAP state machine, session management) is split between:
- `internal/eap/engine.go` (EAP state machine)
- `internal/api/nssaa/handler.go` (HTTP handler + session orchestration)

The NssaaStatus state machine (`NOT_EXECUTED → PENDING → EAP_SUCCESS/EAP_FAILURE`) spans multiple packages.

**Recommendation:** Consider a dedicated domain layer with clear entity/value object separation.

---

## 4. Coupling Concerns Between Modules

### High Coupling: `cmd/biz/main.go`

The Biz Pod main file imports 16 internal packages:

```go
import (
    "github.com/operator/nssAAF/internal/amf"
    "github.com/operator/nssAAF/internal/api/aiw"
    "github.com/operator/nssAAF/internal/api/common"
    "github.com/operator/nssAAF/internal/api/nssaa"
    "github.com/operator/nssAAF/internal/ausf"
    "github.com/operator/nssAAF/internal/cache/redis"
    "github.com/operator/nssAAF/internal/config"
    // ... 9 more
)
```

This creates:
- **Tight coupling** between initialization and business logic
- **Testing difficulty** — main.go is not easily testable
- **Fragility** — changes to any dependency can break startup

### Moderate Coupling: API → EAP → AAA Transport

```
HTTP Handler → EAP Engine → AAA Client → HTTP AAA Gateway → RADIUS/Diameter
```

Each layer depends on the next via interfaces, but concrete types leak:

```go
// internal/eap/engine.go
type Engine struct {
    aaaClient      AAARouter
    sessionManager *sessionManager
    fragmentMgr    *FragmentManager
}
```

### Low Coupling: Proto Package

`internal/proto/` provides clean message definitions used across components:

```go
type AaaForwardRequest struct {
    Version       string
    SessionID     string
    AuthCtxID     string
    TransportType TransportType
    // ...
}
```

---

## 5. Top 5 Architectural Improvement Opportunities

### 1. Extract Factory Functions from `cmd/*/main.go` (HIGH IMPACT)

**Problem:** 500+ line main files are hard to test, maintain, and extend.

**Opportunity:** Create factory packages that bundle initialization:

```go
// internal/biz/factory.go
func NewBizPod(ctx context.Context, cfg *config.Config) (*BizPod, error) {
    // Returns initialized pod with all dependencies wired
}

type BizPod struct {
    Server       *http.Server
    NRFClient    *nrf.Client
    SessionStore *postgres.SessionStore
    Engine       *eap.Engine
    // ...
}
```

**Impact:** Reduces main.go to ~50 lines, enables integration testing of initialization logic.

---

### 2. Implement Real Metrics for `internal/biz/router.go` (MEDIUM IMPACT)

**Problem:** No-op metrics provide no observability into AAA routing decisions.

**Opportunity:** Wire Prometheus metrics:

```go
var (
    aaaRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "nssaa_biz_aaa_requests_total",
        Help: "Total AAA requests by protocol, host, and result",
    }, []string{"protocol", "host", "result"})

    aaaLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "nssaa_biz_aaa_latency_seconds",
        Help:    "AAA request latency",
        Buckets: []float64{.01, .05, .1, .25, .5, 1},
    }, []string{"protocol", "host"})
)
```

**Impact:** Production debugging, capacity planning, SLO tracking.

---

### 3. Add a Domain Layer for NssaaStatus State Machine (MEDIUM IMPACT)

**Problem:** State machine logic is fragmented across `eap/engine.go` and `api/nssaa/handler.go`.

**Opportunity:** Create `internal/domain/` with:

```
internal/domain/
├── auth_context.go    # AuthCtx entity with state transitions
├── nssaa_status.go    # NssaaStatus value object
├── session.go         # EAP session aggregate root
└── events.go          # Domain events (AuthStarted, AuthCompleted, etc.)
```

**Impact:** Cleaner separation of concerns, easier to test state machine in isolation, clearer DDD boundaries.

---

### 4. Standardize Error Handling to ProblemDetails (LOW-MEDIUM IMPACT)

**Problem:** Inconsistent error responses across handlers.

**Opportunity:** Audit all HTTP handlers and ensure all errors return ProblemDetails:

```go
// Before
http.Error(w, "invalid GPSI", http.StatusBadRequest)

// After
common.WriteProblem(w, common.ValidationProblem("gpsi", "invalid format"))
```

**Impact:** Consistent API contract, better client error handling, spec compliance.

---

### 5. Consolidate Thin Wrapper Modules (LOW IMPACT)

**Problem:** `internal/biz/router.go` and `internal/aaa/router.go` are thin wrappers.

**Opportunity:** Evaluate whether these modules add value or should be merged into their primary consumers:

- If `biz/router` is only used by Biz Pod main.go, consider moving into `cmd/biz/`
- If `aaa/router` delegates to `aaa/gateway`, consolidate

**Impact:** Simplified package structure, reduced indirection.

---

## Summary

| Category | Rating | Notes |
|----------|--------|-------|
| Component Boundaries | ✅ Good | 3-pod model is well-designed |
| Layering | ⚠️ Fair | API→EAP→AAA flow is clear, but domain logic is fragmented |
| Testability | ⚠️ Fair | Interfaces exist, but main.go initialization is hard to test |
| Observability | ⚠️ Fair | Logging/metrics exist, but biz metrics are placeholders |
| Modularity | ⚠️ Fair | Some thin wrappers, empty pkg/ directory |
| Configuration | ✅ Good | Centralized config package with options pattern |

**Overall Architecture Grade: B+**

The codebase demonstrates solid understanding of 5G NSSAAF requirements and implements a clean 3-component separation. The primary improvement areas are around reducing coupling in main.go files and completing observability instrumentation.
