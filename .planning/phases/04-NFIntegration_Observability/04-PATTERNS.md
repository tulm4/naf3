# Phase 4: NF Integration & Observability — Pattern Map

**Generated:** 2026-04-25
**Phase:** 04-NFIntegration_Observability
**Purpose:** Extract concrete code patterns from existing files to guide implementation of Phase 4 new/modified files.

---

## 1. NF Client Package Pattern

### Closest Analog
`internal/storage/postgres/pool.go` (lines 1–109) and `internal/cache/redis/pool.go` (lines 1–93)

### Pattern Summary
Every NF client package (`nrf`, `udm`, `amf`, `ausf`) follows the same structure:
- A `Config` struct matching the YAML config field
- A `NewPool`/`NewClient` factory that returns a `*Pool`/`*Client`
- Methods on the struct for each API operation
- Private `httpClient *http.Client` field for HTTP calls

### Code Excerpt — Config + Factory

```go
// internal/storage/postgres/pool.go:14-27
type Config struct {
    DSN               string
    MaxConns          int32
    MinConns          int32
    MaxConnLifetime   time.Duration
    MaxConnIdleTime   time.Duration
    HealthCheckPeriod time.Duration
}

type Pool struct {
    pool *pgxpool.Pool
}

// internal/cache/redis/pool.go:25-28
type Pool struct {
    client redis.Cmdable
}

// NewPool creates a new single-node Redis client.
func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
    opt := &redis.Options{...}
    client := redis.NewClient(opt)
    if err := client.Ping(ctx).Err(); err != nil {
        client.Close()
        return nil, fmt.Errorf("redis: ping failed: %w", err)
    }
    return &Pool{client: client}, nil
}
```

### Code Excerpt — HTTP Client Factory

```go
// Pattern for NF clients: wrap with OTel and resilience
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

func NewClient(cfg config.NRFConfig, cb *resilience.CircuitBreaker) *Client {
    httpClient := &http.Client{
        Transport: otelhttp.NewTransport(http.DefaultTransport),
        Timeout:   cfg.DiscoverTimeout,
    }
    return &Client{
        baseURL:    cfg.BaseURL,
        httpClient: httpClient,
        cb:         cb,
    }
}
```

### Where to Apply
- `internal/nrf/client.go` — register, heartbeat, deregister, discover methods
- `internal/udm/client.go` — GetAuthContext, UpdateAuthContext
- `internal/amf/notifier.go` — SendReAuthNotification, SendRevocationNotification
- `internal/ausf/client.go` — ForwardMSK (new package)

---

## 2. Option Function Pattern for Handlers

### Closest Analog
`internal/api/nssaa/handler.go` (lines 91–117), `internal/api/aiw/handler.go` (lines 91–118), `internal/aaa/router.go` (lines 102–131)

### Pattern Summary
Handler structs are configured via `HandlerOption func(*Handler)` functions, applied variadically at construction time via `NewHandler(store, opts...)`. This pattern allows optional dependencies without breaking existing callers.

### Code Excerpt

```go
// internal/api/nssaa/handler.go:88-117
type Handler struct {
    store   AuthCtxStore
    aaa     AAARouter
    apiRoot string
}

type HandlerOption func(*Handler)

func WithAAA(aaa AAARouter) HandlerOption {
    return func(h *Handler) { h.aaa = aaa }
}

func WithAPIRoot(apiRoot string) HandlerOption {
    return func(h *Handler) { h.apiRoot = apiRoot }
}

func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler {
    h := &Handler{store: store}
    for _, opt := range opts {
        opt(h)
    }
    return h
}
```

### Where to Apply
- `internal/api/nssaa/handler.go`: add `WithNRFClient(*nrf.Client)` and `WithUDMClient(*udm.Client)` — adds `nrfClient *nrf.Client` and `udmClient *udm.Client` fields to `Handler` struct
- `internal/api/aiw/handler.go`: add `WithAUSFClient(*ausf.Client)` — adds `ausfClient *ausf.Client` field to `Handler` struct
- Both use existing `HandlerOption` pattern, no new files needed

---

## 3. Config Struct Pattern

### Closest Analog
`internal/config/config.go` (lines 1–331)

### Pattern Summary
Each NF gets a `Config` struct with YAML field tags, added to the top-level `Config` struct. Defaults are applied in `applyDefaults()`. Validation happens in `Validate()`.

### Code Excerpt

```go
// internal/config/config.go:33-36 (NRF/UDM already exist)
type Config struct {
    ...
    NRF       NRFConfig     `yaml:"nrf"`
    UDM       UDMConfig     `yaml:"udm"`
    // NEW: AUSF AUSFConfig `yaml:"ausf"`  ← add here
}

// internal/config/config.go:152-162 (NRF/UDM configs)
type NRFConfig struct {
    BaseURL         string        `yaml:"baseURL"`
    DiscoverTimeout time.Duration `yaml:"discoverTimeout"`
}

type UDMConfig struct {
    BaseURL string        `yaml:"baseURL"`
    Timeout time.Duration `yaml:"timeout"`
}

// internal/config/config.go:248-331 (defaults)
func applyDefaults(cfg *Config) {
    if cfg.NRF.DiscoverTimeout == 0 {
        cfg.NRF.DiscoverTimeout = 5 * time.Second
    }
    if cfg.UDM.Timeout == 0 {
        cfg.UDM.Timeout = 10 * time.Second
    }
    // NEW: add AUSF defaults here
}
```

