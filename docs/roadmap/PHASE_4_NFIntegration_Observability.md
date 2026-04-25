# Phase 4: NF Integration & Observability

## Overview

Phase 4 combines three cross-cutting concerns into a single phase: **NF (Network Function) integration**, **resilience patterns**, and **observability**. All three are essential for telecom-grade operation (>99.999% availability) and 5G network integration.

NF integration wires the NRF, UDM, AMF, and AUSF clients into `cmd/biz/main.go`, making NSSAAF visible and functional in a real 5G network. Without this, AMF cannot discover NSSAAF and no slice authentication can occur.

Resilience patterns (circuit breakers, retries, timeouts) prevent cascade failures when AAA servers are unhealthy. Observability (metrics, logging, tracing) provides the visibility needed to detect and diagnose issues in production.

**Spec Foundation:** TS 29.510 §6, TS 29.526 §7.2-7.3, ETSI NFV-IFA 032, OpenTelemetry, Prometheus Operator

---

## Modules to Implement

### 0. `internal/nrf/` — NRF Client Wiring

**Priority:** P0 (cross-cutting, blocks Phase 4 NF integration)
**Dependencies:** `internal/config/`, `internal/metrics/`
**Design Doc:** `docs/design/05_nf_profile.md`
**Status:** `internal/nrf/` is a stub (4 lines) — must be wired in `cmd/biz/main.go`

#### 0.1 NRF Client Interface

```go
// internal/nrf/client.go — extend existing stub
type NRFClient interface {
    Register(ctx context.Context) error          // Nnrf_NFRegistration
    StartHeartbeat(ctx context.Context)          // Nnrf_NFHeartBeat (periodic)
    DiscoverNF(ctx context.Context, nfType string, plmnID string) (*NFProfile, error) // Nnrf_NFDiscovery
    SubscribeStatus(ctx context.Context, nfType string, ids []string) error           // Nnrf_NFStatusSubscribe
    HandleStatusChange(w http.ResponseWriter, r *http.Request)                        // Callback receiver
}
```

#### 0.2 Startup Registration in cmd/biz/main.go

**Priority: P0 — blocks 5G network integration**

Required changes in `cmd/biz/main.go`:

```go
// 1. Create NRF client
nrfClient := nrf.NewClient(nrf.Config{
    BaseURL: cfg.NRF.BaseURL,
    NfType: "NSSAAF",
    NfInstanceID: podID,
    HeartbeatInterval: 5 * time.Minute,
})

// 2. Register (blocks until registered or 10s timeout)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := nrfClient.Register(ctx); err != nil {
    slog.Error("NRF registration failed", "error", err)
    os.Exit(1)
}

// 3. Start heartbeat goroutine
go nrfClient.StartHeartbeat(context.Background())

// 4. Wire NRF client to N58 handler
nssaaHandler := nssaa.NewHandler(nssaaStore,
    nssaa.WithUDMClient(udmClient),
    nssaa.WithAAA(aaaClient),
    nssaa.WithNRFClient(nrfClient),  // NEW: for AMF discovery
)

// 5. Wire NRF discovery to AMF notifier
amfNotifier := amf.NewNotifier(amf.Config{
    HTTPClient: &http.Client{Timeout: 5 * time.Second},
    MaxRetries: 3,
    NRFClient: nrfClient,  // NEW: discover AMF FQDN before POST
})
```

#### 0.3 Nnrf_NFDiscovery Usage

```go
// Discover AMF FQDN before sending re-auth/revocation notifications
amfProfile, err := nrfClient.DiscoverNF(ctx, "AMF", servingPlmnId)
if err != nil {
    return fmt.Errorf("AMF discovery failed: %w", err)
}
amfNotifyURI := amfProfile.SBIAddresses[0].IPv4Address + "/namf-comm/v1/"
```

#### 0.4 Nnrf_NFStatusSubscribe Usage

```go
// Subscribe to AMF status changes
if err := nrfClient.SubscribeStatus(ctx, "AMF", []string{amfInstanceID}); err != nil {
    slog.Warn("AMF status subscription failed", "error", err)
}

// Register callback endpoint for NRF → NSSAAF notifications
http.HandleFunc("/nrf/callback", nrfClient.HandleStatusChange)
```

---

### 1. `internal/udm/` — UDM Client Wiring

