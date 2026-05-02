# Phase 4: NF Integration & Observability — Research

**Researched:** 2026-04-25
**Domain:** Go 1.25 5G Network Function — NF client wiring, resilience patterns, observability stack
**Confidence:** HIGH

## Summary

Phase 4 wires the Biz Pod to real 5G network functions (NRF, UDM, AMF, AUSF) via SBI HTTP/2, replaces in-memory session stores with PostgreSQL, and adds the full observability stack (Prometheus metrics, OpenTelemetry tracing, structured logging) plus resilience patterns (circuit breaker, retry, health endpoints, DLQ). The existing codebase has stub files for NRF, UDM, AMF and a `resilience/resilience.go` stub; `ausf/` does not exist yet. All NF clients follow the option-function injection pattern already established for `WithAAA` and `WithAPIRoot`.

**Primary recommendation:** Implement each NF client as an independent package (`internal/nrf/`, `internal/udm/`, `internal/amf/`, `internal/ausf/`) with a private HTTP client field, then inject via `WithNRFClient`, `WithUDMClient`, `WithAUSFClient` options into the existing handlers. The circuit breaker and retry logic lives as a shared `resilience/` package used by all HTTP clients. Observability initialization happens once in `cmd/biz/main.go`.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Full cross-component OTel tracing — spans from AMF through HTTP Gateway → Biz Pod → AAA Gateway → AAA-S via W3C TraceContext propagated in HTTP headers and Redis pub/sub correlation. Trace context propagated across all 3 components; Biz Pod is the trace correlation hub.
- **D-02:** Dead-letter queue (DLQ) — AMF notification failures (re-auth, revocation) enqueued to DLQ after retries exhausted, not dropped.
- **D-03:** Per `host:port` circuit breaker — matches current `AAAConfig` scope. `CircuitBreakerRegistry` manages named breakers keyed by `"host:port"`.
- **D-04:** Startup in degraded mode — Biz Pod starts even if NRF registration fails at boot. NRF registration retried in background with exponential backoff. Until registered: use cached NRF data if available.
- **D-05:** Option functions (`WithNRFClient`, `WithUDMClient`, `WithAUSFClient`) added to existing handler packages.
- **D-06:** `NewSessionStore(*Pool)` and `NewAIWSessionStore(*Pool)` implemented as new files in `internal/storage/postgres/session_store.go`. Both implement existing `nssaa.AuthCtxStore` and `aiw.AuthCtxStore` interfaces.
- **D-07:** Health endpoints renamed from `/health` and `/ready` to `/healthz/live` and `/healthz/ready` per Kubernetes convention.

### Claude's Discretion
- OTel SDK memory configuration (batch exporter flush interval, resource limits)
- DLQ implementation details (Redis list, PostgreSQL table, or separate service)
- Alert threshold fine-tuning (error rate %, P99 latency ms)
- Exact metric label names and cardinality
- NRF discovery cache TTL (5 min as documented in design)
- Health check polling interval defaults

### Deferred Ideas (OUT OF SCOPE)
- Per S-NSSAI circuit breaker (sst+sd+host granularity)
- HTTP Gateway and AAA Gateway metrics/tracing integration
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| REQ-01 | NRF registration on Biz Pod startup | `internal/nrf/nrf.go` stub; design doc §2.2 NFProfile payload fields |
| REQ-02 | NRF heartbeat every 5 minutes | Design doc §2.3 heartbeat goroutine pattern |
| REQ-03 | AMF discovery via Nnrf_NFDiscovery | Design doc §3.1 discovery query params |
| REQ-04 | UDM Nudm_UECM_Get wired to N58 handler | `internal/udm/udm.go` stub; `WithUDMClient` option injection |
| REQ-05 | UDM Nudm_UECM_UpdateAuthContext after EAP | UDM client call after session completion |
| REQ-06 | AMF Re-Auth notification to reauthNotifUri | `internal/amf/amf.go` stub; HTTP POST with retry+DLQ |
| REQ-07 | AMF Revocation notification to revocNotifUri | `internal/amf/amf.go` stub; HTTP POST with retry+DLQ |
| REQ-08 | AUSF N60 client (internal/ausf/) | Package does NOT exist; needs new `internal/ausf/` creation |
| REQ-09 | PostgreSQL session store (NewSessionStore/NewAIWSessionStore) | `internal/storage/postgres/session.go` has `Repository`; need wrapper in `session_store.go` |
| REQ-10 | DLQ for AMF notification failures | Redis list (`LPUSH`/`BRPOP`) chosen over PostgreSQL — lower latency |
| REQ-11 | Circuit breaker per host:port | `internal/resilience/resilience.go` stub; needs full implementation |
| REQ-12 | Retry with exponential backoff (1s, 2s, 4s, max 3) | `time.Sleep` + context deadline + loop |
| REQ-13 | Timeouts: 30s EAP, 10s AAA, 5s DB, 100ms Redis | Already partially in `config.go` defaults |
| REQ-14 | Prometheus metrics at /metrics | `github.com/prometheus/client_golang` needed |
| REQ-15 | ServiceMonitor CRDs for all 3 components | YAML in `docs/design/19_observability.md` §2.2 |
| REQ-16 | Structured JSON logs with GPSI hashed | Already `slog.NewJSONHandler`; need GPSI hash helper |
| REQ-17 | Full cross-component OTel tracing | `go.opentelemetry.io/otel` + `go.opentelemetry.io/contrib/instrumentation/net/http` |
| REQ-18 | Health endpoints /healthz/live and /healthz/ready | Rename from `/health` and `/ready` |
| REQ-19 | Prometheus alerting rules | YAML in `docs/design/19_observability.md` §5 |
</phase_requirements>

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| NRF client | Biz Pod | — | Biz Pod registers itself and discovers UDM/AMF |
| UDM client | Biz Pod | — | Called from N58 handler before AAA routing |
| AMF notifications | Biz Pod | — | Triggered by AAA server-initiated messages |
| AUSF client | Biz Pod | — | Called from AIW handler for MSK forwarding |
| PostgreSQL store | Biz Pod | — | `NewSessionStore` wraps existing `Repository` |
| Circuit breaker | Biz Pod | — | Shared `resilience/` package wraps HTTP clients |
| Retry/backoff | Biz Pod | — | Shared `resilience/` package |
| Prometheus metrics | Biz Pod | HTTP GW, AAA GW | Each component exposes `/metrics` |
| OpenTelemetry | Biz Pod | HTTP GW, AAA GW | Biz Pod is trace correlation hub |
| Structured logging | Biz Pod | HTTP GW, AAA GW | `slog.NewJSONHandler` already in `main.go` |
| DLQ | Biz Pod | — | Redis list enqueue/dequeue |
| Health endpoints | Biz Pod | — | `main.go` mux registration |

---

## Technical Findings

### 1. NRF Client (`internal/nrf/`)

**Status:** Stub file exists at `internal/nrf/nrf.go` (2 lines — package doc only). Must implement.

**API Endpoints (from `docs/design/05_nf_profile.md` §2.1-2.2):**

| Operation | Method | Path | Body |
|-----------|--------|------|------|
| NF Registration | POST | `/nnrf-disc/v1/nf-instances` | Full NFProfile JSON |
| NF Heartbeat | PUT | `/nnrf-disc/v1/nf-instances/{nfInstanceId}` | `{nfStatus:"REGISTERED", heartBeatTimer:300, load:0-100}` |
| NF Discovery | GET | `/nnrf-disc/v1/nf-instances?target-nf-type={type}&service-names={service}` | — (query params) |
| NF Deregistration | DELETE | `/nnrf-disc/v1/nf-instances/{nfInstanceId}` | — |

