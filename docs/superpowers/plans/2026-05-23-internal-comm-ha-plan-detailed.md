# Internal Communication HA Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement high availability improvements for NSSAAF internal communication to ensure seamless failover when pods die.

**Architecture:** Replace in-memory stores with Redis-backed implementations. Add TTL-cached NRF discovery. Add database reconnection strategy. All changes maintain existing interfaces for backward compatibility.

**Tech Stack:** Go 1.21+, Redis (go-redis/v9), pgx/v5, existing config patterns

---

## File Structure

```
internal/
├── api/nssaa/
│   ├── handler.go          # MODIFY: Remove InMemoryStore usage
│   ├── redis_store.go      # CREATE: Redis-backed AuthCtxStore
│   └── redis_store_test.go # CREATE: Tests for Redis store
├── eap/
│   ├── engine.go          # MODIFY: Add interface-based session store
│   ├── session_redis.go   # CREATE: Redis-backed session manager
│   └── session_redis_test.go # CREATE: Tests for Redis sessions
├── nrf/
│   └── client.go          # MODIFY: Already has cache, verify + enhance
├── storage/postgres/
│   ├── reconnect.go        # CREATE: Reconnection wrapper
│   └── reconnect_test.go  # CREATE: Tests for reconnection
└── metrics/
    └── database.go        # MODIFY: Add reconnection metrics
```

---

## Task 1: Create RedisAuthCtxStore

**Files:**
- Create: `internal/api/nssaa/redis_store.go`
- Test: `internal/api/nssaa/redis_store_test.go`
- Modify: `internal/api/nssaa/handler.go` (remove InMemoryStore, use Redis)

**Context needed:**
- `internal/api/nssaa/handler.go:41-48` — AuthCtxStore interface
- `internal/api/nssaa/handler.go:53-88` — InMemoryStore to replace
- `internal/cache/redis/pool.go` — Redis pool patterns

- [ ] **Step 1: Write the failing test**

```go
// internal/api/nssaa/redis_store_test.go
package nssaa

import (
    "context"
    "testing"
    "time"

    "github.com/alicebob/miniredis/v2"
    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestRedisAuthCtxStore(t *testing.T) {
    // Setup miniredis
    sr, err := miniredis.Run()
    require.NoError(t, err)
    t.Cleanup(func() { sr.Close() })

    client := redis.NewClient(&redis.Options{Addr: sr.Addr()})
    t.Cleanup(func() { client.Close() })

    store := NewRedisAuthCtxStore(client)

    t.Run("Save and Load", func(t *testing.T) {
        authCtx := &AuthCtx{
            AuthCtxID:   "test-123",
            GPSI:        "msisdn-1234567890",
            SnssaiSST:   1,
            SnssaiSD:    "000001",
            AmfInstance: "amf-1",
        }

        err := store.Save(context.Background(), authCtx)
        require.NoError(t, err)

        loaded, err := store.Load(context.Background(), "test-123")
        require.NoError(t, err)
        assert.Equal(t, authCtx.AuthCtxID, loaded.AuthCtxID)
        assert.Equal(t, authCtx.GPSI, loaded.GPSI)
        assert.Equal(t, authCtx.SnssaiSST, loaded.SnssaiSST)
        assert.Equal(t, authCtx.SnssaiSD, loaded.SnssaiSD)
    })

    t.Run("Load not found", func(t *testing.T) {
        _, err := store.Load(context.Background(), "nonexistent")
        assert.ErrorIs(t, err, ErrNotFound)
    })

    t.Run("Delete", func(t *testing.T) {
        authCtx := &AuthCtx{AuthCtxID: "delete-123"}
        err := store.Save(context.Background(), authCtx)
        require.NoError(t, err)

        err = store.Delete(context.Background(), "delete-123")
        require.NoError(t, err)

        _, err = store.Load(context.Background(), "delete-123")
        assert.ErrorIs(t, err, ErrNotFound)
    })

    t.Run("TTL is 24 hours", func(t *testing.T) {
        authCtx := &AuthCtx{AuthCtxID: "ttl-test"}
        err := store.Save(context.Background(), authCtx)
        require.NoError(t, err)

        // Verify TTL via miniredis
        ttl := sr.TTL("nssaa:auth:ctx:ttl-test")
        assert.True(t, ttl > 23*time.Hour && ttl <= 24*time.Hour, "TTL should be ~24 hours, got %v", ttl)
    })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/nssaa/... -run TestRedisAuthCtxStore -v`
