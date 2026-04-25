# Phase 4: NF Integration & Observability — Implementation Plan

## Current State Summary

### READY Modules (from Phases 1-3)
| Module | Status | Files |
|--------|--------|-------|
| `internal/types/` | READY | snssai.go, gpsi.go, supi.go, nssaa_status.go |
| `internal/api/nssaa/` | READY | handler.go, router.go, handler_test.go |
| `internal/api/aiw/` | READY | handler.go, router.go, handler_test.go |
| `internal/api/common/` | READY | middleware.go, validator.go, context.go |
| `internal/config/` | READY | config.go, config_test.go |
| `internal/eap/` | READY | engine.go, session.go, state.go, tls.go |
| `internal/radius/` | READY | client.go, packet.go, attribute.go |
| `internal/diameter/` | READY | client.go, eap_avp.go, snssai_avp.go |
| `internal/aaa/` | READY | aaa.go, router.go, metrics.go |
| `internal/storage/postgres/` | READY | pool.go, session.go, migrate.go |

### STUB Modules (Need Implementation)
| Module | Current | Lines | Status |
|--------|---------|-------|--------|
| `internal/nrf/` | Package declaration only | 4 | STUB — needs full implementation |
| `internal/udm/` | Package declaration only | 5 | STUB — needs full implementation |
| `internal/amf/` | Package declaration only | 3 | STUB — needs full implementation |
| `internal/resilience/` | Package declaration only | 4 | STUB — needs full implementation |

### MISSING Modules (Need Creation)
| Module | Current | Status |
|--------|---------|--------|
| `internal/ausf/` | Does NOT exist | MISSING — must create directory and implement |
| `internal/metrics/` | Does NOT exist | MISSING — must create directory and implement |
| `internal/logging/` | Does NOT exist | MISSING — must create directory and implement |
| `internal/tracing/` | Does NOT exist | MISSING — must create directory and implement |

### `cmd/biz/main.go` State
- Uses `nssaa.NewInMemoryStore()` and `aiw.NewInMemoryStore()` (lines 59-60)
- Has stub handlers: `handleReAuth` (returns hardcoded bytes), `handleRevocation`, `handleCoA` (lines 188-201)
- Missing: NRF registration, UDM client, AMF notifier, AUSF client wiring
- Health endpoints at `/health` and `/ready` (lines 103-104)

### External Dependencies (in go.mod)
- `github.com/prometheus/client_golang/prometheus` — NOT present, need to add
- `go.opentelemetry.io/otel` — NOT present, need to add

---

## Implementation Order

```
Phase 4.1: Foundation Packages (CRITICAL — used everywhere)
├── 4.1.1 Resilience patterns (circuit breaker, retry, timeout)
├── 4.1.2 Structured logging
├── 4.1.3 Prometheus metrics
└── 4.1.4 OpenTelemetry tracing

Phase 4.2: Database Wiring
└── 4.2.1 Wire PostgreSQL session store in main.go

Phase 4.3: NRF Client Implementation
├── 4.3.1 Implement NRF client (full)
├── 4.3.2 Wire in main.go (registration, heartbeat)
└── 4.3.3 Wire NRF to UDM/AMF/AUSF clients

Phase 4.4: UDM Client Implementation
├── 4.4.1 Implement UDM client
└── 4.4.2 Wire in N58 handler + add handler option functions

Phase 4.5: AMF Notification Implementation
├── 4.5.1 Implement AMF notifier
└── 4.5.2 Replace stub handlers in main.go

Phase 4.6: AUSF N60 Client (CREATE from scratch)
└── 4.6.1 Create internal/ausf/ and implement + wire to AIW handler

Phase 4.7: Alerting Rules
└── 4.7.1 Create Prometheus alerting rules YAML
```

---

## Detailed Task Breakdown

### Phase 4.1: Foundation Packages

#### P4-TASK-101: Resilience Patterns — Circuit Breaker
| Field | Value |
|-------|-------|
| **Module** | `internal/resilience/` |
| **Action** | CREATE |
| **Files to Create** | `internal/resilience/circuit_breaker.go`, `internal/resilience/circuit_breaker_test.go`, `internal/resilience/registry.go` |
| **Spec Reference** | `docs/design/10_ha_architecture.md` §5.1, `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §5.1 |
| **Priority** | P0 |
| **Dependencies** | None (foundation) |
| **Acceptance Criteria** | Circuit breaker has CLOSED→OPEN→HALF_OPEN states; per-AAA-S granularity; exported via metrics; CircuitBreakerRegistry manages named breakers |

**Implementation Details:**
```go
// internal/resilience/circuit_breaker.go
type CircuitState int
const (
    CB_CLOSED CircuitState = iota
    CB_OPEN
    CB_HALF_OPEN
)

