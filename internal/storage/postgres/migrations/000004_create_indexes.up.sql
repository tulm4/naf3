-- 000004_create_indexes.up.sql
-- Additional indexes and supi_ranges table
-- Spec: TS 28.541 §5.3.146

BEGIN;

-- Supi ranges for SNPN authorization
CREATE TABLE IF NOT EXISTS supi_ranges (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    range_name  VARCHAR(64) NOT NULL,
    start_supi  VARCHAR(32) NOT NULL,
    end_supi    VARCHAR(32) NOT NULL,
    plmn_mcc    VARCHAR(3),
    plmn_mnc    VARCHAR(3),
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (start_supi, end_supi)
);

CREATE INDEX IF NOT EXISTS idx_supi_ranges_plmn
    ON supi_ranges(plmn_mcc, plmn_mnc)
    WHERE enabled = TRUE;

CREATE INDEX IF NOT EXISTS idx_supi_ranges_enabled
    ON supi_ranges(enabled)
    WHERE enabled = TRUE;

-- Partition management: create next 12 months of session partitions
CREATE OR REPLACE FUNCTION create_monthly_partition()
RETURNS void AS $$
DECLARE
    v_partition_name TEXT;
    v_start_date    DATE;
    v_end_date      DATE;
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

COMMIT;
