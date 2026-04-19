# Phase 3: Data & Storage — PostgreSQL & Redis

## Overview

Phase 3 xây dựng persistence layer: PostgreSQL schemas và Redis cache.

## Modules to Implement

### 1. `internal/storage/postgres/` — PostgreSQL Repository

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/04_data_model.md`, `docs/design/11_database_ha.md`

**Deliverables:**
- [x] `session.go` — Session repository
- [x] `aaa_config.go` — AAA config repository
- [x] `audit.go` — Audit log repository
- [x] `migrations/` — Database migrations
- [x] `pool.go` — Connection pool management
- [x] `session_test.go` — Unit tests

### 2. `internal/storage/postgres/migrations/` — Migrations

**Priority:** P0

**Deliverables:**
- [x] `000001_create_sessions_table.up.sql`
- [x] `000002_create_aaa_configs_table.up.sql`
- [x] `000003_create_audit_log_table.up.sql`
- [x] `000004_create_indexes.up.sql`
- [x] `migrate.go` — Migration runner

**Key Tables:**
```sql
-- Session table (partitioned by month)
CREATE TABLE slice_auth_sessions (
    auth_ctx_id    VARCHAR(64) PRIMARY KEY,
    gpsi          VARCHAR(32) NOT NULL,
    snssai_sst    INTEGER NOT NULL,
    snssai_sd     VARCHAR(8),
    aaa_config_id UUID NOT NULL,
    eap_session_state BYTEA NOT NULL,  -- encrypted
    nssaa_status  VARCHAR(20) NOT NULL DEFAULT 'NOT_EXECUTED',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL
) PARTITION BY RANGE (created_at);

-- AAA config table
CREATE TABLE aaa_server_configs (
    id              UUID PRIMARY KEY,
    snssai_sst      INTEGER NOT NULL,
    snssai_sd       VARCHAR(8),  -- NULL = wildcard
    protocol        VARCHAR(10) NOT NULL,  -- RADIUS or DIAMETER
    aaa_server_host VARCHAR(255) NOT NULL,
    aaa_server_port INTEGER NOT NULL,
    shared_secret   TEXT NOT NULL,  -- encrypted
    UNIQUE (snssai_sst, snssai_sd)
);
```

### 3. `internal/cache/redis/` — Redis Cache

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/04_data_model.md`, `docs/design/12_redis_ha.md`

**Deliverables:**
- [x] `session_cache.go` — Session hot cache
- [x] `idempotency.go` — Idempotency cache
- [x] `ratelimit.go` — Rate limiter
- [x] `lock.go` — Distributed lock
- [x] `pool.go` — Redis cluster pool
- [x] `cache_test.go` — Unit tests

**Key Redis Keys:**
```
nssaa:session:{authCtxId}    → Hash (5 min TTL)
nssaa:idempotency:{ctxId}:{msgHash} → String JSON (1 hour TTL)
nssaa:ratelimit:gpsi:{gpsiHash} → Counter (1 min TTL)
nssaa:ratelimit:amf:{amfId} → Counter (5 sec TTL)
nssaa:lock:session:{authCtxId} → String NX (30 sec TTL)
nssaa:session-corr:{sessionId} → Hash (authCtxID, podID, snssai) (5 min TTL)
```

**Note (Phase R):** The `nssaa:session-corr:{sessionId}` key is used for routing AAA-S responses back to the correct Biz Pod via Redis pub/sub. See `docs/roadmap/PHASE_Refactor_3Component.md` Phase 1 for details.

## Validation Checklist

- [x] Monthly partitions auto-created
- [x] GPSI hashed in audit log (SHA-256)
- [x] Session state encrypted at rest (AES-256-GCM)
- [x] Redis TTL: session 5min, idempotency 1h
- [x] Sliding window rate limiting implemented
- [ ] Unit test coverage >80%