type CircuitBreaker struct {
    mu sync.RWMutex
    state CircuitState
    failureThreshold int           // default: 5
    recoveryTimeout  time.Duration // default: 30s
    halfOpenMax     int           // default: 3
    failures    int64
    successes   int64
    lastFailure time.Time
    lastStateChange time.Time
}

func (cb *CircuitBreaker) Do(ctx context.Context, fn func() error) error
func (cb *CircuitBreaker) getState() CircuitState
func (cb *CircuitBreaker) onFailure()
func (cb *CircuitBreaker) onSuccess()

// internal/resilience/registry.go
type CircuitBreakerRegistry struct {
    mu      sync.RWMutex
    breakers map[string]*CircuitBreaker
}

func NewRegistry() *CircuitBreakerRegistry
func (r *CircuitBreakerRegistry) Get(name string) *CircuitBreaker
func (r *CircuitBreakerRegistry) GetOrCreate(name string, cfg CircuitBreakerConfig) *CircuitBreaker
```

---

#### P4-TASK-102: Resilience Patterns — Retry with Exponential Backoff
| Field | Value |
|-------|-------|
| **Module** | `internal/resilience/` |
| **Action** | CREATE |
| **Files to Create** | `internal/resilience/retry.go`, `internal/resilience/retry_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §5.2 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-101 (same package, no explicit dep) |
| **Acceptance Criteria** | Retry 3x with exponential backoff; respects retryable error types; context cancellation works |

**Implementation Details:**
```go
// internal/resilience/retry.go
type RetryConfig struct {
    MaxAttempts int           // default: 3
    BaseDelay   time.Duration // default: 1s
    MaxDelay    time.Duration // default: 30s
    Multiplier  float64       // default: 2.0
    Jitter      bool          // default: true
}

func IsRetryable(err error) bool
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error
```

---

#### P4-TASK-103: Resilience Patterns — Timeout & Health Endpoints
| Field | Value |
|-------|-------|
| **Module** | `internal/resilience/` |
| **Action** | CREATE |
| **Files to Create** | `internal/resilience/timeout.go`, `internal/resilience/health.go`, `internal/resilience/health_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §5.3, §5.4 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-101, P4-TASK-102 |
| **Acceptance Criteria** | `WithOperationTimeout` returns context with appropriate timeout; health package provides liveness/readiness checkers |

**Health Endpoint Implementation (to be wired in main.go by P4-TASK-201):**
- `/healthz/live` → returns 200 always (liveness probe, no dependency checks)
- `/healthz/ready` → checks PostgreSQL connectivity, Redis connectivity, AAA availability
- Replace existing `/health` and `/ready` handlers with the new paths

---

#### P4-TASK-104: Structured Logging
| Field | Value |
|-------|-------|
| **Module** | `internal/logging/` |
| **Action** | CREATE |
| **Files to Create** | `internal/logging/logging.go`, `internal/logging/logging_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §7, `docs/design/19_observability.md` §3 |
| **Priority** | P0 |
| **Dependencies** | None (uses standard library `log/slog`) |
| **Acceptance Criteria** | JSON format; includes trace_id, request_id, gpsi_hash fields; GPSI hashed for privacy |

**Implementation Details:**
```go
// internal/logging/logging.go
func Init(service, version string)
func Info(msg string, args ...any)
func Warn(msg string, args ...any)
func Error(msg string, args ...any)
func Debug(msg string, args ...any)
func WithContext(ctx context.Context) *slog.Logger
func HashGpsi(gpsi string) string  // SHA256, first 8 bytes, base64url
```

---

#### P4-TASK-105: Prometheus Metrics
| Field | Value |
|-------|-------|
| **Module** | `internal/metrics/` |
| **Action** | CREATE |
| **Files to Create** | `internal/metrics/metrics.go`, `internal/metrics/middleware.go`, `internal/metrics/metrics_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §6, `docs/design/19_observability.md` §2 |
| **Priority** | P0 |
| **Dependencies** | None |
| **Acceptance Criteria** | All metrics from §6.1 defined; `RecordRequest` middleware works; `prometheus.Handler()` returns the metrics handler |

**Metrics to implement:**
- `nssaa_requests_total` (counter, labels: component, endpoint, method, status_code)
- `nssaa_request_duration_seconds` (histogram)
- `nssaa_eap_sessions_active` (gauge)
- `nssaa_eap_sessions_total` (counter, labels: result, eap_method)
- `nssaa_eap_session_duration_seconds` (histogram)
- `nssaa_aaa_requests_total` (counter, labels: component, protocol, server, result)
- `nssaa_aaa_request_duration_seconds` (histogram)
- `nssaa_circuit_breaker_state` (gauge, labels: component, server)
- `nssaa_db_query_duration_seconds` (histogram)
- `nssaa_redis_operations_total` (counter)
- `nssaa_redis_operation_duration_seconds` (histogram)

---

#### P4-TASK-106: OpenTelemetry Tracing
| Field | Value |
|-------|-------|
| **Module** | `internal/tracing/` |
| **Action** | CREATE |
| **Files to Create** | `internal/tracing/tracing.go`, `internal/tracing/tracing_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §8, `docs/design/19_observability.md` §4 |
| **Priority** | P1 |
| **Dependencies** | P4-TASK-104 (logging for trace ID extraction) |
| **Acceptance Criteria** | W3C TraceContext propagation; `StartSpan`, `StartHTTPSpan`, `StartAAASpan` helpers; `InjectTraceContext` for outgoing HTTP |

