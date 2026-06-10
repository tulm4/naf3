-- 000008_add_reverse_flow_ownership_columns.down.sql
-- Removes reverse-flow ownership metadata from NSSAA sessions.

BEGIN;

DROP INDEX IF EXISTS idx_slice_auth_sessions_callback_owner;

ALTER TABLE slice_auth_sessions
    DROP COLUMN IF EXISTS has_aiw_context;

ALTER TABLE slice_auth_sessions
    DROP COLUMN IF EXISTS callback_owner;

COMMIT;
