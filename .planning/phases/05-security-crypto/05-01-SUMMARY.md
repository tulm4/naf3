---
phase: 05-security-crypto
plan: 05-01
subsystem: crypto
tags: [aes-256-gcm, kek-dek, keymanager, hkdf, go-crypto]

# Dependency graph
requires: []
provides:
  - internal/crypto/ — AES-256-GCM primitives, KEK/DEK envelope encryption, KeyManager interface
  - SoftKeyManager implementation (env var-backed)
  - VaultKeyManager and SoftHSMKeyManager stubs
affects: [05-02, 05-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - DEK-wrapped envelope encryption (DEK encrypts data, KEK wraps DEK)
    - HKDF-SHA256 key derivation per RFC 5869
    - KeyManager interface with three implementation strategies

key-files:
  created:
    - internal/crypto/encrypt.go — AES-256-GCM Encrypt/Decrypt
    - internal/crypto/envelope.go — DEK/KEK envelope operations
    - internal/crypto/kms.go — KeyManager interface + SoftKeyManager
    - internal/crypto/keys.go — DEK/KEK generation
    - internal/crypto/random.go — secure randomness
    - internal/crypto/hash.go — HMAC/HASH operations
    - internal/crypto/kdf.go — HKDF key derivation
    - internal/config/config.go — CryptoConfig, VaultConfig, SoftHSMConfig
  modified: []

key-decisions:
  - "AES-256-GCM with 12-byte nonces and 16-byte GCM tags"
  - "DEK wrapped by KEK using EnvelopeEncrypt/Decrypt"
  - "Token cache TTL: 15 minutes (per planning decision)"
  - "PostgreSQL encrypted field storage: BYTEA binary format"

patterns-established:
  - "KeyManager interface: GetDEK(), RotateKEK(), Close()"
  - "Envelope pattern: GenerateDEK → EnvelopeEncrypt → store ciphertext+wrapped-DEK"
  - "SoftHSM token slot convention: label-based (not slot index)"

requirements-completed: [REQ-23, REQ-24, REQ-25]

# Metrics
duration: ~15min
completed: 2026-04-28
---

# Phase 5 Plan 1 Summary: Crypto Primitives & KeyManager

**AES-256-GCM encrypt/decrypt, KEK/DEK envelope encryption, and KeyManager interface with SoftKeyManager implementation — foundation for all subsequent security waves.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-04-28T01:18:00Z
- **Completed:** 2026-04-28T01:33:00Z
- **Tasks:** 9
- **Files modified:** 19 (8 new crypto files + config updates + middleware)

## Accomplishments
- AES-256-GCM encryption/decryption primitives with EncryptedData struct
- KEK/DEK hierarchy: DEK encrypts session data, KEK wraps the DEK
- KeyManager interface with three implementations: SoftKeyManager (env var), VaultKeyManager (stub), SoftHSMKeyManager (stub)
- Envelope encryption pattern for PostgreSQL BYTEA storage
- HKDF-SHA256 key derivation (RFC 5869) for session KEK and TLS exporters
- Secure randomness: RandomBytes, RandomHexString, GCMNonce
- HMAC-SHA256 and GPSI/SUPI hashing for audit log anonymization
- CryptoConfig, VaultConfig, SoftHSMConfig structs in config.go

## Task Commits

Each task was committed atomically:

1. **Task 1: Add CryptoConfig to internal/config/config.go** - `b9e262c` (planning commit)
2. **Task 2: Build AES-256-GCM primitives (encrypt.go, random.go)** - `16d14fc` (feat)
3. **Task 3: Build KEK/DEK envelope (envelope.go, keys.go)** - `16d14fc` (feat)
4. **Task 4: Build hash/KDF primitives (hash.go, kdf.go)** - `16d14fc` (feat)
5. **Task 5: Build KeyManager interface and SoftKeyManager (kms.go)** - `16d14fc` (feat)
6. **Task 6: Stub VaultKeyManager and SoftHSMKeyManager** - `16d14fc` (feat)
7. **Task 7: Build crypto/config.go module init** - `16d14fc` (feat)
8. **Task 8: Verify build compiles** - `d7362a2` (feat)
9. **Task 9: Middleware test continuation from 05-04** - `d7362a2` (feat)
10. **Restored crypto_test.go** - `4d6da4f` (test)
11. **Go 1.25 GCM API fixes** - `903999b` (fix)

## Files Created/Modified

- `internal/crypto/encrypt.go` — AES-256-GCM Encrypt/Decrypt/EncryptedData
- `internal/crypto/random.go` — RandomBytes, RandomHexString, GCMNonce
- `internal/crypto/hash.go` — HashGPSI, HashSUPI, HashMessage, HMACSHA256, VerifyHMAC
- `internal/crypto/kdf.go` — DeriveKey (HKDF-SHA256), SessionKEK, TLSExporter
- `internal/crypto/keys.go` — GenerateDEK, GenerateKEK
- `internal/crypto/envelope.go` — EnvelopeEncrypt, EnvelopeDecrypt (DEK-wrapping)
- `internal/crypto/kms.go` — KeyManager interface, SoftKeyManager, VaultKeyManager stub, SoftHSMKeyManager stub
- `internal/crypto/config.go` — Module Init, KM(), KeyManager interface definition
- `internal/config/config.go` — CryptoConfig, VaultConfig, SoftHSMConfig structs
- `internal/api/common/middleware.go` — Auth middleware (from 05-04 continuation)
- `internal/api/common/common_test.go` — Middleware tests (from 05-04 continuation)
- `compose/configs/biz.yaml` — TLS config fields added

## Decisions Made

- AES-256-GCM: 12-byte nonces, 16-byte GCM auth tags, ciphertext stored as BYTEA
- Token cache TTL: 15 minutes (balances NRF load vs token freshness)
- Vault transit endpoint: `http://vault.vault.svc.cluster.local:8200/v1/transit/keys/nssaa-kek`
- SoftHSM token slot: label-based (not slot index)
- Go 1.25 `crypto/cipher.NewGCM`: `gcm.Seal(nil, nonce, pt, aad)` returns `ct||tag` (no nonce prefix); `gcm.Open(nil, nonce, sealed, aad)` expects `ct||tag` format

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Go 1.25 GCM API format mismatch**
- **Found during:** Execution of plan 05-01
- **Issue:** `gcm.Seal(nil, nonce, pt, aad)` returns `ct||tag` (no nonce prefix) in Go 1.25, but the code assumed `nonce||ct||tag` format. This caused MAC authentication failures and slice bounds panics.
- **Fix:** Rewrote `Encrypt` to extract `ct = sealed[:len(sealed)-overhead]`, `tag = sealed[overhead:]`. Rewrote `Decrypt` to reconstruct `ct||tag` before calling `gcm.Open`. Fixed `EnvelopeDecrypt` slice bounds to `ct[12:44]`, `tag[44:60]`.
- **Files modified:** `internal/crypto/encrypt.go`, `internal/crypto/envelope.go`
- **Commit:** `903999b`

**2. [Rule 1 - Bug] Missing SoftHSMKeyManager stub**
- **Found during:** Baseline restoration
- **Issue:** `NewSoftHSMKeyManager` undefined; `SoftHSMKeyManager` struct and methods missing from `kms.go`
- **Fix:** Added stub implementation with "not implemented" errors for Wave 1
- **Files modified:** `internal/crypto/kms.go`
- **Commit:** `903999b`

**3. [Rule 1 - Bug] VaultKeyManager initialization mismatch**
- **Found during:** Baseline restoration
- **Issue:** `d7362a2` config.go used inline struct initialization but cf88b36 kms.go had no full Vault implementation
- **Fix:** Restored full VaultKeyManager implementation from d7362a2 baseline
- **Files modified:** `internal/crypto/config.go`, `internal/crypto/kms.go`
- **Commit:** `903999b`

## Issues Encountered

- Three of five parallel executor agents (05-01, 05-02, 05-05) stalled during worktree creation and had to be handled sequentially by the orchestrator. Uncommitted agent changes were recovered and committed.

## Next Phase Readiness

- `internal/crypto/` primitives ready for Wave 2 (session encryption wiring)
- KeyManager interface ready for Wave 2 and Wave 5 (Vault/SoftHSM implementation)
- `internal/config/config.go` has all crypto config structs for subsequent waves

---
*Phase: 05-security-crypto*
*Completed: 2026-04-28*