**Note:** NRF client (P4-TASK-301) can proceed with logging + metrics only, deferring tracing integration. The tracing package is created in Wave 1 but may be integrated later.

---

### Phase 4.2: Database Wiring

#### P4-TASK-201: Wire PostgreSQL in main.go
| Field | Value |
|-------|-------|
| **Module** | `cmd/biz/` |
| **Action** | CREATE + EXTEND |
| **Files to Create** | `internal/storage/postgres/session_store.go` |
| **Files to Modify** | `cmd/biz/main.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §4, `docs/design/11_database_ha.md` |
| **Priority** | P0 |
| **Dependencies** | None (independent — can run parallel with Wave 1) |
| **Acceptance Criteria** | `postgres.NewSessionStore()` replaces `nssaa.NewInMemoryStore()` and `aiw.NewInMemoryStore()`; `postgres.NewAIWSessionStore()` replaces `aiw.NewInMemoryStore()`; `db.Migrate()` called; `db.Close()` on shutdown; `/metrics` endpoint wired via `mux.Handle("/metrics", promhttp.Handler())`; health endpoints renamed to `/healthz/live` and `/healthz/ready` |

**Changes to `cmd/biz/main.go`:**
1. Import `internal/storage/postgres`
2. Replace lines 59-60 (in-memory stores) with PostgreSQL initialization via `NewSessionStore()` and `NewAIWSessionStore()`
3. Add migration on startup
4. Defer `db.Close()`
5. Rename `handleHealth` endpoint from `/health` to `/healthz/live`
6. Rename `handleReady` endpoint from `/ready` to `/healthz/ready`
7. Add `mux.Handle("/metrics", promhttp.Handler())` for Prometheus metrics

**Session Store Implementation:**
```go
// internal/storage/postgres/session_store.go
type PostgresSessionStore struct {
    pool *Pool
    // implements nssaa.AuthCtxStore interface
}

func NewSessionStore(pool *Pool) *PostgresSessionStore
// implements: GetAuthCtx, SetAuthCtx, DeleteAuthCtx, ListAuthCtxs

type PostgresAIWSessionStore struct {
    pool *Pool
    // implements aiw.AuthCtxStore interface
}

func NewAIWSessionStore(pool *Pool) *PostgresAIWSessionStore
// implements: GetAuthCtx, SetAuthCtx, DeleteAuthCtx, ListAuthCtxs
```

---

### Phase 4.3: NRF Client Implementation

#### P4-TASK-301: Implement NRF Client
| Field | Value |
|-------|-------|
| **Module** | `internal/nrf/` |
| **Action** | CREATE |
| **Files to Create** | `internal/nrf/client.go`, `internal/nrf/profile.go`, `internal/nrf/discovery.go`, `internal/nrf/client_test.go` |
| **Spec Reference** | `docs/design/05_nf_profile.md`, `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §0 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-104 (logging), P4-TASK-105 (metrics) |
| **Acceptance Criteria** | `Register()`, `StartHeartbeat()`, `DiscoverNF()`, `SubscribeStatus()`, `HandleStatusChange()` all implemented; NRF discovery cache with TTL |

**Note:** Tracing integration (P4-TASK-106) is not a hard dependency for the NRF client to function. The NRF client uses structured logging and metrics. Tracing can be added incrementally.

**Key types to implement:**
```go
type NRFClient struct {
    baseURL string
    nfInstanceId string
    nfType string
    heartbeatInterval time.Duration
    httpClient *http.Client
    discoveryCache *NRFDiscoveryCache
    tokenCache *TokenCache
}

type NFProfile struct {
    NFInstanceID string `json:"nfInstanceId"`
    NFType string `json:"nfType"`
    NFStatus string `json:"nfStatus"`
    PlmnId PlmnId `json:"plmnId"`
    // ... per TS 29.510 §6
}

type HeartbeatPayload struct {
    NFInstanceID string `json:"nfInstanceId"`
    NFStatus string `json:"nfStatus"`
    HeartBeatTimer int `json:"heartBeatTimer"`
    Load int `json:"load"`
}
```