**Priority:** P0 — required by TS 23.502 §4.2.9.1
**Status:** `internal/udm/` is a stub (5 lines) — must be implemented and wired in `cmd/biz/main.go`

#### 1.1 Nudm_UECM_Get Implementation

```go
// internal/udm/uecm.go — implement Nudm_UECM_Get

// GetAuthSubscription retrieves auth subscription from UDM (TS 29.526 §7.3.2)
// GET /nudm-uecm/v1/{gpsi}/auth-subscriptions?snssai.sst={sst}&snssai.sd={sd}
func (c *UDMClient) GetAuthSubscription(ctx context.Context, gpsi string, snssai Snssai) (*AuthSubscription, error)

// UpdateAuthContext updates UDM with final NssaaStatus (TS 29.526 §7.2.3.3)
// PUT /nudm-uecm/v1/{gpsi}/auth-contexts/{authCtxId}
func (c *UDMClient) UpdateAuthContext(ctx context.Context, authCtxId string, status NssaaStatus) error
```

#### 1.2 Wiring in cmd/biz/main.go

```go
import "github.com/operator/nssAAF/internal/udm"

// 1. Create UDM client
udmClient := udm.NewClient(udm.Config{
    BaseURL: cfg.UDM.BaseURL,
    NrfClient: nrfClient,  // Discover UDM via NRF
})

// 2. Inject into N58 handler
// In internal/api/nssaa/handler.go — before routing to AAA:
// authSub, err := udmClient.GetAuthSubscription(ctx, gpsi, snssai)
// if err != nil { return 403 Forbidden }
// Route to AAA based on authSub.EapMethod
```

---

### 2. `internal/amf/` — AMF Notification Sender

**Priority:** P0 — required by TS 23.502 §4.2.9.3 and §4.2.9.4
**Status:** `internal/amf/` is a stub (3 lines) — must be implemented and wired

#### 2.1 AMF Notifier Implementation

```go
// internal/amf/notifier.go — implement AMF notification sender

// SendReAuthNotification POSTs to AMF reauthNotifUri (TS 23.502 §4.2.9.3)
// POST {reauthNotifUri}
// Body: Namf_Communication_N1N2MessageTransferRequest
func (n *AMFNotifier) SendReAuthNotification(ctx context.Context, uri string, req *ReAuthRequest) error {
    // Retry 3x with exponential backoff
}

// SendRevocationNotification POSTs to AMF revocNotifUri (TS 23.502 §4.2.9.4)
func (n *AMFNotifier) SendRevocationNotification(ctx context.Context, uri string, req *RevocRequest) error {
    // On persistent failure: enqueue to DLQ
}
```

#### 2.2 Replace Stub Handlers in cmd/biz/main.go

Replace `handleReAuth` and `handleRevocation` (currently return hardcoded bytes):

```go
// REPLACE stubs (lines 188-201) with actual implementation:
amfNotifier := amf.NewNotifier(amf.Config{
    HTTPClient: &http.Client{Timeout: 5 * time.Second},
    MaxRetries: 3,
    NRFClient: nrfClient,  // Discover AMF FQDN before POST
})

func handleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    // Lookup session by authCtxId to get reauthNotifUri
    session, err := sessionStore.Get(ctx, req.AuthCtxID)
    if err != nil {
        slog.Error("session not found for re-auth", "auth_ctx_id", req.AuthCtxID)
        return nil
    }
    // POST to AMF reauthNotifUri
    err = amfNotifier.SendReAuthNotification(ctx, session.ReauthNotifURI, &amf.ReAuthRequest{
        Gpsi:   session.Gpsi,
        Snssai: session.Snssai,
        Supi:   session.Supi,
    })
    if err != nil {
        slog.Error("re-auth notification failed", "error", err)
    }
    return []byte{2, 0, 0, 12} // RADIUS CoA-Ack
}
```

---

### 3. `internal/ausf/` — AUSF N60 Client (CREATE)

**Priority:** P0 — required by TS 23.502 §4.2.9.2 for SNPN authentication
**Status:** `internal/ausf/` directory does NOT exist — must be created
**Design Doc:** `docs/design/23_ausf_integration.md`

#### 3.1 Package Structure

```
internal/ausf/
├── client.go       # N60Client with Authenticate() and ForwardMSK()
├── types.go        # UEEuthRequest, UEEuthResponse types
└── client_test.go  # Unit tests
```