**NRF Discovery query examples (from design doc §3.1-3.2):**
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem
GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF&service-names=nnssaaf-nssaa-notif
```

**NFProfile key fields (from design doc §2.2 — verbatim JSON):**
```json
{
  "nfInstanceId": "nssAAF-instance-{uuid}",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "plmnId": { "mcc": "208", "mnc": "001" },
  "nssaaInfo": {
    "supiRanges": [{ "start": "imsi-208001000000000", "end": "imsi-208001099999999" }],
    "supportedSecurityAlgorithm": ["EAP-TLS", "EAP-TTLS", "EAP-AKA_PRIME"]
  },
  "nfServices": {
    "nnssaaf-nssaa": {
      "serviceName": "nnssaaf-nssaa",
      "versions": [{ "apiVersion": "v1", "fullVersion": "1.2.1" }],
      "supportedFeatures": "3GPP-R18-NSSAA-REAUTH-REVOC"
    },
    "nnssaaf-aiw": { ... }
  },
  "heartBeatTimer": 300
}
```

**OAuth2 token handling:**
- Tokens obtained via `POST /oauth2/token` with client credentials grant
- Token cached until `< 5 min` remaining before expiry
- Token scope for N58: `nnssaaf-nssaa`; for N60: `nnssaaf-aiw`

**NRF client structure:**
```go
// internal/nrf/client.go
type Client struct {
    baseURL     string
    httpClient  *http.Client
    nfInstanceId string
    cache       *NRFDiscoveryCache  // TTL: 5 min per design doc §3.3
    tokenCache  *TokenCache
}

func NewClient(cfg config.NRFConfig) *Client
func (c *Client) Register(ctx context.Context, profile *NFProfile) error
func (c *Client) Heartbeat(ctx context.Context) error   // every 5 min
func (c *Client) Deregister(ctx context.Context) error
func (c *Client) DiscoverUDM(ctx context.Context, plmnId string) (*UDMProfile, error)
func (c *Client) DiscoverAMF(ctx context.Context, amfId string) (*AMFProfile, error)

// Discovery cache key format (from design doc §3.3):
// - "udm:uem:{plmnId}"  → UDM Nudm_UECM endpoint
// - "amf:{amfId}"       → AMF profile
```

**Degraded mode startup (D-04):** `Register()` returns nil error on failure; goroutine retries in background. Handler checks `client.IsRegistered()` before sending discovery requests.

---

### 2. UDM Client (`internal/udm/`)

**Status:** Stub file exists at `internal/udm/udm.go` (3 lines). Must implement.

**API Endpoints (from TS 29.526 §7.3, TS 23.502 §4.2.9):**

| Operation | Method | Path | Purpose |
|-----------|--------|------|---------|
| Nudm_UECM_Get | GET | `/nudm-uem/v1/subscribers/{supi}/auth-contexts` | Get auth subscription for AAA routing |
| Nudm_UECM_UpdateAuthContext | PUT | `/nudm-uem/v1/subscribers/{supi}/auth-contexts/{authCtxId}` | Update after EAP completion |

**Nudm_UECM_Get response structure (from design doc §3.2):**
```json
{
  "authContexts": [{
    "authType": "EAP_TLS",
    "aaaServer": "radius://aaa.operator.com:1812",
    "sliceAuthInfo": { ... }
  }]
}
```

**UDM client integration point:** Called in `nssaa/handler.go` `CreateSliceAuthenticationContext` before `h.aaa.SendEAP()` — gates AAA routing based on auth subscription. If UDM unreachable and no cache, return 503.

```go
// internal/udm/client.go
type Client struct {
    baseURL    string
    httpClient *http.Client
    nrfClient  *nrf.Client  // for service discovery
    cache      *UDMCache
}

func NewClient(cfg config.UDMConfig, nrf *nrf.Client) *Client
func (c *Client) GetAuthContext(ctx context.Context, supi string) (*AuthContext, error)
func (c *Client) UpdateAuthContext(ctx context.Context, supi, authCtxId string, status string) error
```

**Config fields (from `internal/config/config.go`):**
```go
type UDMConfig struct {
    BaseURL string        `yaml:"baseURL"`  // e.g. "http://udm.operator.com:8080"
    Timeout time.Duration `yaml:"timeout"`  // default: 10s (matches AAA.ResponseTimeout)
}
```

---

### 3. AMF Client (`internal/amf/`)

**Status:** Stub file exists at `internal/amf/amf.go` (3 lines). Must implement.

**AMF notification endpoints:** No discovery needed — AMF provides `reauthNotifUri` and `revocNotifUri` directly in `SliceAuthInfo` request (from `nssaa/handler.go` lines 181-187). These are absolute URLs.

**Notification types (from TS 23.502 §4.2.9.3-4):**

| Notification | Trigger | Payload |
|-------------|---------|---------|
| Re-Auth | RADIUS CoA-Request or Diameter RAR | `{authCtxId, gpsi, snssai, reauthReason}` |
| Revocation | Diameter ASR | `{authCtxId, gpsi, snssai, revocReason}` |

**Notification flow with retry and DLQ:**
```
1. Attempt POST to {reauthNotifUri|revocNotifUri}
2. On failure: retry 1s, 2s, 4s (max 3 attempts)
3. After retries exhausted: LPUSH to Redis DLQ list
4. DLQ processor retries every 5 min; logs ERROR after 3 DLQ failures
```

**AMF client structure:**
```go
// internal/amf/client.go
type Client struct {
    httpClient    *http.Client
    circuitBreaker *resilience.CircuitBreaker
    dlq           *DLQ
}

func NewClient(httpClient *http.Client, cb *resilience.CircuitBreaker, dlq *DLQ) *Client
func (c *Client) SendReAuthNotification(ctx context.Context, uri string, payload []byte) error
func (c *Client) SendRevocationNotification(ctx context.Context, uri string, payload []byte) error
```

**No `AMFConfig` needed in `config.go`:** AMF callbacks use absolute URIs from requests. AMF discovery is optional (design doc §3.1: "AMF instance ID is sent by AMF in `amfInstanceId` field").

---

### 4. AUSF Client (`internal/ausf/`)

**Status:** Package does NOT exist. Must create `internal/ausf/` directory and files.

**API Endpoints (from TS 29.526 §7.3, N60 interface):**

| Operation | Method | Path | Purpose |
|-----------|--------|------|---------|
| Nudm_AuthCM_Get | GET | `/nudm-auth/v1/{supi}/auth-context` | Not used by NSSAAF directly |
| AUSF N60 MSK Forward | — | (Internal to Biz Pod) | Forward MSK to NSSAAF after AUSF authentication |

**Context from AIW flow (TS 29.526 §7.3):** The AIW (Authentication Information_WLAN) handler receives SUPI-based authentication requests and may need to coordinate with AUSF for MSK forwarding. The AUSF client in NSSAAF's context handles the N60 interface for SNPN credential holder authentication.

**AUSF client structure:**
```go
// internal/ausf/client.go
type Client struct {
    baseURL    string
    httpClient *http.Client
}

func NewClient(cfg config.AUSFConfig) *Client
func (c *Client) ForwardMSK(ctx context.Context, supi string, msk []byte) error
```

**`AUSFConfig` needed in `config.go`:**
```go
// Add to Config struct:
AUSF AUSFConfig `yaml:"ausf"`