---

#### P4-TASK-302: Wire NRF Client in main.go
| Field | Value |
|-------|-------|
| **Module** | `cmd/biz/` |
| **Action** | EXTEND |
| **Files to Modify** | `cmd/biz/main.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §0.2 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-301 |
| **Acceptance Criteria** | NSSAAF registers with NRF on startup; heartbeat goroutine started; NRF client injected into N58 handler |

**Changes:**
1. Import `internal/nrf`
2. After config load, create NRF client
3. Call `nrfClient.Register(ctx)` with 10s timeout — exit(1) on failure
4. Start heartbeat goroutine: `go nrfClient.StartHeartbeat(context.Background())`
5. Inject `nrfClient` into `nssaaHandler` via `nssaa.WithNRFClient()`

---

### Phase 4.4: UDM Client Implementation

#### P4-TASK-401: Implement UDM Client
| Field | Value |
|-------|-------|
| **Module** | `internal/udm/` |
| **Action** | CREATE |
| **Files to Create** | `internal/udm/client.go`, `internal/udm/uecm.go`, `internal/udm/types.go`, `internal/udm/client_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §1, `docs/design/05_nf_profile.md` §3.2 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-301 (NRF client for discovery), P4-TASK-101 (retry) |
| **Acceptance Criteria** | `GetAuthSubscription()` (GET /nudm-uecm/v1/{gpsi}/auth-subscriptions); `UpdateAuthContext()` (PUT /nudm-uecm/v1/{gpsi}/auth-contexts/{authCtxId}) |

**Key types:**
```go
type UDMClient struct {
    baseURL string
    httpClient *http.Client
    nrfClient *nrf.NRFClient  // for UDM discovery
}

type AuthSubscription struct {
    Gpsi string `json:"gpsi"`
    Snssai Snssai `json:"snssai"`
    EapMethod string `json:"eapMethod"`
    // ... per TS 29.526 §7.3.2
}
```

---

#### P4-TASK-402: Wire UDM Client in N58 Handler + Add Handler Option Functions
| Field | Value |
|-------|-------|
| **Module** | `internal/api/nssaa/`, `internal/udm/` |
| **Action** | CREATE + EXTEND |
| **Files to Create** | `internal/api/nssaa/nrf_udm_options.go` |
| **Files to Modify** | `internal/api/nssaa/handler.go`, `cmd/biz/main.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §1.2 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-401 |
| **Acceptance Criteria** | `nssaa.WithNRFClient(*nrf.Client)` and `nssaa.WithUDMClient(*udm.Client)` option functions exist; N58 handler calls UDM `GetAuthSubscription()` before routing to AAA |

**Changes:**
1. Create `internal/api/nssaa/nrf_udm_options.go` with:
   - `WithNRFClient(nrfClient *nrf.Client) HandlerOption` — adds `nrfClient *nrf.Client` field to Handler
   - `WithUDMClient(udmClient *udm.Client) HandlerOption` — adds `udmClient *udm.Client` field to Handler
2. In `internal/api/nssaa/handler.go`: Add `nrfClient *nrf.Client` and `udmClient *udm.Client` fields to the `Handler` struct
3. In `cmd/biz/main.go`: Create UDM client; inject into `nssaaHandler` via `nssaa.WithUDMClient()`

---

### Phase 4.5: AMF Notification Implementation

#### P4-TASK-501: Implement AMF Notifier
| Field | Value |
|-------|-------|
| **Module** | `internal/amf/` |
| **Action** | CREATE |
| **Files to Create** | `internal/amf/notifier.go`, `internal/amf/types.go`, `internal/amf/notifier_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §2, `docs/design/05_nf_profile.md` §3.1 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-301 (NRF for AMF discovery), P4-TASK-102 (retry) |
| **Acceptance Criteria** | AMF discovered via NRF discovery (`nrfClient.DiscoverNF("AMF", plmnID)`); `SendReAuthNotification()` POSTs to AMF; `SendRevocationNotification()` POSTs to AMF; retries with exponential backoff |

**Key types:**
```go
type AMFNotifier struct {
    httpClient *http.Client
    nrfClient *nrf.NRFClient  // discovers AMF via NRF
    maxRetries int
}

type ReAuthRequest struct {
    Gpsi string `json:"gpsi"`
    Snssai Snssai `json:"snssai"`
    Supi string `json:"supi"`
    AuthCtxId string `json:"authCtxId"`
}

type RevocRequest struct {
    Gpsi string `json:"gpsi"`
    Snssai Snssai `json:"snssai"`
    RevocType string `json:"revocType"`
    AuthCtxId string `json:"authCtxId"`
}
```