Expected: FAIL — redis_store.go does not exist

- [ ] **Step 3: Write the implementation**

```go
// internal/api/nssaa/redis_store.go
package nssaa

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

const (
    // AuthCtxKeyPrefix is the Redis key prefix for auth contexts.
    AuthCtxKeyPrefix = "nssaa:auth:ctx:"
    // AuthCtxTTL is the TTL for auth contexts (24 hours).
    AuthCtxTTL = 24 * time.Hour
)

// RedisAuthCtxStore implements AuthCtxStore backed by Redis.
type RedisAuthCtxStore struct {
    client redis.Cmdable
}

// NewRedisAuthCtxStore creates a new Redis-backed auth context store.
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

    var authCtx AuthCtx
    if err := json.Unmarshal(data, &authCtx); err != nil {
        return nil, fmt.Errorf("unmarshal auth context: %w", err)
    }

    return &authCtx, nil
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/nssaa/... -run TestRedisAuthCtxStore -v`
Expected: PASS

- [ ] **Step 5: Remove InMemoryStore from handler.go**

Modify `internal/api/nssaa/handler.go`:
- Remove lines 53-88 (InMemoryStore struct and methods)
- Update any NewHandler calls to use RedisAuthCtxStore

```go
// Remove this entire block from handler.go:
// InMemoryStore struct and all its methods (lines 53-88)
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/api/nssaa/... -v`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/api/nssaa/redis_store.go internal/api/nssaa/redis_store_test.go internal/api/nssaa/handler.go
git commit -m "feat: add Redis-backed AuthCtxStore, remove InMemoryStore

- Add RedisAuthCtxStore implementing AuthCtxStore interface
- 24-hour TTL for auth contexts
- Remove deprecated InMemoryStore
- Add comprehensive tests with miniredis"
```

---

## Task 2: Create RedisSessionManager for EAP Engine

**Files:**
- Create: `internal/eap/session_redis.go`
- Test: `internal/eap/session_redis_test.go`
- Modify: `internal/eap/engine.go` (add SessionStore interface, NewEngineWithRedis)
- Modify: `internal/eap/session.go` (add SessionStore interface)

**Context needed:**
- `internal/eap/engine.go:64-73` — Engine struct with sessionManager
- `internal/session/session.go:21-33` — Session struct
- `internal/eap/state.go` — SessionState constants
- `internal/eap/session.go` — SessionManager interface (if exists)

- [ ] **Step 1: Write the failing test**

```go
// internal/eap/session_redis_test.go
package eap

