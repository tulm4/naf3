# DLQ Gaps Spec

**Date:** 2026-06-03
**Status:** Draft
**Scope:** DLQ delivery semantics, retry progression, and stalled-item observability

## 1. Purpose

This spec defines the work needed to close the remaining dead-letter queue gaps in NSSAAF. The current DLQ exists as a Redis-backed queue with a background processor, but the implementation is not yet trustworthy enough to guarantee eventual retry delivery behavior under real failure conditions.

The goal is not to invent a new queue system. The goal is to make the existing DLQ deterministic, bounded, observable, and useful when AMF notification retries are exhausted.

## 2. Problem Statement

The DLQ implementation is present, but the current code and prior gap analysis show several weaknesses:

- the processor behavior is not obviously delivering items in a production-hardened way
- retry progression and exhaustion semantics need to be explicit and verifiable
- transient Redis or HTTP failures can make re-enqueue behavior hard to reason about
- metrics are incomplete for operational visibility
- tests do not yet prove the full operational contract of the DLQ under realistic failure and recovery conditions

This creates risk that the DLQ becomes a log-and-loop mechanism instead of a reliable safety net for failed AMF notifications.

## 3. Current State

The graph and code inspection show:

- `internal/cache/redis/dlq.go` contains `Enqueue`, `Dequeue`, `Len`, `Process`, `Stop`, and `Done`
- the processor attempts HTTP delivery to AMF and re-enqueues failures with incremented attempts
- the implementation already tracks `Attempt`, `MaxAttempts`, `CreatedAt`, and `LastError` on the item structure
- tests exist for enqueue/dequeue and some process scenarios
- the existing gap-analysis document identified missing delivery guarantees, max-attempt handling concerns, and metrics gaps

What remains unclear operationally:

- whether exhaustion handling is strict enough and consistently applied
- whether retry delivery is guaranteed to advance item state correctly
- whether the processor shutdown semantics are safe under repeated start/stop cycles
- whether DLQ processing produces the metrics needed to detect stuck items or runaway queues

## 4. Goals

1. Make DLQ processing explicitly bounded by `MaxAttempts`.
2. Ensure failed items are retried in a controlled and observable way.
3. Preserve item metadata across queue hops so failures can be diagnosed.
4. Make processor lifecycle safe to start, stop, and observe.
5. Add tests that validate the full retry-exhaustion-success lifecycle.

## 5. Non-Goals

- Replacing Redis list semantics with streams or another queue backend
- Adding distributed scheduling or job orchestration
- Building a general-purpose message queue abstraction
- Changing AMF notification protocol semantics beyond retry handling

## 6. Proposed Architecture

### 6.1 DLQ item contract

An item stored in the DLQ must preserve:

- identity (`ID`)
- notification type
- destination URI
- payload
- authentication context identifier
- attempt count
- maximum attempts
- creation time
- last error string

The DLQ processor must treat these fields as durable retry state, not optional metadata.

### 6.2 Processing loop

The processor should:

1. dequeue one item at a time
2. drop immediately when `Attempt >= MaxAttempts`
3. attempt delivery over HTTP to the original AMF URI
4. mark success and discard the item if delivery returns a 2xx response
5. increment `Attempt`, update `LastError`, and re-enqueue if delivery fails
6. treat transient queue or delivery failures as retryable within the loop

### 6.3 Retry semantics

Retry behavior must be bounded and deterministic:

- failed deliveries increment `Attempt` exactly once per failed processing cycle
- exhausted items are discarded and counted, not silently retried forever
- re-enqueue attempts may be retried internally if Redis write temporarily fails
- once exhaustion is reached, the item should not return to the active queue

### 6.4 Lifecycle semantics

The processor should support clean lifecycle management:

- `Process()` starts the background loop once per processor lifecycle
- `Stop()` stops the loop and waits for clean exit
- `Done()` provides a stable signal for shutdown verification

The lifecycle contract should remain simple enough that the Biz Pod can own the goroutine without hidden side effects.

## 7. Observability Requirements

The DLQ must be observable enough for operations to answer three questions:

1. Are items being processed?
2. Are items exhausting retries?
3. Is the queue stuck or growing without progress?

Minimum requirements:

- counter for successful DLQ deliveries
- counter for exhausted items discarded
- counter for retry/delivery errors
- gauge or equivalent visibility for current queue depth
- log fields for item ID, attempt, max attempts, and last error

Alert conditions should be able to detect:

- DLQ depth stuck above zero for too long
- exhaustion spikes
- processor stagnation

## 8. Functional Acceptance Criteria

- items with `Attempt >= MaxAttempts` are never retried again
- successful HTTP delivery removes the item from the DLQ processing flow
- failed delivery increments the attempt count and preserves the latest error
- transient Redis enqueue failures do not silently lose the item without logging or metrics
- the processor can be stopped cleanly and restarted without corrupting lifecycle state
- tests prove success, retry, and exhaustion behavior with realistic item metadata

## 9. Testing Strategy

### Unit tests

- enqueue and dequeue preserve item structure
- attempts increment correctly after failed delivery
- items are discarded when exhaustion is reached
- `Stop()` and `Done()` behavior remains safe

### Integration tests

- Redis-backed queue round-trip works end to end
- a mock AMF endpoint receives successful retry delivery
- transient delivery failures re-enqueue with updated attempt count

### Regression tests

- exhaustion is enforced using the actual item fields
- re-enqueue does not drop `MaxAttempts` or `LastError`
- processor shutdown does not leak or deadlock

## 10. Risks

- too-aggressive retry loops may amplify load during AMF outages
- item metadata drift could make retries hard to diagnose
- incorrect shutdown handling could cause goroutine leaks or duplicate processors

## 11. Success Definition

This spec is complete when the DLQ behaves as a bounded, observable retry safety net rather than a best-effort queue loop.