**Note:** `AMFConfig { Timeout, MaxRetries }` is added to `internal/config/config.go` as part of Phase 4.1 (or this task). BaseURL comes from NRF discovery, not static config.

---

#### P4-TASK-502: Replace Stub Handlers in main.go
| Field | Value |
|-------|-------|
| **Module** | `cmd/biz/` |
| **Action** | EXTEND |
| **Files to Modify** | `cmd/biz/main.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §2.2 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-501 |
| **Acceptance Criteria** | `handleReAuth` uses real AMF notifier (POST to reauthNotifUri); `handleRevocation` uses real AMF notifier (POST to revocNotifUri) |

**Changes:**
1. Replace `handleReAuth` body (lines 188-191): lookup session, call `amfNotifier.SendReAuthNotification()`, return RADIUS CoA-Ack
2. Replace `handleRevocation` body (lines 193-195): lookup session, call `amfNotifier.SendRevocationNotification()`
3. Import `internal/amf`

---

### Phase 4.6: AUSF N60 Client (CREATE from scratch)

#### P4-TASK-601: Create AUSF N60 Client
| Field | Value |
|-------|-------|
| **Module** | `internal/ausf/` |
| **Action** | CREATE |
| **Files to Create** | `internal/ausf/client.go`, `internal/ausf/types.go`, `internal/ausf/client_test.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §3, `docs/design/05_nf_profile.md` |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-301 (NRF for AUSF discovery), P4-TASK-102 (retry), P4-TASK-105 (metrics) |
| **Acceptance Criteria** | AUSF discovered via NRF (`nrfClient.DiscoverNF("AUSF", plmnID)`); `N60Client` with `ForwardMSK()` method |

**Key types:**
```go
type N60Client struct {
    httpClient *http.Client
    baseURL string  // from NRF discovery
    nrfClient *nrf.NRFClient  // discovers AUSF via NRF
}

type AUSFConfig struct {
    Timeout time.Duration  // from config.go
}

// ForwardMSK forwards MSK to AUSF after successful EAP-TLS
// POST /nausf-auth/v1/ue-authentications/{authCtxId}/msk
// Spec: TS 23.502 §4.2.9.2
func (c *N60Client) ForwardMSK(ctx context.Context, authCtxId string, msk []byte) error
```

**Note:** `AUSFConfig { Timeout }` is added to `internal/config/config.go` as part of Phase 4.1 (or this task). BaseURL comes from NRF discovery.

---

#### P4-TASK-602: Wire AUSF Client in AIW Handler + Add Handler Option Function
| Field | Value |
|-------|-------|
| **Module** | `internal/api/aiw/`, `internal/ausf/` |
| **Action** | CREATE + EXTEND |
| **Files to Create** | `internal/api/aiw/ausf_option.go` |
| **Files to Modify** | `internal/api/aiw/handler.go`, `cmd/biz/main.go` |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §3.3 |
| **Priority** | P0 |
| **Dependencies** | P4-TASK-601 |
| **Acceptance Criteria** | `aiw.WithAUSFClient(*ausf.Client)` option function exists; AUSF client injected into AIW handler; MSK forwarded after EAP-TLS success |

**Changes:**
1. Create `internal/api/aiw/ausf_option.go` with:
   - `WithAUSFClient(ausfClient *ausf.Client) HandlerOption` — adds `ausfClient *ausf.Client` field to AIW Handler
2. In `internal/api/aiw/handler.go`: Add `ausfClient *ausf.Client` field to the `Handler` struct
3. In `cmd/biz/main.go`: Create AUSF client; inject via `aiw.WithAUSFClient()`

---

### Phase 4.7: Alerting Rules

#### P4-TASK-701: Create Prometheus Alerting Rules
| Field | Value |
|-------|-------|
| **Module** | Observability |
| **Action** | CREATE |
| **Files to Create** | `deployments/k8s/nssaa-alerts.yaml` (directory `deployments/k8s/` will be created implicitly by file creation) |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §9, `docs/design/19_observability.md` §5 |
| **Priority** | P1 |
| **Dependencies** | P4-TASK-105 (metrics must be defined first) |
| **Acceptance Criteria** | All alerts from §9 defined as PrometheusRule CRD |

**Alerts to implement:**
1. `NssaaHighErrorRate` — 5xx rate > 1%, for 2m, severity: critical
2. `NssaaCircuitBreakerOpen` — CB state == 1, for 1m, severity: major
3. `NssaaHighLatencyP99` — P99 > 500ms, for 5m, severity: major
4. `NssaaSessionTableFull` — active sessions > 45000, for 5m, severity: critical
5. `NssaaHighAuthFailureRate` — failure rate > 10%, for 5m, severity: major
6. `NssaaDatabaseUnreachable` — DB queries stopped, for 2m, severity: critical
7. `NssaaAaaServerFailures` — AAA failures > 10/min, for 3m, severity: major

