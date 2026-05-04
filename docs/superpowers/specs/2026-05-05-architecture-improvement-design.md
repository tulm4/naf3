# Architecture Improvement Design — Approach A (Incremental Cleanup)

**Date:** 2026-05-05
**Author:** Architecture Review
**Confidence:** HIGH
**Status:** Draft

---

## Overview

This design covers five targeted architectural improvements to the NSSAAF codebase. Each improvement addresses a specific pain point identified during architectural analysis, with minimal risk and maximum impact. The approach is incremental — each item is independent and can be verified in isolation.

---

## 1. Wire Real Prometheus Metrics into biz/router.go

### Problem

`internal/biz/router.go` has a no-op `Metrics` struct:

```go
type Metrics struct{}
func (m *Metrics) RecordAAARequest(protocol, host, result string) {}
func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {}
```

Meanwhile, `internal/metrics/metrics.go` has the real Prometheus collectors (`AaaRequestsTotal`, `AaaRequestDuration`) already registered.

### Solution

Replace the no-op with real metric recording using the existing metrics package.

**Files modified:** `internal/biz/router.go`

**Changes:**
1. Add import for `"github.com/operator/nssAAF/internal/metrics"`
2. Replace empty `Metrics` struct with fields referencing `metrics.AaaRequestsTotal` and `metrics.AaaRequestDuration`
3. Implement `RecordAAARequest` to call `requestsTotal.WithLabelValues(...).Inc()`
4. Implement `RecordAAALatency` to call `latencySeconds.WithLabelValues(...).Observe(d.Seconds())`
5. Add `WithMetrics` option that accepts `*Metrics` and initializes default metrics in `NewRouter`

**Verification:** `go build ./internal/biz/...` and `go test ./internal/biz/... -short`

---

## 2. Create Domain Package for NssaaStatus State Machine

### Problem

State machine logic is implicit in the EAP engine. `internal/types/nssaa_status.go` has helper methods (`IsTerminal`, `IsPending`) but no transition logic.

### Solution

Create `internal/domain/nssaa_status.go` with explicit state machine.

**Files created:** `internal/domain/nssaa_status.go`, `internal/domain/nssaa_status_test.go`

**Design:**

```go
// NssaaStatus = types.NssaaStatus (type alias)

// AuthEvent represents events that trigger state transitions.
type AuthEvent int

const (
    EventAuthStarted AuthEvent = iota // NSSAA procedure initiated
    EventEAPRound                     // Intermediate EAP exchange round
    EventAAAComplete                  // AAA server responded with success
    EventAAAFailed                   // AAA server responded with failure
)

// TransitionTo validates and returns the next status.
func TransitionTo(current NssaaStatus, event AuthEvent) (NssaaStatus, error)

// State machine (per TS 29.571 §5.4.4.60):
//   NOT_EXECUTED + EventAuthStarted → PENDING
//   PENDING + EventAAAComplete → EAP_SUCCESS
//   PENDING + EventAAAFailed → EAP_FAILURE
//   PENDING + EventEAPRound → PENDING (intermediate round)
//   Terminal states absorb all events (return current, nil)
```

**Tests:** 16 table-driven test cases (4 states × 4 events)

**Verification:** `go build ./internal/domain/...` and `go test ./internal/domain/... -v`

---

## 3. Delete Deprecated `internal/aaa/router.go`

### Problem

`internal/aaa/router.go` is marked DEPRECATED with comment: "This package is no longer used by any binary." It contains 287 lines of duplicate types that also exist in `internal/biz/router.go`.

### Solution

Delete the file and update any imports if they exist.

**Files deleted:** `internal/aaa/router.go`

**Precondition:** Verify no imports exist:
```bash
grep -r "internal/aaa" --include="*.go" | grep -v "_test.go"
```

**Verification:** `go build ./...` passes after deletion

---

## 4. Audit and Standardize Error Handling in API Handlers

### Problem

The codebase has `internal/api/common/problem.go` with full ProblemDetails support, but some handlers may use raw `http.Error()` instead.

### Solution

Audit all API handlers and ensure consistent ProblemDetails responses per TS 29.526.

**Files to audit:**
- `internal/api/nssaa/handler.go`
- `internal/api/aiw/handler.go`
- `internal/nrm/` handlers (RESTCONF)

**Pattern to enforce:**

```go
// Before (inconsistent)
http.Error(w, "invalid GPSI", http.StatusBadRequest)

// After (consistent)
common.WriteProblem(w, common.ValidationProblem("gpsi", "invalid format"))
```

