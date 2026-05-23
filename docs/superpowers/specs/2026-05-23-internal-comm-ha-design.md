# NSSAAF Internal Communication HA Design

## 1. Overview

This document covers high availability improvements for internal communication between NSSAAF components, ensuring seamless failover when pods die and remaining pods can handle requests normally.

**Target:** Telecom-grade availability (>99.999% uptime)

**Scope:**
- Single-pod failure recovery (EAP session state)
- Database failover (PostgreSQL reconnection)
- NRF caching (service discovery resilience)
- In-memory store replacement (Redis-backed persistence)

---

## 2. Current State Analysis

### 2.1 Architecture Components

| Component | Description | HA Status |
|-----------|-------------|-----------|
| HTTP Gateway | Stateless TLS terminator | ✅ Stateless, scales horizontally |
| Biz Pod | EAP processing + AAA forwarding | ⚠️ Has in-memory session state |
| AAA Gateway | RADIUS/Diameter proxy | ⚠️ Limited to 2 replicas (active-standby) |
| PostgreSQL | Session persistence | ✅ Patroni HA with sync replication |
| Redis | Session cache, pub/sub | ✅ Cluster mode with auto-failover |

### 2.2 Identified Gaps

| Gap | Severity | Impact |
|-----|----------|--------|
| EAP session in-memory only | HIGH | Pod death loses session state |
| InMemoryStore still present | MEDIUM | Not used in production but present |
| No NRF caching | MEDIUM | NRF unavailability blocks service discovery |
| No DB reconnection strategy | LOW | pgxpool handles most cases |

---

## 3. EAP Session Persistence Design

### 3.1 Problem Statement

The EAP Engine (`internal/eap/engine.go`) maintains session state in-memory:

```go
type Engine struct {
    cfg            Config
    sessionManager *sessionManager  // IN-MEMORY
    fragmentMgr    *FragmentManager
    aaaClient      AAARouter
}
```

When a Biz Pod dies mid-authentication:
1. AMF sends EAP-Response-Identity → Pod 1
2. Pod 1 creates in-memory session
3. Pod 1 forwards to AAA-S
4. **Pod 1 crashes**
5. AMF retries → routes to Pod 2
6. **Pod 2 has no session state** → Session lost

### 3.2 Solution: Redis-Backed Session Manager

**Design Decision:** Store EAP session state in Redis with 5-minute TTL.

