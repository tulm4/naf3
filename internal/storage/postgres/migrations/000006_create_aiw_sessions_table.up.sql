-- 000006_create_aiw_sessions_table.up.sql
-- AIW (Nnssaaf_AIW) authentication session table (partitioned by month)
-- Spec: TS 29.571 §5.4.4.60, TS 29.526 §7.3
-- Design: docs/design/04_data_model.md §3.6

BEGIN;

-- AIW session: per-SUPI SNPN authentication
-- Key differences from slice_auth_sessions:
--   - No S-NSSAI (AIW is per-user, not per-slice)
--   - No AMF callbacks (no reauth/revocation for AIW)
--   - MSK and pvsInfo stored on success
--   - Default status is PENDING (vs NOT_EXECUTED for NSSAA)

CREATE TABLE IF NOT EXISTS aiw_auth_sessions (
    auth_ctx_id        VARCHAR(64) NOT NULL,

    -- Subscriber identification (TS 29.571 — SUPI, imu-scheme)
    -- Stored encrypted (AES-256-GCM), base64-encoded ciphertext in TEXT column
    supi               TEXT NOT NULL,
    supi_hash          VARCHAR(32),  -- SHA-256(supi) for lookups

    -- AUSF identification (AIW is AUSF-triggered, not AMF-triggered)
    ausf_id            VARCHAR(64),

    -- AAA configuration reference
    aaa_config_id      UUID NULL REFERENCES aaa_server_configs(id),

    -- EAP session state (serialized binary blob)
    eap_session_state  BYTEA NOT NULL,

    -- Session counters
    eap_rounds          INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds      INTEGER NOT NULL DEFAULT 20,
    eap_last_nonce      VARCHAR(64),           -- sha256(eap_payload) for retry detection

    -- Auth result
    nssaa_status        VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    auth_result         VARCHAR(20),           -- Final result

    -- MSK: derived from EAP-TLS on Success (RFC 5216 §2.1.4)
    -- Stored encrypted at application layer; NULL if not EAP-TLS or on Failure
    msk                 BYTEA,

    -- pvsInfo: Privacy-Violating Servers info from AAA-S (TS 29.526 §7.3.3)
    -- JSON array: [{"serverType":"PROSE","serverId":"pvs-001"},...]
    pvs_info            JSONB,

    -- EAP-TTLS inner method container (optional)
    ttls_inner_container BYTEA,

    -- Supported features echo (from request)
    supported_features  VARCHAR(64),

    -- Failure tracking
    failure_reason      TEXT,
    failure_cause       VARCHAR(50),

    -- Audit
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ NOT NULL,
    completed_at        TIMESTAMPTZ,

    PRIMARY KEY (auth_ctx_id, created_at)
) PARTITION BY RANGE (created_at);

-- Default partition for 2025-2030
CREATE TABLE aiw_auth_sessions_default PARTITION OF aiw_auth_sessions
    FOR VALUES FROM ('2025-01-01') TO ('2030-01-01');

-- Indexes
CREATE INDEX IF NOT EXISTS idx_aiw_sessions_supi
    ON aiw_auth_sessions(supi_hash);

CREATE INDEX IF NOT EXISTS idx_aiw_sessions_status
    ON aiw_auth_sessions(nssaa_status)
    WHERE nssaa_status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_aiw_sessions_expires
    ON aiw_auth_sessions(expires_at)
    WHERE completed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_aiw_sessions_created
    ON aiw_auth_sessions(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_aiw_sessions_ausf
    ON aiw_auth_sessions(ausf_id);

COMMIT;
