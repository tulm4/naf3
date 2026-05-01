-- 000002_create_aaa_configs_table.down.sql
-- Drop AAA server configurations table.

BEGIN;

DROP TABLE IF EXISTS aaa_server_configs CASCADE;

COMMIT;