#### 3.2 N60Client Implementation

```go
// internal/ausf/client.go
package ausf

// N60Client handles AUSF N60 interface (TS 29.526 §7.3)
// POST /nnssaaf-aiw/v1/ue-authentications — Nudm_UEAuthentication_Get callout
type N60Client struct {
    httpClient *http.Client
    baseURL    string  // Discovered via NRF
    validator  *auth.TokenValidator
}

// ForwardMSK forwards MSK to AUSF after successful EAP-TLS (TS 23.502 §4.2.9.2)
// POST /nausf-auth/v1/ue-authentications/{authCtxId}/msk
func (c *N60Client) ForwardMSK(ctx context.Context, authCtxId string, msk []byte) error
```

#### 3.3 Wiring in cmd/biz/main.go

```go
ausfClient := ausf.NewClient(ausf.Config{
    BaseURL: cfg.AUSF.BaseURL,
    HTTPClient: &http.Client{Timeout: 30 * time.Second},
    NRFClient: nrfClient,  // Discover AUSF via NRF
})

aiwHandler := aiw.NewHandler(aiwStore,
    aiw.WithAUSFClient(ausfClient),  // NEW: inject AUSF client
    aiw.WithAAA(aaaClient),
)
```

---

### 4. PostgreSQL Session Storage (CRITICAL)

**Priority:** P0 — production requires persistent sessions
**Status:** Package exists (`internal/storage/postgres/`) but NOT wired in `cmd/biz/main.go`
**Spec:** TS 29.526 §7.2, `docs/design/11_database_ha.md`

```go
// cmd/biz/main.go — REPLACE in-memory store:

import "github.com/operator/nssAAF/internal/storage/postgres"

db, err := postgres.New(postgres.Config{
    DSN: cfg.Database.DSN,
    MaxConns: 50,
    MaxIdleConns: 10,
})
if err != nil {
    slog.Error("database connection failed", "error", err)
    os.Exit(1)
}
defer db.Close()

// Migrate schema
if err := db.Migrate(context.Background()); err != nil {
    slog.Error("database migration failed", "error", err)
    os.Exit(1)
}

nssaaStore := postgres.NewSessionStore(db)
```

---

## Modules to Implement

### 5. `internal/resilience/` — Resilience Patterns

**Priority:** P0
**Dependencies:** `internal/config/`, `internal/metrics/`
**Design Doc:** `docs/design/10_ha_architecture.md`, `docs/design/19_observability.md`

#### 5.1 Circuit Breaker (`circuit_breaker.go`)

Per-AAA-S circuit breaker prevents cascade failures when an AAA server is unhealthy.

```go
// Circuit breaker states
type CircuitState int

const (
    CB_CLOSED CircuitState = iota // Normal operation
    CB_OPEN                       // Failing fast, no requests
    CB_HALF_OPEN                  // Testing recovery
)

// Per-AAA-S circuit breaker
type CircuitBreaker struct {
    mu sync.RWMutex
    state CircuitState

    // Configuration
    failureThreshold int           // Open after N consecutive failures (default: 5)
    recoveryTimeout  time.Duration // Try again after this duration (default: 30s)
    halfOpenMax     int           // Max requests in half-open (default: 3)

    // Metrics
    failures    int64
    successes   int64
    lastFailure time.Time
    lastStateChange time.Time
}

// Per-S-NSSAI circuit breaker (for S-NSSAI-specific AAA routing)
type SnssaiCircuitBreaker struct {
    snssai   Snssai
    breaker  *CircuitBreaker
    aaaHost  string
}

// Global circuit breaker registry
type CircuitBreakerRegistry struct {
    mu        sync.RWMutex
    breakers  map[string]*CircuitBreaker  // key: "host:port"
    bySnssai  map[string]*SnssaiCircuitBreaker
}

var globalBreakerRegistry *CircuitBreakerRegistry

// Wrap calls automatically
func (cb *CircuitBreaker) Do(ctx context.Context, fn func() error) error {
    cb.mu.Lock()
    state := cb.getState()
    cb.mu.Unlock()

    if state == CB_OPEN {
        return ErrCircuitOpen
    }

    err := fn()

    cb.mu.Lock()
    defer cb.mu.Unlock()

    if err != nil {
        cb.onFailure()
    } else {
        cb.onSuccess()
    }

    return err
}
```

