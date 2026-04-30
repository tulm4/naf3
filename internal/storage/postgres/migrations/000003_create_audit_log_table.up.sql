-- 000003_create_audit_log_table.up.sql
-- Immutable append-only audit log (partitioned by month)
-- Spec: TS 28.541 §5.3

BEGIN;

-- Drop existing non-partitioned table if any (partial migration recovery)
DROP TABLE IF EXISTS nssaa_audit_log CASCADE;

-- Main table must be partitioned for PARTITION OF to work
CREATE TABLE nssaa_audit_log (
    id              BIGSERIAL,
    auth_ctx_id     VARCHAR(64),
    gpsi_hash       VARCHAR(64) NOT NULL,
    snssai_sst      INTEGER,
    snssai_sd       VARCHAR(8),
    amf_instance_id VARCHAR(64),
    amf_ip          INET,
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
    nssaa_status    VARCHAR(20),
    error_code      INTEGER,
    error_message   TEXT,
    request_id      VARCHAR(64),
    correlation_id  VARCHAR(64),
    client_ip       INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Default partition covering 2025-2030
CREATE TABLE nssaa_audit_log_default PARTITION OF nssaa_audit_log
    FOR VALUES FROM ('2025-01-01') TO ('2030-01-01');

-- Partial indexes on parent table (propagate to partitions)
CREATE INDEX IF NOT EXISTS idx_audit_gpsi_created
    ON nssaa_audit_log(gpsi_hash, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_ctx_created
    ON nssaa_audit_log(auth_ctx_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_action_created
    ON nssaa_audit_log(action, created_at DESC);

-- Prevent updates/deletes (immutable)
CREATE OR REPLACE FUNCTION prevent_audit_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Audit log entries cannot be modified or deleted';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_immutable ON nssaa_audit_log;
CREATE TRIGGER audit_immutable
    BEFORE UPDATE OR DELETE ON nssaa_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();

COMMIT;