import (
    "context"
    "testing"
    "time"

    "github.com/alicebob/miniredis/v2"
    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestRedisSessionManager(t *testing.T) {
    // Setup miniredis
    sr, err := miniredis.Run()
    require.NoError(t, err)
    t.Cleanup(func() { sr.Close() })

    client := redis.NewClient(&redis.Options{Addr: sr.Addr()})
    t.Cleanup(func() { client.Close() })

    mgr := NewRedisSessionManager(client, 5*time.Minute)

    t.Run("Put and Get", func(t *testing.T) {
        session := &Session{
            AuthCtxID:    "eap-session-123",
            GPSI:         "msisdn-1234567890",
            SnssaiKey:    "1:000001",
            State:        SessionStateEapExchange,
            Rounds:       2,
            MaxRounds:    20,
            ExpectedID:   3,
            LastActivity: time.Now(),
            CreatedAt:    time.Now(),
        }

        err := mgr.Put(context.Background(), session)
        require.NoError(t, err)

        loaded, err := mgr.Get(context.Background(), "eap-session-123")
        require.NoError(t, err)
        assert.Equal(t, session.AuthCtxID, loaded.AuthCtxID)
        assert.Equal(t, session.GPSI, loaded.GPSI)
        assert.Equal(t, session.State, loaded.State)
        assert.Equal(t, session.Rounds, loaded.Rounds)
    })

    t.Run("Get not found", func(t *testing.T) {
        _, err := mgr.Get(context.Background(), "nonexistent")
        assert.ErrorIs(t, err, ErrSessionNotFound)
    })

    t.Run("Delete", func(t *testing.T) {
        session := &Session{AuthCtxID: "delete-session"}
        err := mgr.Put(context.Background(), session)
        require.NoError(t, err)

        err = mgr.Delete(context.Background(), "delete-session")
        require.NoError(t, err)

        _, err = mgr.Get(context.Background(), "delete-session")
        assert.ErrorIs(t, err, ErrSessionNotFound)
    })

    t.Run("TTL is 5 minutes", func(t *testing.T) {
        session := &Session{AuthCtxID: "ttl-test"}
        err := mgr.Put(context.Background(), session)
        require.NoError(t, err)

        ttl := sr.TTL("nssaa:eap:session:ttl-test")
        assert.True(t, ttl > 4*time.Minute && ttl <= 5*time.Minute, "TTL should be ~5 minutes, got %v", ttl)
    })

    t.Run("Preserves byte slices", func(t *testing.T) {
        session := &Session{
            AuthCtxID:      "bytes-test",
            LastNonce:      []byte{0x01, 0x02, 0x03},
            CachedResponse: []byte{0x04, 0x05, 0x06},
        }

        err := mgr.Put(context.Background(), session)
        require.NoError(t, err)

        loaded, err := mgr.Get(context.Background(), "bytes-test")
        require.NoError(t, err)
        assert.Equal(t, []byte{0x01, 0x02, 0x03}, loaded.LastNonce)
        assert.Equal(t, []byte{0x04, 0x05, 0x06}, loaded.CachedResponse)
    })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eap/... -run TestRedisSessionManager -v`
Expected: FAIL — session_redis.go does not exist

- [ ] **Step 3: Write the implementation**

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
)

// SessionStore defines the interface for session storage.
type SessionStore interface {
    Get(ctx context.Context, authCtxID string) (*Session, error)
    Put(ctx context.Context, s *Session) error
    Delete(ctx context.Context, authCtxID string) error
}

// RedisSessionManager implements SessionStore backed by Redis.
type RedisSessionManager struct {
    client redis.Cmdable
    ttl    time.Duration
}

// NewRedisSessionManager creates a new Redis-backed session manager.
func NewRedisSessionManager(client redis.Cmdable, ttl time.Duration) *RedisSessionManager {
    return &RedisSessionManager{client: client, ttl: ttl}
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

    var rs redisSession
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

// redisSession is the serializable form of Session for Redis storage.
type redisSession struct {
    AuthCtxID      string        `json:"authCtxId"`
    GPSI          string       `json:"gpsi"`
    SnssaiKey     string       `json:"snssaiKey"`
    State         SessionState `json:"state"`
    Rounds        int          `json:"rounds"`
    MaxRounds     int          `json:"maxRounds"`
    ExpectedID    uint8        `json:"expectedId"`
    LastNonce      []byte       `json:"lastNonce,omitempty"`
    CachedResponse []byte      `json:"cachedResponse,omitempty"`
    LastActivity   time.Time    `json:"lastActivity"`
    Timeout       time.Duration `json:"timeout"`
    Method        Method       `json:"method"`
    CreatedAt     time.Time    `json:"createdAt"`
}

// toSession converts redisSession to Session.
func (rs *redisSession) toSession() *Session {
    return &Session{
        AuthCtxID:      rs.AuthCtxID,
        GPSI:          rs.GPSI,
        SnssaiKey:     rs.SnssaiKey,
        State:         rs.State,
        Rounds:        rs.Rounds,
        MaxRounds:     rs.MaxRounds,
        ExpectedID:    rs.ExpectedID,
        LastNonce:     rs.LastNonce,
        CachedResponse: rs.CachedResponse,
        LastActivity:   rs.LastActivity,
        Timeout:       rs.Timeout,
        Method:        rs.Method,
        CreatedAt:     rs.CreatedAt,
    }
}

// fromSession converts Session to redisSession.
func fromSession(s *Session) *redisSession {
    return &redisSession{
        AuthCtxID:      s.AuthCtxID,
        GPSI:          s.GPSI,
        SnssaiKey:     s.SnssaiKey,
        State:         s.State,
        Rounds:        s.Rounds,
        MaxRounds:     s.MaxRounds,
        ExpectedID:    s.ExpectedID,
        LastNonce:     s.LastNonce,
        CachedResponse: s.CachedResponse,
        LastActivity:   s.LastActivity,
        Timeout:       s.Timeout,
        Method:        s.Method,
        CreatedAt:     s.CreatedAt,
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eap/... -run TestRedisSessionManager -v`
Expected: PASS

- [ ] **Step 5: Add SessionStore interface to session/memory.go**

```go
// Add to internal/session/memory.go

// SessionStore defines the interface for session storage.
// This interface is implemented by both MemoryStore and eap.RedisSessionManager.
type SessionStore interface {
    Load(ctx context.Context, id string) (*Session, error)
    Save(ctx context.Context, s *Session) error
    Delete(ctx context.Context, id string) error
}
```

- [ ] **Step 6: Update Engine to use SessionStore interface**

Modify `internal/eap/engine.go`:

```go
// Replace lines 64-73 (Engine struct):
type Engine struct {
    cfg       Config
    sessions  SessionStore  // Changed from *sessionManager to interface
    fragments *FragmentManager
    aaaClient AAARouter
    logger    Logger

    // TLS config for EAP-TLS
    tlsConfig *tls.Config
}

// Add NewEngineWithRedis constructor after NewEngine:
const DefaultSessionTTL = 5 * time.Minute

// NewEngineWithRedis creates engine with Redis-backed sessions.
func NewEngineWithRedis(cfg Config, aaaClient AAARouter, redisClient redis.Cmdable, logger *slog.Logger) *Engine {
    if cfg.SessionTTL == 0 {
        cfg.SessionTTL = DefaultSessionTTL
    }
    return &Engine{
        cfg:       cfg,
        sessions:  NewRedisSessionManager(redisClient, cfg.SessionTTL),
        fragments: NewFragmentManager(cfg.FragmentTTLSeconds),
        aaaClient: aaaClient,
        logger:    &defaultLogger{logger},
    }
}
```

- [ ] **Step 7: Update sessionManager method calls**

The existing `sessionManager` has `get`, `put`, `delete`, `size` methods. The Engine calls these on `sessionManager`. Need to verify interface compatibility.

Read `internal/eap/session.go` to see sessionManager interface:

```go
// internal/eap/session.go - verify these methods exist
func (e *Engine) StartSession(...) {
    e.sessionManager.put(session)  // → e.sessions.Put(ctx, session)
}

func (e *Engine) GetSession(...) {
    return e.sessionManager.get(authCtxID)  // → e.sessions.Get(ctx, authCtxID)
}

func (e *Engine) DeleteSession(...) {
    e.sessionManager.delete(authCtxID)  // → e.sessions.Delete(ctx, authCtxID)
}

func (e *Engine) Stats() {
    return e.sessionManager.size()  // May need Stats() method on interface
}
```

Update the calls to use the SessionStore interface methods with context.

- [ ] **Step 8: Run all tests**

Run: `go build ./... && go test ./internal/eap/... -v`
Expected: All tests pass

- [ ] **Step 9: Commit**

```bash
git add internal/eap/session_redis.go internal/eap/session_redis_test.go internal/eap/engine.go internal/session/memory.go
git commit -m "feat: add Redis-backed session manager for EAP engine

- Add SessionStore interface for testability
- Add RedisSessionManager with 5-min TTL
- Add NewEngineWithRedis constructor
- Preserve byte slices (LastNonce, CachedResponse) in serialization
- Update Engine to use SessionStore interface"
```

---

## Task 3: NRF Caching Enhancement

**Files:**
- Modify: `internal/nrf/client.go` (already has cache, verify + add graceful degradation)
- Modify: `internal/config/config.go` (add NRF cache TTL config)

**Context needed:**
- `internal/nrf/client.go:30-65` — Existing NRFDiscoveryCache
- `internal/config/config.go:211-215` — NRFConfig

- [ ] **Step 1: Review existing NRF cache implementation**

Read `internal/nrf/client.go` lines 30-65. The existing `NRFDiscoveryCache` already has:
- TTL-based caching
- Get/Set methods
- 5-minute default TTL

- [ ] **Step 2: Add graceful degradation on NRF failure**

The spec requires returning cached data even if expired when NRF is unavailable. Update `NRFDiscoveryCache`:

```go
// Modify NRFDiscoveryCache.Get in internal/nrf/client.go:
// Add parameter for stale-okay mode

func (c *NRFDiscoveryCache) Get(key string, allowStale bool) (interface{}, bool) {
    c.mu.RLock()
    entry, ok := c.cache[key]
    c.mu.RUnlock()
    
    if !ok {
        return nil, false
    }
    
    // If not expired, return immediately
    if time.Now().Before(entry.expiresAt) {
        return entry.data, true
    }
    
    // If expired but stale allowed, return cached data
    if allowStale {
        return entry.data, true
    }
    
    return nil, false
}
```

Update all cache Get calls to pass `allowStale: true` for discovery operations.

- [ ] **Step 3: Add NRF cache TTL config**

Add to `internal/config/config.go`:

```go
// NRFConfig - add CacheTTL field:
type NRFConfig struct {
    BaseURL         string        `yaml:"baseURL"`
    DiscoverTimeout time.Duration `yaml:"discoverTimeout"`
    CacheTTL        time.Duration `yaml:"cacheTtl"` // Default: 5m
}

// In applyDefaults():
if cfg.NRF.CacheTTL == 0 {
    cfg.NRF.CacheTTL = 5 * time.Minute
}
```

- [ ] **Step 4: Update NewClient to use config TTL**

```go
// internal/nrf/client.go - NewClient function:
func NewClient(cfg config.NRFConfig) *Client {
    cacheTTL := cfg.CacheTTL
    if cacheTTL == 0 {
        cacheTTL = 5 * time.Minute
    }
    return &Client{
        // ... existing fields ...
        cache: &NRFDiscoveryCache{
            ttl: cacheTTL,
        },
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go build ./... && go test ./internal/nrf/... -v`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/nrf/client.go internal/config/config.go
git commit -m "feat: enhance NRF cache with graceful degradation

- Add allowStale parameter to NRFDiscoveryCache.Get
- Return cached data on NRF failure (even if expired)
- Add NRFConfig.CacheTTL config option
- Preserve existing cache behavior as default"
```

---

## Task 4: Database Reconnection Strategy

**Files:**
- Create: `internal/storage/postgres/reconnect.go`
- Test: `internal/storage/postgres/reconnect_test.go`
- Modify: `internal/metrics/` (add reconnection metrics if needed)

**Context needed:**
- `internal/storage/postgres/pool.go` — Existing Pool struct
- `internal/resilience/retry.go` — Existing retry patterns

- [ ] **Step 1: Write the failing test**

```go
// internal/storage/postgres/reconnect_test.go
package postgres

import (
    "context"
    "errors"
    "net"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestReconnectingPool(t *testing.T) {
    t.Run("ExecuteWithRetry succeeds on first attempt", func(t *testing.T) {
        rp := &ReconnectingPool{pool: &mockPool{}}
        
        called := 0
        err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
            called++
            return nil
        })
        
        require.NoError(t, err)
        assert.Equal(t, 1, called)
    })

    t.Run("ExecuteWithRetry retries on connection error", func(t *testing.T) {
        rp := &ReconnectingPool{pool: &mockPool{failConn: true}, cfg: Config{}}
        
        called := 0
        err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
            called++
            if called < 3 {
                return errors.New("connection reset")
            }
            return nil
        })
        
        require.NoError(t, err)
        assert.Equal(t, 3, called)
    })

    t.Run("ExecuteWithRetry returns non-connection errors immediately", func(t *testing.T) {
        rp := &ReconnectingPool{pool: &mockPool{}}
        
        err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
            return errors.New("business error")
        })
        
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "business error")
    })
}