**State Transitions:**
```
CLOSED ──(5 consecutive failures)──► OPEN
OPEN ──(30s recovery timeout)────► HALF_OPEN
HALF_OPEN ──(3 requests succeed)──► CLOSED
HALF_OPEN ──(1 failure)───────────► OPEN
```

#### 5.2 Retry with Exponential Backoff (`retry.go`)

```go
// Retry configuration
type RetryConfig struct {
    MaxAttempts int           // Maximum retry attempts (default: 3)
    BaseDelay   time.Duration // Initial delay (default: 1s)
    MaxDelay    time.Duration // Cap delay at (default: 30s)
    Multiplier  float64       // Exponential multiplier (default: 2.0)
    Jitter      bool          // Add random jitter (default: true)
}

// Retryable errors
var retryableErrors = []error{
    ErrAaaTimeout,
    ErrAaaConnectionReset,
    ErrNetworkTemporarilyUnavailable,
}

func IsRetryable(err error) bool {
    for _, e := range retryableErrors {
        if errors.Is(err, e) {
            return true
        }
    }
    return false
}

// Retry with backoff
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
    var lastErr error
    delay := cfg.BaseDelay

    for attempt := 0; attempt <= cfg.MaxAttempts; attempt++ {
        if attempt > 0 {
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(delay):
            }

            delay = time.Duration(float64(delay) * cfg.Multiplier)
            if delay > cfg.MaxDelay {
                delay = cfg.MaxDelay
            }
        }

        lastErr = fn()
        if lastErr == nil {
            return nil
        }

        if !IsRetryable(lastErr) {
            return lastErr
        }
    }

    return fmt.Errorf("retry exhausted after %d attempts: %w", cfg.MaxAttempts, lastErr)
}
```

#### 5.3 Timeout Handling (`timeout.go`)

```go
// Timeout configuration per operation
const (
    DefaultEAPRoundTimeout = 30 * time.Second  // Per EAP round
    DefaultAAARequestTimeout = 10 * time.Second // AAA request
    DefaultDBTimeout        = 5  * time.Second // Database
    DefaultRedisTimeout     = 100 * time.Millisecond // Cache
    DefaultNRFTimeout       = 5  * time.Second // NRF discovery
)

// Context with timeout
func WithOperationTimeout(ctx context.Context, op string) (context.Context, context.CancelFunc) {
    var timeout time.Duration
    switch op {
    case "eap_round":
        timeout = DefaultEAPRoundTimeout
    case "aaa_request":
        timeout = DefaultAAARequestTimeout
    case "db_query":
        timeout = DefaultDBTimeout
    case "redis_op":
        timeout = DefaultRedisTimeout
    case "nrf_discovery":
        timeout = DefaultNRFTimeout
    default:
        timeout = 30 * time.Second
    }

    return context.WithTimeout(ctx, timeout)
}
```

#### 5.4 Health Endpoints (`health.go`)

Kubernetes-style health endpoints for liveness and readiness probes.

```go
// Health check types
type HealthStatus struct {
    Status  string            `json:"status"`  // "healthy", "degraded", "unhealthy"
    Checks  []HealthCheck     `json:"checks"`
    Version string            `json:"version"`
    Uptime string            `json:"uptime"`
}

type HealthCheck struct {
    Name    string `json:"name"`
    Status  string `json:"status"`  // "ok", "failed"
    Message string `json:"message,omitempty"`
    Latency string `json:"latency,omitempty"`
}

// GET /healthz/live — Liveness probe
// Returns 200 if process is alive (can be degraded)
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(HealthStatus{
        Status:  "healthy",
        Checks:  []HealthCheck{{Name: "process", Status: "ok"}},
        Version: version,
        Uptime:  time.Since(startTime).String(),
    })
}

// GET /healthz/ready — Readiness probe
// Returns 200 only if ready to serve traffic
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
    checks := h.aggregator.RunAll()
    overall := "healthy"

    for _, c := range checks {
        if c.Status == "failed" {
            overall = "unhealthy"
            break
        }
        if c.Status == "degraded" {
            overall = "degraded"
        }
    }

    status := 200
    if overall == "unhealthy" {
        status = 503
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(HealthStatus{
        Status:  overall,
        Checks:  checks,
        Version: version,
        Uptime:  time.Since(startTime).String(),
    })
}

// Per-component health checks
func (h *HealthHandler) runChecks() []HealthCheck {
    return []HealthCheck{
        h.checkDatabase(),
        h.checkRedis(),
        h.checkAAAConnections(),
        h.checkCircuitBreakers(),
    }
}

func (h *HealthHandler) checkDatabase() HealthCheck {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    start := time.Now()
    err := h.db.PingContext(ctx)
    latency := time.Since(start)

    if err != nil {
        return HealthCheck{Name: "database", Status: "failed", Message: err.Error()}
    }
    return HealthCheck{Name: "database", Status: "ok", Latency: latency.String()}
}
```