```go
// internal/eap/session_redis.go

package eap

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

const (
    // SessionKeyPrefix is the Redis key prefix for EAP sessions.
    SessionKeyPrefix = "nssaa:eap:session:"
    // SessionTTL is the TTL for EAP sessions (5 minutes).
    SessionTTL = 5 * time.Minute
)

// RedisSessionManager implements sessionManager interface backed by Redis.
type RedisSessionManager struct {
    client redis.Cmdable
    ttl    time.Duration
}

// RedisSession represents EAP session state stored in Redis.
type RedisSession struct {
    AuthCtxID      string        `json:"authCtxId"`
    GPSI           string        `json:"gpsi"`
    SnssaiKey      string        `json:"snssaiKey"`
    State          SessionState  `json:"state"`
    Rounds         int           `json:"rounds"`
    MaxRounds      int           `json:"maxRounds"`
    ExpectedID     uint8         `json:"expectedId"`
    LastNonce      []byte        `json:"lastNonce,omitempty"`
    CachedResponse []byte        `json:"cachedResponse,omitempty"`
    LastActivity   time.Time     `json:"lastActivity"`
    Timeout        time.Duration `json:"timeout"`
    Method         Method        `json:"method"`
    CreatedAt      time.Time     `json:"createdAt"`
}

// Get retrieves a session by authCtxID.
func (m *RedisSessionManager) Get(ctx context.Context, authCtxID string) (*Session, error) {
    key := SessionKeyPrefix + authCtxID

    data, err := m.client.Get(ctx, key).Bytes()
    if err != nil {
        if err == redis.Nil {
            return nil, ErrSessionNotFound
        }
        return nil, fmt.Errorf("redis get: %w", err)
    }

    var rs RedisSession
    if err := json.Unmarshal(data, &rs); err != nil {
        return nil, fmt.Errorf("unmarshal session: %w", err)
    }

    return rs.toSession(), nil
}

// Put stores or updates a session.
func (m *RedisSessionManager) Put(ctx context.Context, s *Session) error {
    key := SessionKeyPrefix + s.AuthCtxID

    rs := fromSession(s)
    data, err := json.Marshal(rs)
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }

    if err := m.client.Set(ctx, key, data, m.ttl).Err(); err != nil {
        return fmt.Errorf("redis set: %w", err)
    }

    return nil
}

// Delete removes a session.
func (m *RedisSessionManager) Delete(ctx context.Context, authCtxID string) error {
    key := SessionKeyPrefix + authCtxID
    return m.client.Del(ctx, key).Err()
}

// Size returns the number of sessions (approximate for Redis).
func (m *RedisSessionManager) Size(ctx context.Context) (int, error) {
    // Redis doesn't support direct key count, use SCAN
    var count int
    var cursor uint64
    for {
        keys, nextCursor, err := m.client.Scan(ctx, cursor, SessionKeyPrefix+"*", 100).Result()
        if err != nil {
            return 0, err
        }
        count += len(keys)
        cursor = nextCursor
        if cursor == 0 {
            break
        }
    }
    return count, nil
}

// toSession converts RedisSession to Session.
func (rs *RedisSession) toSession() *Session {
    s := &Session{
        AuthCtxID:    rs.AuthCtxID,
        GPSI:         rs.GPSI,
        SnssaiKey:    rs.SnssaiKey,
        State:        rs.State,
        Rounds:       rs.Rounds,
        MaxRounds:    rs.MaxRounds,
        ExpectedID:   rs.ExpectedID,
        LastNonce:    rs.LastNonce,
        CachedResponse: rs.CachedResponse,
        LastActivity: rs.LastActivity,
        Timeout:      rs.Timeout,
        Method:       rs.Method,
        CreatedAt:    rs.CreatedAt,
    }
    return s
}

// fromSession converts Session to RedisSession.
func fromSession(s *Session) *RedisSession {
    return &RedisSession{
        AuthCtxID:      s.AuthCtxID,
        GPSI:           s.GPSI,
        SnssaiKey:      s.SnssaiKey,
        State:          s.State,
        Rounds:         s.Rounds,
        MaxRounds:      s.MaxRounds,
        ExpectedID:     s.ExpectedID,
        LastNonce:      s.LastNonce,
        CachedResponse: s.CachedResponse,
        LastActivity:   s.LastActivity,
        Timeout:        s.Timeout,
        Method:         s.Method,
        CreatedAt:      s.CreatedAt,
    }
}
```

### 3.3 Engine Integration

```go
// internal/eap/engine.go - Modified

type Engine struct {
    cfg         Config
    sessions    SessionStore  // Changed from *sessionManager
    fragments   *FragmentManager
    aaaClient   AAARouter
    logger      Logger
}

// SessionStore interface for testability.
type SessionStore interface {
    Get(ctx context.Context, authCtxID string) (*Session, error)
    Put(ctx context.Context, s *Session) error
    Delete(ctx context.Context, authCtxID string) error
    Size(ctx context.Context) (int, error)
}

// NewEngineWithRedis creates engine with Redis-backed sessions.
func NewEngineWithRedis(cfg Config, aaaClient AAARouter, redisClient redis.Cmdable, logger *slog.Logger) *Engine {
    return &Engine{
        cfg:       cfg,
        sessions:  &RedisSessionManager{client: redisClient, ttl: cfg.SessionTTL},
        fragments: NewFragmentManager(cfg.FragmentTTLSeconds),
        aaaClient: aaaClient,
        logger:    &defaultLogger{logger},
    }
}
```

### 3.4 Idempotency Enhancement

The retry detection mechanism already exists but can be enhanced:

```go
// In Process method - already implements retry detection
msgHash := sha256Hash(eapPayload)
if bytesEqual(session.LastNonce, msgHash) && session.CachedResponse != nil {
    e.logger.Debug("eap_retry_detected", "auth_ctx_id", authCtxID)
    respMsg := types.NewEapMessage(session.CachedResponse)
    return &respMsg, types.AuthResultPending, nil
}
```

