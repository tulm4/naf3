-- 000001_create_sessions_table.up.sql
-- Slice authentication session table (partitioned by month)
-- Spec: TS 29.571 §5.4.4.60, TS 29.526 §7

BEGIN;

CREATE TABLE IF NOT EXISTS slice_auth_sessions (
    auth_ctx_id        VARCHAR(64) NOT NULL,
    gpsi              VARCHAR(32) NOT NULL,
    supi              VARCHAR(32),
    snssai_sst        INTEGER NOT NULL CHECK (snssai_sst BETWEEN 0 AND 255),
    snssai_sd         VARCHAR(8),
    amf_instance_id    VARCHAR(64),
    amf_ip            INET,
    amf_region        VARCHAR(16),
    reauth_notif_uri  TEXT,
    revoc_notif_uri   TEXT,
    aaa_config_id     UUID NOT NULL,
    eap_session_state BYTEA NOT NULL,
    eap_rounds        INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds    INTEGER NOT NULL DEFAULT 20,
    eap_last_nonce    VARCHAR(64),
    nssaa_status      VARCHAR(20) NOT NULL DEFAULT 'NOT_EXECUTED',
    auth_result       VARCHAR(20),
    failure_reason    TEXT,
    failure_cause     VARCHAR(50),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ,
    terminated_at     TIMESTAMPTZ,
    PRIMARY KEY (auth_ctx_id)
) PARTITION BY RANGE (created_at);

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

COMMIT;