// mockPool implements Pool interface for testing
type mockPool struct {
    failConn bool
}

func (m *mockPool) Ping(ctx context.Context) error {
    if m.failConn {
        return errors.New("connection refused")
    }
    return nil
}

func (m *mockPool) Close() error { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/postgres/... -run TestReconnectingPool -v`
Expected: FAIL — reconnect.go does not exist

- [ ] **Step 3: Write the implementation**

```go
// internal/storage/postgres/reconnect.go
package postgres

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

// ErrConnectionFailed is returned when all reconnection attempts fail.
var ErrConnectionFailed = errors.New("failed to reconnect to database")

// pgx connection error codes that indicate connection issues
var connectionErrorCodes = map[string]bool{
    "08000": true, // connection_exception
    "08003": true, // connection_does_not_exist
    "08006": true, // connection_failure
    "08001": true, // sqlclient_unable_to_establish_sqlconnection
    "08004": true, // sqlserver_rejected_establishment_of_sqlconnection
    "57P01": true, // admin_shutdown
    "57P02": true, // crash_shutdown
    "57P03": true, // cannot_connect_now
    "40001": true, // serialization_failure (after retry)
}

// isConnectionError classifies connection-related errors.
func isConnectionError(err error) bool {
    if err == nil {
        return false
    }

    var pgxErr *pgconn.PgError
    if errors.As(err, &pgxErr) {
        return connectionErrorCodes[pgxErr.Code]
    }

    // Network errors
    var opErr *net.OpError
    if errors.As(err, &opErr) {
        return true
    }

    return false
}

// ReconnectingPool wraps Pool with automatic reconnection.
type ReconnectingPool struct {
    pool *Pool
    cfg  Config
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

            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(time.Duration(attempt+1) * time.Second):
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
    rp.pool.Close()

    newPool, err := NewPool(ctx, rp.cfg)
    if err != nil {
        return err
    }

    rp.pool = newPool
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/postgres/... -run TestReconnectingPool -v`
Expected: PASS

- [ ] **Step 5: Add reconnection metrics**

Add to `internal/metrics/` (create if not exists):

```go
// internal/metrics/database.go
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    DatabaseReconnectAttempts = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "nssAAF_database_reconnect_attempts_total",
        Help: "Total number of database reconnection attempts",
    })
    
    DatabaseReconnections = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "nssAAF_database_reconnections_total",
        Help: "Total number of successful database reconnections",
    })
)

