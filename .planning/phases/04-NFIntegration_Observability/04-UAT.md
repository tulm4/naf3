---
status: testing
phase: 04-NFIntegration_Observability
source: 04-PLAN.md, 04-VALIDATION.md
started: 2026-04-26T14:37:00Z
updated: 2026-04-26T16:00:00Z
---

## Current Test

number: 5
name: GPSI Hashing in Structured Logs
expected: |
  Structured JSON logs contain a hashed GPSI (not raw GPSI) in log output.
  Hash format: SHA256(gpsi)[0:8], base64url encoded (e.g. "PDowrA4u").
  Raw GPSI never appears in log fields.
awaiting: user response

## Tests

### 1. Cold Start Smoke Test
expected: |
  Start the Biz Pod using the config file at compose/configs/biz.yaml.
  The server boots without panics or exit(1) errors.
  Any seed/migration completes. A health check (GET /healthz/live)
  returns HTTP 200 with JSON body {"status":"ok","service":"nssAAF-biz"}.
result: pass
note: Docker build succeeded (ghcr.io/operator/nssaaf-biz:a0613b4-dirty).
  go build ./... passes. Fixed Makefile (nssAAF→nssaaf), Dockerfile.biz
  (golang:1.22→1.25-alpine), added oapi-gen/ copy before go mod download.
  biz.yaml starts without exit(1) — PostgreSQL and Redis are healthy.

### 2. /healthz/live Endpoint
expected: |
  GET /healthz/live returns HTTP 200.
  Response body is JSON with "status":"ok" and "service":"nssAAF-biz".
  No dependency checks are performed.
result: pass
note: curl http://localhost:8080/healthz/live returns HTTP 200 with
  {"status":"ok","service":"nssAAF-biz"}. Confirmed via live test.

### 3. /healthz/ready Endpoint
expected: |
  GET /healthz/ready returns HTTP 200 or 503.
  Response body is JSON listing each dependency (postgres, redis, etc.)
  with its status. Returns 503 only if a critical dependency is unhealthy
  (not just "degraded" or "not initialized").
result: pass
note: curl http://localhost:8080/healthz/ready returns HTTP 200 with
  {"nrf_registration":"degraded (retrying)","postgres":"ok","redis":"ok"}.
  NRF correctly reported as degraded (not unhealthy) per D-04.

### 4. Prometheus Metrics Endpoint
expected: |
  GET /metrics returns HTTP 200 with Prometheus text format.
  Contains nssAAF_requests_total, nssAAF_eap_sessions_active,
  nssAAF_circuit_breaker_state, nssAAF_db_connections_active,
  nssAAF_dlq_depth metrics.
result: issue
reported: "nssAAF_requests_total, nssAAF_aaa_requests_total, and other request-based
  metrics are not appearing in /metrics output. Only EAP session and DB gauge metrics
  are present. nssAAF_circuit_breaker_state also missing."
severity: major
note: FIXED: CircuitBreakerState was declared as CounterVec (wrong type)
  instead of GaugeVec. Added newGaugeVec helper, changed type, added missing
  newGaugeVec registration function. Binary rebuilt. Remaining: request/AAA
  metrics are never incremented by the code (middleware not instrumented yet).

### 5. GPSI Hashing in Structured Logs
expected: |
  Structured JSON logs contain a hashed GPSI (not raw GPSI) in log output.
  Hash format: SHA256(gpsi)[0:8], base64url encoded (e.g. "PDowrA4u").
  Raw GPSI never appears in log fields.
result: [pending]

### 6. Circuit Breaker State Transitions
expected: |
  When NRF or UDM is unreachable, the circuit breaker transitions:
  CLOSED (normal) → OPEN (after 5 consecutive failures) → HALF_OPEN
  (after 30s recovery timeout) → CLOSED (after 3 successes).
  Requests are rejected while circuit is OPEN without attempting the call.
result: [pending]

### 7. Exponential Retry Backoff
expected: |
  When a downstream call (NRF, UDM, AMF) fails with 5xx or 429,
  retries are attempted with delays of approximately 1s, 2s, 4s.
  Maximum 3 retry attempts before returning error.
