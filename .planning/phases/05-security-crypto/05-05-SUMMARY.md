---
phase: "5-security-crypto"
plan: "05-05"
subsystem: "internal/crypto"
tags: ["kms", "vault", "softhsm", "secret-encryption", "crypto"]
dependency-graph:
  requires:
    - "05-01-PLAN.md"
  provides:
    - "VaultKeyManager"
    - "SoftHSMKeyManager"
    - "EncryptedSecret"
tech-stack:
  added:
    - "github.com/miekg/pkcs11 v1.1.2"
  patterns:
    - "HashiCorp Vault transit engine"
    - "PKCS#11 / SoftHSM2"
    - "AES-256-GCM envelope encryption"
    - "HKDF key derivation"
key-files:
  created:
    - "internal/crypto/kms.go"
    - "internal/crypto/secret.go"
  modified:
    - "internal/crypto/encrypt.go"
    - "internal/crypto/envelope.go"
    - "internal/crypto/config.go"
    - "compose/configs/biz.yaml"
decisions:
  - "VaultKeyManager uses Vault transit engine with Kubernetes SA auth"
  - "SoftHSMKeyManager uses AES-GCM via PKCS#11 for DEK wrapping"
  - "RADIUS shared secrets use per-secret DEK with KEK wrapping"
  - "SoftHSM implementation gated with //go:build softhsm tag"
metrics:
  duration: "45 minutes"
  completed: "2026-04-28T02:35:00Z"
---

# Phase 5 Plan 5: VaultKeyManager, SoftHSMKeyManager & Shared Secrets Summary

## One-liner

Implemented full VaultKeyManager (HashiCorp Vault transit engine), SoftHSMKeyManager (PKCS#11), and RADIUS shared secret encryption.

## What was Built

### VaultKeyManager (`internal/crypto/kms.go`)

Full HashiCorp Vault transit engine integration:
- `Wrap()`: Calls `POST /v1/transit/encrypt/{keyName}` with base64-encoded DEK
- `Unwrap()`: Calls `POST /v1/transit/decrypt/{keyName}`
- `GetKeyVersion()`: Calls `GET /v1/transit/keys/{keyName}` to fetch current version
- `Rotate()`: Calls `POST /v1/transit/rotate/{keyName}` via `RotateKey()`
- `setAuthHeader()`: Supports "kubernetes" (K8s SA token) and "token" auth methods

### SoftHSMKeyManager (`internal/crypto/kms_softhsm.go`, `kms_softhsm_stub.go`)

PKCS#11 implementation using SoftHSM2:
- Build-gated with `//go:build softhsm` tag
- AES-GCM encryption for DEK wrapping
- PKCS#11 session management with login and token discovery
- Returns error (no panic) when built without `softhsm` tag

### Secret Encryption (`internal/crypto/secret.go`)

RADIUS shared secret encryption:
- `EncryptedSecret` struct with envelope encryption
- `EncryptSecret()`: Generates per-secret DEK, encrypts with DEK, wraps DEK with KEK
- `DecryptSecret()`: Unwraps DEK with KEK, decrypts secret
- `RotateSecret()`: Creates new encrypted version with incremented version

### KeyManager Interface (`internal/crypto/config.go`)

Added `Rotate(ctx context.Context) error` method to interface with validation for vault and softhsm modes.

## Deviations from Plan

### Auto-fixed Issues

1. **[Rule 3 - Blocking] Removed duplicate SoftHSMKeyManager declarations**
   - Found during: Implementation
   - Issue: `SoftHSMKeyManager` was declared both in `kms.go` and `kms_softhsm_stub.go`, causing redeclaration errors
   - Fix: Removed stub from `kms.go`, kept only in separate build-gated files
   - Files modified: `internal/crypto/kms.go`
   - Commit: `cfbca89`

2. **[Rule 1 - Bug] Fixed Encrypt/Decrypt format mismatch**
   - Found during: Testing
   - Issue: `Encrypt` produced `ciphertext || tag` but `Decrypt` expected different format, causing auth failures
   - Fix: Refactored `Encrypt` to produce `ciphertext || tag` with separate `Nonce` field, updated `Decrypt` to reconstruct `ct||tag` before passing to GCM
   - Files modified: `internal/crypto/encrypt.go`
   - Commit: `903999b`

3. **[Rule 3 - Blocking] Fixed hardcoded 60-byte EncryptedDEK size**
   - Found during: Testing
   - Issue: `EnvelopeDecrypt` checked for exact 60 bytes, failing for different DEK sizes
   - Fix: Dynamic size parsing with minimum size check (28 bytes: 12 nonce + 0 ciphertext + 16 tag)
   - Files modified: `internal/crypto/envelope.go`
   - Commit: `903999b`

4. **[Rule 3 - Blocking] Fixed nil SoftKeyManager panic in Unwrap**
   - Found during: Testing
   - Issue: `SoftKeyManager.Unwrap` called `m.mu.RLock()` when `m` was nil
   - Fix: Added nil check at start of `Unwrap` method
   - Files modified: `internal/crypto/kms.go`
   - Commit: `cfbca89`

## Verification Results

```bash
go build ./internal/crypto/...         # SUCCESS
go build -tags=softhsm ./internal/crypto/...  # SUCCESS
go test ./internal/crypto/... -count=1 # PASS (20 tests)
```

## Acceptance Criteria Status

| Criterion | Status |
|-----------|--------|
| `VaultKeyManager.Wrap` calls Vault transit encrypt | ✅ |
| `VaultKeyManager.Unwrap` calls Vault transit decrypt | ✅ |
| `VaultKeyManager.GetKeyVersion` fetches key version | ✅ |
| `VaultKeyManager.Rotate` rotates KEK | ✅ |
| `setAuthHeader` supports k8s and token auth | ✅ |
| `SoftHSMKeyManager` implements KeyManager interface | ✅ |
| PKCS#11 file gated with `//go:build softhsm` | ✅ |
| Stub returns error (not panic) without softhsm tag | ✅ |
| `EncryptedSecret` struct with all required fields | ✅ |
| `EncryptSecret`/`DecryptSecret` round-trip | ✅ |
| `RotateSecret` increments version | ✅ |
| `KeyManager` interface has `Rotate` method | ✅ |
| `compose/configs/biz.yaml` has full crypto config | ✅ |

## Commits

| Hash | Message |
|------|---------|
| `2d29bb3` | feat(05-05): add RADIUS shared secret encryption |
| `db155ee` | feat(05-05): add RADIUS shared secret encryption |
| `f17351f` | feat(05-05): add secret.go for RADIUS shared secret encryption |
| `cfbca89` | fix(05-05): remove duplicate SoftHSMKeyManager stub |
| `3b8dbaa` | feat(05-05): complete VaultKeyManager and SoftHSMKeyManager |
| `903999b` | fix(05-01): Go 1.25 GCM API fixes and baseline restoration |
| `ad2ec57` | docs(05-01): update summary with Go 1.25 GCM fixes |
| `5e4143c` | fix(05-01): add SoftHSMKeyManager and remove unused crypto/tls import |
| `ab9fde9` | fix(05-01): inline SoftHSMKeyManager init to avoid constructor mismatch |
| `8c5bdb0` | fix(05-01): add SoftHSMKeyManager stub to kms.go |

## Self-Check

- [x] `go build ./internal/crypto/...` succeeds
- [x] `go build -tags=softhsm ./internal/crypto/...` succeeds
- [x] `go test ./internal/crypto/...` passes
- [x] All must_haves from plan are implemented
- [x] Commits created with proper format
