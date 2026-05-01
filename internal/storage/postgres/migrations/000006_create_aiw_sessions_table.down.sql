-- 000006_create_aiw_sessions_table.down.sql
-- Drop AIW authentication sessions table.

BEGIN;

DROP TABLE IF EXISTS aiw_auth_sessions CASCADE;

COMMIT;