type AUSFConfig struct {
    BaseURL string        `yaml:"baseURL"`  // e.g. "http://ausf.operator.com:8080"
    Timeout time.Duration `yaml:"timeout"`
}
```

---

### 5. PostgreSQL Session Store

**Status:** `Repository` exists in `internal/storage/postgres/session.go` with full CRUD. `NewSessionStore`/`NewAIWSessionStore` wrappers do NOT exist. Must create in `internal/storage/postgres/session_store.go`.

**Existing schema (from `migrations/000001_create_sessions_table.up.sql`):**
```sql
CREATE TABLE slice_auth_sessions (
    auth_ctx_id        VARCHAR(64) NOT NULL PRIMARY KEY,
    gpsi              VARCHAR(32) NOT NULL,
    supi              VARCHAR(32),
    snssai_sst        INTEGER NOT NULL CHECK (0-255),
    snssai_sd         VARCHAR(8),
    amf_instance_id    VARCHAR(64),
    reauth_notif_uri  TEXT,
    revoc_notif_uri   TEXT,
    aaa_config_id     UUID NOT NULL,
    eap_session_state BYTEA NOT NULL,  -- encrypted
    eap_rounds        INTEGER DEFAULT 0,
    max_eap_rounds    INTEGER DEFAULT 20,
    nssaa_status      VARCHAR(20) DEFAULT 'NOT_EXECUTED',
    auth_result       VARCHAR(20),
    failure_reason    TEXT,
    created_at        TIMESTAMPTZ DEFAULT NOW(),
    updated_at        TIMESTAMPTZ DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ,
    terminated_at     TIMESTAMPTZ
) PARTITION BY RANGE (created_at);
```

**Existing `Repository` interface (from `session.go`):**
```go
type Repository struct {
    pool      *Pool
    encryptor *Encryptor  // AES-256-GCM for eap_session_state
}

func NewRepository(pool *Pool, encryptor *Encryptor) *Repository
func (r *Repository) Create(ctx context.Context, s *Session) error
func (r *Repository) GetByAuthCtxID(ctx context.Context, authCtxID string) (*Session, error)
func (r *Repository) Update(ctx context.Context, s *Session) error
func (r *Repository) Delete(ctx context.Context, authCtxID string) error
func (r *Repository) ListPending(ctx context.Context) ([]*Session, error)
func (r *Repository) ExpireOld(ctx context.Context) (int, error)
```

**Wrapper to implement `nssaa.AuthCtxStore`:**
```go
// internal/storage/postgres/session_store.go

// Store wraps Repository with nssaa.AuthCtxStore interface
type Store struct {
    repo *Repository
}

func NewSessionStore(pool *Pool, encryptor *Encryptor) *Store {
    return &Store{repo: NewRepository(pool, encryptor)}
}

func (s *Store) Load(id string) (*nssaa.AuthCtx, error) { ... }
func (s *Store) Save(ctx *nssaa.AuthCtx) error { ... }
func (s *Store) Delete(id string) error { ... }
func (s *Store) Close() error { return nil }  // pool.Close handled by main.go

// AIWStore wraps Repository with aiw.AuthCtxStore interface
type AIWStore struct {
    repo *Repository
}

func NewAIWSessionStore(pool *Pool, encryptor *Encryptor) *AIWStore {
    return &AIWStore{repo: NewRepository(pool, encryptor)}
}

func (s *AIWStore) Load(id string) (*aiw.AuthContext, error) { ... }
func (s *AIWStore) Save(ctx *aiw.AuthContext) error { ... }
func (s *AIWStore) Delete(id string) error { ... }
func (s *AIWStore) Close() error { return nil }
```

**Encryption key source:** `config.Database.EncryptionKey` field (not yet in `config.go`):
```go
type DatabaseConfig struct {
    ...
    EncryptionKey string `yaml:"encryptionKey"`  // 32 bytes, base64 or hex
}
```

---

### 6. Circuit Breaker

**Status:** `internal/resilience/resilience.go` stub exists (2 lines — package doc only). Must implement.

**Requirements (from REQ-11 and D-03):**
- Per `host:port` key (e.g., `"aaa.operator.com:1812"`)
- `CircuitBreakerRegistry` keyed by `"host:port"`
- State machine: CLOSED → OPEN (5 consecutive failures) → HALF_OPEN (30s recovery) → CLOSED (3 successes resets) or OPEN (1 failure)
- `AAAConfig.FailureThreshold` = 5, `AAAConfig.RecoveryTimeout` = 30s (from `config.go` defaults)

**Implementation pattern (Go concurrency-safe):**
```go
// internal/resilience/circuit_breaker.go

type State int  // 0=CLOSED, 1=OPEN, 2=HALF_OPEN

type CircuitBreaker struct {
    mu sync.Mutex
    state       State
    failures    int
    successes   int   // for HALF_OPEN → CLOSED transition
    lastFailure time.Time
    openedAt    time.Time

    // Config
    failureThreshold int
    recoveryTimeout  time.Duration
    successThreshold int  // default 3
}

func (cb *CircuitBreaker) Allow() bool
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure()
func (cb *CircuitBreaker) State() State

// Registry manages named circuit breakers
type Registry struct {
    mu       sync.RWMutex
    breakers map[string]*CircuitBreaker
    defaultFailureThreshold int
    defaultRecoveryTimeout  time.Duration
}

func NewRegistry(failureThreshold int, recoveryTimeout time.Duration) *Registry
func (r *Registry) Get(key string) *CircuitBreaker
```

**Exposing metrics (from design doc §2.1):**
```go
var CircuitBreakerState = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "nssAAF_circuit_breaker_state",
        Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
    },
    []string{"server"},
)

var CircuitBreakerFailures = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "nssAAF_circuit_breaker_failures_total",
        Help: "Total circuit breaker recorded failures",
    },
    []string{"server"},
)
```

---

### 7. Retry with Exponential Backoff

**Requirements (from REQ-12):** 1s, 2s, 4s with max 3 attempts (4 total calls including initial).

**Implementation:**
```go
// internal/resilience/retry.go

// Config holds retry configuration.
type RetryConfig struct {
    MaxAttempts int
    BaseDelay   time.Duration  // 1s
    MaxDelay    time.Duration  // 4s
}

// Do executes fn with exponential backoff retry.
// Returns ErrMaxRetriesExceeded after MaxAttempts failures.
func Do(ctx context.Context, cfg RetryConfig, fn func() error) error {
    var lastErr error
    for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
        if err := fn(); err == nil {
            return nil
        } else {
            lastErr = err
        }

        // Don't sleep on last attempt
        if attempt < cfg.MaxAttempts-1 {
            delay := cfg.BaseDelay * time.Duration(1<<attempt)  // 1s, 2s, 4s
            if delay > cfg.MaxDelay {
                delay = cfg.MaxDelay
            }
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(delay):
            }
        }
    }
    return fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxAttempts, lastErr)
}