func init() {
    prometheus.MustRegister(DatabaseReconnectAttempts, DatabaseReconnections)
}
```

- [ ] **Step 6: Run all tests**

Run: `go build ./... && go test ./internal/storage/postgres/... -v`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/storage/postgres/reconnect.go internal/storage/postgres/reconnect_test.go internal/metrics/database.go
git commit -m "feat: add database reconnection strategy

- Add ReconnectingPool with ExecuteWithRetry method
- Classify connection errors (pgx error codes + network errors)
- Exponential backoff reconnection (1s, 2s, 3s...)
- Add reconnection metrics (attempts, successes)
- Return cached data on NRF failure even if expired"
```

---

## Task 5: Integration Verification

**Files:**
- None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `go build ./... && go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 2: Run lint**

Run: `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 3: Verify all acceptance criteria**

From spec §9:

| # | Criteria | Verification |
|---|----------|--------------|
| AC1 | Pod death does not lose in-flight EAP sessions | RedisSessionManager persists sessions |
| AC2 | AMF retry returns cached response | Retry detection in engine.go:159-168 |
| AC3 | NRF unavailable uses cached data | allowStale in NRFDiscoveryCache.Get |
| AC4 | InMemoryStore removed | InMemoryStore deleted from handler.go |
| AC5 | DB reconnection succeeds after failover | ReconnectingPool tested |
| AC6 | All components handle pod death gracefully | All stores use Redis (shared state) |

- [ ] **Step 4: Commit verification**

```bash
git commit -m "chore: verify HA implementation completeness

- All 13 tasks completed
- Full test suite passes
- golangci-lint clean
- Acceptance criteria verified"
```

---

## Summary

| Task | Files | Lines | Complexity |
|------|-------|-------|------------|
| 1. RedisAuthCtxStore | 3 | ~200 | Low |
| 2. RedisSessionManager | 4 | ~350 | Medium |
| 3. NRF Caching | 2 | ~50 | Low |
| 4. DB Reconnection | 3 | ~200 | Low |
| 5. Verification | - | - | Low |
| **Total** | **12** | **~800** | **~4 hours** |

## Verification Commands

```bash
# Build
go build ./...

# Unit tests
go test ./internal/api/nssaa/... ./internal/eap/... ./internal/nrf/... ./internal/storage/postgres/... -v

# Integration tests (requires Redis)
go test ./... -v -tags=integration

# Lint
golangci-lint run ./...

# Coverage
go test ./... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```
