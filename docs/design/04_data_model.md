---
spec: TS 29.571 v18.2.0 / TS 29.526 v18.7.0
section: §5.4.4.60-61, §7
interface: Internal
service: Internal
operation: N/A (data model)
---

# NSSAAF Data Model & Persistence Design

## 1. Overview

Tài liệu này thiết kế data model cho NSSAAF — bao gồm PostgreSQL schema cho persistent state, Redis cache structure, và serialization format cho EAP session state. Tất cả thiết kế tuân thủ 3GPP TS 29.571 (data types), TS 29.526 (session resources), và TS 28.541 (NRM).

Phạm vi data model bao gồm 2 service contexts:
- **NSSAA** (`Nnssaaf_NSSAA`): Slice authentication per GPSI + S-NSSAI, AMF-triggered.
- **AIW** (`Nnssaaf_AIW`): SNPN primary authentication per SUPI, AUSF-triggered. Khác biệt chính: không có S-NSSAI, có MSK output và pvsInfo.

---

## 2. Core Entities

### 2.1 Entity Relationship Diagram

```
┌─────────────────────────┐       ┌──────────────────────────────┐
│   aaa_server_configs   │       │    slice_auth_sessions       │
│─────────────────────────│       │──────────────────────────────│
│ PK id: UUID           │───┐   │ PK auth_ctx_id: VARCHAR(64)│
│    snssai_sst/sd      │   │   │ FK aaa_config_id: UUID    │◀─┐
│    protocol           │   │   │    gpsi: VARCHAR(32)      │  │
│    aaa_server_host    │   └──▶│    snssai_sst: INTEGER    │  │
│    aaa_server_port    │       │    snssai_sd: VARCHAR(8)  │  │
│    shared_secret      │       │    amf_instance_id        │  │
│    priority           │       │    eap_session_state: BYTEA│  │
│    enabled            │       │    eap_rounds: INTEGER    │  │
│    supi_range_start   │       │    nssaa_status: ENUM     │  │
│    supi_range_end     │       │    created_at             │  │
└─────────────────────────┘       │    expires_at             │  │
                                  │    completed_at           │  │
                                  └──────────────────────────┘  │
                                                                   │
                                  ┌──────────────────────────────┐  │
                                  │    aiw_auth_sessions        │──┘
                                  │──────────────────────────────│
                                  │ PK auth_ctx_id: VARCHAR(64)│
                                  │ FK aaa_config_id: UUID    │◀─┐
                                  │    supi: VARCHAR(32)      │  │
                                  │    msk: BYTEA             │  │
                                  │    pvs_info: JSONB        │  │
                                  │    eap_session_state:BYTEA│  │
                                  │    eap_rounds: INTEGER    │  │
                                  │    nssaa_status: ENUM    │  │
                                  │    created_at            │  │
                                  │    expires_at            │  │
                                  │    completed_at          │  │
                                  └──────────────────────────┘  │
                                                                   │
                                  ┌──────────────────────────────┐  │
                                  │     nssaa_audit_log          │  │
                                  │──────────────────────────────│  │
                                  │ PK id: BIGSERIAL           │  │
                                  │    auth_ctx_id             │  │
                                  │    gpsi_hash: VARCHAR(64)  │  │
                                  │    supi_hash: VARCHAR(64)  │  │
                                  │    snssai_sst/sd          │  │
                                  │    action: VARCHAR(30)     │  │
                                  │    nssaa_status           │  │
                                  │    created_at             │  │
                                  └──────────────────────────────┘  │
                                                                   │
                                  ┌──────────────────────────────┐  │
                                  │      supi_ranges (NRM)       │  │
                                  │──────────────────────────────│  │
                                  │ PK id: UUID                 │  │
                                  │    start_supi / end_supi   │  │
                                  │    aaa_config_id           │  │
                                  │    enabled                 │  │
                                  └──────────────────────────────┘  │
```

### 2.2 NssaaStatus Enum (Source: TS 29.571 §5.4.4.60)

```sql
CREATE TYPE nssaa_status AS ENUM (
    'NOT_EXECUTED',   -- NSSAA chưa chạy cho S-NSSAI này
    'PENDING',        -- NSSAA đang chạy, chờ kết quả cuối
    'EAP_SUCCESS',    -- EAP authentication thành công
    'EAP_FAILURE'     -- EAP authentication thất bại
);

COMMENT ON TYPE nssaa_status IS
    'TS 29.571 §5.4.4.60: NssaaStatus — status of NSSAA for a specific S-NSSAI';
```