---

### 6. `internal/metrics/` — Prometheus Metrics

**Priority:** P0
**Dependencies:** Prometheus client library

#### 6.1 Core Metrics (`metrics.go`)

```go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // HTTP request metrics
    RequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_requests_total",
            Help: "Total HTTP requests to NSSAAF API",
        },
        []string{"component", "endpoint", "method", "status_code"},
    )

    RequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_request_duration_seconds",
            Help:    "HTTP request latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
        },
        []string{"component", "endpoint", "method"},
    )

    // EAP session metrics
    EapSessionsActive = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "nssaa_eap_sessions_active",
            Help: "Number of active EAP sessions",
        },
    )

    EapSessionsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_eap_sessions_total",
            Help: "Total EAP sessions completed",
        },
        []string{"result", "eap_method"},  // result: success, failure, timeout
    )

    EapSessionDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_eap_session_duration_seconds",
            Help:    "EAP session duration",
            Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
        },
        []string{"eap_method"},
    )

    // AAA protocol metrics
    AaaRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_aaa_requests_total",
            Help: "Total AAA protocol requests",
        },
        []string{"component", "protocol", "server", "result"},
    )

    AaaRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_aaa_request_duration_seconds",
            Help:    "AAA request latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
        },
        []string{"component", "protocol", "server"},
    )

    // Circuit breaker metrics
    CircuitBreakerState = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nssaa_circuit_breaker_state",
            Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
        },
        []string{"component", "server"},
    )

    CircuitBreakerFailures = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_circuit_breaker_failures_total",
            Help: "Total circuit breaker recorded failures",
        },
        []string{"component", "server"},
    )

    // Database metrics
    DbQueryDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_db_query_duration_seconds",
            Help:    "Database query latency",
            Buckets: []float64{.001, .002, .005, .01, .025, .05, .1},
        },
        []string{"component", "operation", "table"},
    )

    DbConnectionsActive = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nssaa_db_connections_active",
            Help: "Active database connections",
        },
        []string{"component"},
    )

    // Redis metrics
    RedisOperationsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nssaa_redis_operations_total",
            Help: "Total Redis operations",
        },
        []string{"component", "operation", "result"},
    )

    RedisOperationDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nssaa_redis_operation_duration_seconds",
            Help:    "Redis operation latency",
            Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05},
        },
        []string{"component", "operation"},
    )

    // P99 latency tracking (using histogram quantiles)
    ComponentLatencyP99 = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nssaa_component_latency_p99_seconds",
            Help: "P99 latency per component",
        },
        []string{"component", "operation"},
    )
)

// RecordRequest middleware
func RecordRequest(component, endpoint, method string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()

            // Wrap response writer to capture status
            wrapped := &statusWriter{ResponseWriter: w, statusCode: 200}
            next.ServeHTTP(wrapped, r)

            duration := time.Since(start).Seconds()
            status := fmt.Sprintf("%d", wrapped.statusCode)

            RequestsTotal.WithLabelValues(component, endpoint, method, status).Inc()
            RequestDuration.WithLabelValues(component, endpoint, method).Observe(duration)
        })
    }
}
```

#### 6.2 ServiceMonitor CRDs (`servicemonitor.yaml`)

```yaml
# Per-component ServiceMonitors for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-http-gw-monitor
  labels:
    app: nssaa-http-gateway
    team: platform
spec:
  selector:
    matchLabels:
      app: nssaa-http-gateway
      component: http-gateway
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-biz-monitor
  labels:
    app: nssaa-biz
    team: platform
spec:
  selector:
    matchLabels:
      app: nssaa-biz
      component: biz
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: nssaa-aaa-gw-monitor
  labels:
    app: nssaa-aaa-gateway
    team: platform
spec:
  selector:
    matchLabels:
      app: nssaa-aaa-gateway
      component: aaa-gateway
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
  namespaceSelector:
    matchNames:
      - nssaa
```

