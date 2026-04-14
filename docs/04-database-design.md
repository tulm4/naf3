# NSSAAF Detail Design - Part 4: Database Design

**Document Version:** 1.0.0
**Date:** 2026-04-13
**Project:** NSSAAF (Network Slice-Specific Authentication and Authorization Function)
**Database:** PostgreSQL 15+ with Citus (for horizontal scaling), Redis Cluster

---

## 1. Database Architecture Overview

### 1.1 High-Level Architecture

```mermaid
graph TB
    subgraph "PostgreSQL Cluster"
        subgraph "Primary Region"
            PG_P["PostgreSQL Primary<br/>:5432"]
            PG_R1["PostgreSQL Replica 1<br/>:5432"]
            PG_R2["PostgreSQL Replica 2<br/>:5432"]
        end
        
        subgraph "DR Region"
            PG_DR["PostgreSQL DR<br/>Async Replica"]
        end
    end

    subgraph "Redis Cluster"
        RD_M["Redis Master<br/>:6379"]
        RD_S1["Redis Replica 1<br/>:6379"]
        RD_S2["Redis Replica 2<br/>:6379"]
        RD_S3["Redis Sentinel"]
    end

    subgraph "Application Layer"
        NSSAAF["NSSAAF Service<br/>Pooled Connections"]
    end

    NSSAAF -->|"Pool: min=10, max=100"| PG_P
    NSSAAF -->|"Pool: min=10, max=50"| PG_R1
    NSSAAF -->|"Pool: min=10, max=50"| PG_R2
    PG_P -.->|"Streaming<br/>Replication"| PG_R1
    PG_P -.->|"Streaming<br/>Replication"| PG_R2
    PG_P -.->|"Async<br/>Replication"| PG_DR

    NSSAAF -->|"Cluster Mode"| RD_M
    RD_M -.->|"Async<br/>Replication"| RD_S1
    RD_M -.->|"Async<br/>Replication"| RD_S2
```

### 1.2 Data Classification

| Data Type | Storage | Retention | Sensitivity |
|-----------|---------|-----------|-------------|
| Auth Context | PostgreSQL + Redis | 24h active, 7d archive | HIGH |
| Audit Logs | PostgreSQL | 90 days | HIGH |
| NF Profile | PostgreSQL | Permanent | MEDIUM |
| Session Keys | Redis (encrypted) | 1h | CRITICAL |
| Rate Limiting | Redis | 1 minute | LOW |
| Config | ConfigMap | N/A | HIGH |

---

## 2. PostgreSQL Schema Design

### 2.1 Core Tables

#### 2.1.1 Authentication Context Table

```sql
-- ===============================================
-- Table: slice_auth_context
-- Purpose: Store slice authentication contexts
-- ===============================================
CREATE TABLE slice_auth_context (
    -- Primary Key
    auth_ctx_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Identifiers
    gpsi VARCHAR(50) NOT NULL,
    supi VARCHAR(25) NOT NULL,
    snssai_sst INTEGER NOT NULL,
    snssai_sd VARCHAR(6),
    
    -- Context Information
    amf_instance_id VARCHAR(50) NOT NULL,
    eap_method VARCHAR(20) NOT NULL, -- 'EAP-AKA', 'EAP-TLS', 'EAP-TTLS'
    
    -- EAP Data (stored as binary)
    eap_challenge BYTEA,
    eap_response BYTEA,
    eap_session_id VARCHAR(64),
    
    -- State Management
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (
        status IN ('PENDING', 'CHALLENGE_SENT', 'AUTHENTICATING', 
                   'SUCCESS', 'FAILURE', 'REVOKED', 'ARCHIVED')
    ),
    
    -- Authentication Result
    auth_result VARCHAR(20),
    auth_failure_reason TEXT,
    
    -- MSK (Master Session Key) - Encrypted
    msk_encrypted BYTEA,
    msk_iv BYTEA,
    
    -- AAA Server Info
    aaa_server_type VARCHAR(10), -- 'RADIUS', 'DIAMETER'
    aaa_server_id VARCHAR(50),
    aaa_transaction_id VARCHAR(64),
    
    -- Callback URLs
    reauth_notif_uri TEXT,
    revoc_notif_uri TEXT,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '5 minutes',
    
    -- Indexes
    CONSTRAINT snssai_composite UNIQUE (gpsi, snssai_sst, snssai_sd, amf_instance_id)
);

-- Indexes for fast lookups
CREATE INDEX idx_auth_ctx_gpsi ON slice_auth_context(gpsi);
CREATE INDEX idx_auth_ctx_supi ON slice_auth_context(supi);
CREATE INDEX idx_auth_ctx_status ON slice_auth_context(status);
CREATE INDEX idx_auth_ctx_created ON slice_auth_context(created_at);
CREATE INDEX idx_auth_ctx_expires ON slice_auth_context(expires_at)
    WHERE status NOT IN ('ARCHIVED', 'REVOKED');

-- GIN Index for JSONB queries (future extensions)
CREATE INDEX idx_auth_ctx_extra ON slice_auth_context USING GIN (extra_data jsonb_path_ops);
```