---

## 3. PostgreSQL Schema

### 3.1 AAA Server Configuration Table

```sql
-- Static configuration: loaded at startup, rarely updated
CREATE TABLE aaa_server_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Slice identification (composite key)
    snssai_sst      INTEGER NOT NULL,
    snssai_sd       VARCHAR(8),   -- NULL = wildcard (match any SD for this SST)

    -- Protocol
    protocol        VARCHAR(10) NOT NULL CHECK (protocol IN ('RADIUS', 'DIAMETER')),

    -- Primary AAA server
    aaa_server_host VARCHAR(255) NOT NULL,
    aaa_server_port INTEGER NOT NULL CHECK (aaa_server_port BETWEEN 1 AND 65535),

    -- AAA Proxy (optional, for third-party AAA-S)
    aaa_proxy_host  VARCHAR(255),
    aaa_proxy_port  INTEGER CHECK (aaa_proxy_port BETWEEN 1 AND 65535),

    -- Security: encrypted at application layer
    shared_secret   TEXT NOT NULL,

    -- AAA-S authorization check config
    allow_reauth    BOOLEAN NOT NULL DEFAULT TRUE,
    allow_revoke    BOOLEAN NOT NULL DEFAULT TRUE,

    -- Load balancing
    priority        INTEGER NOT NULL DEFAULT 100,  -- Lower = higher priority
    weight          INTEGER NOT NULL DEFAULT 1,

    -- Operational
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints
    UNIQUE (snssai_sst, snssai_sd)
);

CREATE INDEX idx_aaa_config_snssai
    ON aaa_server_configs(snssai_sst, snssai_sd, enabled)
    WHERE enabled = TRUE;

CREATE INDEX idx_aaa_config_priority
    ON aaa_server_configs(priority, weight)
    WHERE enabled = TRUE;

-- Function: lookup AAA config with 3-level fallback
CREATE OR REPLACE FUNCTION get_aaa_config(
    p_sst INTEGER,
    p_sd VARCHAR(8)
) RETURNS aaa_server_configs AS $$
DECLARE
    v_config aaa_server_configs%ROWTYPE;
BEGIN
    -- Level 1: Exact match (SST + SD)
    SELECT * INTO v_config
    FROM aaa_server_configs
    WHERE snssai_sst = p_sst
      AND snssai_sd = p_sd
      AND enabled = TRUE
    ORDER BY priority ASC, weight DESC
    LIMIT 1;

    IF v_config.id IS NOT NULL THEN
        RETURN v_config;
    END IF;

    -- Level 2: SST match only (SD wildcard)
    SELECT * INTO v_config
    FROM aaa_server_configs
    WHERE snssai_sst = p_sst
      AND snssai_sd IS NULL
      AND enabled = TRUE
    ORDER BY priority ASC, weight DESC
    LIMIT 1;

    IF v_config.id IS NOT NULL THEN
        RETURN v_config;
    END IF;

    -- Level 3: Default (all wildcards)
    SELECT * INTO v_config
    FROM aaa_server_configs
    WHERE snssai_sst IS NULL
      AND snssai_sd IS NULL
      AND enabled = TRUE
    ORDER BY priority ASC, weight DESC
    LIMIT 1;

    RETURN v_config;
END;
$$ LANGUAGE plpgsql STABLE;
```

### 3.2 Slice Authentication Session Table