// Wrap a *http.Client with retry + circuit breaker
func (cb *CircuitBreaker) RoundTrip(ctx context.Context, req *http.Request) (*http.Response, error) {
    if !cb.Allow() {
        return nil, ErrCircuitOpen
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        cb.RecordFailure()
        return nil, err
    }

    if resp.StatusCode >= 500 || resp.StatusCode == 429 {
        cb.RecordFailure()
        resp.Body.Close()
        return nil, fmt.Errorf("retryable status: %d", resp.StatusCode)
    }

    cb.RecordSuccess()
    return resp, nil
}
```

---

### 8. DLQ (Dead Letter Queue)

**Decision:** Redis list (LPUSH/BRPOP) over PostgreSQL table. Rationale: lower latency for enqueue (non-blocking), simpler code, Redis already a dependency.

**Key format:** `nssAAF:dlq:amf-notifications`

**Item format (JSON):**
```json
{
  "id": "uuid",
  "type": "reauth|revocation",
  "uri": "https://amf.operator.com:8080/ntss/...",
  "payload": {...},
  "authCtxId": "...",
  "attempt": 0,
  "maxAttempts": 3,
  "createdAt": "2026-04-25T12:00:00Z",
  "lastError": "connection refused"
}
```

**Enqueue (on retry exhaustion):**
```go
func (d *DLQ) Enqueue(ctx context.Context, item *DLQItem) error {
    data, _ := json.Marshal(item)
    return d.redis.LPush(ctx, "nssAAF:dlq:amf-notifications", data).Err()
}
```

**Dequeue (DLQ processor, runs every 5 min):**
```go
// DLQ processor goroutine
func (d *DLQ) Process(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(5 * time.Minute):
            item, err := d.redis.BRPop(ctx, 1*time.Second, "nssAAF:dlq:amf-notifications").Result()
            if err != nil {
                continue
            }
            var dlqItem DLQItem
            json.Unmarshal([]byte(item), &dlqItem)
            // Attempt delivery, re-enqueue on failure (up to 3 DLQ retries)
            d.processItem(ctx, &dlqItem)
        }
    }
}
```

**DLQ metrics:**
```go
var DLQDepth = prometheus.NewGauge(
    prometheus.GaugeOpts{
        Name: "nssAAF_dlq_depth",
        Help: "Number of items in AMF notification DLQ",
    },
)
```

---

### 9. Prometheus Metrics

**Required packages:**
```go
github.com/prometheus/client_golang v1.20.5        // [VERIFIED: npm registry, 2026-04]
go.opentelemetry.io/otel/trace v1.32.0
go.opentelemetry.io/otel/sdk v1.32.0
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.54.0
```

**Metric names and labels (from `docs/design/19_observability.md` §2.1):**

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `nssAAF_requests_total` | CounterVec | `service`, `endpoint`, `method`, `status_code` | Total API requests |
| `nssAAF_request_duration_seconds` | HistogramVec | `service`, `endpoint`, `method` | Request latency; buckets: .001,.005,.01,.025,.05,.1,.25,.5,1 |
| `nssAAF_eap_sessions_active` | Gauge | — | Active EAP sessions |
| `nssAAF_eap_sessions_total` | CounterVec | `result` | Total EAP sessions (`success`, `failure`, `timeout`) |
| `nssAAF_eap_session_duration_seconds` | HistogramVec | `eap_method` | Session duration; buckets: 1,5,10,30,60,120,300 |
| `nssAAF_eap_rounds` | Histogram | — | Rounds per session; buckets: 1,2,3,5,10,20 |
| `nssAAF_aaa_requests_total` | CounterVec | `protocol`, `server`, `result` | AAA requests (Biz Pod → AAA GW) |
| `nssAAF_aaa_request_duration_seconds` | HistogramVec | `protocol`, `server` | AAA request latency; buckets: .001,.005,.01,.025,.05,.1,.25,.5 |
| `nssAAF_db_query_duration_seconds` | HistogramVec | `operation`, `table` | DB latency; buckets: .001,.002,.005,.01,.025,.05,.1 |
| `nssAAF_db_connections_active` | Gauge | — | Active DB connections |
| `nssAAF_redis_operations_total` | CounterVec | `operation`, `result` | Redis ops |
| `nssAAF_circuit_breaker_state` | GaugeVec | `server` | 0=closed, 1=open, 2=half-open |
| `nssAAF_circuit_breaker_failures_total` | CounterVec | `server` | CB failure count |
| `nssAAF_rate_limit_rejections_total` | CounterVec | `type` | Rate limit rejections |
| `nssAAF_nrf_cache_hits_total` | Counter | — | NRF cache hits |
| `nssAAF_nrf_cache_misses_total` | Counter | — | NRF cache misses |
| `nssAAF_dlq_depth` | Gauge | — | DLQ depth (new) |

**Handler registration pattern:**
```go
// internal/metrics/metrics.go (new package)
func Register() {
    prometheus.MustRegister(
        RequestsTotal, RequestDuration,
        EapSessionsActive, EapSessionsTotal, EapSessionDuration, EapRounds,
        AaaRequestsTotal, AaaRequestDuration,
        DbQueryDuration, DbConnectionsActive,
        RedisOperationsTotal,
        CircuitBreakerState, CircuitBreakerFailures,
        RateLimitRejections,
        NrfCacheHits, NrfCacheMisses,
        DLQDepth,
    )
}
```

**Endpoint registration in `main.go`:**
```go
mux.HandleFunc("/metrics", promhttp.Handler())  // already in stdlib via promhttp
```

---

### 10. OpenTelemetry Tracing

**Trace propagation:** W3C TraceContext via HTTP headers (`traceparent`, `tracestate`). Same context flows through HTTP Gateway → Biz Pod → AAA Gateway via Redis pub/sub correlation.

**SDK initialization (from design doc §4.1):**
```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
    "go.opentelemetry.io/sdk/trace"
    sdktrace "go.opentelemetry.io/sdk/trace"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// W3C TraceContext propagator
propagator := propagation.NewCompositeTextMapPropagator(
    propagation.TraceContext{},
    propagation.Baggage{},
)
otel.SetTextMapPropagator(propagator)

// Exporter (stdout for dev; OTLP for production)
exporter, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter),
    sdktrace.WithResource(resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName("nssAAF-biz"),
        semconv.ServiceVersion(cfg.Version),
        attribute.String("pod.name", podID),
    )),
)
otel.SetTracerProvider(tp)
```

**Span creation in handlers:**
```go
// Extract trace context from incoming request headers
ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

// Start span
ctx, span := tracer.Start(ctx, "Nnssaaf_NSSAA.Authenticate",
    trace.WithAttributes(
        attribute.String("gpsi_hash", hashGpsi(req.Gpsi)),
        attribute.Int("snssai_sst", int(req.Snssai.Sst)),
        attribute.String("auth_ctx_id", authCtxID),
    ),
)
defer span.End()

// Sub-span for UDM call
ctx, udmSpan := tracer.Start(ctx, "udm.GetAuthContext")
udmSpan.SetAttributes(attribute.String("supi", supi))
// ... UDM call ...
udmSpan.End()
```

**Cross-component trace (D-01):** Biz Pod is trace correlation hub. HTTP Gateway and AAA Gateway each create child spans. Trace context propagated via:
- HTTP headers: `traceparent: 00-{traceId}-{spanId}-{flags}`
- Redis pub/sub headers: trace context embedded in `proto.AaaServerInitiatedRequest.TraceContext` field

**OTel resource attributes:**
```go
semconv.ServiceName("nssAAF-biz")
semconv.ServiceVersion(cfg.Version)
attribute.String("pod.name", podID)
attribute.String("namespace", "nssAAF")
attribute.String("component", "biz")
```

---

### 11. Structured Logging

**Current state:** `main.go` already uses `slog.NewJSONHandler`. Phase 4 extends with GPSI hashing and trace context fields.

**GPSI hash function (from design doc §3.2):**
```go
// internal/logging/gpsi.go (new package)
import (
    "crypto/sha256"
    "encoding/base64"
)