---

### 7. `internal/logging/` — Structured Logging

**Priority:** P0
**Dependencies:** Standard library `log/slog`

#### 7.1 JSON Structured Logs (`logging.go`)

```go
package logging

import (
    "log/slog"
    "os"
    "runtime"
    "time"
)

type LogEntry struct {
    Timestamp   string `json:"timestamp"`
    Level       string `json:"level"`
    Message     string `json:"message"`
    RequestID   string `json:"request_id,omitempty"`
    TraceID     string `json:"trace_id,omitempty"`
    SpanID      string `json:"span_id,omitempty"`
    Service     string `json:"service"`
    Version     string `json:"version"`
    Hostname    string `json:"hostname"`
    PodName     string `json:"pod_name,omitempty"`
    Namespace   string `json:"namespace,omitempty"`
    GpsiHash    string `json:"gpsi_hash,omitempty"`
    SnssaiSst   int    `json:"snssai_sst,omitempty"`
    SnssaiSd    string `json:"snssai_sd,omitempty"`
    AuthCtxId   string `json:"auth_ctx_id,omitempty"`
    Operation   string `json:"operation,omitempty"`
    DurationMs  int64  `json:"duration_ms,omitempty"`
    StatusCode  int    `json:"status_code,omitempty"`
    Error       string `json:"error,omitempty"`
    StackTrace  string `json:"stack_trace,omitempty"`
}

var defaultLogger *slog.Logger

func Init(service, version string) {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level:     slog.LevelInfo,
        AddSource: false,
        ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
            if a.Key == slog.TimeKey {
                return slog.Attr{Key: "timestamp", Value: slog.StringValue(a.Value.Time().Format(time.RFC3339Nano))}
            }
            return a
        },
    })

    defaultLogger = slog.New(handler)
    defaultLogger = defaultLogger.With(
        slog.String("service", service),
        slog.String("version", version),
        slog.String("hostname", getHostname()),
        slog.String("pod_name", getPodName()),
        slog.String("namespace", getNamespace()),
    )
}

func getPodName() string {
    if pod, ok := os.LookupEnv("POD_NAME"); ok {
        return pod
    }
    return ""
}

func Info(msg string, args ...any) {
    defaultLogger.Info(msg, args...)
}

func Warn(msg string, args ...any) {
    defaultLogger.Warn(msg, args...)
}

func Error(msg string, args ...any) {
    defaultLogger.Error(msg, args...)
}

func Debug(msg string, args ...any) {
    defaultLogger.Debug(msg, args...)
}

// WithContext returns logger with trace context
func WithContext(ctx context.Context) *slog.Logger {
    traceID := getTraceID(ctx)
    spanID := getSpanID(ctx)
    requestID := getRequestID(ctx)

    return defaultLogger.With(
        slog.String("trace_id", traceID),
        slog.String("span_id", spanID),
        slog.String("request_id", requestID),
    )
}

// Helper to hash GPSI for privacy
func HashGpsi(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return base64.RawURLEncoding.EncodeToString(h[:8])  // First 8 bytes
}
```

---

### 8. `internal/tracing/` — Distributed Tracing

**Priority:** P1
**Dependencies:** OpenTelemetry SDK

#### 8.1 OpenTelemetry Setup (`tracing.go`)

```go
package tracing

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func Init(ctx context.Context, serviceName, otlpEndpoint string) error {
    // W3C TraceContext propagation
    propagator := propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    )
    otel.SetTextMapPropagator(propagator)

    // OTLP exporter
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(otlpEndpoint),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return err
    }

    // Tracer provider
    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(serviceName),
        )),
    )

    otel.SetTracerProvider(tp)
    tracer = tp.Tracer(serviceName)

    return nil
}

// StartSpan creates a new span with common attributes
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
    return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// Span helper for HTTP Gateway
func StartHTTPSpan(ctx context.Context, method, path string) (context.Context, trace.Span) {
    return tracer.Start(ctx, fmt.Sprintf("HTTP %s %s", method, path),
        trace.WithAttributes(
            attribute.String("http.method", method),
            attribute.String("http.route", path),
        ),
    )
}

// Span helper for AAA operations
func StartAAASpan(ctx context.Context, protocol, server string) (context.Context, trace.Span) {
    return tracer.Start(ctx, "AAA "+protocol,
        trace.WithAttributes(
            attribute.String("aaa.protocol", protocol),
            attribute.String("aaa.server", server),
        ),
    )
}

// InjectTraceContext injects trace context into HTTP headers
func InjectTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) {
    otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractTraceContext extracts trace context from HTTP headers
func ExtractTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
    return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
```