#### 2.1.2 AIW Authentication Context Table

```sql
-- ===============================================
-- Table: aiw_auth_context
-- Purpose: Store AAA Interworking auth contexts
-- ===============================================
CREATE TABLE aiw_auth_context (
    -- Primary Key
    auth_ctx_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- UE Identifier
    supi VARCHAR(25) NOT NULL,
    
    -- Authentication Method
    auth_method VARCHAR(20) NOT NULL, -- 'EAP-TTLS', 'EAP-PEAP'
    inner_method VARCHAR(20), -- 'PAP', 'CHAP', 'MSCHAPv2'
    
    -- TTLS Data
    ttls_session_id VARCHAR(64),
    ttls_tunnel_established BOOLEAN DEFAULT FALSE,
    ttls_inner_container BYTEA,
    
    -- State
    status VARCHAR(20) NOT NULL DEFAULT 'CREATED' CHECK (
        status IN ('CREATED', 'TTLS_PROCESSING', 'AUTHENTICATING',
                   'MSK_READY', 'SUCCESS', 'FAILURE', 'ARCHIVED')
    ),
    
    -- AAA Server
    aaa_server_type VARCHAR(10) NOT NULL,
    aaa_server_host VARCHAR(255),
    aaa_transaction_id VARCHAR(64),
    
    -- Result
    auth_result VARCHAR(20),
    
    -- MSK (Encrypted)
    msk_encrypted BYTEA,
    msk_iv BYTEA,
    
    -- PVS (Proxy VS Address)
    pvs_info JSONB,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 minutes',
    
    -- Indexes
    CONSTRAINT supi_unique UNIQUE (supi, created_at)
);

CREATE INDEX idx_aiw_ctx_supi ON aiw_auth_context(supi);
CREATE INDEX idx_aiw_ctx_status ON aiw_auth_context(status);
CREATE INDEX idx_aiw_ctx_expires ON aiw_auth_context(expires_at)
    WHERE status NOT IN ('ARCHIVED');
```

#### 2.1.3 NSSAA Subscription Table

```sql
-- ===============================================
-- Table: nssaa_subscription
-- Purpose: Store per-user slice authorization rules
-- ===============================================
CREATE TABLE nssaa_subscription (
    -- Primary Key
    sub_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- UE Identifier
    supi VARCHAR(25) NOT NULL,
    gpsi VARCHAR(50),
    
    -- Slice Information
    snssai_sst INTEGER NOT NULL,
    snssai_sd VARCHAR(6),
    
    -- Subscription Rules
    auth_policy VARCHAR(50) NOT NULL, -- 'ALWAYS_REQUIRE', 'ON_DEMAND', 'BYPASS'
    max_auth_lifetime INTEGER, -- in seconds
    allowed_auth_methods TEXT[], -- ['EAP-AKA', 'EAP-TLS']
    
    -- AAA Server Configuration
    aaa_server_pool VARCHAR(50), -- reference to aaa_server_pool table
    roaming_allowed BOOLEAN DEFAULT TRUE,
    
    -- Status
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'SUSPENDED', 'DELETED')
    ),
    
    -- Audit
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Unique constraint per user + slice
    CONSTRAINT sub_unique UNIQUE (supi, snssai_sst, snssai_sd)
);

CREATE INDEX idx_nssaa_sub_supi ON nssaa_subscription(supi);
CREATE INDEX idx_nssaa_sub_snssai ON nssaa_subscription(snssai_sst, snssai_sd);
CREATE INDEX idx_nssaa_sub_status ON nssaa_subscription(status);
```