```sql
-- Session table: high-volume, partitioned by month
CREATE TABLE slice_auth_sessions (
    auth_ctx_id        VARCHAR(64) NOT NULL,

    -- Subscriber identification (TS 29.571)
    gpsi               VARCHAR(32) NOT NULL,  -- GPSI pattern: ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$ (TS 29.571 §5.2.2)
    supi               VARCHAR(32),           -- May be resolved later

    -- Slice identification (TS 29.571 Snssai)
    snssai_sst         INTEGER NOT NULL CHECK (snssai_sst BETWEEN 0 AND 255),
    snssai_sd          VARCHAR(8),           -- 6 hex chars, nullable

    -- AMF identification
    amf_instance_id    VARCHAR(64),
    amf_ip             INET,
    amf_region         VARCHAR(16),           -- From AMF instance ID

    -- Callback URIs (TS 29.526)
    reauth_notif_uri   TEXT,
    revoc_notif_uri    TEXT,

    -- AAA configuration reference
    aaa_config_id      UUID NOT NULL REFERENCES aaa_server_configs(id),

    -- EAP session state (serialized binary blob)
    -- Format: proto-tlv encoded
    eap_session_state  BYTEA NOT NULL,

    -- Session counters
    eap_rounds         INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds     INTEGER NOT NULL DEFAULT 20,
    eap_last_nonce     VARCHAR(64),           -- For duplicate detection

    -- Auth result (TS 29.571 NssaaStatus)
    nssaa_status       nssaa_status NOT NULL DEFAULT 'NOT_EXECUTED',
    auth_result        nssaa_status,          -- Final result (EAP_SUCCESS/FAILURE)

    -- Failure tracking
    failure_reason     TEXT,
    failure_cause      VARCHAR(50),

    -- Audit
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at         TIMESTAMPTZ NOT NULL,
    completed_at       TIMESTAMPTZ,
    terminated_at      TIMESTAMPTZ,

    -- Constraint
    PRIMARY KEY (auth_ctx_id)
) PARTITION BY RANGE (created_at);

-- Indexes
CREATE INDEX idx_sessions_gpsi_snssai
    ON slice_auth_sessions(gpsi, snssai_sst, snssai_sd);

CREATE INDEX idx_sessions_nssaa_status
    ON slice_auth_sessions(nssaa_status)
    WHERE nssaa_status IN ('PENDING', 'NOT_EXECUTED');

CREATE INDEX idx_sessions_expires
    ON slice_auth_sessions(expires_at)
    WHERE completed_at IS NULL;

CREATE INDEX idx_sessions_created
    ON slice_auth_sessions(created_at DESC);

-- Partition management function
CREATE OR REPLACE FUNCTION create_monthly_partition()
RETURNS void AS $$
DECLARE
    v_partition_name TEXT;
    v_start_date DATE;
    v_end_date DATE;
BEGIN
    v_start_date := DATE_TRUNC('month', NOW() + INTERVAL '1 month');
    v_end_date := v_start_date + INTERVAL '1 month';
    v_partition_name := 'slice_auth_sessions_' || TO_CHAR(v_start_date, 'YYYY_MM');

    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF slice_auth_sessions
         FOR VALUES FROM (%L) TO (%L)',
        v_partition_name,
        v_start_date,
        v_end_date
    );
END;
$$ LANGUAGE plpgsql;

-- Create partitions for next 12 months
SELECT create_monthly_partition()
FROM generate_series(0, 11);
```

### 3.3 Audit Log Table

```sql
-- Immutable append-only audit log
CREATE TABLE nssaa_audit_log (
    id              BIGSERIAL PRIMARY KEY,

    -- Session context
    auth_ctx_id     VARCHAR(64),

    -- GPSI hashed for GDPR/privacy compliance (SHA-256, first 16 bytes hex)
    gpsi_hash       VARCHAR(64) NOT NULL,

    -- Slice
    snssai_sst      INTEGER,
    snssai_sd       VARCHAR(8),

    -- Actor
    amf_instance_id VARCHAR(64),
    amf_ip          INET,

    -- Action
    action          VARCHAR(30) NOT NULL CHECK (
        action IN (
            'SESSION_CREATED',
            'EAP_ROUND_ADVANCED',
            'EAP_SUCCESS',
            'EAP_FAILURE',
            'SESSION_EXPIRED',
            'SESSION_TERMINATED',
            'NOTIF_REAUTH_SENT',
            'NOTIF_REAUTH_ACK',
            'NOTIF_REAUTH_FAILED',
            'NOTIF_REVOC_SENT',
            'NOTIF_REVOC_ACK',
            'NOTIF_REVOC_FAILED',
            'AAA_CONNECTED',
            'AAA_FAILED'
        )
    ),

    -- Result
    nssaa_status    VARCHAR(20),
    error_code      INTEGER,
    error_message   TEXT,

    -- Meta
    request_id      VARCHAR(64),
    correlation_id  VARCHAR(64),
    client_ip       INET,
    user_agent      TEXT,

    -- Timestamp
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partition by month (audit logs grow large)
CREATE TABLE nssaa_audit_log_2025 PARTITION OF nssaa_audit_log
    FOR VALUES FROM ('2025-01-01') TO ('2026-01-01');

CREATE INDEX idx_audit_gpsi_created
    ON nssaa_audit_log(gpsi_hash, created_at DESC);

CREATE INDEX idx_audit_ctx_created
    ON nssaa_audit_log(auth_ctx_id, created_at DESC);

CREATE INDEX idx_audit_action_created
    ON nssaa_audit_log(action, created_at DESC);

-- Prevent updates/deletes (immutable)
CREATE OR REPLACE FUNCTION prevent_audit_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Audit log entries cannot be modified or deleted';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_immutable
    BEFORE UPDATE OR DELETE ON nssaa_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();
```