**Key Point:** The cached response ensures AMF retries return the same response without re-forwarding to AAA-S.

---

## 4. In-Memory Store Replacement

### 4.1 Current State

The `InMemoryStore` in `internal/api/nssaa/handler.go` is documented as "Phase 3" replacement:

```go
// InMemoryStore is a simple in-memory implementation of AuthCtxStore.
// Phase 3 replaces this with Redis-based storage.
type InMemoryStore struct {
    data map[string]*AuthCtx
}
```

### 4.2 Solution: Remove InMemoryStore

Replace with Redis-backed implementation:

```go
// internal/api/nssaa/redis_store.go

package nssaa

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/operator/nssAAF/internal/types"
    "github.com/redis/go-redis/v9"
)

const (
    AuthCtxKeyPrefix = "nssaa:auth:ctx:"
    AuthCtxTTL       = 24 * time.Hour  // 1 day TTL for auth contexts
)

// RedisAuthCtxStore implements AuthCtxStore backed by Redis.
type RedisAuthCtxStore struct {
    client redis.Cmdable
}

// NewRedisAuthCtxStore creates a new Redis-backed store.
func NewRedisAuthCtxStore(client redis.Cmdable) *RedisAuthCtxStore {
    return &RedisAuthCtxStore{client: client}
}

// Load retrieves an auth context by ID.
func (s *RedisAuthCtxStore) Load(ctx context.Context, id string) (*AuthCtx, error) {
    key := AuthCtxKeyPrefix + id

    data, err := s.client.Get(ctx, key).Bytes()
    if err != nil {
        if err == redis.Nil {
            return nil, ErrNotFound
        }
        return nil, fmt.Errorf("redis get: %w", err)
    }

    var ctx AuthCtx
    if err := json.Unmarshal(data, &ctx); err != nil {
        return nil, fmt.Errorf("unmarshal auth context: %w", err)
    }

    return &ctx, nil
}

// Save stores an auth context.
func (s *RedisAuthCtxStore) Save(ctx context.Context, authCtx *AuthCtx) error {
    key := AuthCtxKeyPrefix + authCtx.AuthCtxID

    data, err := json.Marshal(authCtx)
    if err != nil {
        return fmt.Errorf("marshal auth context: %w", err)
    }

    if err := s.client.Set(ctx, key, data, AuthCtxTTL).Err(); err != nil {
        return fmt.Errorf("redis set: %w", err)
    }

    return nil
}

// Delete removes an auth context.
func (s *RedisAuthCtxStore) Delete(ctx context.Context, id string) error {
    key := AuthCtxKeyPrefix + id
    return s.client.Del(ctx, key).Err()
}

// Close implements AuthCtxStore (no-op for Redis).
func (s *RedisAuthCtxStore) Close() error {
    return nil
}

var _ AuthCtxStore = (*RedisAuthCtxStore)(nil)
```

---

## 5. NRF Caching Design

### 5.1 Problem

Service discovery without caching:
- NRF unavailable → no new service discovery possible
- NRF slow → all requests blocked

### 5.2 Solution: TTL-Cached NRF Client