#### 2.1.4 AAA Server Pool Table

```sql
-- ===============================================
-- Table: aaa_server_pool
-- Purpose: Manage AAA server configurations
-- ===============================================
CREATE TABLE aaa_server_pool (
    pool_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_name VARCHAR(100) NOT NULL UNIQUE,
    pool_type VARCHAR(20) NOT NULL CHECK (pool_type IN ('RADIUS', 'DIAMETER')),
    
    -- Protocol Config
    protocol_config JSONB NOT NULL, -- {
    --   "auth_port": 1812,
    --   "acct_port": 1813,
    --   "secret": "encrypted_secret",
    --   "transport": "UDP" or "SCTP"
    -- }
    
    -- Load Balancing
    load_balancing_method VARCHAR(20) DEFAULT 'ROUND_ROBIN',
    health_check_interval INTEGER DEFAULT 30,
    
    -- Status
    status VARCHAR(20) DEFAULT 'ACTIVE',
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- ===============================================
-- Table: aaa_server
-- Purpose: Individual AAA server instances
-- ===============================================
CREATE TABLE aaa_server (
    server_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id UUID NOT NULL REFERENCES aaa_server_pool(pool_id),
    
    -- Server Info
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL,
    priority INTEGER DEFAULT 100, -- Lower = higher priority
    weight INTEGER DEFAULT 1, -- For weighted LB
    
    -- Health Status
    status VARCHAR(20) DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'STANDBY', 'MAINTENANCE', 'FAILED')
    ),
    last_health_check TIMESTAMPTZ,
    consecutive_failures INTEGER DEFAULT 0,
    
    -- Statistics
    total_requests BIGINT DEFAULT 0,
    failed_requests BIGINT DEFAULT 0,
    avg_response_time_ms NUMERIC(10,2) DEFAULT 0,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_aaa_server_pool ON aaa_server(pool_id);
CREATE INDEX idx_aaa_server_status ON aaa_server(status);
```

#### 2.1.5 Audit Log Table

```sql
-- ===============================================
-- Table: auth_audit_log
-- Purpose: Immutable audit trail for all auth events
-- ===============================================
CREATE TABLE auth_audit_log (
    -- Primary Key
    log_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Correlation
    trace_id VARCHAR(64) NOT NULL,
    span_id VARCHAR(32),
    
    -- Context Reference
    auth_ctx_id UUID,
    auth_type VARCHAR(20) NOT NULL, -- 'SLICE_AUTH', 'AIW_AUTH', 'REAUTH', 'REVOKE'
    
    -- Actors
    supi VARCHAR(25),
    gpsi VARCHAR(50),
    snssai_sst INTEGER,
    snssai_sd VARCHAR(6),
    amf_instance_id VARCHAR(50),
    
    -- Event Details
    event_type VARCHAR(50) NOT NULL, -- 'AUTH_START', 'AUTH_SUCCESS', 'AUTH_FAILURE', etc.
    event_data JSONB, -- Additional event-specific data
    
    -- Result
    result VARCHAR(20),
    error_code VARCHAR(50),
    error_detail TEXT,
    
    -- Timing
    duration_ms INTEGER,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    
    -- Source
    source_ip INET,
    source_nf_type VARCHAR(20),
    source_nf_id VARCHAR(50)
);

-- Partitioned by time for efficient retention management
CREATE INDEX idx_audit_trace ON auth_audit_log(trace_id);
CREATE INDEX idx_audit_supi ON auth_audit_log(supi);
CREATE INDEX idx_audit_time ON auth_audit_log(started_at DESC);
CREATE INDEX idx_audit_event ON auth_audit_log(event_type);
```

### 2.2 Partitioning Strategy