---

#### P4-TASK-702: Create ServiceMonitor CRDs
| Field | Value |
|-------|-------|
| **Module** | Observability |
| **Action** | CREATE |
| **Files to Create** | `deployments/k8s/servicemonitor.yaml` (directory `deployments/k8s/` will be created implicitly by file creation) |
| **Spec Reference** | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` §6.2 |
| **Priority** | P1 |
| **Dependencies** | P4-TASK-105 |
| **Acceptance Criteria** | ServiceMonitor for HTTP Gateway, Biz Pod, AAA Gateway |

---

## File Creation Map

### Files to CREATE (32 files)
```
Phase 4.1 — Foundation Packages:
  internal/resilience/circuit_breaker.go
  internal/resilience/circuit_breaker_test.go
  internal/resilience/retry.go
  internal/resilience/retry_test.go
  internal/resilience/timeout.go
  internal/resilience/health.go
  internal/resilience/health_test.go
  internal/resilience/registry.go          [FIX: added CircuitBreakerRegistry]
  internal/logging/logging.go
  internal/logging/logging_test.go
  internal/metrics/metrics.go
  internal/metrics/middleware.go
  internal/metrics/metrics_test.go
  internal/tracing/tracing.go
  internal/tracing/tracing_test.go

Phase 4.2 — Database Wiring:
  internal/storage/postgres/session_store.go  [FIX: added NewSessionStore/NewAIWSessionStore]

Phase 4.3 — NRF Client:
  internal/nrf/client.go
  internal/nrf/profile.go
  internal/nrf/discovery.go
  internal/nrf/client_test.go

Phase 4.4 — UDM Client:
  internal/udm/client.go
  internal/udm/uecm.go
  internal/udm/types.go
  internal/udm/client_test.go

Phase 4.4.5 — Handler Option Functions:
  internal/api/nssaa/nrf_udm_options.go      [FIX: added WithNRFClient/WithUDMClient]
  internal/api/aiw/ausf_option.go             [FIX: added WithAUSFClient]

Phase 4.5 — AMF Notifier:
  internal/amf/notifier.go
  internal/amf/types.go
  internal/amf/notifier_test.go

Phase 4.6 — AUSF N60 Client:
  internal/ausf/client.go       (NEW directory)
  internal/ausf/types.go
  internal/ausf/client_test.go

Phase 4.7 — Observability Config:
  deployments/k8s/nssaa-alerts.yaml    [directory created implicitly by kubectl]
  deployments/k8s/servicemonitor.yaml
```

### Files to MODIFY (4 files)
```
Phase 4.1 — Config Additions:
  internal/config/config.go               (add AMFConfig + AUSFConfig structs)

Phase 4.2 — Database Wiring:
  cmd/biz/main.go                         (replace in-memory stores, add healthz/paths, add /metrics)

Phase 4.3 — NRF Wiring:
  cmd/biz/main.go                         (add NRF registration + injection)

Phase 4.4 — UDM Wiring:
  internal/api/nssaa/handler.go           (add nrfClient + udmClient fields, use option functions)
  cmd/biz/main.go                         (create UDM client, inject via WithUDMClient)

Phase 4.5 — AMF Wiring:
  cmd/biz/main.go                         (replace stub handlers, create notifier)

Phase 4.6 — AUSF Wiring:
  internal/api/aiw/handler.go             (add ausfClient field, use option function)
  cmd/biz/main.go                         (create AUSF client, inject via WithAUSFClient)
```

---

## Test Plan

| Module | Tests to Write |
|--------|----------------|
| `internal/resilience/` | Test circuit breaker state transitions (CLOSE→OPEN→HALF→CLOSE); Test retry with retryable/non-retryable errors; Test timeout context cancellation; Test CircuitBreakerRegistry Get/GetOrCreate |
| `internal/logging/` | Test JSON output format; Test GPSI hashing; Test WithContext trace ID extraction |
| `internal/metrics/` | Test `RecordRequest` middleware captures status code and duration; Test all metric counters increment |
| `internal/tracing/` | Test W3C TraceContext propagation (inject/extract); Test span creation with attributes |
| `internal/nrf/` | Test registration payload format; Test heartbeat goroutine; Test NF discovery cache TTL |
| `internal/storage/postgres/` | Test `NewSessionStore` implements `nssaa.AuthCtxStore`; Test `NewAIWSessionStore` implements `aiw.AuthCtxStore` |
| `internal/udm/` | Test `GetAuthSubscription` request/response; Test `UpdateAuthContext` PUT format |
| `internal/amf/` | Test `SendReAuthNotification` retry behavior; Test `SendRevocationNotification`; Test NRF-based AMF discovery |
| `internal/ausf/` | Test `ForwardMSK` payload format; Test N60 client initialization; Test NRF-based AUSF discovery |

---

## Dependency Graph

```
P4-TASK-101 ─┬─► P4-TASK-102 ─┬─► P4-TASK-103 ─┐
             │                 │                 │
