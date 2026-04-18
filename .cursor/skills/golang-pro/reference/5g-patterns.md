# 5G SBA-Specific Patterns

## EAP Engine Plugin Architecture

```go
// internal/domain/eap.go
type EAPMethodType string

const (
    EAPAKAPrime   EAPMethodType = "AKA_PRIME"
    EAPTLS        EAPMethodType = "TLS"
    EAPTTLS       EAPMethodType = "TTLS"
    EAPPEAP       EAPMethodType = "PEAP"
)

type EAPMethod interface {
    MethodType() EAPMethodType
    Init(ctx context.Context, params EAPParams) error
    Process(ctx context.Context, pkt []byte) (EAPResult, error)
    Abort(ctx context.Context) error
    RequiresAAA() bool
}

type EAPParams struct {
    Supi      Supi
    Snssai   Snssai
    UserID   string
    AAARealm  string
}

type EAPResult struct {
    State      EAPState   // CONTINUE | SUCCESS | FAILURE
    OutPackets [][]byte   // EAP messages to forward
    MSK        []byte     // Master Session Key (on SUCCESS)
    KeyIV      []byte
    ExtraData  map[string]any
}

type EAPState int
```

## Plugin Registry

```go
// internal/infrastructure/eap/registry.go
type Registry struct {
    mu      sync.RWMutex
    methods map[EAPMethodType]EAPMethod
}

func NewRegistry() *Registry {
    return &Registry{methods: make(map[EAPMethodType]EAPMethod)}
}

func (r *Registry) Register(m EAPMethod) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.methods[m.MethodType()] = m
}

func (r *Registry) Get(t EAPMethodType) (EAPMethod, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    m, ok := r.methods[t]
    return m, ok
}

// Wire at startup:
func WireEAPRegistry() *Registry {
    r := NewRegistry()
    r.Register(&AkaPrimeMethod{})
    r.Register(&TLSMethod{})
    return r
}
```

## NRF Client with Caching

```go
// internal/infrastructure/nrf/client.go
type NRFClient struct {
    baseURL  string
    httpClient *http.Client
    cache    *ristretto.Cache  // TTL-based in-memory cache
    log      *slog.Logger
}

func (c *NRFClient) DiscoverAMF(ctx context.Context, snssai Snssai) ([]nfProfile, error) {
    cacheKey := fmt.Sprintf("amf:%s:%s", snssai.SST, snssai.SD)

    if cached, ok := c.cache.Get(cacheKey); ok {
        return cached.([]nfProfile), nil
    }

    // Stale-while-revalidate: use stale data while refreshing
    url := fmt.Sprintf("%s/nnrf-nfd/v1/nf-instances?target-nf-type=AMF", c.baseURL)
    profiles, err := c.doDiscovery(ctx, url)
    if err != nil {
        // Try stale cache on error
        if cached, ok := c.cache.Get(cacheKey); ok {
            c.log.WarnContext(ctx, "NRF discovery failed, using stale cache",
                "error", err, "cache_key", cacheKey)
            return cached.([]nfProfile), nil
        }
        return nil, err
    }

    c.cache.Set(cacheKey, profiles, time.Minute*5)
    return profiles, nil
}
```

## SBI Middleware Chain

```go
// internal/api/middleware/middleware.go
func Chain(h http.Handler, m ...func(http.Handler) http.Handler) http.Handler {
    for i := len(m) - 1; i >= 0; i-- {
        h = m[i](h)
    }
    return h
}

// Usage in main.go:
handler = Chain(handler,
    middleware.Tracing(tracer),          // OpenTelemetry
    middleware.Auth(jwtValidator),       // JWT validation
    middleware.RateLimit(limiter),      // Per-NF rate limiting
    middleware.RequestID(),              // X-Request-ID
    middleware.Recoverer(log),           // Panic recovery
    middleware.Logger(log),             // Structured logging
)
```

## Circuit Breaker per AAA Backend

```go
// internal/infrastructure/aaa/breaker.go
func NewAAAClient(cfg AAAConfig) *AAAClient {
    cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        cfg.ID,
        MaxRequests: 3,             // requests in half-open
        Interval:    10 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
            return counts.Requests >= 10 && failureRatio >= 0.5
        },
        OnStateChange: func(name string, from, to gobreaker.State) {
            log.Warn("circuit breaker state change",
                "backend", name, "from", from, "to", to)
            // Emit alert if state == Open
        },
    })
    return &AAAClient{pool: newPool(cfg), cb: cb}
}
```

## Redis Distributed Lock

```go
// internal/repository/lock.go
type DistributedLock struct {
    redis *redis.Client
}

func (l *DistributedLock) Acquire(ctx context.Context, key string, ttl time.Duration) (func(), error) {
    lockKey := "lock:" + key
    token := uuid.New().String()

    acquired, err := l.redis.SetNX(ctx, lockKey, token, ttl).Result()
    if err != nil {
        return nil, fmt.Errorf("acquire lock: %w", err)
    }
    if !acquired {
        return nil, ErrLockNotAcquired
    }

    release := func() {
        // Lua script for atomic release
        script := redis.NewScript(`
            if redis.call("get", KEYS[1]) == ARGV[1] then
                return redis.call("del", KEYS[1])
            else
                return 0
            end
        `)
        script.Run(ctx, l.redis, []string{lockKey}, token)
    }
    return release, nil
}
```

## DB Partitioned Table Setup

```sql
-- PostgreSQL range partitioning by month
CREATE TABLE auth_context (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    auth_ctx_id     VARCHAR(64) NOT NULL,
    supi            VARCHAR(15) NOT NULL,
    snssai_sst      SMALLINT NOT NULL,
    snssai_sd       VARCHAR(6),
    state           VARCHAR(20) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE auth_context_2026_04 PARTITION OF auth_context
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
```
