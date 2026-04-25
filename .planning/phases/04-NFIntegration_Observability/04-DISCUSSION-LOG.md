# Phase 4: NF Integration & Observability - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-25
**Phase:** 04-nf-integration-observability
**Areas discussed:** Tracing approach, AMF notification failures, Circuit breaker granularity, NRF startup behavior

---

## Area: Tracing approach

|| Option | Description | Selected |
|--------|---------|------------|---------|
| Full cross-component (Recommended) | OTel spans from AMF through HTTP GW → Biz Pod → AAA GW → AAA-S via W3C TraceContext propagated in HTTP headers and Redis pub/sub correlation | ✓ |
| Biz Pod only | Trace only inside Biz Pod; HTTP GW and AAA GW are simple forwarders without trace context propagation | |
| Skip tracing entirely | Defer OpenTelemetry to a later phase | |

**User's choice:** Full cross-component (Recommended)
**Notes:** Cross-component traces are important for telecom debugging across all 3 components.

---

## Area: AMF notification failure handling

|| Option | Description | Selected |
|--------|---------|------------|---------|
| DLQ — retry later (Recommended) | After retries exhausted, enqueue to dead-letter queue for later reprocessing; operations can monitor DLQ depth | ✓ |
| Log and drop | After retries exhausted, log at ERROR and discard the notification | |

**User's choice:** DLQ — retry later (Recommended)
**Notes:** Telecom-grade reliability requires not silently dropping failed notifications. DLQ enables reprocessing.

---

## Area: Circuit breaker granularity

|| Option | Description | Selected |
|--------|---------|------------|---------|
| Per host:port (Recommended) | One breaker per AAA server host:port; matches current AAAConfig scope | ✓ |
| Per SST+SD+host | One breaker per S-NSSAI slice+AAA host combination; finer isolation for S-NSSAI-specific AAA routing | |

**User's choice:** Per host:port (Recommended)
**Notes:** Keep it simple to start. Per S-NSSAI can be added if needed.

---

## Area: NRF unavailable at startup

|| Option | Description | Selected |
|--------|---------|------------|---------|
| Exit at startup (Recommended) | Biz Pod exits with code 1 if NRF registration fails at boot; Kubernetes readiness probe prevents traffic until registered | |
| Start degraded, retry in background | Biz Pod starts; NRF registration retried with exponential backoff; use cached NRF data or return errors until registered | ✓ |

**User's choice:** Start degraded, retry in background
**Notes:** Important for dev/test environments where NRF may not be immediately available.

---

## Deferred Ideas

None — all discussion stayed within phase scope.

