-- 000003_create_audit_log_table.down.sql
-- Drop audit log table and partitions.

BEGIN;

DROP TABLE IF EXISTS nssaa_audit_log CASCADE;

COMMIT;