### Where to Apply
- `internal/config/config.go`: add `AUSFConfig` struct and `AUSF` field to `Config`
- Add `applyDefaults` for `AUSFConfig.Timeout` (default 10s)
- `DatabaseConfig` already exists but needs `EncryptionKey string` field for session encryption

---

## 4. PostgreSQL Repository / Store Wrapper Pattern

### Closest Analog
`internal/storage/postgres/session.go` (lines 1–375) and `internal/storage/postgres/pool.go` (lines 1–109)

### Pattern Summary
The `Repository` struct wraps a `*Pool` and provides typed CRUD methods. Phase 4 wraps `Repository` into `Store` types that implement the `AuthCtxStore` interface from handler packages.

### Code Excerpt — Repository with Encryption

```go
// internal/storage/postgres/session.go:50-120
type Encryptor struct {
    key []byte
}

func NewEncryptor(key []byte) (*Encryptor, error) {
    if len(key) != 16 && len(key) != 24 && len(key) != 32 {
        return nil, errors.New("encryptor: key must be 16, 24, or 32 bytes")
    }
    return &Encryptor{key: key}, nil
}

type Repository struct {
    pool      *Pool
    encryptor *Encryptor
}

func NewRepository(pool *Pool, encryptor *Encryptor) *Repository {
    return &Repository{pool: pool, encryptor: encryptor}
}
```

### Code Excerpt — Store Wrapper (target pattern)

```go
// Target: internal/storage/postgres/session_store.go
// Wraps Repository with nssaa.AuthCtxStore interface

type Store struct {
    repo *Repository
}

func NewSessionStore(pool *Pool, encryptor *Encryptor) *Store {
    return &Store{repo: NewRepository(pool, encryptor)}
}

func (s *Store) Load(id string) (*nssaa.AuthCtx, error) {
    session, err := s.repo.GetByAuthCtxID(context.Background(), id)
    if err != nil {
        if errors.Is(err, ErrSessionNotFound) {
            return nil, nssaa.ErrNotFound
        }
        return nil, err
    }
    return sessionToAuthCtx(session), nil
}

// AIW store follows same pattern with aiw.AuthCtxStore interface
type AIWStore struct {
    repo *Repository
}

func NewAIWSessionStore(pool *Pool, encryptor *Encryptor) *AIWStore {
    return &AIWStore{repo: NewRepository(pool, encryptor)}
}
```

### Where to Apply
- `internal/storage/postgres/session_store.go` (new file): implement `NewSessionStore` and `NewAIWSessionStore`
- Both stores implement existing `nssaa.AuthCtxStore` and `aiw.AuthCtxStore` interfaces
- `Close()` method is a no-op (pool.Close handled by `main.go` shutdown)

---

## 5. Redis Client / DLQ Pattern

### Closest Analog
`internal/cache/redis/pool.go` (lines 1–93) and `internal/cache/redis/lock.go` (lines 1–131)

### Pattern Summary
Redis operations use the existing `Pool` struct with `redis.Cmdable` interface. DLQ uses Redis LIST operations (LPUSH/BRPOP). Key prefix follows `nssAAF:` namespace.

### Code Excerpt — Pool + Key Pattern

```go
// internal/cache/redis/lock.go:27-30
func lockKey(resource string) string {
    return fmt.Sprintf("nssaa:lock:session:%s", resource)
}

// internal/cache/redis/pool.go:80-82
func (p *Pool) Client() redis.Cmdable {
    return p.client
}
```

### Code Excerpt — DLQ Pattern (target)

```go
// Target: internal/cache/redis/dlq.go (new file)
const dlqKey = "nssAAF:dlq:amf-notifications"

type DLQItem struct {
    ID        string    `json:"id"`
    Type      string    `json:"type"` // "reauth" | "revocation"
    URI       string    `json:"uri"`
    Payload   []byte    `json:"payload"`
    AuthCtxID string    `json:"authCtxId"`
    Attempt   int       `json:"attempt"`
    MaxAttempts int     `json:"maxAttempts"`
    CreatedAt time.Time `json:"createdAt"`
    LastError string    `json:"lastError"`
}

type DLQ struct {
    pool *redis.Pool
}

func NewDLQ(pool *redis.Pool) *DLQ {
    return &DLQ{pool: pool}
}

func (d *DLQ) Enqueue(ctx context.Context, item *DLQItem) error {
    data, err := json.Marshal(item)
    if err != nil {
        return fmt.Errorf("dlq marshal: %w", err)
    }
    return d.pool.Client().LPush(ctx, dlqKey, data).Err()
}

func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*DLQItem, error) {
    result, err := d.pool.Client().BRPop(ctx, timeout, dlqKey).Result()
    if err != nil {
        return nil, err
    }
    var item DLQItem
    if err := json.Unmarshal([]byte(result[1]), &item); err != nil {
        return nil, fmt.Errorf("dlq unmarshal: %w", err)
    }
    return &item, nil
}
```

