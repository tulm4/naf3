-- 000008_add_reverse_flow_ownership_columns.up.sql
-- Adds explicit reverse-flow ownership metadata to NSSAA sessions.

BEGIN;

ALTER TABLE slice_auth_sessions
    ADD COLUMN IF NOT EXISTS callback_owner TEXT NOT NULL DEFAULT '';

ALTER TABLE slice_auth_sessions
    ADD COLUMN IF NOT EXISTS has_aiw_context BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE slice_auth_sessions
SET callback_owner = CASE
        WHEN reauth_notif_uri IS NOT NULL AND reauth_notif_uri <> '' THEN 'amf'
        WHEN revoc_notif_uri IS NOT NULL AND revoc_notif_uri <> '' THEN 'amf'
        ELSE callback_owner
    END,
    has_aiw_context = COALESCE(has_aiw_context, FALSE)
WHERE callback_owner = '' OR has_aiw_context IS DISTINCT FROM COALESCE(has_aiw_context, FALSE);

CREATE INDEX IF NOT EXISTS idx_slice_auth_sessions_callback_owner
    ON slice_auth_sessions(callback_owner)
    WHERE callback_owner <> '';

COMMIT;
