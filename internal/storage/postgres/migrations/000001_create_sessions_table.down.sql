-- 000001_create_sessions_table.down.sql
-- Drop slice authentication sessions table and partitions.

BEGIN;

DROP TABLE IF EXISTS slice_auth_sessions CASCADE;

COMMIT;
