# Circuit Breaker Gaps Spec

**Date:** 2026-06-03
**Status:** Draft
**Scope:** Circuit breaker wiring, configuration, and observability gaps

## 1. Purpose

This spec defines the work needed to close the remaining circuit breaker gaps in NSSAAF. The current codebase includes a reusable `resilience.CircuitBreaker` and `resilience.Registry`, but the implementation is not yet fully applied to the outbound dependency paths that matter operationally.

The goal is not to redesign the circuit breaker primitive. The goal is to make the existing breaker active, correctly configured, and observable in the real execution paths that protect NSSAAF against dependency failures.

## 2. Problem Statement

The circuit breaker implementation exists, but the codebase still shows these gaps:

- the breaker is not clearly wired into all critical outbound client paths
- breaker configuration is not consistently sourced from the canonical configuration path
- metric transitions can be emitted incorrectly when the previous state is not captured correctly
- there is no clear evidence that breaker behavior is validated in the same paths that will run in production
- breaker visibility is incomplete when compared to the project’s observability goals

This creates a false sense of resilience: the primitive exists, tests cover the primitive, but the real application may still repeatedly call failing dependencies instead of isolating them.

## 3. Current State

The graph and code inspection show:

- `internal/resilience/circuit_breaker.go` contains a functioning local state machine
- unit and integration tests exist for the breaker primitive
- the project documents a host:port breaker model as a validated decision
- the current gap-analysis document explicitly calls out missing client wiring and a state-transition bug

What is still unclear from the implementation:

- whether all outbound dependencies use the registry in live request flows
- whether the factory constructs breaker registries from the canonical config path
- whether metrics reflect genuine state transitions only
- whether the same breaker keying strategy is used consistently across NRF, UDM, AUSF, and AMF notification flows

## 4. Goals

1. Apply the circuit breaker to the real outbound dependency paths that can fail independently.
2. Keep the breaker keyed by dependency identity in a way that matches the existing host:port design.
3. Source breaker thresholds from the canonical configuration path used by the application.
4. Emit only correct breaker transition metrics.
5. Add tests that prove the breaker is active in the production wiring, not just in isolation.

## 5. Non-Goals

- Per-S-NSSAI breaker granularity
- Replacing the current breaker implementation with a different pattern
- Introducing a distributed breaker state store
- Changing retry policy semantics beyond what is needed to integrate with the breaker

## 6. Proposed Architecture

### 6.1 Breaker ownership

Each outbound client family should receive a `resilience.Registry` instance from the Biz Pod factory. The registry remains local to the process and stores one breaker per dependency key.

Expected consumers:

- NRF client calls
- UDM client calls
- AUSF client calls
- AMF notification client calls

### 6.2 Breaker keying

The breaker key should identify the real external endpoint that is being protected. For host-based clients, that means the host and port or a normalized base URL identity that maps to the dependency endpoint.

The keying strategy must be deterministic and stable across retries so that repeated failures on the same dependency accumulate against the same breaker.

### 6.3 State transition flow

For a guarded request:

1. resolve the breaker for the target dependency
2. call `Allow()` before the outbound request
3. fail fast if the breaker is open
4. on successful outbound completion, call `RecordSuccess()`
5. on failure, call `RecordFailure()`
6. emit state transition metrics only when the breaker state actually changes

## 7. Configuration Requirements

The breaker thresholds must come from the canonical configuration path that already belongs to internal communication / native dependency resilience.

Minimum config fields:

- failure threshold
- recovery timeout
- success threshold

Defaults should remain aligned with the current breaker defaults unless the configuration explicitly overrides them.

## 8. Observability Requirements

The breaker must be visible through the existing observability stack.

Minimum requirements:

- counter or gauge for breaker open/close/half-open transitions
- counter for fast-fail decisions caused by an open breaker
- logs that identify the dependency key without exposing sensitive payloads
- existing alert conditions should be able to reference breaker-open behavior consistently

## 9. Functional Acceptance Criteria

- outbound calls to protected dependencies are blocked when the breaker is open
- the breaker transitions to half-open after the configured recovery timeout
- successful half-open probes close the breaker after the configured success threshold
- failures during half-open reopen the breaker immediately
- metric transitions are emitted exactly once per real state transition
- tests prove the breaker is applied in at least one real client path, not only in the primitive tests

## 10. Testing Strategy

### Unit tests

- breaker transitions remain correct for closed, open, and half-open behavior
- config defaults are applied correctly when config values are zero
- metric transition logic does not emit false closed-to-closed transitions

### Integration tests

- a failing dependency trips the breaker in a real client call path
- the same dependency recovers after timeout and success threshold
- different dependency keys remain isolated from each other

### Regression tests

- a test for the previous-state metric bug
- a test for config path correctness in the factory wiring

## 11. Risks

- inconsistent key derivation could fragment breaker history across equivalent endpoints
- wiring the breaker too broadly could block unrelated calls if the key is too coarse
- missing metric alignment could create alert noise or hide real failures

## 12. Success Definition

This spec is complete when the circuit breaker is no longer just a reusable helper and instead becomes an enforced resilience control in the outbound paths that matter.