```go
// internal/nrf/cache.go

package nrf

import (
    "context"
    "sync"
    "time"

    "github.com/operator/nssAAF/oapi-gen/gen/nrf"
)

// CachedClient wraps NRF client with TTL-based caching.
type CachedClient struct {
    inner  *Client
    cache  map[string]*cachedResponse
    mu     sync.RWMutex
    ttl    time.Duration
}

type cachedResponse struct {
    data      *nrf.NfProfile
    expiresAt time.Time
}

// NewCachedClient creates a cached NRF client.
func NewCachedClient(inner *Client, ttl time.Duration) *CachedClient {
    return &CachedClient{
        inner: inner,
        cache: make(map[string]*cachedResponse),
        ttl:   ttl,
    }
}

// GetNfInstance retrieves NfProfile with caching.
func (c *CachedClient) GetNfInstance(ctx context.Context, nfInstanceID string) (*nrf.NfProfile, error) {
    // Check cache
    c.mu.RLock()
    if cached, ok := c.cache[nfInstanceID]; ok && time.Now().Before(cached.expiresAt) {
        c.mu.RUnlock()
        return cached.data, nil
    }
    c.mu.RUnlock()

    // Cache miss - fetch from NRF
    profile, err := c.inner.GetNfInstance(ctx, nfInstanceID)
    if err != nil {
        // On NRF failure, return cached data even if expired
        c.mu.RLock()
        if cached, ok := c.cache[nfInstanceID]; ok {
            data := cached.data
            c.mu.RUnlock()
            return data, nil
        }
        c.mu.RUnlock()
        return nil, err
    }

    // Update cache
    c.mu.Lock()
    c.cache[nfInstanceID] = &cachedResponse{
        data:      profile,
        expiresAt: time.Now().Add(c.ttl),
    }
    c.mu.Unlock()

    return profile, nil
}

// DiscoverService discovers services with caching.
func (c *CachedClient) DiscoverService(ctx context.Context, service string) ([]*nrf.NfProfile, error) {
    // Check cache
    c.mu.RLock()
    if cached, ok := c.cache[service]; ok && time.Now().Before(cached.expiresAt) {
        c.mu.RUnlock()
        // Return as single-item slice for consistency
        return []*nrf.NfProfile{cached.data}, nil
    }
    c.mu.RUnlock()

    // Cache miss - fetch from NRF
    profiles, err := c.inner.DiscoverService(ctx, service)
    if err != nil {
        // On NRF failure, return cached data even if expired
        c.mu.RLock()
        if cached, ok := c.cache[service]; ok {
            data := cached.data
            c.mu.RUnlock()
            return []*nrf.NfProfile{data}, nil
        }
        c.mu.RUnlock()
        return nil, err
    }

    if len(profiles) > 0 {
        // Cache first profile
        c.mu.Lock()
        c.cache[service] = &cachedResponse{
            data:      profiles[0],
            expiresAt: time.Now().Add(c.ttl),
        }
        c.mu.Unlock()
    }

    return profiles, nil
}

// Invalidate removes a cached entry.
func (c *CachedClient) Invalidate(key string) {
    c.mu.Lock()
    delete(c.cache, key)
    c.mu.Unlock()
}

// Clear removes all cached entries.
func (c *CachedClient) Clear() {
    c.mu.Lock()
    c.cache = make(map[string]*cachedResponse)
    c.mu.Unlock()
}
```

**Configuration:**
```yaml
# Default: 5 minutes TTL
nrf:
  cache:
    ttl: 5m
```

---

## 6. Database Reconnection Strategy

### 6.1 Current State

PostgreSQL pool uses pgxpool with health checks but no explicit reconnection strategy:

```go
// internal/storage/postgres/pool.go
func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
    // ... creates pool with health checks ...
    config.HealthCheckPeriod = time.Minute  // Configurable
}
```

### 6.2 Enhanced Reconnection Logic