#### 8.2 Cross-Component Trace Propagation

```
Trace flow across 3-component architecture:

AMF (TraceID: abc123)
    │
    │  N58: Nnssaaf_NSSAA_Authenticate
    │  Headers: traceparent: 00-abc123-def456-01
    ▼
HTTP Gateway (Span: gw001)
    │  Extract trace context, create child span
    │  span: "http.route"
    ▼
Biz Pod (Span: biz001)
    │
    ├─ span: db.insert_session
    │
    ├─ span: biz.forward_to_aaa (HTTP POST /aaa/forward)
    │       │  Headers: traceparent propagated
    │       ▼
    │    AAA Gateway (Span: aaa001)
    │       │  Extract trace context
    │       │  span: "aaa.recv_raw" (RADIUS UDP)
    │       │
    │       │  RADIUS DER → AAA-S
    │       ▼
    │    AAA-S (external, no trace)
    │       │
    │       │  RADIUS DEA response
    │       ▼
    │    AAA Gateway span: "aaa.send_raw"
    │       │
    │       │  HTTP response back to Biz Pod
    │       ▼
    │  span: biz.recv_aaa_response
    │
    ├─ span: db.update_session
    │
    └─ span: amf.notification
            │
            │  Re-Auth notification
            ▼
         AMF (new trace: xyz789)
```

---

### 9. Alerting Rules

**Priority:** P1
**Dependencies:** Prometheus Operator

```yaml
# Prometheus alerting rules
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: nssaa-alerts
  labels:
    app: nssaa
    team: platform
spec:
  groups:
    - name: nssaa-availability
      rules:
        - alert: NssaaHighErrorRate
          expr: |
            sum(rate(nssaa_requests_total{status_code=~"5.."}[5m]))
            / sum(rate(nssaa_requests_total[5m])) > 0.01
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "NSSAAF error rate > 1%"
            description: "Error rate: {{ $value | humanizePercentage }}"

        - alert: NssaaCircuitBreakerOpen
          expr: nssaa_circuit_breaker_state == 1
          for: 1m
          labels:
            severity: major
          annotations:
            summary: "Circuit breaker OPEN for {{ $labels.server }}"
            description: "AAA server {{ $labels.server }} is not accepting requests"

    - name: nssaa-latency
      rules:
        - alert: NssaaHighLatencyP99
          expr: |
            histogram_quantile(0.99,
              sum(rate(nssaa_request_duration_seconds_bucket[5m]))
              by (le, component)) > 0.5
          for: 5m
          labels:
            severity: major
          annotations:
            summary: "NSSAAF P99 latency > 500ms"
            description: "Component: {{ $labels.component }}, P99: {{ $value }}s"

    - name: nssaa-capacity
      rules:
        - alert: NssaaSessionTableFull
          expr: nssaa_eap_sessions_active > 45000
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "EAP sessions approaching limit (45k/50k)"

        - alert: NssaaHighAuthFailureRate
          expr: |
            sum(rate(nssaa_eap_sessions_total{result="failure"}[5m]))
            / sum(rate(nssaa_eap_sessions_total[5m])) > 0.10
          for: 5m
          labels:
            severity: major
          annotations:
            summary: "Authentication failure rate > 10%"
```

---

## Validation Checklist

### NF Integration (Priority: P0)