P4-TASK-104 ─┤                 │                 │
             │                 │            P4-TASK-201  (independent)
             │                 │                 │
P4-TASK-105 ─┤                 │                 │
             │                 │                 ▼
P4-TASK-106 ─┘                 │            P4-TASK-301
                                │            (NRF client)
                                │                 │
                                │                 ├─► P4-TASK-401 ─► P4-TASK-402
                                │                 │
                                │                 ├─► P4-TASK-501 ─► P4-TASK-502
                                │                 │
                                │                 └─► P4-TASK-601 ─► P4-TASK-602
                                │                               │
                                │                               ▼
                                │                          P4-TASK-701
                                │                          P4-TASK-702
```

---

## Wave Structure

| Wave | Tasks | Parallelizable |
|------|-------|----------------|
| 1 | P4-TASK-101, P4-TASK-102, P4-TASK-103, P4-TASK-104, P4-TASK-105, P4-TASK-106 | Yes (foundation packages — all independent) |
| 1.5 | P4-TASK-201 (DB wiring + session stores + /metrics + healthz paths) | Independent — no dependency on Wave 1 (can start immediately) |
| 2 | P4-TASK-301 (NRF client) | No (depends on Wave 1 logging + metrics) |
| 3 | P4-TASK-401, P4-TASK-501 | Yes (both depend on P4-TASK-301, independent of each other) |
| 4 | P4-TASK-402 (UDM wiring — modifies nssaa/handler.go + main.go) | No (depends on P4-TASK-401) |
| 5 | P4-TASK-601 (AUSF client) | No (depends on P4-TASK-301, parallel with Wave 4) |
| 6 | P4-TASK-502 (AMF stub replacement in main.go) | No (depends on P4-TASK-501, must run after Wave 4 main.go changes complete) |
| 7 | P4-TASK-602 (AUSF wiring — modifies aiw/handler.go + main.go) | No (depends on P4-TASK-601) |
| 8 | P4-TASK-701, P4-TASK-702 | No (depend on Wave 1 metrics) |

**Wave 4 vs Wave 5 vs Wave 6 vs Wave 7 reasoning:**

- **Wave 4 (P4-TASK-402):** Modifies `main.go` AND `nssaa/handler.go`. Must run after P4-TASK-401 (UDM client). Runs before Wave 6 (P4-TASK-502 also modifies main.go).

- **Wave 5 (P4-TASK-601):** Creates AUSF client directory and files. No file conflicts. Runs in parallel with Wave 4. Both depend on P4-TASK-301.

- **Wave 6 (P4-TASK-502):** Modifies `main.go`. Must run AFTER Wave 4 (P4-TASK-402) because both add code to main.go and must not conflict. Depends on P4-TASK-501.

- **Wave 7 (P4-TASK-602):** Modifies `main.go` AND `aiw/handler.go`. Must run after Wave 5 (P4-TASK-601). Runs after Wave 6 because both modify main.go.

---

## External Dependency Additions

The following packages must be added to `go.mod`:
```
github.com/prometheus/client_golang v1.20.x
go.opentelemetry.io/otel v1.30.x
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.30.x
go.opentelemetry.io/otel/sdk/trace v1.30.x
go.opentelemetry.io/otel/trace v1.30.x
go.opentelemetry.io/otel/propagation v1.30.x
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.53.x
go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.53.x
```

Add via `go get` during execution.

---

## Config Additions

### AMFConfig and AUSFConfig

The following structs must be added to `internal/config/config.go`:

```go
// AMFConfig holds AMF client configuration.
// Timeout and MaxRetries are configurable; BaseURL is discovered via NRF.
type AMFConfig struct {
    Timeout    time.Duration // default: 5s
    MaxRetries int           // default: 3
}