```sql
-- ===============================================
-- Table: auth_audit_log_partitioned
-- Purpose: Time-based partitioned audit log
-- ===============================================
CREATE TABLE auth_audit_log_partitioned (
    log_id UUID DEFAULT gen_random_uuid(),
    trace_id VARCHAR(64) NOT NULL,
    -- ... (same columns as auth_audit_log)
    
    started_at TIMESTAMPTZ NOT NULL
) PARTITION BY RANGE (started_at);

-- Create monthly partitions
CREATE TABLE auth_audit_log_2026_04 PARTITION OF auth_audit_log_partitioned
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE TABLE auth_audit_log_2026_05 PARTITION OF auth_audit_log_partitioned
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

-- Indexes on partitioned table
CREATE INDEX idx_partitioned_audit_trace ON auth_audit_log_partitioned(trace_id);
CREATE INDEX idx_partitioned_audit_supi ON auth_audit_log_partitioned(supi);
```

### 2.3 High Availability Configuration

```sql
-- ===============================================
-- Streaming Replication Setup
-- ===============================================
-- On Primary:
CREATE PUBLICATION nssaaf_pub FOR ALL TABLES;

-- On Replicas:
CREATE SUBSCRIPTION nssaaf_sub 
    CONNECTION 'host=pg-primary port=5432 dbname=nssaaf user=repl password=xxx'
    PUBLICATION nssaaf_pub
    WITH (copy_data = true);

-- ===============================================
-- Synchronous Commit for Critical Data
-- ===============================================
ALTER TABLE slice_auth_context SET (
    synchronous_commit = 'on'
);

-- ===============================================
-- Row-Level Security (for multi-tenant)
-- ===============================================
ALTER TABLE slice_auth_context ENABLE ROW LEVEL SECURITY;

CREATE POLICY auth_ctx_tenant_isolation ON slice_auth_context
    USING (amf_instance_id IN (
        SELECT amf_instance_id FROM tenant_amf_mapping 
        WHERE tenant_id = current_setting('app.current_tenant')::UUID
    ));
```

---

## 3. Redis Schema Design

### 3.1 Session Cache Structure

```yaml
# Key Patterns and TTLs

# Authentication Context Cache
Key Pattern: "ctx:nssaa:{authCtxId}"
TTL: 300 seconds (5 minutes)
Value:
  {
    "authCtxId": "uuid",
    "gpsi": "msisdn-84-xxx",
    "supi": "imsi-xxx",
    "snssai": {"sst": 1, "sd": "001V01"},
    "status": "CHALLENGE_SENT",
    "eapSessionId": "session-xxx",
    "amfInstanceId": "amf-001",
    "createdAt": "2026-04-13T10:00:00Z"
  }

# Active Authentication Sessions by GPSI
Key Pattern: "user:{gpsi}:active_ctxs"
TTL: 3600 seconds
Value: SET of authCtxId

# MSK Cache (Encrypted)
Key Pattern: "msk:{authCtxId}"
TTL: 3600 seconds
Value: <encrypted_msk_binary>

# AAA Server Health
Key Pattern: "aaa:health:{serverId}"
TTL: 30 seconds
Value:
  {
    "status": "UP",
    "lastCheck": "2026-04-13T10:00:00Z",
    "responseTimeMs": 45,
    "consecutiveFailures": 0
  }

# Rate Limiting
Key Pattern: "ratelimit:{type}:{identifier}"
TTL: 60 seconds
Value: Counter (integer)

# Distributed Lock
Key Pattern: "lock:ctx:{authCtxId}"
TTL: 30 seconds
Value: Lock holder ID

# NF Service Discovery Cache
Key Pattern: "nrf:service:{serviceName}:instances"
TTL: 300 seconds
Value: JSON array of NF instances
```

### 3.2 Redis Cluster Topology

```yaml
# 3-master, 3-replica configuration
Cluster:
  nodes:
    - id: redis-1
      ip: 10.100.1.10
      port: 6379
      role: master
      slots: 0-5460
    
    - id: redis-2
      ip: 10.100.1.11
      port: 6379
      role: master
      slots: 5461-10922
    
    - id: redis-3
      ip: 10.100.1.12
      port: 6379
      role: master
      slots: 10923-16383
    
    - id: redis-4
      ip: 10.100.1.20
      port: 6379
      role: replica
      master: redis-1
    
    - id: redis-5
      ip: 10.100.1.21
      port: 6379
      role: replica
      master: redis-2
    
    - id: redis-6
      ip: 10.100.1.22
      port: 6379
      role: replica
      master: redis-3
```

