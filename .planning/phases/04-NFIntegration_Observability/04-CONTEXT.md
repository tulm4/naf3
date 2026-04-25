# Phase 4: NF Integration & Observability - Context

**Gathered:** 2026-04-25
**Status:** Ready for planning

<domain>
## Phase Boundary

Wire NRF, UDM, AMF, AUSF clients into Biz Pod (`cmd/biz/main.go`); implement resilience patterns (circuit breaker, retry, timeout, health endpoints); implement observability (Prometheus metrics, structured logging, OpenTelemetry tracing); replace in-memory session stores with PostgreSQL via `NewSessionStore()`/`NewAIWSessionStore()`. This phase makes NSSAAF functional in a real 5G network and observable by operations teams.

Not this phase: TLS/mTLS (Phase 5), Kubernetes manifests (Phase 7), load testing (Phase 8).

</domain>

<decisions>
## Implementation Decisions

### Tracing approach
- **D-01:** Full cross-component OpenTelemetry tracing — spans from AMF through HTTP Gateway → Biz Pod → AAA Gateway → AAA-S via W3C TraceContext propagated in HTTP headers and Redis pub/sub correlation
- Trace context propagated across all 3 components; Biz Pod is the trace correlation hub
- See `docs/design/19_observability.md` §4.2 for cross-component trace flow diagram

### AMF notification failure handling
- **D-02:** Dead-letter queue (DLQ) — AMF notification failures (re-auth, revocation) are enqueued to DLQ after retries exhausted, not dropped
- DLQ enables later reprocessing; operations team can monitor DLQ depth
- On persistent DLQ failure: log at ERROR level with full notification context

### Circuit breaker granularity
- **D-03:** Per `host:port` circuit breaker — matches current `AAAConfig` scope
- Per S-NSSAI circuit breaker (`sst+sd+host`) deferred to future phase if needed
- `CircuitBreakerRegistry` manages named breakers keyed by `"host:port"`

### NRF unavailable at startup
- **D-04:** Startup in degraded mode — Biz Pod starts even if NRF registration fails at boot
- NRF registration retried in background with exponential backoff
- Until registered: use cached NRF data if available; AMF/UDM/AUSF discovery returns errors until NRF is reachable
- This avoids blocking startup in dev/test environments where NRF may not be immediately available

### Handler injection pattern
- **D-05:** Option functions (`WithNRFClient`, `WithUDMClient`, `WithAUSFClient`) added to existing handler packages — no new files needed for option functions alone
- `nssaa.Handler` gains `nrfClient *nrf.Client` and `udmClient *udm.Client` fields
- `aiw.Handler` gains `ausfClient *ausf.Client` field

### PostgreSQL session store wiring
- **D-06:** `NewSessionStore(*Pool)` and `NewAIWSessionStore(*Pool)` implemented as new files in `internal/storage/postgres/session_store.go`
- Both implement existing `nssaa.AuthCtxStore` and `aiw.AuthCtxStore` interfaces respectively

### Health endpoint paths
- **D-07:** Health endpoints renamed from `/health` and `/ready` to `/healthz/live` and `/healthz/ready` per Kubernetes convention
- `/healthz/live`: always 200 (process alive, no dependency checks)
- `/healthz/ready`: checks PostgreSQL, Redis, AAA Gateway connectivity before returning 200

### Claude's Discretion
- OTel SDK memory configuration (batch exporter flush interval, resource limits)
- DLQ implementation details (Redis list, PostgreSQL table, or separate service)
- Alert threshold fine-tuning (error rate %, P99 latency ms)
- Exact metric label names and cardinality (component labels across all metrics)
- NRF discovery cache TTL (5 min as documented in `docs/design/05_nf_profile.md` §3.3)
- Health check polling interval defaults

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### NF Integration
- `docs/design/05_nf_profile.md` — NRF registration payload, heartbeat mechanism, discovery caching, token validation, graceful shutdown
- `docs/design/10_ha_architecture.md` §4-6 — HA patterns, circuit breaker state machine, health endpoints
- `docs/design/19_observability.md` — Prometheus metrics definitions, ServiceMonitor CRDs, structured logging format, OTel trace flow, alerting rules

### Implementation patterns (from existing code)
- `cmd/biz/main.go` — Existing main.go structure; new clients wired here
- `internal/config/config.go` — Config structs; `NRFConfig`, `UDMConfig` exist; `AMFConfig`/`AUSFConfig` needed
- `internal/api/nssaa/handler.go` — Existing handler with `WithAAA`, `WithAPIRoot` options; add `WithNRFClient`, `WithUDMClient`
- `internal/api/aiw/handler.go` — Existing handler; add `WithAUSFClient`
- `internal/storage/postgres/pool.go` — Existing `NewPool()`; `NewSessionStore()`/`NewAIWSessionStore()` wrappers needed

### 3GPP Specifications
- TS 29.510 §6 — NRF NF Profile registration
- TS 29.526 §7.2-7.3 — N58 API, UDM integration
- TS 23.502 §4.2.9.3-4 — AMF re-auth/revocation notification flows

</canonical_refs>

<codebase_context>
## Existing Code Insights

### Reusable Assets
- `internal/cache/redis/` (READY) — Redis rate limiting, locking, idempotency patterns available for DLQ
- `internal/storage/postgres/pool.go` (READY) — Connection pool exists; session store wrappers needed
- `cmd/biz/main.go` (EXISTS) — In-memory stores at lines 59-60; stub handlers at lines ~188-201; `/health` and `/ready` handlers at lines ~257-267
- `internal/api/common/` (READY) — Middleware, validators, context helpers for N58 handler extension

### Established Patterns
- Option function pattern (`WithAAA`, `WithAPIRoot`) already established in handler packages
- `slog.NewJSONHandler` already used in main.go for logging
- `internal/aaa/metrics.go` — existing metrics pattern for AAA protocol counters

### Integration Points
- `cmd/biz/main.go` — Central wiring hub; all new clients (NRF, UDM, AMF, AUSF) and stores (PostgreSQL) wire here
- `internal/api/nssaa/handler.go` — N58 handler gains UDM call before AAA routing
- `internal/api/aiw/handler.go` — AIW handler gains AUSF MSK forwarding
- `internal/aaa/router.go` — Already has protocol routing; circuit breaker wraps here

### Known Gaps (from plan checker)
- `NewSessionStore()`/`NewAIWSessionStore()` do NOT exist — must be implemented
- `WithNRFClient`/`WithUDMClient`/`WithAUSFClient` do NOT exist — must be implemented
- `AMFConfig`/`AUSFConfig` do NOT exist in config.go — use NRF discovery (no static baseURL needed)
- `deployments/k8s/` directory does NOT exist — create implicitly

</codebase_context>

<deferred>
## Deferred Ideas

- Per S-NSSAI circuit breaker (sst+sd+host granularity) — can be added if current per-host approach proves insufficient
- HTTP Gateway and AAA Gateway metrics/tracing integration — may be needed in Phase 7 when Kubernetes manifests are created

</deferred>

---

*Phase: 04-nf-integration-observability*
*Context gathered: 2026-04-25*
