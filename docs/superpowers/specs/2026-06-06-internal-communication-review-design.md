# Internal Communication Review Design

**Date:** 2026-06-06
**Topic:** current bidirectional internal communication assessment and gap analysis
**Status:** approved design

## Goal

Produce a detailed, evidence-based review of the current internal communication between NSSAAF components in both directions, and identify implementation gaps that affect correctness, resilience, observability, or 3GPP alignment.

The review is not an implementation task. It is a design and analysis deliverable that should make the next implementation plan obvious and bounded.

## Scope

### In scope

The review covers the current communication surface across deployed and supporting components:

- `HTTP Gateway -> Biz Pod`
- `Biz Pod -> AAA Gateway`
- `AAA Gateway -> Biz Pod`
- `Biz Pod -> NRF`
- `Biz Pod -> UDM`
- `Biz Pod -> AUSF`
- `Biz Pod -> Redis`
- `Biz Pod -> PostgreSQL`

The review must analyze both directions where relevant:

- client-initiated request flow
- server-initiated callback or notification flow
- synchronous request/response behavior
- asynchronous or deferred processing paths

### Out of scope

- Kubernetes deployment design changes
- load/performance benchmarking
- implementing the fixes
- broad refactoring unrelated to communication gaps

## Review Method

The review will be flow-driven rather than file-driven.

For each communication path, the analysis should capture:

1. caller and callee
2. transport or protocol used
3. payload or contract shape
4. state mutation or persistence side effects
5. timeout, retry, circuit breaker, or fallback behavior
6. tracing, metrics, and logging coverage
7. current completion status: implemented, partial, stubbed, or missing
8. trust boundary and internal hop security expectations
9. correlation model and keys used across the hop
10. completion semantics for the hop
11. ownership boundary: which component is the source of truth for the relevant responsibility

The review must explicitly identify ownership for at least these responsibilities:

- session state
- auth context lifecycle
- AAA routing decision
- AMF notification responsibility
- reverse-path response or callback correlation

The review should then group findings into end-to-end flows.

## Flows to review

### Flow A: northbound client-initiated NSSAA path

Target architectural path to validate:

`AMF -> HTTP Gateway -> Biz Pod -> AAA Gateway -> AAA-S -> return path via correlation, callback, or synchronous response back toward Biz Pod and AMF`

Review focus:

- request forwarding from HTTP Gateway to Biz Pod
- route decision and forwarding from Biz Pod to AAA Gateway / AAA-S
- response propagation back to the caller
- persistence and session correlation
- error mapping back to N58

### Flow B: northbound client-initiated AIW path

Target architectural path to validate:

`AUSF-triggered AIW or equivalent client-initiated request -> HTTP Gateway -> Biz Pod -> AAA Gateway -> AAA-S -> return path toward Biz Pod and initiating consumer`

Review focus:

- AIW handler flow
- EAP result handling
- MSK forwarding to AUSF
- response and state completion semantics

### Flow C: AAA server-initiated downstream-to-upstream path

Target architectural path to validate:

`AAA-S -> AAA Gateway -> Biz Pod -> AMF or other affected state owner`

Relevant cases:

- Re-Auth
- Revocation
- CoA or equivalent attribute update flow

Review focus:

- how server-initiated messages enter the system
- whether Biz Pod can load enough state to act on them
- whether AMF notification or slice-state change is actually completed
- what happens on failure

### Flow D: Biz Pod support-plane communication

Review focus:

- `Biz Pod -> NRF` registration, heartbeat, and discovery
- `Biz Pod -> UDM` gating and post-auth update
- `Biz Pod -> AUSF` key forwarding
- `Biz Pod -> Redis` for runtime coordination, caching, DLQ, and correlation
- `Biz Pod -> PostgreSQL` for durable auth context/session state

## Deliverable Shape

The resulting review should contain five sections.

### 1. Current-state communication map

A concise map of active component interactions and directionality.

### 2. Bidirectional flow inventory

Each flow should list the concrete hops and what each hop currently does.

### 3. Per-hop contract summary

For every important hop, capture:

- initiator
- receiver
- interface/protocol
- expected request
- expected response
- persistence effects
- resilience behavior
- observability behavior
- trust boundary and security expectation
- correlation keys and correlation mechanism
- completion semantics
- source-of-truth owner for the affected state or decision

### 4. Gap catalog

Each gap should be classified as one of:

- missing path
- partial implementation
- stubbed behavior
- contract mismatch
- incorrect ownership boundary
- resilience gap
- observability gap
- likely spec-alignment gap

Each gap should include:

- evidence from current code
- user or operational impact
- likely remediation direction
- severity: high, medium, or low

### 5. Recommended next implementation slices

The review should conclude with a short list of practical implementation slices that can be planned next, ordered to reduce system risk fastest.

## Review Quality Bar

The review is successful if it:

- explains the current bidirectional communication clearly enough that a new engineer can follow it
- distinguishes implemented behavior from intended architecture
- surfaces incomplete reverse-direction flows, not just happy-path request forwarding
- points to concrete gaps instead of vague “needs improvement” statements
- is narrow enough to become a single implementation plan or a small sequence of plans

## Expected Initial Findings

Based on the current architecture reading, the review will likely need to validate these suspected gap areas:

- incomplete `AAA Gateway -> Biz Pod -> AMF` server-initiated flow completion
- weak or missing response correlation details across some reverse paths
- stubs or placeholders in re-auth, revocation, or CoA handling
- inconsistent ownership of routing, persistence, and callback responsibilities
- uneven observability across cross-component internal calls

These are hypotheses only and must be validated against the code.

## Constraints

- All analysis should stay in English.
- The review must stay evidence-based and code-grounded.
- The review should favor actionable engineering conclusions over long narrative prose.
- The review should remain focused on communication behavior, not general code cleanup.

## Next Step After Review Approval

After the written spec is reviewed and approved, the next step is to create an implementation plan for the communication gaps using the planning workflow.