// AUSFConfig holds AUSF client configuration.
// Timeout is configurable; BaseURL is discovered via NRF.
type AUSFConfig struct {
    Timeout time.Duration // default: 30s
}
```

**Discovery-based URL resolution:** AMF and AUSF clients do NOT use static `BaseURL` from config. Instead, they use `nrfClient.DiscoverNF("AMF", plmnID)` and `nrfClient.DiscoverNF("AUSF", plmnID)` respectively. The NRF discovery cache returns the NF profile with its service endpoint, which is used as the target URL.

---

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| NRF not available at startup | Medium | High | 10s timeout, exit(1), Kubernetes readiness probe prevents traffic routing |
| PostgreSQL connection pool exhaustion | Medium | High | PgBouncer transaction mode (Phase 3 design); configure `MaxConns=50` |
| Circular dependency: NRF uses resilience, resilience needs metrics | Low | Medium | Metrics use Prometheus static registration (no circular imports) |
| OpenTelemetry SDK adds significant memory | Low | Medium | Use batch exporter with 5s flush; configure resource limits |
| AUSF N60 interface not standardized | Medium | Medium | Implement minimal `ForwardMSK` only; defer full N60 to Phase 6 |
| Circuit breaker state not thread-safe | High | Critical | Use `sync.RWMutex` per breaker; test concurrent access |
| GPSI logging leaks PII | Medium | High | Always hash GPSI with SHA256; never log raw GPSI |
| AMF notification retry storm | Medium | Medium | Cap retry at 3 attempts with exponential backoff; DLQ after exhausted |
| File conflicts in main.go during parallel execution | Medium | High | Wave structure enforces sequential main.go modifications |

---

## Time Estimate

| Section | Files | Complexity | Estimated |
|---------|-------|------------|-----------|
| Resilience patterns | 8 files | Medium | 2-3 days |
| Structured logging | 2 files | Low | 0.5 day |
| Prometheus metrics | 3 files | Medium | 1 day |
| OpenTelemetry tracing | 2 files | Medium | 1 day |
| Database wiring | 2 files | Low | 0.5 day |
| NRF client | 4 files | High | 2-3 days |
| UDM client | 4 files | Medium | 1-2 days |
| AMF notifier | 3 files | Medium | 1 day |
| AUSF N60 client | 3 files | Medium | 1-2 days |
| Handler option functions | 2 files | Low | 0.5 day |
| Alerting rules | 2 files | Low | 0.5 day |
| Integration wiring | 4 files | Medium | 1 day |
| **Total** | **39 files** | — | **12-17 days** |

---

## Implementation Sequence for Executor

1. **Week 1:** P4-TASK-101 → P4-TASK-106 (foundation packages — Wave 1)
2. **Week 1:** P4-TASK-201 (database wiring + session stores + healthz paths + /metrics — Wave 1.5, parallel with Wave 1)
3. **Week 1-2:** P4-TASK-301 (NRF client — Wave 2)
4. **Week 2:** P4-TASK-401, P4-TASK-402, P4-TASK-501, P4-TASK-601 (Wave 3-5)
5. **Week 2-3:** P4-TASK-502, P4-TASK-602 (Wave 6-7 — main.go wiring)
6. **Week 3:** P4-TASK-701, P4-TASK-702 (alerting + ServiceMonitors)
7. **Week 3:** Integration test, `go build ./...`, `go test ./...`

---

## Success Criteria (from PHASE_4_NFIntegration_Observability.md)

1. NSSAAF registers with NRF on startup — Nnrf_NFRegistration
2. Nnrf_NFHeartBeat sent every 5 minutes
3. AMF discovered via Nnrf_NFDiscovery before sending notifications
4. UDM Nudm_UECM_Get wired to N58 handler (gates AAA routing)
5. AMF Re-Auth/Revocation notifications POSTed correctly
6. AUSF N60 handler created with MSK forwarding
7. PostgreSQL session store replaces in-memory store (`NewSessionStore()` / `NewAIWSessionStore()`)
8. Circuit breaker: CLOSED → OPEN (5 failures) → HALF_OPEN (30s) → CLOSED
9. Retry: exponential backoff 1s, 2s, 4s with max 3 retries
10. Health endpoints: `/healthz/live` and `/healthz/ready` functional (replacing `/health` and `/ready`)
11. Prometheus metrics visible at `/metrics`
12. Structured JSON logs with trace context
13. OpenTelemetry traces span all components
14. Alert rules defined for error rate, latency, circuit breakers
15. `go build ./...` compiles without errors
16. `go test ./...` passes for all new modules

---

## Verification Checklist

- [ ] All handler option functions created: `WithNRFClient`, `WithUDMClient`, `WithAUSFClient`
- [ ] `NewSessionStore()` and `NewAIWSessionStore()` implemented in `internal/storage/postgres/session_store.go`
- [ ] `CircuitBreakerRegistry` implemented in `internal/resilience/registry.go`
- [ ] Health endpoints renamed to `/healthz/live` and `/healthz/ready`
- [ ] `/metrics` endpoint wired in main.go
- [ ] `AMFConfig` and `AUSFConfig` added to `internal/config/config.go`
- [ ] AMF/AUSF clients use NRF discovery, not static BaseURL
- [ ] Wave structure enforces no file conflicts on main.go
- [ ] `deployments/k8s/` directory created implicitly (kubectl apply -f creates parent dirs)
- [ ] All 39 files accounted for in File Creation Map