### 3.3 Redis Sentinel Configuration

```yaml
# sentinel.conf
sentinel monitor nssaaf-cluster redis-1 6379 2
sentinel down-after-milliseconds nssaaf-cluster 5000
sentinel failover-timeout nssaaf-cluster 30000
sentinel parallel-syncs nssaaf-cluster 1

# Application connection string
redis://redis-sentinel:26379,nssaaf-cluster?masterName=nssaaf-cluster
```

---

## 4. Database Connection Management

### 4.1 Connection Pool Configuration

```yaml
# PostgreSQL Connection Pool (pgbouncer or built-in)
PgBouncer:
  pools:
    nssaaf_auth:
      dbname: nssaaf
      host: pgbouncer
      port: 5432
      pool_mode: transaction
      max_client_conn: 1000
      default_pool_size: 50
      min_pool_size: 10
      reserve_pool_size: 10
      reserve_pool_timeout: 5
      max_db_connections: 200
      idle_timeout: 600

  # Application Connection
  dsn: postgresql://app_user:xxx@pgbouncer:5432/nssaaf?sslmode=require
```

### 4.2 Application Code Integration

```go
// Golang Database Connection Example
package db

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "fmt"
    "time"

    _ "github.com/lib/pq"
    "github.com/jmoiron/sqlx"
)

type Config struct {
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
    SSLMode         string
}

func NewConnectionPool(cfg Config) (*sqlx.DB, error) {
    db, err := sqlx.Connect("postgres", fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
    ))
    if err != nil {
        return nil, fmt.Errorf("failed to connect: %w", err)
    }

    db.SetMaxOpenConns(cfg.MaxOpenConns)
    db.SetMaxIdleConns(cfg.MaxIdleConns)
    db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

    return db, nil
}

// Generate MSK Encryption Key
func GenerateMSKKey() ([]byte, error) {
    key := make([]byte, 32) // AES-256
    if _, err := rand.Read(key); err != nil {
        return nil, err
    }
    return key, nil
}

// Encrypt MSK before storage
func EncryptMSK(msk, key []byte) (encrypted, iv []byte, err error) {
    // AES-256-GCM encryption
    // Returns encrypted MSK and IV for storage
}
```

---

## 5. Index Strategy

### 5.1 Query Patterns and Indexes

```sql
-- ===============================================
-- Query 1: Find active context by GPSI
-- Pattern: Look up context for UE during auth
-- ===============================================
CREATE INDEX idx_auth_ctx_active_gpsi ON slice_auth_context(gpsi)
    WHERE status IN ('PENDING', 'CHALLENGE_SENT', 'AUTHENTICATING', 'SUCCESS');

-- ===============================================
-- Query 2: Cleanup expired contexts
-- Pattern: Batch job to clean old contexts
-- ===============================================
CREATE INDEX idx_auth_ctx_expired ON slice_auth_context(expires_at, status)
    WHERE status NOT IN ('ARCHIVED');

-- ===============================================
-- Query 3: Find contexts by AMF
-- Pattern: Audit by AMF instance
-- ===============================================
CREATE INDEX idx_auth_ctx_amf_time ON slice_auth_context(amf_instance_id, created_at DESC);

-- ===============================================
-- Query 4: Statistics by S-NSSAI
-- Pattern: Count auths per slice per hour
-- ===============================================
CREATE INDEX idx_auth_ctx_snssai_hour ON slice_auth_context(
    snssai_sst, 
    date_trunc('hour', created_at)
) INCLUDE (status);
```

### 5.2 Partial Indexes for Performance

```sql
-- Only index active contexts (reduces index size significantly)
CREATE INDEX idx_active_contexts ON slice_auth_context(created_at DESC)
    WHERE status NOT IN ('ARCHIVED', 'REVOKED');

-- Index for re-auth lookups
CREATE INDEX idx_reauth_lookup ON slice_auth_context(supi, snssai_sst, snssai_sd)
    WHERE status = 'SUCCESS';
```

---

## 6. Data Migration and Retention

### 6.1 Retention Policy

