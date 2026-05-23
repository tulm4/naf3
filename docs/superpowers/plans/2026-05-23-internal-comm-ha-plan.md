# Internal Communication HA Implementation Plan

## Overview

Implementation plan for high availability improvements in NSSAAF internal communication, based on `docs/superpowers/specs/2026-05-23-internal-comm-ha-design.md`.

## Task Breakdown

### Phase 1: Remove InMemoryStore (Low Risk)

**Task 1.1: Create RedisAuthCtxStore**
- File: `internal/api/nssaa/redis_store.go`
- Implement `AuthCtxStore` interface backed by Redis
- 24-hour TTL for auth contexts
- Keys: `nssaa:auth:ctx:{authCtxId}`

**Task 1.2: Update Handler to Use Redis Store**
- File: `internal/api/nssaa/handler.go`
- Replace `InMemoryStore` initialization with `NewRedisAuthCtxStore`
- Remove `InMemoryStore` struct and `NewInMemoryStore`

**Task 1.3: Add Integration Tests**
- File: `internal/api/nssaa/handler_redis_test.go`
- Test store CRUD operations
- Test handler with Redis store

---

### Phase 2: NRF Caching (Low Risk)

**Task 2.1: Create CachedNRFClient**
- File: `internal/nrf/cache.go`
- Wrap existing NRF `Client` with TTL-based caching
- 5-minute default TTL
- Return cached data on NRF failure (graceful degradation)

**Task 2.2: Add Cache Configuration**
- File: `internal/config/config.go`
- Add `NRFCacheTTL` configuration option

**Task 2.3: Add Cache Metrics**
- File: `internal/metrics/nrf.go`
- Track cache hits/misses
- Track NRF failures served from cache

---

### Phase 3: EAP Session Persistence (Medium Risk)

**Task 3.1: Create RedisSessionManager**
- File: `internal/eap/session_redis.go`
- Implement `SessionStore` interface backed by Redis
- 5-minute TTL for EAP sessions
- Keys: `nssaa:eap:session:{authCtxId}`

**Task 3.2: Add SessionTTL Configuration**
- File: `internal/config/eap.go`
- Add `SessionTTL` configuration option

**Task 3.3: Update Engine for Redis Sessions**
- File: `internal/eap/engine.go`
- Add constructor `NewEngineWithRedis(cfg, redisClient, ...)`
- Keep `NewEngine` for backward compatibility (in-memory)

**Task 3.4: Verify Idempotency**
- Test: AMF retry returns cached response without re-forwarding to AAA-S
- Confirm retry detection mechanism works across pods

---

### Phase 4: DB Reconnection (Low Risk)

**Task 4.1: Create ReconnectingPool Wrapper**
- File: `internal/storage/postgres/reconnect.go`
- Implement `ExecuteWithRetry` method
- Classify connection errors (pgx error codes)
- Exponential backoff reconnection

**Task 4.2: Add Reconnection Metrics**
- File: `internal/metrics/database.go`
- Track reconnection attempts
- Track successful reconnections

**Task 4.3: Integration Test**
- File: `internal/storage/postgres/reconnect_test.go`
- Simulate connection failure
- Verify automatic reconnection

---

## Implementation Order

```
┌─────────────────────────────────────────────────────────────────┐
│  Phase 1: InMemoryStore → Redis                                  │
│  ├── 1.1 Create RedisAuthCtxStore                                │
│  ├── 1.2 Update Handler                                          │
│  └── 1.3 Add tests                                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Phase 2: NRF Caching                                           │
│  ├── 2.1 Create CachedNRFClient                                 │
│  ├── 2.2 Add configuration                                      │
│  └── 2.3 Add metrics                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Phase 3: EAP Session Persistence                                │
│  ├── 3.1 Create RedisSessionManager                              │
│  ├── 3.2 Add configuration                                      │
│  ├── 3.3 Update Engine                                          │
│  └── 3.4 Verify idempotency                                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Phase 4: DB Reconnection                                       │
│  ├── 4.1 Create ReconnectingPool                                │
│  ├── 4.2 Add metrics                                           │
│  └── 4.3 Integration test                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Verification Criteria

| # | Criterion | Verification |
|---|-----------|--------------|
| V1 | `go build ./...` compiles | `go build ./... && echo "OK"` |
| V2 | `go test ./internal/api/nssaa/...` passes | Unit tests for handler + store |
| V3 | `go test ./internal/eap/...` passes | Session persistence tests |
| V4 | `go test ./internal/nrf/...` passes | NRF cache tests |
| V5 | `go test ./internal/storage/...` passes | Reconnection tests |
| V6 | `golangci-lint run ./...` passes | No lint errors |

## Files to Create/Modify

### New Files

| File | Description |
|------|-------------|
| `internal/api/nssaa/redis_store.go` | Redis-backed AuthCtxStore |
| `internal/api/nssaa/handler_redis_test.go` | Integration tests |
| `internal/nrf/cache.go` | NRF client with TTL caching |
| `internal/eap/session_redis.go` | Redis-backed session manager |
| `internal/storage/postgres/reconnect.go` | Reconnection wrapper |
| `internal/storage/postgres/reconnect_test.go` | Reconnection tests |

### Modified Files

| File | Changes |
|------|---------|
| `internal/api/nssaa/handler.go` | Use Redis store, remove InMemoryStore |
| `internal/config/config.go` | Add NRF cache TTL config |
| `internal/config/eap.go` | Add session TTL config |
| `internal/eap/engine.go` | Add NewEngineWithRedis constructor |
| `internal/metrics/nrf.go` | Add NRF cache metrics |
| `internal/metrics/database.go` | Add reconnection metrics |

## Dependencies

- Redis: `github.com/redis/go-redis/v9`
- pgx: `github.com/jackc/pgx/v5`
- Existing config patterns from `internal/config/`

## Estimated Effort

| Phase | Tasks | Complexity |
|-------|-------|------------|
| 1: InMemoryStore → Redis | 3 | Low |
| 2: NRF Caching | 3 | Low |
| 3: EAP Session Persistence | 4 | Medium |
| 4: DB Reconnection | 3 | Low |
| **Total** | **13** | **~2-3 days** |
