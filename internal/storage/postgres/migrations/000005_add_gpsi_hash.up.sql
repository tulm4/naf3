-- 000005_add_gpsi_hash.up.sql
-- Add gpsi_hash column for encrypted GPSI lookups and widen gpsi/supi to TEXT.
-- GPSI/SUPI are now stored encrypted (AES-256-GCM) in the gpsi/supi columns.
-- A hash is stored for efficient WHERE gpsi_hash = HashGPSI(...) lookups.
-- Spec: TS 29.571 §5.4.4.60, TS 33.501 §16 (GPSI pseudonymization)

BEGIN;

-- Widen gpsi/supi columns to TEXT to hold base64-encoded ciphertext.
-- AES-256-GCM: 12-byte nonce + N-byte plaintext + 16-byte tag.
-- A 15-char GPSI becomes ~64 chars base64-encoded ciphertext.
ALTER TABLE slice_auth_sessions
    ALTER COLUMN gpsi TYPE TEXT,
    ALTER COLUMN supi TYPE TEXT;

-- gpsi_hash stores SHA-256(gpsi) for lookups; first 16 bytes as hex string (32 chars).
ALTER TABLE slice_auth_sessions
    ADD COLUMN IF NOT EXISTS gpsi_hash VARCHAR(32);

-- Index for GPSI hash lookups.
CREATE INDEX IF NOT EXISTS idx_sessions_gpsi_hash
    ON slice_auth_sessions(gpsi_hash);

-- Backfill existing rows: compute hash from the GPSI column.
UPDATE slice_auth_sessions
SET gpsi_hash = encode(sha256(gpsi::bytea), 'hex')
WHERE gpsi_hash IS NULL;

COMMIT;