```go
// internal/storage/postgres/reconnect.go

package postgres

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/operator/nssAAF/internal/metrics"
)

// ErrConnectionFailed is returned when all reconnection attempts fail.
var ErrConnectionFailed = errors.New("failed to reconnect to database")

// ReconnectingPool wraps pgxpool with automatic reconnection.
type ReconnectingPool struct {
    pool   *Pool
    leader string
    cfg    Config
}

// NewReconnectingPool creates a reconnecting pool wrapper.
func NewReconnectingPool(ctx context.Context, cfg Config) (*ReconnectingPool, error) {
    pool, err := NewPool(ctx, cfg)
    if err != nil {
        return nil, err
    }
    return &ReconnectingPool{pool: pool, cfg: cfg}, nil
}

// ExecuteWithRetry executes a function with automatic reconnection on failure.
func (rp *ReconnectingPool) ExecuteWithRetry(ctx context.Context, maxRetries int, fn func(context.Context, *Pool) error) error {
    var lastErr error

    for attempt := 0; attempt < maxRetries; attempt++ {
        if err := fn(ctx, rp.pool); err != nil {
            lastErr = err

            if !isConnectionError(err) {
                return err // Non-connection error, don't retry
            }

            // Connection error - try to reconnect
            metrics.DatabaseReconnectAttempts.Inc()

            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(time.Duration(attempt+1) * time.Second):
                // Attempt reconnection
                if err := rp.reconnect(ctx); err != nil {
                    lastErr = err
                    continue
                }
            }
            continue
        }
        return nil
    }

    return errors.Join(ErrConnectionFailed, lastErr)
}

// reconnect attempts to reconnect to the database.
func (rp *ReconnectingPool) reconnect(ctx context.Context) error {
    // Close existing pool
    rp.pool.Close()

    // Create new pool
    newPool, err := NewPool(ctx, rp.cfg)
    if err != nil {
        return err
    }

    rp.pool = newPool
    metrics.DatabaseReconnections.Inc()
    return nil
}

// isConnectionError classifies connection-related errors.
func isConnectionError(err error) bool {
    if err == nil {
        return false
    }

    // pgx connection errors
    var pgxErr *pgconn.PgError
    if errors.As(err, &pgxErr) {
        // Classify connection-related PostgreSQL errors
        switch pgxErr.Code {
        case "08000", // connection_exception
            "08003", // connection_does_not_exist
            "08006", // connection_failure
            "08001", // sqlclient_unable_to_establish_sqlconnection
            "08004", // sqlserver_rejected_establishment_of_sqlconnection
            "57P01", // admin_shutdown
            "57P02", // crash_shutdown
            "57P03", // cannot_connect_now
            "40001":  // serialization_failure (after retry)
            return true
        }
    }

    // Network errors
    return errors.Is(err, context.DeadlineExceeded) ||
           errors.Is(err, context.Canceled)
}
```

---

## 7. AAA Gateway HA Considerations

### 7.1 Current Design

The AAA Gateway is limited to 2 replicas (active-standby) due to:
- RADIUS/Diameter sessions are stateful
- Failover requires state transfer

### 7.2 Options

| Option | Pros | Cons |
|--------|------|------|
| Keep active-standby | Simple, proven | Max 2 replicas, no horizontal scaling |
| Stateless AAA protocol | Horizontal scaling | Requires AAA-S changes, breaking |
| Redis session correlation | Enables active-active | Complexity, latency |

### 7.3 Recommendation

**Keep active-standby for initial deployment.** The 2-replica limit is acceptable because:
1. AAA Gateway is typically I/O bound (waiting for AAA-S), not CPU bound
2. 2 replicas provide sufficient capacity for most deployments
3. Failover time is <3 seconds with keepalived VRRP

**Future enhancement:** Implement Redis-based session correlation to enable active-active mode.

---

## 8. Implementation Plan

### Phase 1: Remove InMemoryStore (Low Risk)
1. Create `RedisAuthCtxStore` implementation
2. Update handler to use Redis store
3. Remove `InMemoryStore` code
4. Add integration tests

### Phase 2: NRF Caching (Low Risk)
1. Create `CachedNRFClient` wrapper
2. Add configuration options
3. Add cache metrics
4. Update NRF client usage

### Phase 3: EAP Session Persistence (Medium Risk)
1. Create `RedisSessionManager`
2. Add session TTL configuration
3. Update engine to use Redis sessions
4. Add idempotency tests
5. Performance testing

### Phase 4: DB Reconnection (Low Risk)
1. Create `ReconnectingPool` wrapper
2. Add configuration options
3. Add reconnection metrics
4. Test failover scenarios

---

## 9. Acceptance Criteria

| # | Criteria | Verification |
|---|----------|---------------|
| AC1 | Pod death does not lose in-flight EAP sessions | Redis contains session state |
| AC2 | AMF retry returns cached response | CachedResponse not nil on retry |
| AC3 | NRF unavailable uses cached data | Cache hit on NRF failure |
| AC4 | InMemoryStore removed | Code removed, Redis store in use |
| AC5 | DB reconnection succeeds after failover | Reconnection metrics increment |
| AC6 | All components handle pod death gracefully | Integration test passes |

---

## 10. References

- [HA Architecture Design](../design/10_ha_architecture.md)
- [Database HA Design](../design/11_database_ha.md)
- [Redis HA Design](../design/12_redis_ha.md)
- [EAP Engine](../eap/engine.go)
- [Session Manager](../session/memory.go)
- [Circuit Breaker](../resilience/circuit_breaker.go)
