-- 000007_create_sessions_partitions.down.sql
-- Drop monthly partitions created by 000007_create_sessions_partitions.up.sql
-- Partitions must be dropped before the parent table can be dropped.

BEGIN;

DROP TABLE IF EXISTS slice_auth_sessions_2026_09;
DROP TABLE IF EXISTS slice_auth_sessions_2026_08;
DROP TABLE IF EXISTS slice_auth_sessions_2026_07;
DROP TABLE IF EXISTS slice_auth_sessions_2026_06;
DROP TABLE IF EXISTS slice_auth_sessions_2026_05;

COMMIT;