### Where to Apply
- `internal/cache/redis/dlq.go` (new file): DLQ enqueue/dequeue using Redis LPUSH/BRPOP
- Key format: `nssAAF:dlq:amf-notifications`
- DLQ processor runs as a goroutine in `main.go`, retries every 5 minutes
- After 3 DLQ retries: log at ERROR level with full context, discard item

---

## 6. Prometheus Metrics Pattern

### Closest Analog
`internal/aaa/metrics.go` (lines 1–126) and `docs/design/19_observability.md` §2.1

### Pattern Summary
Metrics use `sync/atomic` for counters/gauges (not Prometheus library directly for in-process collection). For Prometheus exposition, use `prometheus.NewCounterVec` + `prometheus.MustRegister`. All metrics go in a single `internal/metrics/metrics.go` file with a `Register()` function.

### Code Excerpt — In-Process Metrics (existing)

```go
// internal/aaa/metrics.go:12-29
type Metrics struct {
    requests  map[string]map[string]map[string]int64
    latencies map[string]map[string]*atomic.Int64
    counters  map[string]map[string]*atomic.Int64
    mu        sync.RWMutex
}

func (m *Metrics) RecordAAARequest(protocol, host, result string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.requests[protocol] == nil {
        m.requests[protocol] = make(map[string]map[string]int64)
    }
    ...
}
```

### Code Excerpt — Prometheus Pattern (target)

```go
// Target: internal/metrics/metrics.go (new file)
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    EapSessionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "nssAAF_eap_sessions_active",
        Help: "Number of active EAP sessions",
    })

    EapSessionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "nssAAF_eap_sessions_total",
        Help: "Total EAP sessions by result",
    }, []string{"result"})

    RequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "nssAAF_request_duration_seconds",
        Help:    "HTTP request latency",
        Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
    }, []string{"service", "endpoint", "method"})

    CircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "nssAAF_circuit_breaker_state",
        Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
    }, []string{"server"})

    DLQDepth = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "nssAAF_dlq_depth",
        Help: "Number of items in AMF notification DLQ",
    })
)

func Register() {
    prometheus.MustRegister(
        EapSessionsActive,
        EapSessionsTotal,
        RequestDuration,
        CircuitBreakerState,
        DLQDepth,
    )
}
```

### Where to Apply
- `internal/metrics/metrics.go` (new package): all `prometheus.MustRegister` calls in one `Register()` function
- Called once from `main.go` during startup
- `/metrics` endpoint registered via `mux.Handle("/metrics", promhttp.Handler())` (stdlib via `github.com/prometheus/client_golang/prometheus/promhttp`)

---

## 7. Circuit Breaker Pattern

### Closest Analog
`internal/aaa/router.go` (lines 90–100) for mutex discipline; no existing circuit breaker implementation in the codebase

### Pattern Summary
Circuit breaker state machine: CLOSED → OPEN (after `failureThreshold` failures) → HALF_OPEN (after `recoveryTimeout`) → CLOSED (after `successThreshold` successes) or OPEN (on any failure). State is protected by `sync.Mutex`. Metrics exposed via Prometheus gauge.

### Code Excerpt — State Machine (target)

```go
// Target: internal/resilience/circuit_breaker.go (new file)
type State int // 0=CLOSED, 1=OPEN, 2=HALF_OPEN

type CircuitBreaker struct {
    mu sync.Mutex
    state           State
    failures        int
    successes       int
    lastFailure     time.Time
    openedAt        time.Time
    failureThreshold int
    recoveryTimeout  time.Duration
    successThreshold int // default 3
}

func (cb *CircuitBreaker) Allow() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    switch cb.state {
    case CLOSED:
        return true
    case OPEN:
        if time.Since(cb.openedAt) >= cb.recoveryTimeout {
            cb.state = HALF_OPEN
            cb.successes = 0
            return true
        }
        return false
    case HALF_OPEN:
        return true
    }
    return false
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    switch cb.state {
    case HALF_OPEN:
        cb.successes++
        if cb.successes >= cb.successThreshold {
            cb.state = CLOSED
            cb.failures = 0
        }
    case CLOSED:
        cb.failures = 0
    }
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    cb.lastFailure = time.Now()
    cb.failures++

    switch cb.state {
    case CLOSED:
        if cb.failures >= cb.failureThreshold {
            cb.state = OPEN
            cb.openedAt = time.Now()
        }
    case HALF_OPEN:
        cb.state = OPEN
        cb.openedAt = time.Now()
    }
}

// Registry manages named circuit breakers keyed by "host:port"
type Registry struct {
    mu       sync.RWMutex
    breakers map[string]*CircuitBreaker
    defaultFailureThreshold int
    defaultRecoveryTimeout  time.Duration
}

func NewRegistry(failureThreshold int, recoveryTimeout time.Duration) *Registry {
    return &Registry{
        breakers:              make(map[string]*CircuitBreaker),
        defaultFailureThreshold: failureThreshold,
        defaultRecoveryTimeout:  recoveryTimeout,
    }
}

func (r *Registry) Get(key string) *CircuitBreaker {
    r.mu.RLock()
    cb, ok := r.breakers[key]
    r.mu.RUnlock()
    if ok {
        return cb
    }

    r.mu.Lock()
    defer r.mu.Unlock()
    if cb, ok := r.breakers[key]; ok {
        return cb
    }
    cb = &CircuitBreaker{
        failureThreshold: r.defaultFailureThreshold,
        recoveryTimeout:  r.defaultRecoveryTimeout,
        successThreshold: 3,
        state:            CLOSED,
    }
    r.breakers[key] = cb
    return cb
}
```

