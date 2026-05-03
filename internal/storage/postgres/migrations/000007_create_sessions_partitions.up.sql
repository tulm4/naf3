-- 000007_create_sessions_partitions.up.sql
-- Create monthly partitions for slice_auth_sessions.
-- This is a separate migration (not bundled in 000001) because
-- PARTITION OF does not support IF NOT EXISTS — a new migration
-- lets the idempotent harness runner skip it after first success.
--
-- Partitioning strategy: monthly by created_at.
-- Partitions are created in advance; a scheduled job (or operator)
-- adds future partitions as needed.
--
-- Note: No default partition — specific monthly partitions cover all
-- future INSERTs. Add a default partition only if needed for
-- historical data or out-of-range dates.
--
-- Spec: TS 29.571 §5.4.4.60 (partitioned session table)

BEGIN;

-- Partition for May 2026
CREATE TABLE slice_auth_sessions_2026_05 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

-- Partition for June 2026
CREATE TABLE slice_auth_sessions_2026_06 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

-- Partition for July 2026
CREATE TABLE slice_auth_sessions_2026_07 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

-- Partition for August 2026
CREATE TABLE slice_auth_sessions_2026_08 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

-- Partition for September 2026
CREATE TABLE slice_auth_sessions_2026_09 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

COMMIT;