**Existing ProblemDetails available:**
- `ValidationProblem(field, reason)` — HTTP 400
- `ForbiddenProblem(detail)` — HTTP 403 (AAA reject)
- `NotFoundProblem(detail)` — HTTP 404
- `BadGatewayProblem(detail)` — HTTP 502 (AAA unreachable)
- `ServiceUnavailableProblem(detail)` — HTTP 503 (AAA overloaded)
- `GatewayTimeoutProblem(detail)` — HTTP 504 (AAA timeout)
- `InternalServerProblem(detail)` — HTTP 500

**Verification:** `go vet ./internal/api/... ./internal/nrm/...`

---

## 5. Extract Factory Functions from cmd/biz/main.go

### Problem

`cmd/biz/main.go` is 512 lines. Initialization logic (PostgreSQL pool, Redis, crypto, NRF client, etc.) is mixed with server startup.

### Solution

Extract initialization into factory functions in `cmd/biz/factory.go`.

**Files created:** `cmd/biz/factory.go`

**Design:**

```go
// cmd/biz/factory.go

type BizPodFactory struct {
    cfg  *config.Config
    opts []BizPodOption
}

type BizPodOption func(*BizPod)

type BizPod struct {
    Server       *http.Server
    NRFClient    *nrf.Client
    SessionStore *postgres.SessionStore
    AIWSessionStore *postgres.SessionStore
    RedisPool   *redis.Pool
    DLQ         *redis.DLQ
    Handler     *nssaa.Handler
    Logger      *slog.Logger
    Shutdown    func()
}

func NewBizPodFactory(cfg *config.Config) *BizPodFactory

func (f *BizPodFactory) WithLogger(logger *slog.Logger) *BizPodFactory

func (f *BizPodFactory) Build(ctx context.Context) (*BizPod, error)

// cmd/biz/main.go reduced to ~80 lines
func main() {
    flag.Parse()
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

    cfg, err := config.Load(*configPath)
    if err != nil {
        logger.Error("failed to load config", "error", err)
        os.Exit(1)
    }

    factory := biz.NewBizPodFactory(cfg, biz.WithLogger(logger))
    pod, err := factory.Build(context.Background())
    if err != nil {
        logger.Error("failed to build BizPod", "error", err)
        os.Exit(1)
    }
    defer pod.Shutdown()

    // Server startup only
    if err := pod.Server.ListenAndServe(); err != nil {
        logger.Error("server error", "error", err)
        os.Exit(1)
    }
}
```

**Testing benefit:** Factory accepts interfaces, enabling mock injection:

```go
func TestBizPod_E2E(t *testing.T) {
    pod, err := NewBizPodFactory(cfg,
        WithSessionStore(&mockStore{}),
        WithAAAClient(&mockAAA{}),
    )
}
```

**Verification:** `go build ./cmd/biz/...` and `go test ./cmd/biz/... -short`

---

## Implementation Order

| # | Task | Risk | Effort |
|---|------|------|--------|
| 1 | Wire real metrics | LOW | 30 min |
| 2 | Create domain package | LOW | 1 hour |
| 3 | Delete deprecated code | LOW | 5 min |
| 4 | Audit error handling | MEDIUM | 1-2 hours |
| 5 | Extract factory functions | MEDIUM | 2 hours |

**Rationale:** Tasks 1-3 are extraction-only (no new logic). Tasks 4-5 involve code changes that could introduce bugs, so they're last.

---

## Acceptance Criteria

1. `internal/biz/router.go` calls `metrics.AaaRequestsTotal.WithLabelValues`
2. `internal/domain/nssaa_status.go` exists with `TransitionTo` function
3. `internal/domain/nssaa_status_test.go` has 16 passing test cases
4. `internal/aaa/router.go` is deleted
5. All API handlers use ProblemDetails for errors (no raw `http.Error`)
6. `cmd/biz/main.go` reduced by >50% via factory extraction
7. `go build ./...` passes
8. `go test ./...` passes
9. `go vet ./...` passes

---

## Out of Scope

- `cmd/http-gateway/main.go` factory extraction (doesn't exist yet)
- `cmd/nrm/main.go` factory extraction (separate binary lifecycle)
- Full DDD refactoring beyond NssaaStatus state machine
- Package renames beyond what factory extraction requires

---

## Dependencies

None. All improvements are independent and can be executed in any order.