### 3.4 Supi Ranges Table (NRM)

```sql
-- Per TS 28.541 §5.3.146: NssaafInfo.supiRanges
CREATE TABLE supi_ranges (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    range_name      VARCHAR(64) NOT NULL,

    -- Range boundaries
    start_supi      VARCHAR(32) NOT NULL,
    end_supi        VARCHAR(32) NOT NULL,

    -- PLMN binding
    plmn_mcc        VARCHAR(3),
    plmn_mnc        VARCHAR(3),

    -- Operational
    enabled         BOOLEAN DEFAULT TRUE,
    description     TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),

    CONSTRAINT supi_range_valid CHECK (start_supi <= end_supi)
);

CREATE INDEX idx_supi_ranges_supi
    ON supi_ranges(start_supi, end_supi)
    WHERE enabled = TRUE;
```

### 3.5 AIW AAA Server Configuration (Nnssaaf_AIW)

AIW uses the same `aaa_server_configs` table as NSSAA, but with a different lookup key. Where NSSAA resolves by `(snssai_sst, snssai_sd)`, AIW resolves by SUPI range. The `supi_ranges` table (NRM, §3.4) provides the mapping from SUPI to `aaa_config_id`.

> **Note:** SUPI range → AAA config mapping is **operator-specific configuration**, not defined in 3GPP specs. The 3GPP specs (TS 29.526 §7.3) define the API interface but not the internal routing logic between SUPI and AAA servers.

```sql
-- AIW AAA config lookup: SUPI → supi_ranges → aaa_server_configs
-- Resolution order:
--   1. Exact SUPI range match (most specific)
--   2. Default entry (supi_range_start = NULL, supi_range_end = NULL)

CREATE OR REPLACE FUNCTION get_aaa_config_for_aiw(p_supi VARCHAR(32))
RETURNS aaa_server_configs AS $$
DECLARE
    v_range supi_ranges%ROWTYPE;
    v_config aaa_server_configs%ROWTYPE;
BEGIN
    -- Step 1: Find matching SUPI range (longest prefix match)
    SELECT * INTO v_range
    FROM supi_ranges
    WHERE enabled = TRUE
      AND start_supi <= p_supi
      AND end_supi >= p_supi
    ORDER BY length(start_supi) DESC  -- Most specific range first
    LIMIT 1;

    IF v_range.id IS NOT NULL THEN
        SELECT * INTO v_config
        FROM aaa_server_configs
        WHERE id = v_range.aaa_config_id AND enabled = TRUE;
        IF v_config.id IS NOT NULL THEN
            RETURN v_config;
        END IF;
    END IF;

    -- Step 2: Fall back to default AIW AAA config
    SELECT * INTO v_config
    FROM aaa_server_configs
    WHERE snssai_sst IS NULL
      AND snssai_sd IS NULL
      AND enabled = TRUE
    ORDER BY priority ASC, weight DESC
    LIMIT 1;

    RETURN v_config;
END;
$$ LANGUAGE plpgsql STABLE;

-- Index: supi_ranges supports both NSSAA (SUPI-based) and AIW lookups
CREATE INDEX idx_supi_ranges_aiw
    ON supi_ranges(start_supi, end_supi DESC, aaa_config_id)
    WHERE enabled = TRUE;
```

### 3.6 AIW Authentication Session Table