### Where to Apply
- `internal/resilience/circuit_breaker.go` (new file)
- Wire defaults from `AAAConfig.FailureThreshold` (5) and `AAAConfig.RecoveryTimeout` (30s)
- Key format: `"host:port"` matching `AAAConfig` scope

---

## 8. Retry with Exponential Backoff Pattern

### Closest Analog
`internal/cache/redis/lock.go` (lines 49–74) for retry loop with context cancellation

### Pattern Summary
Retry loop with exponential backoff: delays of 1s, 2s, 4s. Context cancellation respected during sleep. Last attempt does not sleep before returning error. Retryable errors: 5xx and 429.

### Code Excerpt

```go
// internal/cache/redis/lock.go:49-74
func (l *DistributedLock) TryLock(ctx context.Context, resource string, timeout time.Duration) (string, error) {
    deadline := time.Now().Add(timeout)
    ticker := time.NewTicker(50 * time.Millisecond)
    defer ticker.Stop()

    for {
        token, err := l.Lock(ctx, resource)
        if err != nil {
            return "", err
        }
        if token != "" {
            return token, nil
        }

        if time.Now().After(deadline) {
            return "", nil
        }

        select {
        case <-ctx.Done():
            return "", ctx.Err()
        case <-ticker.C:
        }
    }
}

// Target: internal/resilience/retry.go (new file)
type RetryConfig struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
}

var ErrMaxRetriesExceeded = errors.New("max retries exceeded")

func Do(ctx context.Context, cfg RetryConfig, fn func() error) error {
    var lastErr error
    for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
        if err := fn(); err == nil {
            return nil
        } else {
            lastErr = err
        }

        if attempt < cfg.MaxAttempts-1 {
            delay := cfg.BaseDelay * time.Duration(1<<attempt)
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
    return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
}
```

### Where to Apply
- `internal/resilience/retry.go` (new file)
- Default config: `MaxAttempts=3`, `BaseDelay=1s`, `MaxDelay=4s`
- Used by `internal/amf/notifier.go` for AMF notification retries (1s, 2s, 4s)

---

## 9. Structured Logging Pattern

### Closest Analog
`cmd/biz/main.go` (lines 32–50), `internal/cache/redis/lock.go` (lines 186–192), `internal/api/common/middleware.go` (lines 32–50)

### Pattern Summary
All logging uses `slog` with `slog.NewJSONHandler` for structured JSON output. Never log raw GPSI — always use hash. Always include `request_id` and `auth_ctx_id` for correlation.

### Code Excerpt

```go
// cmd/biz/main.go:32-50
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(logger)

slog.Info("starting NSSAAF Biz Pod",
    "pod_id", podID,
    "version", cfg.Version,
    "use_mtls", cfg.Biz.UseMTLS,
)

// internal/cache/redis/lock.go:186-192
r.logger.Debug("aaa_route",
    "auth_ctx_id", authCtxID,
    "protocol", decision.Protocol,
    "host", decision.Host,
    "mode", decision.Mode,
)

// internal/api/common/middleware.go:42-49
slog.Log(r.Context(), slog.LevelInfo, "http request",
    "method", r.Method,
    "path", r.URL.Path,
    "status", wrapped.statusCode,
    "duration_ms", time.Since(start).Milliseconds(),
    "request_id", reqID,
    "client_ip", r.RemoteAddr,
)
```

### Code Excerpt — GPSI Hash (target)

```go
// Target: internal/logging/gpsi.go (new file)
package logging

import (
    "crypto/sha256"
    "encoding/base64"
)

func HashGPSI(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return base64.RawURLEncoding.EncodeToString(h[:8])
}

// Usage:
slog.Info("session_created",
    "auth_ctx_id", authCtxID,
    "gpsi_hash", logging.HashGPSI(gpsi), // NEVER log raw gpsi
    "snssai_sst", snssai.Sst,
    "snssai_sd", snssai.Sd,
)
```

### Where to Apply
- `internal/logging/gpsi.go` (new file): `HashGPSI(gpsi string) string` — SHA256, first 8 bytes, base64url
- `internal/logging/logger.go` (new file): helper with trace context injection

---

## 10. OpenTelemetry Tracing Pattern

### Closest Analog
`internal/api/common/middleware.go` (lines 18–28) for context propagation pattern; no existing OTel implementation in the codebase

