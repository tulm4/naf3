---
spec: OpenTelemetry / Prometheus / ETSI NFV-IFA 032
section: Observability
interface: N/A
service: Metrics, Logs, Traces
---

# NSSAAF Observability Design

## 1. Overview

Full-stack observability gồm Prometheus metrics, structured logging, và distributed tracing cho phép operations team giám sát và debug NSSAAF production.

---

## 2. Prometheus Metrics

### 2.1 Core Metrics

```go
// Metrics definitions
var (
    // Request metrics
    RequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_requests_total",
            Help: "Total NSSAA API requests",
        },
        []string{"service", "endpoint", "method", "status_code"},
    )

    RequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssAAF_request_duration_seconds",
            Help:    "NSSAA API request latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
        },
        []string{"service", "endpoint", "method"},
    )

    // EAP session metrics
    EapSessionsActive = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "nssAAF_eap_sessions_active",
            Help: "Number of active EAP sessions",
        },
    )

    EapSessionsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_eap_sessions_total",
            Help: "Total EAP sessions",
        },
        []string{"result"},  // success, failure, timeout
    )

    EapSessionDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssAAF_eap_session_duration_seconds",
            Help:    "EAP session duration",
            Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
        },
        []string{"eap_method"},  // EAP-TLS, EAP-TTLS, etc.
    )

    EapRounds = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "nssAAF_eap_rounds",
            Help:    "Number of EAP rounds per session",
            Buckets: []float64{1, 2, 3, 5, 10, 20},
        },
    )

    // AAA protocol metrics
    AaaRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_aaa_requests_total",
            Help: "Total AAA protocol requests",
        },
        []string{"protocol", "server", "result"},
    )

    AaaRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssAAF_aaa_request_duration_seconds",
            Help:    "AAA request latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
        },
        []string{"protocol", "server"},
    )

    // Database metrics
    DbQueryDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssAAF_db_query_duration_seconds",
            Help:    "Database query latency",
            Buckets: []float64{.001, .002, .005, .01, .025, .05, .1},
        },
        []string{"operation", "table"},
    )

    DbConnectionsActive = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "nssAAF_db_connections_active",
            Help: "Active database connections",
        },
    )

    // Redis metrics
    RedisOperationsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_redis_operations_total",
            Help: "Total Redis operations",
        },
        []string{"operation", "result"},
    )

    // Circuit breaker metrics
    CircuitBreakerState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nssAAF_circuit_breaker_state",
            Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
        },
        []string{"server"},
    )

    CircuitBreakerFailures = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_circuit_breaker_failures_total",
            Help: "Total circuit breaker recorded failures",
        },
        []string{"server"},
    )

    // Rate limiter metrics
    RateLimitRejections = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssAAF_rate_limit_rejections_total",
            Help: "Total rate limit rejections",
        },
        []string{"type"},  // per-gpsi, per-amf, global
    )

    // NRF discovery cache
    NrfCacheHits = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "nssAAF_nrf_cache_hits_total",
            Help: "NRF cache hits",
        },
    )

    NrfCacheMisses = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "nssAAF_nrf_cache_misses_total",
            Help: "NRF cache misses",
        },
    )
)
```

### 2.2 ServiceMonitor for Prometheus

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssAAF-monitor
  labels:
    team: platform
spec:
  selector:
    matchLabels:
      app: nssAAF
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssAAF
```

---

## 3. Structured Logging

### 3.1 Log Format

```go
// JSON structured logs
type LogEntry struct {
    Timestamp    string `json:"timestamp"`
    Level        string `json:"level"`  // DEBUG, INFO, WARN, ERROR
    Message      string `json:"message"`
    RequestID    string `json:"request_id,omitempty"`
    TraceID      string `json:"trace_id,omitempty"`
    SpanID       string `json:"span_id,omitempty"`

    // Context
    Service      string `json:"service"`
    Version      string `json:"version"`
    Hostname     string `json:"hostname"`
    PodName      string `json:"pod_name"`
    Namespace    string `json:"namespace"`

    // Request context
    GpsiHash     string `json:"gpsi_hash,omitempty"`  // hashed for privacy
    AmfInstanceId string `json:"amf_instance_id,omitempty"`
    SnssaiSst    int    `json:"snssai_sst,omitempty"`
    SnssaiSd     string `json:"snssai_sd,omitempty"`
    AuthCtxId    string `json:"auth_ctx_id,omitempty"`

    // Operation
    Operation    string `json:"operation,omitempty"`
    DurationMs   int64  `json:"duration_ms,omitempty"`
    StatusCode   int    `json:"status_code,omitempty"`

    // Errors
    Error        string `json:"error,omitempty"`
    StackTrace   string `json:"stack_trace,omitempty"`
}
```

### 3.2 Log Levels

```go
// Log level guidelines:
DEBUG: Detailed debugging info (EAP round details, cache hits, decoded packets)
INFO:  Normal operations (session created, completed, notification sent)
WARN:  Unexpected but handled (retry, fallback, rate limited)
ERROR: Failed operations (AAA unreachable, DB error, validation failure)

// Examples:
log.Info("session_created",
    "auth_ctx_id", authCtxId,
    "gpsi_hash", hashGpsi(gpsi),
    "snssai_sst", snssai.Sst,
    "eap_method", method,
)

