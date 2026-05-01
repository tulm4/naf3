-- 000005_add_gpsi_hash.down.sql
-- Revert gpsi_hash column changes.

BEGIN;

-- Drop the gpsi_hash column
ALTER TABLE slice_auth_sessions
    DROP COLUMN IF EXISTS gpsi_hash;

-- Revert gpsi/supi columns back to VARCHAR (approximate original type)
-- Note: This may cause data loss for long values
ALTER TABLE slice_auth_sessions
    ALTER COLUMN gpsi TYPE VARCHAR(255),
    ALTER COLUMN supi TYPE VARCHAR(255);

COMMIT;