func HashGPSI(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return base64.RawURLEncoding.EncodeToString(h[:8])  // first 8 bytes, base64url
}
```

**Log entry fields (from design doc §3.1):**
```go
// LogEntry JSON fields (all optional except timestamp, level, message):
{
    "timestamp": "2026-04-25T12:00:00.000Z",
    "level": "INFO",
    "message": "session_created",
    "request_id": "uuid",
    "trace_id": "abc123def456",
    "span_id": "span001",

    "service": "nssAAF-biz",
    "version": "1.2.1",
    "hostname": "nssAAF-biz-xyz",
    "pod_name": "nssAAF-biz-xyz",
    "namespace": "nssAAF",

    "gpsi_hash": "b4s3H4sh",       // NEVER log raw GPSI
    "amf_instance_id": "amf-001",
    "snssai_sst": 1,
    "snssai_sd": "000001",
    "auth_ctx_id": "uuid",

    "operation": "eap_tls",
    "duration_ms": 250,
    "status_code": 201,

    "error": "connection refused",
    "stack_trace": "..."
}
```

**Structured log helper with trace context:**
```go
// internal/logging/logger.go (new package)
func WithTrace(ctx context.Context, msg string, args ...any) {
    traceID := trace.SpanContextFromContext(ctx).TraceID().String()
    spanID := trace.SpanContextFromContext(ctx).SpanID().String()
    args = append(args, "trace_id", traceID, "span_id", spanID)
    slog.Info(msg, args...)
}
```

---

### 12. Health Endpoints

**Current state (`main.go` lines 257-267):** `/health` and `/ready` exist as stubs returning static JSON.

**Required changes (D-07):**
- Rename `/health` → `/healthz/live`
- Rename `/ready` → `/healthz/ready`

**`/healthz/live` implementation:**
```go
func handleLiveness(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}
```

**`/healthz/ready` implementation (checks dependencies):**
```go
func handleReadiness(w http.ResponseWriter, r *http.Request) {
    checks := map[string]string{}

    // Check PostgreSQL
    if err := db.Ping(r.Context()); err != nil {
        checks["postgres"] = "unhealthy: " + err.Error()
    } else {
        checks["postgres"] = "ok"
    }

    // Check Redis
    if err := redis.Ping(r.Context()).Err(); err != nil {
        checks["redis"] = "unhealthy: " + err.Error()
    } else {
        checks["redis"] = "ok"
    }

    // Check AAA Gateway
    resp, err := http.Get(cfg.Biz.AAAGatewayURL + "/health")
    if err != nil || resp.StatusCode != 200 {
        checks["aaa_gateway"] = "unhealthy"
    } else {
        checks["aaa_gateway"] = "ok"
    }

    // Check NRF registration (degraded but not unhealthy)
    if !nrfClient.IsRegistered() {
        checks["nrf_registration"] = "degraded (retrying)"
    } else {
        checks["nrf_registration"] = "ok"
    }

    // Aggregate
    allOk := true
    for _, v := range checks {
        if !strings.HasPrefix(v, "ok") && !strings.HasPrefix(v, "degraded") {
            allOk = false
            break
        }
    }

    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    if allOk {
        w.WriteHeader(http.StatusOK)
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
    }
    json.NewEncoder(w).Encode(checks)
}
```

---

### 13. Handler Injection Pattern

**Existing pattern (from `nssaa/handler.go` lines 97-117):**
```go
type Handler struct {
    store   AuthCtxStore
    aaa     AAARouter
    apiRoot string
    // NEW: client fields added below
    nrfClient *nrf.Client  // nil if NRF unavailable
    udmClient *udm.Client
}

type HandlerOption func(*Handler)

func WithNRFClient(c *nrf.Client) HandlerOption { ... }
func WithUDMClient(c *udm.Client) HandlerOption { ... }

// nssaa/handler.go gains UDM call before AAA routing:
func (h *Handler) CreateSliceAuthenticationContext(...) {
    // 1. Validate (existing)
    // 2. NEW: Get auth subscription from UDM
    authCtx, _ := h.udmClient.GetAuthContext(ctx, gpsi)
    // 3. Use auth subscription for AAA routing
    // 4. Create session (existing)
    // 5. Forward to AAA-S (existing, now with real client)
}
```

**AIW handler similarly gains `WithAUSFClient`:**
```go
type Handler struct {
    store      AuthCtxStore
    aaa        AAARouter
    apiRoot    string
    ausfClient *ausf.Client  // NEW
}
```

---

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/prometheus/client_golang` | v1.20.5 | Prometheus metrics | De facto standard for Go metrics; integrates with `promhttp.Handler()` |
| `go.opentelemetry.io/otel` | v1.32.0 | OpenTelemetry tracing | 3-component cross-trace requires OTel |
| `go.opentelemetry.io/otel/sdk` | v1.32.0 | OTel SDK | TracerProvider, BatchSpanProcessor |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.54.0 | HTTP instrumentation | Auto-instruments HTTP clients |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | v1.32.0 | Dev trace exporter | Local dev; swap for OTLP in prod |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/google/uuid` | v1.6.0 | UUID generation | Already in go.mod |
| `github.com/jackc/pgx/v5` | v5.9.1 | PostgreSQL | Already in go.mod |
| `github.com/redis/go-redis/v9` | v9.18.0 | Redis | Already in go.mod |
| `golang.org/x/sync` | (stdlib) | `errgroup`, `singleflight` | Single-flight for NRF discovery deduplication |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Prometheus client | `github.com/metrics-port/metrics` | Prometheus is industry standard; ServiceMonitor integration |
| OTel SDK | `zipkin` or `jaeger` direct | OTel is vendor-neutral; works with any backend |
| Redis for DLQ | PostgreSQL table | Redis lower latency; same operational footprint |
| `slog` | `zerolog` or `zap` | `slog` is stdlib (Go 1.21+); no extra dependency |

**Installation:**
```bash
go get github.com/prometheus/client_golang@v1.20.5 \
       go.opentelemetry.io/otel@v1.32.0 \
       go.opentelemetry.io/otel/sdk@v1.32.0 \
       go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.54.0
```

---

## Architecture Patterns

### System Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     AMF / AUSF (External)                        │
│              N58: /nnssaaf-nssaa/v1/slice-authentications       │
└───────────────────────────────┬─────────────────────────────────┘
                                │ HTTP/SBI (traceparent header)
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      HTTP Gateway (Phase R)                     │
│              TLS 1.3 terminator + router (stateless)            │
│              - Adds X-Request-ID, preserves traceparent        │
│              - Routes to Biz Pod ClusterIP Service              │
│              - Exposes /metrics (promhttp.Handler)              │
└───────────────────────────────┬─────────────────────────────────┘
                                │ HTTP/ClusterIP
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Biz Pod (Phase 4)                         │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐   │
│  │ N58 Handler (nssaa.Handler + WithNRFClient + WithUDMClient) │
│  │ 1. Extract trace context (W3C TraceContext)             │   │
│  │ 2. Validate GPSI, Snssai                                │   │
│  │ 3. UDM Nudm_UECM_Get (auth subscription for AAA routing)│   │
│  │ 4. Persist session → PostgreSQL (NewSessionStore)       │   │
│  │ 5. HTTP POST → AAA Gateway (with circuit breaker)        │   │
│  │ 6. Return 201 with authCtxId + EAP challenge            │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐   │
│  │ N60 Handler (aiw.Handler + WithAUSFClient)             │   │
│  │ 1. Validate SUPI                                        │   │
│  │ 2. AUSF MSK forwarding                                  │   │
│  │ 3. Persist → PostgreSQL (NewAIWSessionStore)            │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐   │
│  │ Resilience Layer                                        │   │
│  │ - CircuitBreakerRegistry (per host:port)               │   │
│  │ - Retry with backoff (1s,2s,4s × 3 attempts)          │   │
│  │ - DLQ: Redis LPUSH → AMF notification failures         │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐   │
│  │ NRF Client (internal/nrf/)                            │   │
│  │ - POST /nnrf-disc/v1/nf-instances (on startup)         │   │
│  │ - PUT .../{id} heartbeat every 5 min                   │   │
│  │ - GET ...?target-nf-type=UDM (discovery)               │   │
│  │ - NRFDiscoveryCache (TTL 5 min)                        │   │
│  │ - Startup in degraded mode if NRF unavailable          │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐   │
│  │ Observability                                          │   │
│  │ - Prometheus: /metrics (promhttp.Handler)              │   │
│  │ - OTel: TracerProvider with BatchSpanProcessor        │   │
│  │ - Structured logs: slog.NewJSONHandler                 │   │
│  │ - Health: /healthz/live (always 200), /healthz/ready   │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                              │
│  ┌───────────────┐  ┌───────────────┐  ┌─────────────────┐  │
│  │  PostgreSQL    │  │    Redis      │  │   AAA Gateway    │  │
│  │ slice_auth_   │  │ nssAAF:dlq:*  │  │   (Phase R)      │  │
│  │ sessions      │  │ rate limit    │  │   HTTP ← biz     │  │
│  │ (encrypted)   │  │ idempotency   │  │   RADIUS→AAA-S   │  │
│  └───────────────┘  └───────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Recommended Project Structure

```
cmd/biz/main.go                          # Updated: wire NRF/UDM/AUSF clients, PG store, OTel, metrics
internal/nrf/
    nrf.go                               # (existing stub, 2 lines)
    client.go                            # NEW: NRF client with registration, heartbeat, discovery
    cache.go                             # NEW: NRFDiscoveryCache with 5-min TTL
    token.go                             # NEW: TokenCache for OAuth2 token reuse