```sql
-- AIW session: per-SUPI SNPN authentication (Nnssaaf_AIW, TS 29.526 §7.3)
-- Key differences from slice_auth_sessions:
--   - No S-NSSAI (AIW is per-user, not per-slice)
--   - No AMF callbacks (no reauth/revocation for AIW)
--   - MSK and pvsInfo stored on success
--   - Partitioned by month (same as slice_auth_sessions)

CREATE TABLE aiw_auth_sessions (
    auth_ctx_id        VARCHAR(64) NOT NULL,

    -- Subscriber identification (TS 29.571 — SUPI, imu-scheme)
    supi               VARCHAR(32) NOT NULL,  -- pattern: ^imu-[0-9]{15}$

    -- AAA configuration reference
    aaa_config_id      UUID NOT NULL REFERENCES aaa_server_configs(id),

    -- EAP session state (serialized binary blob)
    eap_session_state  BYTEA NOT NULL,

    -- Session counters
    eap_rounds         INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds     INTEGER NOT NULL DEFAULT 20,
    eap_last_nonce     VARCHAR(64),           -- sha256(eap_payload) for retry detection

    -- Auth result
    nssaa_status       nssaa_status NOT NULL DEFAULT 'PENDING',
    auth_result        nssaa_status,          -- Final result

    -- MSK: derived from EAP-TLS on Success (RFC 5216 §2.1.4)
    -- Stored encrypted at application layer; NULL if not EAP-TLS or on Failure
    msk                BYTEA,

    -- pvsInfo: Privacy-Violating Servers info from AAA-S (TS 29.526 §7.3.3)
    -- JSON array: [{"serverType":"PROSE","serverId":"pvs-001"},...]
    pvs_info           JSONB,

    -- EAP-TTLS inner method container (optional)
    ttls_inner_container BYTEA,

    -- Supported features echo (from request)
    supported_features VARCHAR(64),

    -- Audit
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at         TIMESTAMPTZ NOT NULL,
    completed_at       TIMESTAMPTZ,

    PRIMARY KEY (auth_ctx_id)
) PARTITION BY RANGE (created_at);

-- Indexes
CREATE INDEX idx_aiw_sessions_supi
    ON aiw_auth_sessions(supi);

CREATE INDEX idx_aiw_sessions_status
    ON aiw_auth_sessions(nssaa_status)
    WHERE nssaa_status = 'PENDING';

CREATE INDEX idx_aiw_sessions_expires
    ON aiw_auth_sessions(expires_at)
    WHERE completed_at IS NULL;

-- Partition management: reuse same function as slice_auth_sessions
-- but applied to aiw_auth_sessions partition root
SELECT create_monthly_partition();  -- creates aiw_auth_sessions_YYYY_MM partitions
```

### 3.7 Audit Log Extensions for AIW

```sql
-- Extend nssaa_audit_log with AIW-specific action types
-- (add via ALTER TYPE in migration; enum values cannot be added to existing rows)

ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'AIW_SESSION_CREATED';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'AIW_SESSION_COMPLETED';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'AIW_MSK_GENERATED';
ALTER TYPE audit_action ADD VALUE IF NOT EXISTS 'AIW_SESSION_EXPIRED';

-- Note: AIW audit log uses supi_hash (not gpsi_hash)
-- supi_hash: SHA-256(first 16 bytes of supi), hex encoded
-- For AIW rows: supi_hash populated, gpsi_hash = NULL
-- For NSSAA rows: gpsi_hash populated, supi_hash = NULL
-- (enforced via partial triggers if needed)
```

---

## 4. EAP Session State Serialization

### 4.1 State Structure

