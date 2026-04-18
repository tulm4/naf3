-- 000003_create_audit_log_table.up.sql
-- Immutable append-only audit log
-- Spec: TS 28.541 §5.3

BEGIN;

CREATE TABLE IF NOT EXISTS nssaa_audit_log (
    id              BIGSERIAL PRIMARY KEY,
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
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partition by month (audit logs grow large)
CREATE TABLE IF NOT EXISTS nssaa_audit_log_default PARTITION OF nssaa_audit_log
    FOR VALUES FROM ('2025-01-01') TO ('2030-01-01');

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

CREATE OR REPLACE TRIGGER audit_immutable
    BEFORE UPDATE OR DELETE ON nssaa_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();

COMMIT;
