-- 000002_create_aaa_configs_table.up.sql
-- AAA server configuration table
-- Spec: TS 29.561 Ch.16-17

BEGIN;

CREATE TABLE IF NOT EXISTS aaa_server_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snssai_sst     INTEGER NOT NULL,
    snssai_sd      VARCHAR(8),
    protocol       VARCHAR(10) NOT NULL CHECK (protocol IN ('RADIUS', 'DIAMETER')),
    aaa_server_host VARCHAR(255) NOT NULL,
    aaa_server_port INTEGER NOT NULL CHECK (aaa_server_port BETWEEN 1 AND 65535),
    aaa_proxy_host  VARCHAR(255),
    aaa_proxy_port  INTEGER CHECK (aaa_proxy_port BETWEEN 1 AND 65535),
    shared_secret   TEXT NOT NULL,
    allow_reauth   BOOLEAN NOT NULL DEFAULT TRUE,
    allow_revoke   BOOLEAN NOT NULL DEFAULT TRUE,
    priority       INTEGER NOT NULL DEFAULT 100,
    weight         INTEGER NOT NULL DEFAULT 1,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    description    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (snssai_sst, snssai_sd)
);

CREATE INDEX idx_aaa_config_snssai
    ON aaa_server_configs(snssai_sst, snssai_sd, enabled)
    WHERE enabled = TRUE;

CREATE INDEX idx_aaa_config_priority
    ON aaa_server_configs(priority, weight)
    WHERE enabled = TRUE;

-- 3-level fallback lookup function
CREATE OR REPLACE FUNCTION get_aaa_config(
    p_sst INTEGER,
    p_sd VARCHAR(8)
) RETURNS aaa_server_configs AS $$
DECLARE
    v_config aaa_server_configs%ROWTYPE;
BEGIN
    -- Level 1: exact match (SST + SD)
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

    -- Level 3: all wildcards
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

COMMIT;