```go
// EAP Session State — serialized as Protocol Buffers (proto3)
message EapSessionState {
    // Session identification
    string auth_ctx_id = 1;

    // EAP method
    string method = 2;  // "EAP-TLS", "EAP-TTLS", "EAP-AKA_PRIME"

    // Method-specific state
    oneof method_state {
        EapTlsState tls_state = 10;
        EapTtlsState ttls_state = 11;
        EapAkaPrimeState akap_state = 12;
    }

    // Current position in EAP exchange
    EapRound current_round = 3;
    EapMessage last_received = 4;
    EapMessage last_sent = 5;

    // TLS session resumption
    bytes tls_session_id = 6;
    bytes tls_master_secret = 7;  // encrypted at rest

    // Round tracking
    int32 round_count = 8;
    google.protobuf.Timestamp last_activity = 9;
}

message EapTlsState {
    // TLS handshake state
    uint32 tls_version = 1;       // 0x0303 = TLS 1.2, 0x0304 = TLS 1.3
    bytes client_random = 2;
    bytes server_random = 3;
    bytes server_certificate = 4;
    bytes client_certificate = 5;  // encrypted

    // TLS flags
    bool certificate_requested = 6;
    bool certificate_authorities_received = 7;
    repeated bytes trusted_ca = 8;

    // Keying material
    bytes pre_master_secret = 9;  // encrypted
    bytes master_secret = 10;     // encrypted
    bytes msk = 11;               // Master Session Key (RFC 5216) — encrypted
}

message EapRound {
    int32 number = 1;
    string direction = 2;  // "INBOUND" (from UE) or "OUTBOUND" (to UE)
    string source = 3;     // "AMF", "AAA_SERVER", "INTERNAL"
    string eap_code = 4;   // "REQUEST", "RESPONSE", "SUCCESS", "FAILURE"
    bytes payload = 5;     // Raw EAP packet
    google.protobuf.Timestamp timestamp = 6;
}
```

### 4.2 Serialization Strategy

```go
// Encode: EAP session state → binary → encrypt → store
func (s *EapSessionState) Serialize() ([]byte, error) {
    // 1. Serialize to protobuf binary
    raw, err := proto.Marshal(s)
    if err != nil {
        return nil, err
    }

    // 2. Encrypt with AES-256-GCM using per-session KEK
    kek := deriveKEK(s.AuthCtxId, masterKey)
    return encryptAEAD(kek, raw)
}

// Decrypt + deserialize
func DeserializeSession(data []byte, authCtxId string) (*EapSessionState, error) {
    kek := deriveKEK(authCtxId, masterKey)
    raw, err := decryptAEAD(kek, data)
    if err != nil {
        return nil, err
    }

    state := &EapSessionState{}
    if err := proto.Unmarshal(raw, state); err != nil {
        return nil, err
    }
    return state, nil
}
```

---

## 5. Redis Cache Architecture

> **Note (Phase R):** In the 3-component model, Redis additionally serves as the cross-component coordination layer. See `internal/proto/biz_callback.go` for the session correlation key definitions.

### 5.1 Key Schema

```
Namespace: nssaa:{environment}

### 5.1.1 Cross-Component Session Correlation (Phase R)

The following keys coordinate the 3-component model (Biz Pods ↔ AAA Gateway ↔ AAA-S) via Redis:

```
nssaa:session:{sessionId}
  Type: String (JSON)
  TTL: 600s (10 minutes) = proto.DefaultPayloadTTL
  Value: SessionCorrEntry struct (JSON)
  Purpose: Correlates a RADIUS/Diameter session ID with the NSSAAF authCtxId
  Written by: AAA Gateway before forwarding to AAA-S
  Read by: AAA Gateway on response arrival or server-initiated routing
  Fields:
    - authCtxId: string  // NSSAAF auth context ID
    - podId: string       // Biz Pod hostname (observability only; NOT used for routing)
    - sst: uint8          // S-NSSAI SST
    - sd: string          // S-NSSAI SD
    - createdAt: int64    // Unix timestamp
  Source: internal/proto/biz_callback.go (SessionCorrEntry)

nssaa:pods
  Type: Set
  TTL: None (managed by heartbeat)
  Members: Live Biz Pod hostnames
  Purpose: Track which Biz Pods are live (observability / future load balancing)
  Updated by: Biz Pod heartbeat (SADD every 30s), cleaned up on shutdown (SREM)
  Source: internal/proto/biz_callback.go (PodsKey)

nssaa:aaa-response
  Type: Pub/Sub channel
  TTL: N/A
  Purpose: Cross-component response routing
  Publisher: AAA Gateway (when response arrives from AAA-S)
  Subscribers: All Biz Pods (each discards non-matching events)
  Message: AaaResponseEvent struct (JSON)
    - version: string
    - sessionId: string
    - authCtxId: string
    - payload: []byte  // raw response from AAA-S
  Source: internal/proto/biz_callback.go (AaaResponseChannel, AaaResponseEvent)