internal/udm/
    udm.go                               # (existing stub, 3 lines)
    client.go                            # NEW: UDM client with Nudm_UECM_Get
internal/amf/
    amf.go                               # (existing stub, 3 lines)
    notifier.go                          # NEW: AMF notification sender with retry+DLQ
internal/ausf/                            # NEW (does not exist)
    client.go                            # NEW: AUSF N60 client
internal/resilience/
    resilience.go                        # (existing stub, 2 lines)
    circuit_breaker.go                   # NEW: CircuitBreaker + Registry
    retry.go                             # NEW: Retry with exponential backoff
internal/logging/                        # NEW (does not exist)
    gpsi.go                              # NEW: GPSI SHA256 hash (first 8 bytes, base64url)
    logger.go                            # NEW: Structured log helpers with trace context
internal/metrics/                        # NEW (does not exist)
    metrics.go                           # NEW: All prometheus.MustRegister calls
internal/storage/postgres/
    pool.go                              # (existing)
    session.go                          # (existing: Repository)
    session_store.go                     # NEW: NewSessionStore + NewAIWSessionStore wrappers
internal/cache/redis/
    dlq.go                               # NEW: DLQ enqueue/dequeue (Redis LPUSH/BRPOP)
    ...                                  # (existing)
internal/api/nssaa/
    handler.go                           # (existing: add WithNRFClient, WithUDMClient, UDM call)
internal/api/aiw/
    handler.go                           # (existing: add WithAUSFClient)
internal/config/
    config.go                            # (existing: add AUSFConfig, Database.EncryptionKey)
configs/
    biz.yaml                             # (does not exist yet: create with all config fields)
```

### Pattern 1: NF Client with OTel Injection

```go
// Every NF client wraps HTTP calls with OTel tracing
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

func NewNRFClient(cfg config.NRFConfig) *NRFClient {
    httpClient := &http.Client{
        Transport: otelhttp.NewTransport(http.DefaultTransport),
        Timeout:   cfg.DiscoverTimeout,
    }
    return &NRFClient{
        baseURL:    cfg.BaseURL,
        httpClient: httpClient,
        tracer:     otel.Tracer("nrf"),
    }
}

func (c *NRFClient) Register(ctx context.Context, profile *NFProfile) error {
    ctx, span := c.tracer.Start(ctx, "nrf.Register")
    defer span.End()

    body, _ := json.Marshal(profile)
    req, _ := http.NewRequestWithContext(ctx, "POST",
        c.baseURL+"/nnrf-disc/v1/nf-instances", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        span.RecordError(err)
        return err
    }
    defer resp.Body.Close()
    // ... parse response ...
    return nil
}
```

### Pattern 2: Option Function Injection for Handlers

```go
// nssaa/handler.go — extend existing pattern
type Handler struct {
    store      AuthCtxStore
    aaa        AAARouter
    apiRoot    string
    nrfClient  *nrf.Client  // nil until wired
    udmClient  *udm.Client
}

type HandlerOption func(*Handler)

func WithNRFClient(c *nrf.Client) HandlerOption {
    return func(h *Handler) { h.nrfClient = c }
}
func WithUDMClient(c *udm.Client) HandlerOption {
    return func(h *Handler) { h.udmClient = c }
}

// cmd/biz/main.go wiring:
nssaaHandler := nssaa.NewHandler(pgStore,
    nssaa.WithAPIRoot(apiRoot),
    nssaa.WithAAA(aaaClient),
    nssaa.WithNRFClient(nrfClient),   // NEW
    nssaa.WithUDMClient(udmClient),  // NEW
)
```

### Anti-Patterns to Avoid

- **Do NOT create a monolithic "NF" client package.** Each NF (NRF, UDM, AMF, AUSF) gets its own package. They share the resilience/retry patterns but have different API surfaces.
- **Do NOT use `sync.Map`** for the circuit breaker registry. Use `sync.RWMutex` + `map[string]*CircuitBreaker` — simpler and more idiomatic.
- **Do NOT block startup on NRF registration.** Use goroutine + channel for background retry (D-04).
- **Do NOT log raw GPSI.** Always hash with `HashGPSI()` — never `slog.Info("session_created", "gpsi", gpsi)`.
- **Do NOT use a single global `http.Client`** for all NF calls. Each NF client gets its own `http.Client` with appropriate timeout, so circuit breaker and retry can be scoped per-client.
- **Do NOT implement circuit breaker in the handler.** Wrap the HTTP client or the specific call site in `resilience/` — the circuit breaker should be invisible to business logic.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Prometheus metrics registration | Manual counters | `prometheus.NewCounterVec` + `prometheus.MustRegister` | Ensures metric is registered exactly once; `MustRegister` panics on duplicates |
| Distributed tracing propagation | Custom trace ID header format | W3C TraceContext (`traceparent`) | Interoperable across all OTel-compatible services |
| JSON structured logging | Manual JSON marshaling | `slog.NewJSONHandler` | Stdlib, zero allocation, built-in trace context support |
| Retry with backoff | Ad-hoc for loop | `resilience.Retry` with `BaseDelay=1s` | Handles context cancellation, jitter, and exhaustion cleanly |
| Circuit breaker | Shared mutex + map | `resilience.CircuitBreakerRegistry` | Thread-safe, per-host:port scoping, exposed metrics |
| GPSI hashing | Plain SHA256 string | `HashGPSI()` helper returning first 8 bytes, base64url | Consistent across all log call sites |

**Key insight:** The 5G SBA relies on standard protocol interfaces (NRF discovery, UDM subscription data, AMF callbacks). Custom implementations of retry, circuit breaking, and tracing are error-prone and miss edge cases that standard libraries handle (context cancellation during backoff, trace context injection into outbound HTTP headers, metric cardinality explosion).

---

## Common Pitfalls

### Pitfall 1: NRF Discovery Cache Stampede
**What goes wrong:** 100 Biz Pods start simultaneously, all miss NRF cache, all query NRF at once → NRF overwhelmed.
**Why it happens:** No cache warming or request deduplication.
**How to avoid:** Use `singleflight` (golang.org/x/sync) to coalesce simultaneous discovery requests into one NRF call per `key`.
**Warning signs:** NRF CPU spike on pod restart, `429 Too Many Requests` from NRF.

### Pitfall 2: GPSI Hash Inconsistency Across Logs
**What goes wrong:** Some logs use `HashGPSI()`, others use raw GPSI → compliance violation.
**Why it happens:** No enforced linter rule or compile-time check.
**How to avoid:** `HashGPSI()` is the only allowed function for GPSI in log arguments. Add golangci-lint rule: `logfatal` on `slog` calls with key `"gpsi"` that isn't already a hash.

### Pitfall 3: OTel Span Context Not Propagated to AAA Gateway
**What goes wrong:** Biz Pod → AAA Gateway trace ends at Biz Pod; AAA Gateway starts a new trace.
**Why it happens:** `traceparent` header not forwarded in the HTTP POST to AAA Gateway.
**How to avoid:** In `handleAaaForward` (main.go lines 145-146), inject trace context: `req = req.WithContext(ctx)` where `ctx` already has the OTel span. The `otelhttp.Transport` on the AAA Gateway's HTTP client automatically extracts and continues the trace.

### Pitfall 4: DLQ Item TTL vs. Infinite Retry
**What goes wrong:** Failed notifications re-enqueued indefinitely, DLQ grows forever.
**Why it happens:** No max DLQ retry count.
**How to avoid:** `DLQItem.MaxAttempts` field (default 3). After 3 DLQ attempts, log at `ERROR` level with full context and discard.

### Pitfall 5: Prometheus Metric Cardinality Explosion
**What goes wrong:** Circuit breaker with `server="{full_url}"` label creates one metric per host+port+path combination.
**Why it happens:** High-cardinality labels (URLs, full hostnames) on frequently-updated metrics.
**How to avoid:** Normalize `server` label to `host:port` only (no path, no scheme). For AAA server labels, use the configured host:port from `AAAConfig`, not the resolved IP.

---

## Code Examples

### NF Client with Circuit Breaker Wrapper

```go
// internal/resilience/http.go
type HTTPClient struct {
    client  *http.Client
    cb      *CircuitBreaker
    retries RetryConfig
}

