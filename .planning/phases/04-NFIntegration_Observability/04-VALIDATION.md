---
phase: 4
slug: nfintegration-observability
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-25
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` package (stdlib) |
| **Config file** | `compose/configs/biz.yaml` (create as part of phase) |
| **Quick run command** | `go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... -x -count=1` |
| **Full suite command** | `go test ./... -count=1 && go build ./cmd/...` |
| **Estimated runtime** | ~30 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** `go test ./internal/<module>/... -x -count=1`
- **After every wave:** `go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... -count=1`
- **Before `/gsd-verify-work`:** Full suite must be green (`go test ./... -count=1`)
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Requirement | Behavior | Test Type | Automated Command | File Needed | Status |
|---------|-----------|----------|-----------|------------------|-------------|--------|
| NRF registration | REQ-01 | NRF registration on startup (background, non-blocking) | Unit | `go test ./internal/nrf/... -run TestNRFClient_Register` | internal/nrf/client_test.go | pending |
| NRF heartbeat | REQ-02 | Nnrf_NFHeartBeat every 5 minutes | Unit | `go test ./internal/nrf/... -run TestNRFClient_Heartbeat` | internal/nrf/client_test.go | pending |
| AMF discovery | REQ-03 | Nnrf_NFDiscovery returns AMF profile | Unit | `go test ./internal/nrf/... -run TestDiscoverAMF` | internal/nrf/client_test.go | pending |
| UDM wiring | REQ-04 | UDM Nudm_UECM_Get called before AAA routing | Unit | `go test ./internal/udm/... -run TestUdmClient_GetAuthContext` | internal/udm/client_test.go | pending |
| UDM update | REQ-05 | UDM Nudm_UECM_UpdateAuthContext after EAP | Unit | `go test ./internal/udm/... -run TestUpdateAuthContext` | internal/udm/client_test.go | pending |
| AMF re-auth | REQ-06 | Re-Auth POST to reauthNotifUri with retry | Unit | `go test ./internal/amf/... -run TestReAuthNotification` | internal/amf/notifier_test.go | pending |
| AMF revocation | REQ-07 | Revocation POST to revocNotifUri with retry | Unit | `go test ./internal/amf/... -run TestRevocNotification` | internal/amf/notifier_test.go | pending |
| AUSF client | REQ-08 | internal/ausf/ created with ForwardMSK | Unit | `go test ./internal/ausf/... -run TestNewClient` | internal/ausf/client_test.go | pending |
| PG session store | REQ-09 | NewSessionStore implements nssaa.AuthCtxStore | Unit | `go test ./internal/storage/postgres/... -run TestSessionStore` | internal/storage/postgres/session_store_test.go | pending |
| DLQ | REQ-10 | DLQ enqueue on retry exhaustion | Unit | `go test ./internal/cache/redis/... -run TestDLQEnqueue` | internal/cache/redis/dlq_test.go | pending |
| Circuit breaker | REQ-11 | CLOSED→OPEN(5)→HALF_OPEN(30s)→CLOSED(3) | Unit | `go test ./internal/resilience/... -run TestCircuitBreaker` | internal/resilience/circuit_breaker_test.go | pending |
| Retry backoff | REQ-12 | Exponential backoff 1s, 2s, 4s, max 3 retries | Unit | `go test ./internal/resilience/... -run TestRetryWithBackoff` | internal/resilience/retry_test.go | pending |
| Timeouts | REQ-13 | Context deadlines on HTTP calls | Unit | `go test ./internal/nrf/... -run TestClientTimeouts` | internal/nrf/client_test.go | pending |
| Prometheus metrics | REQ-14 | /metrics endpoint with correct metrics | Integration | `curl :8080/metrics` + grep for metric names | configs/biz.yaml | pending |
| ServiceMonitor | REQ-15 | CRDs valid YAML with correct labels | Manual | `yamllint deployments/nssaa-biz/servicemonitor.yaml` | deployments/nssaa-biz/servicemonitor.yaml | pending |
| Structured logging | REQ-16 | GPSI hashed in JSON logs (SHA256, 8 bytes) | Unit | `go test ./internal/logging/... -run TestGPSIHash` | internal/logging/logging_test.go | pending |
| OTel tracing | REQ-17 | Trace spans created and propagated | Integration | `curl -s :8080/healthz/live` + check trace export | OTel config in main.go | pending |
| /healthz/live | REQ-18 | Always 200, no dependency checks | Integration | `curl -s -o /dev/null -w '%{http_code}' :8080/healthz/live` | health handler in main.go | pending |
| /healthz/ready | REQ-19 | 200 only when all deps healthy | Integration | `curl -s :8080/healthz/ready` + check JSON body | health handler in main.go | pending |

*Status: pending = not yet executed*

---

## Wave 0 Requirements

All test files below must be created as Wave 0 before any implementation tasks:

- [ ] `internal/nrf/client_test.go` — covers REQ-01, REQ-02, REQ-03, REQ-13
- [ ] `internal/udm/client_test.go` — covers REQ-04, REQ-05
- [ ] `internal/amf/notifier_test.go` — covers REQ-06, REQ-07
- [ ] `internal/ausf/client_test.go` — covers REQ-08
- [ ] `internal/storage/postgres/session_store_test.go` — covers REQ-09
- [ ] `internal/cache/redis/dlq_test.go` — covers REQ-10
- [ ] `internal/resilience/circuit_breaker_test.go` — covers REQ-11
- [ ] `internal/resilience/retry_test.go` — covers REQ-12
- [ ] `internal/logging/logging_test.go` — covers REQ-16
- [ ] `compose/configs/biz.yaml` — test fixture config
- [ ] `go get github.com/prometheus/client_golang@v1.20.5 github.com/prometheus/client_golang/prometheus@v1.20.5 go.opentelemetry.io/otel@v1.32.0 go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp/v1.32.0`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| ServiceMonitor CRDs applied to cluster | REQ-15 | Requires Kubernetes cluster and Prometheus Operator | `kubectl apply --dry-run=server -f deployments/nssaa-biz/servicemonitor.yaml` |
| Cross-component OTel trace propagation | REQ-17 | Requires Jaeger/Tempo backend to inspect spans | Start Biz Pod + HTTP GW + AAA GW, send test request, verify trace in UI |
| P99 latency tracking per component | REQ-19 | Requires load test + Prometheus histogram queries | `promtool query instant 'histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))'` |

---

## Validation Sign-Off

- [ ] All tasks have automated `go test` verify or Wave 0 dependencies listed above
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all test file gaps listed above
- [ ] `go build ./cmd/...` compiles after each wave
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending

---

*Generated: 2026-04-25 from research*