```

### 5.1.2 Session Hot Cache (Phase 3)

{nssaa}:session:{authCtxId}
  Type: Hash
  TTL: 300s (5 min)
  Fields:
    - gpsi: string
    - snssai: "{sst}:{sd}"
    - status: "PENDING|EAP_SUCCESS|EAP_FAILURE"
    - aaaConfigId: UUID
    - eapRounds: int
    - updatedAt: timestamp
  Purpose: Hot cache for active sessions

### 5.1.3 AIW Session Hot Cache

{nssaa}:aiw:session:{authCtxId}
  Type: Hash
  TTL: 300s (5 min)
  Fields:
    - supi: string
    - status: "PENDING|EAP_SUCCESS|EAP_FAILURE"
    - aaaConfigId: UUID
    - eapRounds: int
    - mskSet: bool           -- true when MSK is present
    - pvsInfoSize: int        -- number of pvsInfo entries
    - updatedAt: timestamp
  Purpose: Hot cache for active AIW sessions

### 5.1.4 Operational Keys

{nssaa}:idempotency:{authCtxId}:{msgHash}
  Type: String (JSON)
  TTL: 3600s (1 hour)
  Value: Cached response body
  Purpose: PUT idempotency

{nssaa}:aaa:health:{configId}
  Type: String
  TTL: 30s
  Value: "UP|DOWN|DEGRADED"
  Purpose: Circuit breaker state

{nssaa}:aaa:circuit:{configId}
  Type: Hash
  TTL: none (persistent)
  Fields:
    - state: CLOSED|OPEN|HALF_OPEN
    - failures: int
    - lastFailure: timestamp
  Purpose: Per-AAA-server circuit breaker

{nssaa}:ratelimit:amf:{amfInstanceId}
  Type: String (counter)
  TTL: 5s
  Purpose: Per-AMF rate limiting (sliding window)

{nssaa}:ratelimit:gpsi:{gpsiHash}
  Type: String (counter)
  TTL: 60s
  Purpose: Per-GPSI rate limiting

{nssaa}:ratelimit:aiw:supi:{supiHash}
  Type: String (counter)
  TTL: 60s
  Purpose: Per-SUPI rate limiting for AIW

{nssaa}:ratelimit:aiw:ausf:{ausfId}
  Type: String (counter)
  TTL: 5s
  Purpose: Per-AUSF rate limiting for AIW

{nssaa}:ratelimit:global
  Type: String (counter)
  TTL: 1s
  Purpose: Global rate limiting
```

### 5.2 Cache-Aside Pattern

```go
// Read-through cache with write-through
func GetSession(ctx context.Context, authCtxId string) (*Session, error) {
    // 1. Try Redis first
    cached, err := redis.Get(ctx, "nssaa:session:"+authCtxId)
    if err == nil && cached != nil {
        return deserializeSession(cached)
    }

    // 2. Fallback to PostgreSQL
    session, err := pg.GetSession(ctx, authCtxId)
    if err != nil {
        return nil, err
    }

    // 3. Populate cache (async, non-blocking)
    go func() {
        redis.Set(ctx, "nssaa:session:"+authCtxId, serializeSession(session), 5*time.Minute)
    }()

    return session, nil
}

// Write-through: update both PG and Redis
func UpdateSession(ctx context.Context, session *Session) error {
    // 1. Write to PostgreSQL (primary)
    if err := pg.UpdateSession(ctx, session); err != nil {
        return err
    }

    // 2. Update Redis cache (async)
    go func() {
        redis.Set(ctx, "nssaa:session:"+session.AuthCtxId, serializeSession(session), 5*time.Minute)
    }()

    return nil
}

// AIW session: same cache-aside pattern, different key prefix
func GetAIWSession(ctx context.Context, authCtxId string) (*AIWSession, error) {
    // 1. Try Redis first
    cached, err := redis.Get(ctx, "nssaa:aiw:session:"+authCtxId)
    if err == nil && cached != nil {
        return deserializeAIWSession(cached)
    }

    // 2. Fallback to PostgreSQL
    session, err := pg.GetAIWSession(ctx, authCtxId)
    if err != nil {
        return nil, err
    }

    // 3. Populate cache (async, non-blocking)
    go func() {
        redis.Set(ctx, "nssaa:aiw:session:"+authCtxId, serializeAIWSession(session), 5*time.Minute)
    }()

    return session, nil
}
```