result: [pending]

### 8. NRF Background Registration
expected: |
  On startup, NSSAAF does not block waiting for NRF registration.
  NRF registration is attempted asynchronously in a background goroutine
  with exponential backoff on failure. Server is immediately ready
  to receive requests (degraded mode).
result: pass
note: Confirmed via live test — server logs "biz HTTP server listening"
  immediately, then logs "nrf registration failed, retrying" with
  backoff=1s, 2s, 4s, 8s in background. Server handles requests
  without blocking.

### 9. PostgreSQL Session Store
expected: |
  After slice authentication, session data (GPSI, S-NSSAI, AMF URI,
  EAP state) is stored in PostgreSQL via NewSessionStore/NewAIWSessionStore.
  Sessions survive Biz Pod restart.
result: [pending]

### 10. UDM Nudm_UECM_Get Call
expected: |
  Before routing to AAA, the N58 handler calls UDM Nudm_UECM_Get
  with the SUPI to retrieve auth subscription (EAP method, AAA server).
  If UDM is unavailable, the call fails gracefully with error response.
result: [pending]

### 11. AMF Re-Auth Notification
expected: |
  When AAA-S triggers slice re-authentication, NSSAAF POSTs a
  re-auth notification to the AMF's reauthNotifUri endpoint.
  On failure, notifications are retried then enqueued to Redis DLQ
  (key: nssAAF:dlq:amf-notifications) instead of dropped.
result: [pending]

### 12. AUSF MSK Forwarding
expected: |
  After EAP-TLS completion, NSSAAF calls AUSF ForwardMSK to forward
  the Master Session Key (MSK) via the N60 interface.
result: [pending]

### 13. OTel Tracing Initialized
expected: |
  OpenTelemetry is initialized at startup with W3C TraceContext
  propagator. HTTP clients use otelhttp transport for automatic
  span creation. No panics or errors at startup.
result: pass
note: Confirmed via live test — NRF HTTP calls produce JSON trace spans
  with TraceID, SpanID, SpanKind=3 (client), http.method, http.url,
  net.peer.name, exception event. otelhttp.NewTransport is confirmed
  in httpClient initialization. W3C TraceContext propagator confirmed
  in tracing.Init(). Stdout exporter outputs spans.

### 14. ServiceMonitor CRDs Exist
expected: |
  deployments/nssaa-biz/servicemonitor.yaml exists and is valid YAML
  containing ServiceMonitor definitions for nssaa-biz, nssaa-http-gw,
  and nssaa-aaa-gw with correct labels and /metrics endpoint path.
result: [pending]

### 15. Prometheus Alerting Rules Exist
expected: |
  deployments/nssaa-biz/prometheusrules.yaml exists and is valid YAML
  containing Prometheus alerts: NssaaHighErrorRate (>1% error rate),
  NssaaHighLatency (P99 >500ms), NssaaCircuitBreakerOpen,
  NssaaSessionTableFull (>45k sessions), NssaaDLQDepthHigh (>100 DLQ).
result: [pending]

### 16. biz.yaml Config Fixture
expected: |
  compose/configs/biz.yaml exists and contains valid YAML with
  nrf.baseURL, udm.baseURL, ausf.baseURL, database.*, redis.*,
  and server.* sections. The service can start using this config
  without requiring additional environment variables.
result: pass
note: compose/configs/biz.yaml exists with all required sections.
  compose/dev.yaml was updated to include postgres service with healthcheck.
  All 6 Dockerfiles updated: golang:1.22→1.25-alpine, added oapi-gen/ copy.

## Summary

total: 16
passed: 6
issues: 1
pending: 9
skipped: 0
blocked: 0

## Gaps

- truth: "GET /metrics returns nssAAF_requests_total, nssAAF_aaa_requests_total, nssAAF_circuit_breaker_state and all other declared metrics"
  status: failed
  reason: "User reported: Only EAP session and DB gauge metrics appear. Request metrics and AAA metrics are not appearing because the handler middleware does not increment RequestsTotal/AaaRequestsTotal counters."
  severity: major
  test: 4
  root_cause: ""
  artifacts: []
  missing: []
  debug_session: ""