- [ ] NSSAAF registers with NRF on startup (Nnrf_NFRegistration) — blocks 5G discovery
- [ ] Nnrf_NFHeartBeat sent every 5 minutes
- [ ] AMF discovered via Nnrf_NFDiscovery before sending notifications
- [ ] UDM Nudm_UECM_Get wired to N58 handler (gates AAA routing)
- [ ] UDM Nudm_UECM_UpdateAuthContext called after EAP completion
- [ ] AMF Re-Auth notification POSTed to reauthNotifUri (on RADIUS CoA-Request)
- [ ] AMF Revocation notification POSTed to revocNotifUri (on Diameter ASR)
- [ ] AUSF N60 handler created (internal/ausf/)
- [ ] AUSF MSK forwarding implemented (POST /nausf-auth/v1/.../msk)
- [ ] PostgreSQL session store wired in cmd/biz/main.go
- [ ] In-memory store replaced with persistent storage

### Resilience Patterns

- [ ] Circuit breaker: CLOSED → OPEN (5 consecutive failures) → HALF_OPEN (30s) → CLOSED
- [ ] Per-AAA-S circuit breaker prevents cascade to healthy servers
- [ ] Per-S-NSSAI circuit breaker for slice-specific AAA routing
- [ ] Retry: exponential backoff 1s, 2s, 4s with max 3 retries
- [ ] Timeout: 30s EAP round, 10s AAA request, 5s DB, 100ms Redis
- [ ] Health endpoint /healthz/live returns 200 if process alive
- [ ] Health endpoint /healthz/ready returns 200 only if ready to serve traffic
- [ ] Unit test coverage >90%

### Observability

- [ ] Prometheus metrics: requests, latency, EAP sessions, AAA stats
- [ ] ServiceMonitor CRDs for all 3 components (HTTP GW, Biz, AAA GW)
- [ ] Structured JSON logs with trace context (slog/json)
- [ ] OpenTelemetry traces with W3C TraceContext propagation
- [ ] Trace spans: per handler, DB, AAA, notification
- [ ] P99 latency tracking per component
- [ ] Alert rules: error rate >1%, P99 >500ms, circuit breaker open

### Integration

- [ ] `go build ./...` compiles without errors
- [ ] `go test ./internal/resilience/...` passes
- [ ] `go test ./internal/metrics/...` passes
- [ ] `go test ./internal/logging/...` passes
- [ ] `go test ./internal/tracing/...` passes
- [ ] Prometheus scrape targets discoverable
- [ ] Grafana dashboards show per-component metrics

---

## Success Criteria (What Must Be TRUE)

1. **NSSAAF is discoverable by AMF** — AMF can discover NSSAAF via NRF Nnrf_NFDiscovery and call N58 endpoints
2. **UDM gates AAA routing** — N58 handler queries UDM Nudm_UECM_Get before forwarding to AAA-S; routes based on EAP method from auth subscription
3. **AMF receives notifications** — On RADIUS CoA-Request or Diameter ASR, NSSAAF POSTs to AMF's reauthNotifUri/revocNotifUri
4. **AUSF N60 works end-to-end** — MSK forwarded correctly to AUSF via POST /nausf-auth/v1/.../msk
5. **Sessions persist across restarts** — PostgreSQL replaces in-memory store; sessions survive pod restarts
6. **Circuit breakers isolate failing AAA servers** — A single AAA-S failure does not affect requests to healthy AAA-S instances
7. **Retries succeed transient failures** — Network blips of <30s do not cause user-visible failures
8. **Health probes accurately reflect state** — Kubernetes can distinguish liveness (alive) from readiness (can serve traffic)
9. **Metrics visible in Prometheus** — Operations team can query P99 latency per component
10. **Traces span all 3 components** — A single trace ID links AMF → HTTP GW → Biz Pod → AAA GW → AAA-S
11. **Alerts fire on degradation** — On-call receives notification within 2 minutes of error rate exceeding 1%

---

## Dependencies

| Module | Status | Blocking |
|--------|--------|----------|
| `internal/config/` | READY (Phase 1) | No |
| `internal/types/` | READY (Phase 1) | No |
| `internal/storage/` | READY (Phase 3) | No |
| `internal/cache/` | READY (Phase 3) | No |
| `internal/nrf/` | STUB — needs wiring | P0 for all NF integrations |
| `internal/udm/` | STUB — needs wiring | P0 for N58 handler |
| `internal/amf/` | STUB — needs wiring | P0 for Re-Auth/Revocation |
| `internal/ausf/` | MISSING — needs creation | P0 for N60 |
| OpenTelemetry SDK | New dependency | No |

---

## Next Phase

Phase 5: Security & Cryptography — TLS/mTLS, OAuth2, AES-256-GCM encryption