// Do executes an HTTP request with circuit breaker and retry.
func (c *HTTPClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
    if !c.cb.Allow() {
        return nil, ErrCircuitOpen
    }

    var lastErr error
    for attempt := 0; attempt < c.retries.MaxAttempts; attempt++ {
        resp, err := c.client.Do(req)
        if err != nil {
            c.cb.RecordFailure()
            lastErr = err
            // Backoff sleep
            sleep(ctx, c.retries.BaseDelay*time.Duration(1<<attempt))
            continue
        }

        if resp.StatusCode >= 500 || resp.StatusCode == 429 {
            resp.Body.Close()
            c.cb.RecordFailure()
            lastErr = fmt.Errorf("retryable: %d", resp.StatusCode)
            sleep(ctx, c.retries.BaseDelay*time.Duration(1<<attempt))
            continue
        }

        c.cb.RecordSuccess()
        return resp, nil
    }
    return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

### GPSI Hashing in Logging

```go
// internal/logging/gpsi.go
import (
    "crypto/sha256"
    "encoding/base64"
)

func HashGPSI(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return base64.RawURLEncoding.EncodeToString(h[:8])
}

// Usage in handlers:
slog.Info("session_created",
    "auth_ctx_id", authCtxID,
    "gpsi_hash", logging.HashGPSI(gpsi),  // NEVER log raw gpsi
    "snssai_sst", snssai.Sst,
    "snssai_sd", snssai.Sd,
)
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Custom JSON logging | `slog.NewJSONHandler` | Go 1.21 (Nov 2023) | Zero allocation, structured, trace context built-in |
| Custom metrics with atomic counters | `prometheus/client_golang` | Industry standard | ServiceMonitor CRD compatibility, Grafana dashboards |
| No distributed tracing | OTel with W3C TraceContext | Industry standard | Cross-component trace visibility |
| In-memory circuit breaker | `resilience/` package | Phase 4 | Exposed Prometheus metrics per breaker |
| In-memory session store | PostgreSQL `NewSessionStore` | Phase 4 | Persistence across pod restarts |
| PostgreSQL DLQ | Redis list DLQ | Phase 4 | Lower enqueue latency, simpler code |

**Deprecated/outdated:**
- `go.uber.org/zap` — Replaced by stdlib `slog` for JSON logging (Go 1.21+)
- Custom Prometheus exposition — Use `promhttp.Handler()` instead of manual `/metrics` handler
- In-memory circuit breaker with no metrics — `resilience/` package with Prometheus gauges

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | NRF uses OAuth2 client credentials flow for token issuance | NRF Client | If NRF uses a different auth scheme, token handling needs redesign |
| A2 | AMF provides absolute `reauthNotifUri`/`revocNotifUri` in `SliceAuthInfo` | AMF Client | If URIs are relative or require AMF discovery first, AMF discovery logic needed |
| A3 | AUSF N60 interface is HTTP-based (like N58/N59) | AUSF Client | If N60 uses a different protocol, AUSF client architecture changes |
| A4 | DLQ processor runs as a goroutine in the Biz Pod | DLQ | If DLQ needs a separate service, Redis stream pattern (not list) is better |
| A5 | OTel OTLP exporter not needed for Phase 4 dev/test | OTel | Phase 7 (K8s) would add OTLP collector; stdouttrace sufficient now |

---

## Open Questions

1. **AUSF N60 Interface Details**
   - What we know: AIW handler exists, `WithAUSFClient` needed
   - What's unclear: Exact API path and request/response format for MSK forwarding
   - Recommendation: Implement as stub returning `nil` error; wire the option function; fill in after TS 29.526 §7.3 is consulted for N60 API spec

2. **OTel Exporter Selection**
   - What we know: stdouttrace works for local dev
   - What's unclear: Which backend (Jaeger, Tempo, Honeycomb) for K8s environment
   - Recommendation: Use OTLP exporter with env var `OTEL_EXPORTER_OTLP_ENDPOINT`; default to stdout for dev

3. **NRF Token Validation**
   - What we know: NRF issues tokens via `/oauth2/token`
   - What's unclear: Does NSSAAF need to validate incoming AMF/AUSF tokens, or does HTTP GW handle it?
   - Recommendation: HTTP GW handles TLS termination + token validation (Phase 5); Biz Pod trusts internal calls

4. **DLQ Monitoring**
   - What we know: `DLQDepth` Prometheus gauge exists
   - What's unclear: Alert threshold for DLQ depth
   - Recommendation: `nssAAF_dlq_depth > 100` → CRITICAL alert

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All | ✓ | 1.25.0 | — |
| PostgreSQL | Session store | — | — | Skip PG wiring, use in-memory store |
| Redis | DLQ, rate limiting, session cache | — | — | Skip DLQ features, use in-memory |
| NRF mock | NRF client testing | — | — | Use NRF client in degraded mode |
| Prometheus | Metrics scraping | — | — | Metrics endpoint still works, no scraping |

**Missing dependencies with no fallback:**
- PostgreSQL: Session store is a core Phase 4 deliverable. Without it, REQ-09 cannot be completed. Recommend: Use Docker Compose with `postgres:16-alpine` for dev.
- Redis: DLQ and rate limiting depend on Redis. Without it, REQ-10 (DLQ) and rate limiting cannot be completed. Recommend: Use Docker Compose with `redis:7-alpine` for dev.

**Missing dependencies with fallback:**
- NRF mock: Use NRF client in degraded mode (D-04). Biz Pod starts, NRF registration retried in background.
- Prometheus: Endpoint works without Prometheus running. Scraping configured in Phase 7 when K8s manifests are created.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` package (stdlib) |
| Config file | `configs/biz.yaml` (create as part of phase) |
| Quick run command | `go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... -v` |
| Full suite command | `go test ./... -count=1` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| REQ-01 | NRF registration on startup | Unit | `go test ./internal/nrf/... -run TestRegister` | ✗ Wave 0 |
| REQ-02 | NRF heartbeat every 5 min | Unit | `go test ./internal/nrf/... -run TestHeartbeat` | ✗ Wave 0 |
| REQ-03 | AMF discovery returns AMF profile | Unit | `go test ./internal/nrf/... -run TestDiscoverAMF` | ✗ Wave 0 |
| REQ-04 | UDM client calls Nudm_UECM_Get | Unit | `go test ./internal/udm/... -run TestGetAuthContext` | ✗ Wave 0 |
| REQ-05 | UDM UpdateAuthContext called | Unit | `go test ./internal/udm/... -run TestUpdateAuthContext` | ✗ Wave 0 |
| REQ-06 | AMF Re-Auth notification with retry | Unit | `go test ./internal/amf/... -run TestReAuthNotification` | ✗ Wave 0 |
| REQ-07 | AMF Revocation notification with retry | Unit | `go test ./internal/amf/... -run TestRevocNotification` | ✗ Wave 0 |
| REQ-08 | AUSF client created with config | Unit | `go test ./internal/ausf/... -run TestNewClient` | ✗ Wave 0 |
| REQ-09 | PostgreSQL NewSessionStore implements AuthCtxStore | Unit | `go test ./internal/storage/postgres/... -run TestSessionStore` | ✗ Wave 0 |
| REQ-10 | DLQ enqueue on retry exhaustion | Unit | `go test ./internal/cache/redis/... -run TestDLQEnqueue` | ✗ Wave 0 |
| REQ-11 | Circuit breaker state transitions | Unit | `go test ./internal/resilience/... -run TestCircuitBreaker` | ✗ Wave 0 |
| REQ-12 | Retry with backoff (1s,2s,4s) | Unit | `go test ./internal/resilience/... -run TestRetry` | ✗ Wave 0 |
| REQ-13 | Timeouts on HTTP calls | Unit | `go test ./internal/nrf/... -run TestTimeouts` | ✗ Wave 0 |
| REQ-14 | Prometheus metrics registered and exposed | Integration | `curl :8080/metrics` + grep | ✗ Wave 0 |
| REQ-15 | ServiceMonitor CRDs valid YAML | Manual | `kubectl apply --dry-run` | ✗ Wave 0 |
| REQ-16 | GPSI hashed in structured logs | Unit | `go test ./internal/logging/... -run TestGPSIHash` | ✗ Wave 0 |
| REQ-17 | OTel trace spans created | Integration | Check Jaeger/Tempo | ✗ Wave 0 |
| REQ-18 | /healthz/live returns 200 | Integration | `curl :8080/healthz/live` | ✗ Wave 0 |
| REQ-19 | /healthz/ready checks dependencies | Integration | `curl :8080/healthz/ready` | ✗ Wave 0 |

### Wave 0 Gaps
- [ ] `internal/nrf/client_test.go` — covers REQ-01, REQ-02, REQ-03
- [ ] `internal/udm/client_test.go` — covers REQ-04, REQ-05
- [ ] `internal/amf/notifier_test.go` — covers REQ-06, REQ-07
- [ ] `internal/ausf/client_test.go` — covers REQ-08
- [ ] `internal/storage/postgres/session_store_test.go` — covers REQ-09
- [ ] `internal/cache/redis/dlq_test.go` — covers REQ-10
- [ ] `internal/resilience/circuit_breaker_test.go` — covers REQ-11
- [ ] `internal/resilience/retry_test.go` — covers REQ-12
- [ ] `internal/logging/gpsi_test.go` — covers REQ-16
- [ ] `configs/biz.yaml` — test fixture config
- [ ] `go.mod` update: `go get prometheus/client_golang@v1.20.5 otel@v1.32.0 ...`

*(If no gaps: "None — existing test infrastructure covers all phase requirements")*

### Sampling Rate
- **Per task commit:** `go test ./internal/<module>/... -x -count=1`
- **Per wave merge:** `go test ./internal/nrf/... ./internal/udm/... ./internal/resilience/... -count=1`
- **Phase gate:** `go test ./... -count=1 && go build ./cmd/...` green before `/gsd-verify-work`

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | OAuth2 JWT validation for SBI (Phase 5 — deferred) |
| V3 Session Management | yes | `EAPSessionState` encrypted with AES-256-GCM (Phase 4) |
| V4 Access Control | yes | GPSI hash in logs — prevents PII leakage |
| V5 Input Validation | yes | GPSI regex (`^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`), SUPI regex, Snssai validation (already in `internal/api/common/`) |
| V6 Cryptography | yes | AES-256-GCM for session state (Phase 4); KEK/DEK envelope encryption (Phase 5) |

### Known Threat Patterns for Go HTTP Clients

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SSRF via AMF notification URI | Tampering, Information Disclosure | Validate URI is HTTPS and matches expected AMF domain prefix; reject if scheme is `http://` (allow only in dev) |
| Token injection via GPSI | Injection | GPSI validated with regex before any DB/log operation |
| NRF cache poisoning | Tampering | NRF cache TTL of 5 min; signed responses (if NRF supports) |
| DLQ replay attack | Repudiation | DLQ items include `createdAt` timestamp; discard items older than 24h |
| Circuit breaker bypass | Denial of Service | Circuit breaker state is in-memory per Biz Pod; if all Biz Pods fail simultaneously, the system is already degraded |

---

## Sources

### Primary (HIGH confidence)
- `docs/design/05_nf_profile.md` — NRF registration payload, discovery queries, cache TTL, token caching
- `docs/design/19_observability.md` §2.1 — Exact Prometheus metric names, labels, bucket values
- `docs/design/19_observability.md` §4.1-4.2 — OTel SDK setup, W3C TraceContext propagation
- `docs/design/10_ha_architecture.md` §8 — Health manager pattern, circuit breaker state machine
- `cmd/biz/main.go` — Existing wiring pattern, in-memory stores, middleware stack
- `internal/api/nssaa/handler.go` — Existing option function pattern, `AuthCtxStore` interface
- `internal/api/aiw/handler.go` — Existing option function pattern
- `internal/config/config.go` — Existing config struct pattern, `NRFConfig`, `UDMConfig`
- `internal/storage/postgres/session.go` — Existing `Repository` with `NewRepository` factory
- `internal/cache/redis/lock.go` — Existing Redis patterns (SETNX, LPUSH)

### Secondary (MEDIUM confidence)
- TS 29.510 §6 — NRF NF Profile registration fields (via design doc reference)
- TS 29.526 §7.2-7.3 — N58/N60 API structure (via design doc reference)
- TS 23.502 §4.2.9.2-4 — AMF re-auth/revocation notification triggers (via design doc reference)

### Tertiary (LOW confidence)
- OTel Go SDK v1.32.0 exact API surface — needs verification against `go.opentelemetry.io/otel@v1.32.0` docs
- Prometheus client_golang v1.20.5 exact API surface — needs verification against `github.com/prometheus/client_golang@v1.20.5` README

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — existing packages with verified versions; Go 1.25 stdlib `slog` eliminates logging dependency
- Architecture: HIGH — option function pattern already established in codebase; OTel pattern standard across 5G implementations
- Pitfalls: MEDIUM — GPSI hashing and DLQ cardinality are domain-specific; standard patterns apply

**Research date:** 2026-04-25
**Valid until:** 2026-05-25 (30 days — OTel and Prometheus APIs are stable; Go 1.25 is current)