### Pattern Summary
OTel initialized once in `main.go`. Trace context extracted from incoming HTTP headers (W3C TraceContext). Spans created per operation with relevant attributes. `otelhttp.NewTransport` auto-instruments HTTP clients.

### Code Excerpt — Initialization (target)

```go
// Target: internal/tracing/tracing.go (new file)
package tracing

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/trace"
    sdktrace "go.opentelemetry.io/sdk/trace"
    "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
    "go.opentelemetry.io/otel/attribute"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func Init(serviceName, version, podID string) (shutdown func()) {
    // W3C TraceContext propagator
    propagator := propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    )
    otel.SetTextMapPropagator(propagator)

    // stdout exporter for dev; swap for OTLP in prod
    exporter, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion(version),
            attribute.String("pod.name", podID),
        )),
    )
    otel.SetTracerProvider(tp)

    return func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        tp.Shutdown(ctx)
    }
}

// Wrapped HTTP transport for automatic span creation
func HTTPTransport() *otelhttp.Transport {
    return otelhttp.NewTransport(http.DefaultTransport)
}
```

### Code Excerpt — Span Creation in Handler (target)

```go
// In handler: extract trace context from incoming request
ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

// Start operation span
ctx, span := tracer.Start(ctx, "Nnssaaf_NSSAA.Authenticate",
    trace.WithAttributes(
        attribute.String("gpsi_hash", logging.HashGPSI(req.Gpsi)),
        attribute.Int("snssai_sst", int(req.Snssai.Sst)),
        attribute.String("auth_ctx_id", authCtxID),
    ),
)
defer span.End()

// Sub-span for downstream calls
ctx, udmSpan := tracer.Start(ctx, "udm.GetAuthContext",
    trace.WithAttributes(attribute.String("supi", supi)),
)
udmSpan.End()
```

### Where to Apply
- `internal/tracing/tracing.go` (new file): `Init()` function called from `main.go`
- NF client constructors use `otelhttp.NewTransport` for automatic HTTP instrumentation
- `cmd/biz/main.go`: call `tracing.Init()` during startup, call shutdown on graceful shutdown

---

## 11. Health Endpoint Pattern

### Closest Analog
`cmd/biz/main.go` (lines 257–267)

### Pattern Summary
Two health endpoints: `/healthz/live` (always 200, no dependency checks) and `/healthz/ready` (checks PostgreSQL, Redis, AAA Gateway, NRF registration). Returns JSON with check results.

### Code Excerpt — Current (to be replaced)

```go
// cmd/biz/main.go:257-267 (current — rename endpoints)
func handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

func handleReady(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ready","service":"nssAAF-biz"}`)
}
```

### Code Excerpt — Target (D-07)

```go
// Target: update cmd/biz/main.go — rename routes
mux.HandleFunc("/healthz/live", handleLiveness)
mux.HandleFunc("/healthz/ready", handleReadiness)

