---
plan_id: 260504-wwk-01
quick_id: 260504-wwk
title: Extract NssaaStatus state machine + wire real metrics
must_haves:
  - internal/domain/nssaa_status.go with TransitionTo() state machine function
  - internal/domain/nssaa_status_test.go with table-driven tests for all transitions
  - internal/biz/router.go wired to use real prometheus metrics
  - go build ./... passes
  - go test ./... passes
---

# Plan 260504-wwk-01: Extract NssaaStatus state machine + wire real metrics

## Objective

Extract NssaaStatus state machine logic to a domain layer and wire real Prometheus metrics into biz/router.go.

## Tasks

### Task 1: Extract NssaaStatus state machine to domain layer

**Action:** Create `internal/domain/nssaa_status.go` that:
- Imports from `internal/types` (NssaaStatus, AuthStatus)
- Defines `TransitionTo(current AuthStatus, event string) (AuthStatus, error)` function
- Implements state transitions per TS 29.571 §5.4.4.60:
  - NOT_EXECUTED + "start" → PENDING
  - PENDING + "eap_success" → EAP_SUCCESS
  - PENDING + "eap_failure" → EAP_FAILURE
  - (terminal states return error on further transitions)

**Files:** internal/domain/nssaa_status.go

**Verify:** go build ./... && go test internal/domain/...

**Done:** When TransitionTo() is implemented and tested

---

### Task 2: Add table-driven tests for state machine

**Action:** Create `internal/domain/nssaa_status_test.go` with:
- Table-driven tests for all valid transitions
- Test for invalid transition attempts (terminal states)
- Edge cases: invalid events, current state validation

**Files:** internal/domain/nssaa_status_test.go

**Verify:** go test -v internal/domain/...

**Done:** When all transition paths are tested

---

### Task 3: Wire real Prometheus metrics into biz/router.go

**Action:** Modify `internal/biz/router.go`:
- Replace no-op `*Metrics` field usage with calls to `internal/metrics` package
- Wire `metrics.AaaRequestsTotal.Inc()` on request start
- Wire `metrics.AaaRequestDuration.Observe()` on request end
- Add error case handling to metrics

**Files:** internal/biz/router.go

**Verify:** go build ./... && go vet ./...

**Done:** When router uses real metrics, not no-ops

---

## Verification

```bash
go build ./...
go test ./...
go vet ./...
```

## Rollback

If issues arise, revert the changes in the order above (Task 3 → Task 2 → Task 1).