```sql
-- ===============================================
-- Function: Archive old authentication contexts
-- ===============================================
CREATE OR REPLACE FUNCTION archive_old_contexts()
RETURNS void AS $$
DECLARE
    cutoff_date TIMESTAMPTZ := NOW() - INTERVAL '7 days';
    batch_size INTEGER := 1000;
    archived_count INTEGER := 0;
BEGIN
    -- Move old success/failure contexts to archive
    WITH archived AS (
        DELETE FROM slice_auth_context
        WHERE status IN ('SUCCESS', 'FAILURE')
          AND updated_at < cutoff_date
        LIMIT batch_size
        RETURNING *
    )
    INSERT INTO slice_auth_context_archive
    SELECT *, NOW()
    FROM archived;
    
    GET DIAGNOSTICS archived_count = ROW_COUNT;
    
    -- Log for monitoring
    RAISE NOTICE 'Archived % contexts older than %', archived_count, cutoff_date;
END;
$$ LANGUAGE plpgsql;

-- ===============================================
-- Table: slice_auth_context_archive
-- ===============================================
CREATE TABLE slice_auth_context_archive (
    LIKE slice_auth_context INCLUDING ALL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (archived_at);

-- Create archive partitions
CREATE TABLE slice_auth_archive_2026_04 PARTITION OF slice_auth_context_archive
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
```

### 6.2 Cleanup Schedule

```yaml
# Cron jobs for database maintenance
cron:
  jobs:
    - name: "Archive old contexts"
      schedule: "0 */6 * * *"  # Every 6 hours
      command: "SELECT archive_old_contexts()"
    
    - name: "Vacuum analyze"
      schedule: "0 3 * * *"  # Daily at 3 AM
      command: "VACUUM ANALYZE slice_auth_context"
    
    - name: "Update statistics"
      schedule: "0 */4 * * *"  # Every 4 hours
      command: "ANALYZE slice_auth_context"
    
    - name: "Check bloat"
      schedule: "0 0 * * 0"  # Weekly
      command: "SELECT check_table_bloat()"
```

---

## 7. Backup and Recovery

### 7.1 Backup Strategy

```yaml
# PostgreSQL Backup Configuration
backup:
  # Continuous WAL Archiving
  wal_archive:
    destination: "/backup/wal/%f"
    compression: "gzip"
    retention: "7 days"
  
  # Base Backup Schedule
  base_backup:
    schedule: "0 2 * * *"  # Daily at 2 AM
    retention: "30 days"
    compression: "pbzip2"
    extra_options: "--wal-method=stream"
  
  # Point-in-Time Recovery (PITR)
  pitr:
    enabled: true
    wal_level: "replica"
    archive_mode: "always"
  
  # Cross-Region Replication
  dr_backup:
    enabled: true
    destination: "s3://backup-dr-region/"
    schedule: "0 4 * * *"  # 4 AM daily
```

### 7.2 Recovery Time Objective (RTO) Targets

| Scenario | RPO | RTO |
|----------|-----|-----|
| Single instance failure | 0 (replica) | < 30 seconds |
| Primary DC failure | < 1 minute | < 5 minutes |
| Region failure | < 15 minutes | < 30 minutes |
| Corrupted data | 0 (PITR) | < 1 hour |

---

## 8. Monitoring Queries

### 8.1 Performance Monitoring

```sql
-- Active authentication contexts
SELECT status, COUNT(*) 
FROM slice_auth_context 
WHERE status NOT IN ('ARCHIVED', 'REVOKED')
GROUP BY status;

-- Average authentication time
SELECT 
    auth_result,
    AVG(EXTRACT(EPOCH FROM (updated_at - created_at))) as avg_seconds
FROM slice_auth_context
WHERE updated_at > NOW() - INTERVAL '1 hour'
GROUP BY auth_result;

-- AAA server response times
SELECT 
    aaa_server_id,
    AVG(response_time_ms) as avg_ms,
    PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY response_time_ms) as p99_ms,
    COUNT(*) as total_requests
FROM aaa_server_metrics
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY aaa_server_id;

-- Slow queries
SELECT 
    query,
    calls,
    mean_time,
    total_time
FROM pg_stat_statements
WHERE query LIKE '%slice_auth_context%'
ORDER BY mean_time DESC
LIMIT 10;
```

---

**Document Author:** NSSAAF Design Team
**Next Document:** Part 5 - High Availability & Kubernetes Deployment