---

## 6. Data Retention & Privacy

### 6.1 Retention Policy

| Table | Retention | Action |
|-------|-----------|--------|
| `slice_auth_sessions` | 90 days | DELETE (PG partition drop) |
| `aiw_auth_sessions` | 90 days | DELETE (PG partition drop) |
| `nssaa_audit_log` | 2 years | DELETE (PG partition drop) |
| MSK (in `aiw_auth_sessions`) | 24 hours after success | Encrypted, auto-purge |
| `eap_session_state` (in-memory) | Until session complete | Encrypted, no persistence |
| Redis cache | 5 min TTL | Auto-eviction |
| EAP payload (audit) | 24 hours | Encrypted blob |

### 6.2 GPSI Privacy

GPSI không bao giờ được lưu plaintext trong audit log hoặc logs. Luôn hash trước khi lưu:

```go
// GPSI hashed with SHA-256, truncated to first 16 bytes
func HashGpsi(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return hex.EncodeToString(h[:16])
}
```

### 6.3 SUPI Privacy

SUPI (imu-scheme) cũng không bao giờ được lưu plaintext trong audit log hoặc logs. Sử dụng cùng hash scheme như GPSI:

```go
// SUPI hashed with SHA-256, truncated to first 16 bytes
// Used for AIW audit logs and rate limiting
func HashSupi(supi string) string {
    h := sha256.Sum256([]byte(supi))
    return hex.EncodeToString(h[:16])
}
```

### 6.4 MSK Handling

MSK (Master Session Key) từ EAP-TLS được lưu encrypted trong `aiw_auth_sessions.msk` sau khi authentication thành công. MSK bị xóa sau 24h hoặc khi session hết hạn. MSK KHÔNG bao giờ được log hoặc gửi qua API trong plaintext.

### 6.5 Encryption at Rest

```go
// All sensitive fields encrypted with AES-256-GCM
type EncryptedField struct {
    Ciphertext   []byte   // encrypted data
    IV           []byte   // 12-byte nonce
    AuthTag     []byte   // 16-byte auth tag
    Version     uint32   // key version for rotation
}

// Fields requiring encryption:
// - eap_session_state (contains MSK, keys)
// - shared_secret (AAA credentials)
// - eap payloads in audit log
```

---

## 7. Connection Pool Configuration

### 7.1 PostgreSQL (PgBouncer)

```ini
[databases]
nssAAF = host=pg-primary port=5432 dbname=nssAAF

[nssAAF]
pool_mode = transaction
max_client_conn = 5000
default_pool_size = 100
min_pool_size = 20
reserve_pool_size = 20
reserve_pool_timeout = 5
max_db_connections = 500
server_lifetime = 3600
server_idle_timeout = 600
query_timeout = 5
```

### 7.2 Redis

```yaml
redis_cluster:
  nodes:
    - host: 10.0.1.10:6379   # AZ1 primary
    - host: 10.0.1.11:6379   # AZ1 replica
    - host: 10.0.2.10:6379   # AZ2 primary
    - host: 10.0.2.11:6379   # AZ2 replica
    - host: 10.0.3.10:6379   # AZ3 primary
    - host: 10.0.3.11:6379   # AZ3 replica

  shards: 3
  replicas_per_shard: 2
  write_consistency: ONE
  read_consistency: ONE

  pool:
    max_connections: 200
    connect_timeout: 5s
    read_timeout: 1s
    write_timeout: 1s
```

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Session stored partitioned by month | PG range partition on created_at |
| AC2 | GPSI hashed in audit log | SHA-256 truncation, never plaintext |
| AC3 | EAP state encrypted at rest | AES-256-GCM, KEK per session |
| AC4 | AAA config 3-level fallback lookup | Exact → SST-only → Default |
| AC5 | Redis cache TTL 5 min | nssaa:session:{id} with 300s TTL |
| AC6 | Idempotency key = authCtxId + msgHash | 1h TTL in Redis |
| AC7 | Connection pool: 100 PG connections per instance | PgBouncer transaction mode |
| AC8 | Partition auto-creation for next 12 months | create_monthly_partition() function |
