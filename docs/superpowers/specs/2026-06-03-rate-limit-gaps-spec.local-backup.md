# Rate Limit Gaps Spec

**Date:** 2026-06-03
**Status:** Draft
**Scope:** Request throttling integration, policy enforcement, and limit-hit observability

## 1. Purpose

This spec defines the work needed to close the remaining rate limit gaps in NSSAAF. The current Redis sliding-window limiter exists as a reusable component, but the application does not yet clearly enforce it on the request paths where it matters.

The goal is not to redesign the limiter algorithm. The goal is to make the existing limiter active, policy-driven, and visible at the boundaries of the system.

## 2. Problem Statement

The rate limiter code is present, but the current codebase shows a clear gap between implementation and enforcement:

- the limiter is implemented as a library but does not appear to be wired into real HTTP handlers
- there is no clear evidence that request entry points are actually blocked when over limit
- limit-hit metrics and alerts are incomplete or missing
- policy scope is not yet explicit enough for operators to know what is being protected
- tests do not yet prove that throttling occurs in the request path, only that the limiter helper works

This creates the risk that rate limiting exists on paper but not in production traffic control.

## 3. Current State

The graph and code inspection show:

- `internal/cache/redis/ratelimit.go` provides `Allow`, `AllowGPSI`, `AllowAMF`, `GetCount`, and `Reset`
- the limiter uses a Redis sorted-set sliding window model
- there is at least some config-related testing around rate limiter settings
- the graph search did not show clear live wiring into request handlers
- the earlier gap-analysis document explicitly states the limiter is not wired into HTTP handlers

What remains unclear:

- which request boundaries should enforce the limiter first
- whether policy is per-GPSI, per-AMF, or both at different layers
- whether the limiter should fail open or fail closed on Redis errors
- whether rate limit decisions are currently observable in metrics and logs

## 4. Goals

1. Wire rate limiting into the real request entry points.
2. Make the protected identity explicit, especially for GPSI- and AMF-scoped limits.
3. Define Redis error behavior clearly so operators know whether traffic is blocked or allowed on limiter failure.
4. Emit metrics and logs for both allowed and limited requests.
5. Add tests proving that over-limit traffic is rejected in the real path.

## 5. Non-Goals

- Introducing a new algorithm such as token bucket or leaky bucket
- Centralized global rate limiting across clusters
- Per-S-NSSAI rate limiting unless explicitly needed later
- Heavy API gateway dependency for throttling enforcement

## 6. Proposed Architecture

### 6.1 Enforcement layers

Rate limiting should be enforced at the earliest appropriate request boundary where the identity is already known.

Expected layers:

- inbound HTTP handler boundary for request initiation
- AMF-scoped guard where the target AMF identity is known
- GPSI-scoped guard where subscriber identity is available

The limiter should not be used only as a helper object in isolation. It must gate real requests.

### 6.2 Policy scope

The limiter should expose policy in terms of the identity being protected:

- GPSI-based limit for subscriber-driven traffic
- AMF-based limit for AMF-targeted operations

The exact enforcement points should be defined so the same identity always maps to the same Redis key.

### 6.3 Redis failure semantics

The limiter needs an explicit policy for Redis outages or Redis command failures.

The spec should decide one of two behaviors and use it consistently:

- fail open for availability-critical control-plane calls
- fail closed for abuse-prevention boundaries where safety is preferred over reachability

Whatever policy is selected, the behavior must be documented and tested.

### 6.4 Decision flow

For a protected request:

1. resolve the relevant identity key
2. call the limiter before executing the expensive request
3. if the request is allowed, continue processing
4. if the request is limited, return a clear throttling response
5. emit metrics and logs for both outcomes

## 7. Observability Requirements

The limiter must be visible enough to answer:

- how many requests were allowed
- how many were limited
- which identity or scope was throttled
- whether Redis errors are preventing enforcement

Minimum requirements:

- counter for allowed requests
- counter for limited requests
- counter for limiter errors
- logs with scope and hashed identity where necessary
- alert support for excessive limit hits or Redis failures

## 8. Functional Acceptance Criteria

- an over-limit request in the real request path returns a throttling response
- allowed requests continue to the business logic path
- GPSI and AMF scopes map to deterministic Redis keys
- Redis error behavior is consistent with the documented policy
- metrics and logs clearly distinguish allowed, limited, and error cases
- tests prove enforcement in at least one real request path, not only in the helper

## 9. Testing Strategy

### Unit tests

- limiter helper behavior remains correct for allowed vs limited cases
- count and reset functions remain consistent with the sliding-window policy
- key derivation is stable and deterministic

### Integration tests

- request handler enforcement rejects over-limit traffic
- rate limit counters increase for both allow and deny decisions
- Redis-backed behavior matches the documented failure policy

### Regression tests

- the limiter is invoked from a real handler path
- the chosen Redis failure mode is preserved
- no raw sensitive identity values are logged

## 10. Risks

- enforcing too early could block requests before sufficient identity context exists
- enforcing too late could allow expensive work before throttling
- Redis outage policy can materially change availability and abuse resistance tradeoffs
- key mismatch can cause either under-throttling or over-throttling

## 11. Success Definition

This spec is complete when rate limiting becomes an enforced boundary behavior rather than a reusable utility that is never exercised by production traffic.