func handleLiveness(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

func handleReadiness(w http.ResponseWriter, r *http.Request) {
    checks := map[string]string{}

    // PostgreSQL
    if err := pgPool.Ping(r.Context()); err != nil {
        checks["postgres"] = "unhealthy: " + err.Error()
    } else {
        checks["postgres"] = "ok"
    }

    // Redis
    if err := redisPool.Client().Ping(r.Context()).Err(); err != nil {
        checks["redis"] = "unhealthy: " + err.Error()
    } else {
        checks["redis"] = "ok"
    }

    // AAA Gateway
    resp, err := http.Get(cfg.Biz.AAAGatewayURL + "/health")
    if err != nil || resp.StatusCode != 200 {
        checks["aaa_gateway"] = "unhealthy"
    } else {
        checks["aaa_gateway"] = "ok"
    }

    // NRF registration (degraded, not unhealthy)
    if !nrfClient.IsRegistered() {
        checks["nrf_registration"] = "degraded (retrying)"
    } else {
        checks["nrf_registration"] = "ok"
    }

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

### Where to Apply
- `cmd/biz/main.go`: rename `/health` → `/healthz/live`, `/ready` → `/healthz/ready`
- Replace stub implementations with dependency checks above
- Pass `pgPool`, `redisPool`, `nrfClient` as closure variables

---

## 12. Main Wiring Pattern

### Closest Analog
`cmd/biz/main.go` (lines 1–278)

### Pattern Summary
`main.go` is the central wiring hub. It loads config, creates stores and clients, applies handler options, registers routes, applies middleware, and runs the HTTP server with graceful shutdown.

### Code Excerpt — Current Wiring

```go
// cmd/biz/main.go:58-91 (current — in-memory stores)
nssaaStore := nssaa.NewInMemoryStore()
aiwStore := aiw.NewInMemoryStore()

aaaClient := newHTTPAAAClient(...)
nssaaHandler := nssaa.NewHandler(nssaaStore,
    nssaa.WithAPIRoot(apiRoot),
    nssaa.WithAAA(aaaClient),
)
```

### Code Excerpt — Target Wiring (D-05, D-06)

```go
// Target: cmd/biz/main.go — Phase 4 wiring
// ─── PostgreSQL ────────────────────────────────────────────────────────────
pgPool, err := postgres.NewPool(ctx, postgres.Config{
    DSN:               cfg.Database.ConnString(),
    MaxConns:          int32(cfg.Database.MaxConns),
    MinConns:          int32(cfg.Database.MinConns),
    MaxConnLifetime:   cfg.Database.ConnMaxLifetime,
    MaxConnIdleTime:   10 * time.Minute,
    HealthCheckPeriod: 30 * time.Second,
})
if err != nil {
    slog.Error("postgres pool", "error", err)
    os.Exit(1)
}
defer pgPool.Close()

encryptor, err := postgres.NewEncryptor([]byte(cfg.Database.EncryptionKey))
if err != nil {
    slog.Error("encryptor", "error", err)
    os.Exit(1)
}

pgStore := postgres.NewSessionStore(pgPool, encryptor)
aiwStore := postgres.NewAIWSessionStore(pgPool, encryptor)

// ─── Redis ──────────────────────────────────────────────────────────────────
redisPool, err := redis.NewPool(ctx, redis.Config{
    Addrs:        []string{cfg.Redis.Addr},
    Password:     cfg.Redis.Password,
    DB:           cfg.Redis.DB,
    PoolSize:     cfg.Redis.PoolSize,
    MinIdleConns: 10,
    DialTimeout:  100 * time.Millisecond,
    ReadTimeout:  100 * time.Millisecond,
    WriteTimeout: 100 * time.Millisecond,
})
if err != nil {
    slog.Error("redis pool", "error", err)
    os.Exit(1)
}
defer redisPool.Close()

// ─── Resilience ──────────────────────────────────────────────────────────────
cbRegistry := resilience.NewRegistry(
    cfg.AAA.FailureThreshold,
    cfg.AAA.RecoveryTimeout,
)

// ─── NRF ─────────────────────────────────────────────────────────────────────
nrfClient := nrf.NewClient(cfg.NRF)
go nrfClient.RegisterAsync(ctx) // startup in degraded mode (D-04)

// ─── UDM ─────────────────────────────────────────────────────────────────────
udmClient := udm.NewClient(cfg.UDM, nrfClient)

// ─── AUSF ─────────────────────────────────────────────────────────────────────
ausfClient := ausf.NewClient(cfg.AUSF)

// ─── AMF Notifier ─────────────────────────────────────────────────────────────
dlq := redis.NewDLQ(redisPool)
amfClient := amf.NewClient(30*time.Second, cbRegistry, dlq)
go dlq.Process(ctx)

// ─── Handlers ────────────────────────────────────────────────────────────────
nssaaHandler := nssaa.NewHandler(pgStore,
    nssaa.WithAPIRoot(apiRoot),
    nssaa.WithAAA(aaaClient),
    nssaa.WithNRFClient(nrfClient),   // D-05
    nssaa.WithUDMClient(udmClient),   // D-05
)

aiwHandler := aiw.NewHandler(aiwStore,
    aiw.WithAPIRoot(apiRoot),
    aiw.WithAUSFClient(ausfClient),   // D-05
)
```

### Where to Apply
- `cmd/biz/main.go`: replace in-memory stores with PostgreSQL pool + session stores; add Redis pool + DLQ; add NRF/UDM/AUSF/AMF client construction and option injection
- Wire `pgPool`, `redisPool`, `nrfClient`, `udmClient`, `ausfClient` as closure variables for health handlers
- Call `metrics.Register()` once during startup

---

## 13. Error Handling Pattern

### Closest Analog
`internal/api/nssaa/handler.go` (lines 48–49, 139–168), `internal/storage/postgres/session.go` (lines 105–109)

### Pattern Summary
Sentinel errors defined at package level (`var ErrNotFound = errors.New(...)`). Wrapped errors use `fmt.Errorf("...: %w", err)`. HTTP handlers use `common.ProblemDetails` (RFC 7807) via `common.WriteProblem()`. Errors checked with `errors.Is()`.

### Code Excerpt

```go
// Sentinel errors
// internal/api/nssaa/handler.go:48-49
var ErrNotFound = errors.New("auth context not found")

// internal/storage/postgres/session.go:105-109
var ErrSessionNotFound = errors.New("postgres: session not found")
var ErrEncryptionFailed = errors.New("postgres: encryption failed")

// Error wrapping with context
// internal/storage/postgres/session.go:157-159
if err != nil {
    return fmt.Errorf("session create: %w", err)
}

// Error checking
// internal/api/nssaa/handler.go:274-279
if err != nil {
    if errors.Is(err, ErrNotFound) {
        common.WriteProblem(w, common.NotFoundProblem(...))
        return
    }
    common.WriteProblem(w, common.InternalServerProblem(...))
    return
}

// ProblemDetails usage
// internal/api/nssaa/handler.go:144-154
if err := common.ValidateGPSI(string(body.Gpsi)); err != nil {
    var pd *common.ProblemDetails
    if errors.As(err, &pd) {
        common.WriteProblem(w, pd)
    } else {
        common.WriteProblem(w, common.ValidationProblem("gpsi", err.Error()))
    }
    return
}
```

### Where to Apply
- All new files: define sentinel errors at package level
- All new files: wrap errors with `fmt.Errorf("prefix: %w", err)` pattern
- All new files: return `*common.ProblemDetails` for HTTP-handled errors; return plain errors for internal errors

---

## 14. Package Structure Conventions

### Established Conventions

```
// Package-level doc comment: one line, what the package provides
// Spec: <3GPP TS reference> (e.g. "Spec: TS 29.510 §6")
package <module>

// File naming: primary functionality (no suffix needed)
client.go   — HTTP client for an NF
handler.go  — HTTP request handlers
pool.go     — connection pool setup
store.go    — storage wrapper implementing an interface
metrics.go  — Prometheus metric definitions
tracing.go  — OTel initialization
dlq.go      — DLQ implementation
gpsi.go     — GPSI hashing utility

// Test files: *_test.go, one per source file
client_test.go
handler_test.go
dlq_test.go
```

---

## 15. Testing Pattern

### Closest Analog
`internal/cache/redis/cache_test.go`, `internal/api/common/common_test.go`

### Pattern Summary
Tests use Go's stdlib `testing` package. External dependencies mocked with interface-based approach. Table-driven tests for multiple test cases. Error cases explicit with named variables.

### Code Excerpt

```go
// internal/cache/redis/cache_test.go pattern
func TestCache_SetGet(t *testing.T) {
    ctx := context.Background()
    pool, teardown := newTestPool(t)
    defer teardown()

    cache := NewCache(pool, 5*time.Minute)
    defer cache.Close()

    if err := cache.Set(ctx, "key1", []byte("value1"), time.Minute); err != nil {
        t.Fatalf("Set failed: %v", err)
    }

    val, err := cache.Get(ctx, "key1")
    if err != nil {
        t.Fatalf("Get failed: %v", err)
    }
    if string(val) != "value1" {
        t.Errorf("expected value1, got %s", string(val))
    }
}

// Table-driven test pattern
func TestCircuitBreaker_Transitions(t *testing.T) {
    tests := []struct {
        name       string
        failures   int
        wantState  State
    }{
        {"below threshold", 4, CLOSED},
        {"at threshold", 5, OPEN},
        {"recovery timeout", 0, HALF_OPEN},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cb := NewCircuitBreaker(5, 30*time.Second, 3)
            for i := 0; i < tt.failures; i++ {
                cb.RecordFailure()
            }
            if got := cb.State(); got != tt.wantState {
                t.Errorf("State() = %v, want %v", got, tt.wantState)
            }
        })
    }
}
```

### Where to Apply
- `internal/nrf/client_test.go` — test registration, heartbeat, discovery with httptest
- `internal/udm/client_test.go` — test GetAuthContext, UpdateAuthContext
- `internal/amf/notifier_test.go` — test notification send with retry exhaustion
- `internal/ausf/client_test.go` — test MSK forwarding
- `internal/storage/postgres/session_store_test.go` — test Store.Load/Save/Delete
- `internal/cache/redis/dlq_test.go` — test DLQ enqueue/dequeue
- `internal/resilience/circuit_breaker_test.go` — test state transitions
- `internal/resilience/retry_test.go` — test exponential backoff
- `internal/logging/gpsi_test.go` — test GPSI hash consistency

---

## 16. Import Path Conventions

### Closest Analog
`cmd/biz/main.go` (lines 1–25), `internal/config/config.go` (lines 1–10)

### Pattern Summary
All internal packages use `github.com/operator/nssAAF/internal/...` import path. Third-party packages follow standard Go conventions. oapi-gen packages use `github.com/operator/nssAAF/oapi-gen/gen/...` with local `replace` directives in `go.mod`.

### Code Excerpt

```go
// go.mod:41-43
replace github.com/operator/nssAAF/oapi-gen/gen/nssaa => ./oapi-gen/gen/nssaa
replace github.com/operator/nssAAF/oapi-gen/gen/aiw => ./oapi-gen/gen/aiw
replace github.com/operator/nssAAF/oapi-gen/gen/specs => ./oapi-gen/gen/specs

// cmd/biz/main.go:19-24
import (
    "github.com/operator/nssAAF/internal/api/aiw"
    "github.com/operator/nssAAF/internal/api/common"
    "github.com/operator/nssAAF/internal/api/nssaa"
    "github.com/operator/nssAAF/internal/config"
    "github.com/operator/nssAAF/internal/proto"
    "github.com/redis/go-redis/v9"
)
```

### Where to Apply
- All new packages: use `github.com/operator/nssAAF/internal/<module>/` import path
- `go.mod`: add new dependencies with verified versions (`prometheus/client_golang v1.20.5`, OTel packages)
- Run `go mod tidy` after adding dependencies

---

## 17. Graceful Shutdown Pattern

### Closest Analog
`cmd/biz/main.go` (lines 129–142)

### Pattern Summary
Signal handling via `signal.Notify`. HTTP server shutdown via `srv.Shutdown(ctx)`. Background goroutines (heartbeat, DLQ processor, NRF registration) clean up via context cancellation.

### Code Excerpt

```go
// cmd/biz/main.go:129-142
go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

select {
case err := <-errCh:
    slog.Error("server error", "error", err)
    os.Exit(1)
case <-signalReceived():
    slog.Info("shutdown signal received")
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    srv.Shutdown(ctx)
    aaaClient.Close()
}

// cmd/biz/main.go:205-225
func podHeartbeat(ctx context.Context, redisAddr, podID string) {
    rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
    defer rdb.Close()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            rdb.SRem(ctx, proto.PodsKey, podID)
            return
        case <-ticker.C:
            rdb.SAdd(ctx, proto.PodsKey, podID)
        }
    }
}
```

### Where to Apply
- `cmd/biz/main.go`: pass cancellable context to `nrfClient.RegisterAsync`, `dlq.Process`, `amfClient`
- All goroutines respect context cancellation for clean shutdown

---

## 18. NRF Client Startup Pattern (Degraded Mode)

### Closest Analog
`cmd/biz/main.go` (lines 129–130) for background goroutine pattern; `internal/cache/redis/lock.go` (lines 51–74) for ticker loop

### Pattern Summary
NRF registration runs in a background goroutine. If registration fails at startup, the Biz Pod continues (degraded mode). Registration is retried with exponential backoff. `nrfClient.IsRegistered()` returns false until successful.

### Code Excerpt — Target

```go
// Target: internal/nrf/client.go
func (c *Client) RegisterAsync(ctx context.Context) {
    go func() {
        backoff := time.Second
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }

            if err := c.Register(ctx); err != nil {
                slog.Warn("nrf registration failed, retrying",
                    "error", err,
                    "backoff", backoff,
                )
                select {
                case <-ctx.Done():
                    return
                case <-time.After(backoff):
                }
                if backoff < 30*time.Second {
                    backoff *= 2
                }
                continue
            }

            slog.Info("nrf registration successful",
                "nf_instance_id", c.nfInstanceId,
            )
            c.registered.Store(true)
            return
        }
    }()
}

func (c *Client) IsRegistered() bool {
    return c.registered.Load().(bool)
}
```

### Where to Apply
- `internal/nrf/client.go`: `RegisterAsync(ctx context.Context)` goroutine in constructor or `NewClient`
- Atomic `registered` field (`sync/atomic`) for thread-safe registration status
- Called from `main.go` as `go nrfClient.RegisterAsync(ctx)`

---

## Established Conventions Summary

### Naming
| Element | Convention | Example |
|---------|------------|---------|
| HTTP client packages | `client.go` | `internal/nrf/client.go` |
| Test files | `*_test.go` | `internal/nrf/client_test.go` |
| Config structs | `*Config` suffix | `NRFConfig`, `AUSFConfig` |
| Handler option functions | `With*` prefix | `WithNRFClient`, `WithUDMClient` |
| Sentinel errors | `Err*` prefix | `ErrNotFound`, `ErrSessionNotFound` |
| Redis keys | `nssAAF:` prefix | `nssAAF:dlq:amf-notifications` |
| Circuit breaker keys | `host:port` | `aaa.operator.com:1812` |
| GPSI in logs | `gpsi_hash` key | `logging.HashGPSI(gpsi)` |

### Error Handling
- Sentinel errors: `var ErrNotFound = errors.New("...")` at package level
- Wrapped errors: `fmt.Errorf("prefix: %w", err)` — always wrap at boundary
- Error checking: `errors.Is(err, ErrNotFound)` for sentinel, `errors.As()` for typed
- HTTP handlers: return `*common.ProblemDetails` via `common.WriteProblem()`
- Never return bare `error` from HTTP handlers — always translate to ProblemDetails

### Testing
- Use stdlib `testing` package
- Mock external dependencies via interfaces (already used in codebase)
- Table-driven tests for multiple cases
- `newTestPool(t)` helper pattern for test setup
- Always `defer teardown()` for cleanup

### Package Structure
```
internal/<module>/
    client.go       — HTTP client (one per NF)
    handler.go      — HTTP handlers (for API packages)
    pool.go         — connection pool (for storage packages)
    metrics.go      — Prometheus metric definitions (single file)
    tracing.go      — OTel initialization
    dlq.go          — DLQ implementation
    gpsi.go         — GPSI hashing
    *_test.go       — one test file per source file
```

### go.mod
- Run `go mod tidy` after adding new dependencies
- Pin versions: `prometheus/client_golang v1.20.5`, `go.opentelemetry.io/otel@v1.32.0`
- No `//go:build` tags needed (single `linux/amd64` target for Phase 4)

### Middleware Order (from `main.go`)
```
1. RecoveryMiddleware   — panic recovery
2. RequestIDMiddleware — correlation ID
3. LoggingMiddleware   — structured request logs
4. CORSMiddleware      — CORS for OAM endpoints
```

### Prometheus Registration
- All metrics registered once via `prometheus.MustRegister` in `Register()` function
- Never call `MustRegister` inside request handlers — call once at startup
- Endpoint: `mux.Handle("/metrics", promhttp.Handler())`