log.Warn("rate_limit_exceeded",
    "gpsi_hash", hashGpsi(gpsi),
    "limit", "10/min",
    "current", count,
)

log.Error("aaa_unreachable",
    "aaa_server", server,
    "error", err.Error(),
    "retry_count", retry,
)
```

---

## 4. Distributed Tracing

### 4.1 OpenTelemetry Setup

```go
// Trace propagation
import "go.opentelemetry.io/otel"
import "go.opentelemetry.io/otel/trace"
import "go.opentelemetry.io/otel/propagation"

// W3C TraceContext propagation
propagator := propagation.NewCompositeTextMapPropagator(
    propagation.TraceContext{},
    propagation.Baggage{},
)
otel.SetTextMapPropagator(propagator)

// Trace spans
func (s *NssaaService) HandleAuthRequest(ctx context.Context, req *SliceAuthInfo) (*SliceAuthContext, error) {
    // Extract trace context from incoming request
    ctx = s.propagator.Extract(ctx, propagation.HeaderCarrier(req.Headers))

    // Start span
    ctx, span := tracer.Start(ctx, "Nnssaaf_NSSAA.Authenticate",
        trace.WithAttributes(
            attribute.String("gpsi_hash", hashGpsi(req.Gpsi)),
            attribute.Int("snssai_sst", req.Snssai.Sst),
        ),
    )
    defer span.End()

    // Sub-span: DB insert
    ctx, dbSpan := tracer.Start(ctx, "db.insert_session")
    err := s.db.InsertSession(ctx, session)
    dbSpan.End()

    // Sub-span: AAA request
    ctx, aaaSpan := tracer.Start(ctx, "aaa.send_request",
        trace.WithAttributes(
            attribute.String("aaa_server", config.ServerHost),
            attribute.String("protocol", "RADIUS"),
        ),
    )

    resp, err := s.aaaClient.Send(ctx, req)
    if err != nil {
        aaaSpan.RecordError(err)
        span.RecordError(err)
    }
    aaaSpan.End()

    return resp, nil
}
```

### 4.2 Trace for Multi-Service Flow

```
AMF (Trace: abc123)
    │
    │  N58: Nnssaaf_NSSAA_Authenticate
    │  Headers: traceparent: 00-abc123-def456-01
    ▼
NSSAAF (Trace: abc123, Span: ghi789)
    │
    ├─ Span: db.insert_session
    │
    ├─ Span: aaa.send_request (RADIUS)
    │       │
    │       │  RADIUS transaction
    │       ▼
    │    AAA-S (external, no trace)
    │
    ├─ Span: db.update_session
    │
    └─ Span: http.post_notification
            │
            │  Re-Auth notification
            ▼
         AMF (Trace: xyz789, new)
```

---

## 5. Alerting Rules

```yaml
# Prometheus alerting rules
groups:
  - name: nssAAF-alerts
    rules:
      # High error rate
      - alert: NssaaHighErrorRate
        expr: |
          sum(rate(nssAAF_requests_total{status_code=~"5.."}[5m]))
          / sum(rate(nssAAF_requests_total[5m])) > 0.01
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "NSSAAF error rate > 1%"
          description: "Error rate: {{ $value | humanizePercentage }}"

      # High latency P99
      - alert: NssaaHighLatency
        expr: |
          histogram_quantile(0.99,
            sum(rate(nssAAF_request_duration_seconds_bucket[5m]))
            by (le)) > 0.5
        for: 5m
        labels:
          severity: major
        annotations:
          summary: "NSSAAF P99 latency > 500ms"

      # Circuit breaker open
      - alert: NssaaCircuitBreakerOpen
        expr: nssAAF_circuit_breaker_state == 1
        for: 1m
        labels:
          severity: major
        annotations:
          summary: "Circuit breaker OPEN for {{ $labels.server }}"

      # Session table near capacity
      - alert: NssaaSessionTableFull
        expr: nssAAF_eap_sessions_active > 45000
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "EAP sessions approaching limit (45k/50k)"

      # Database unavailable
      - alert: NssaaDatabaseUnreachable
        expr: nssAAF_db_query_duration_seconds_count{operation="query"}[1m] == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Database queries stopped"

      # AAA server failures
      - alert: NssaaAaaServerFailures
        expr: |
          sum(rate(nssAAF_aaa_requests_total{result="failure"}[5m]))
          by (server) > 10
        for: 3m
        labels:
          severity: major
        annotations:
          summary: "High failure rate for AAA server {{ $labels.server }}"
```

---

## 6. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Prometheus metrics: requests, latency, EAP sessions, AAA stats | prometheus.NewCounterVec, HistogramVec |
| AC2 | ServiceMonitor for Prometheus scraping | Prometheus Operator CRD |
| AC3 | Structured JSON logs with trace context | LogEntry struct, zerolog |
| AC4 | OpenTelemetry traces with W3C TraceContext | otel.SetTextMapPropagator |
| AC5 | Trace spans: per handler, DB, AAA, notification | tracer.Start() |
| AC6 | Alert rules: error rate >1%, P99 >500ms, circuit breaker | PrometheusRule |
