---
wave: 5
plan: 05-05-PLAN.md
completed: 2026-04-28
---

# Wave 5 Summary: VaultKeyManager, SoftHSM & Shared Secrets

## Objectives

Implement full `VaultKeyManager` (HashiCorp Vault transit engine) and `SoftHSMKeyManager` (PKCS#11) for the `KeyManager` interface. Add RADIUS shared secret encryption.

## What Was Done

### VaultKeyManager — `internal/crypto/kms.go`

Full implementation of the `KeyManager` interface using HashiCorp Vault transit engine:

- `NewVaultKeyManager(cfg *VaultConfig) *VaultKeyManager` — constructor with TLS 1.2 transport
- `Wrap(ctx, dek)` — encrypts a DEK using `POST /v1/transit/encrypt/<keyName>`, base64-encodes DEK
- `Unwrap(ctx, wrappedDEK)` — decrypts a wrapped DEK using `POST /v1/transit/decrypt/<keyName>`
- `GetKeyVersion(ctx)` — fetches current KEK version from `GET /v1/transit/keys/<keyName>`
- `RotateKey(ctx)` — generates new KEK version via `POST /v1/transit/rotate/<keyName>`
- `Rotate(ctx)` — wraps `RotateKey` to implement the `KeyManager` interface
- `setAuthHeader(req)` — supports "kubernetes" (K8s ServiceAccount token) and "token" auth methods

### SoftHSMKeyManager — `internal/crypto/kms.go` (stub) + `internal/crypto/kms_softhsm.go` (real)

- Stub in `kms.go` returns errors without `-tags=softhsm`
- Real implementation in `kms_softhsm.go` (`//go:build softhsm`):
  - Opens PKCS#11 session on labeled SoftHSM token
  - `Wrap` uses `CKM_AES_KEY_WRAP_KWP` (RFC 5649) or `CKM_AES_GCM` fallback
  - `Unwrap` unwraps and retrieves key value via `CKA_VALUE`
  - `Rotate` is a no-op (key management is external)
  - `Close` properly closes session and finalizes context

### RADIUS Shared Secret Encryption — `internal/crypto/secret.go`

- `EncryptedSecret` struct with envelope encryption fields
- `EncryptSecret(ctx, plaintext, km)` — generates per-secret DEK, encrypts secret, wraps DEK with KEK
- `DecryptSecret(ctx, encrypted, km)` — unwraps DEK, decrypts secret
- `RotateSecret(ctx, plaintext, km)` — creates new encrypted version with incremented Version

## Key Design Decisions

1. **Vault auth**: Kubernetes ServiceAccount token (reads `/var/run/secrets/kubernetes.io/serviceaccount/token`) as primary; Vault token as fallback
2. **TLS 1.2 minimum**: `VaultKeyManager` HTTP client enforces `tls.VersionTLS12`
3. **SoftHSM build gating**: PKCS#11 library loaded only with `-tags=softhsm`; stub returns errors without panic
4. **Secret expiry**: 90-day `ExpiresAt` for shared secrets
5. **Envelope encryption**: Per-secret DEK wrapped with KEK (DEK version tracked for rotation)

## Files Changed

| File | Change |
|---|---|
| `internal/crypto/kms.go` | Full `VaultKeyManager` implementation; `SoftHSMKeyManager` stub; `NewVaultKeyManager` constructor |
| `internal/crypto/kms_softhsm.go` | Real PKCS#11 `SoftHSMKeyManager` (build-tagged) |
| `internal/crypto/secret.go` | `EncryptedSecret`, `EncryptSecret`, `DecryptSecret`, `RotateSecret` |
| `internal/crypto/config.go` | Uses `NewVaultKeyManager()` constructor |

## Verification

```
go build ./internal/crypto/...     # PASS
go test ./internal/crypto/...       # PASS (all 31 tests)
go build ./...                     # PASS
go test ./... -count=1             # PASS (35 packages)
```

## Acceptance Criteria

- [x] `VaultKeyManager.Wrap` calls `POST /v1/transit/encrypt/<keyName>` with base64-encoded DEK
- [x] `VaultKeyManager.Unwrap` calls `POST /v1/transit/decrypt/<keyName>`
- [x] `VaultKeyManager.GetKeyVersion` calls `GET /v1/transit/keys/<keyName>`
- [x] `VaultKeyManager.Rotate` calls `POST /v1/transit/rotate/<keyName>`
- [x] `setAuthHeader` supports "kubernetes" and "token" auth methods
- [x] `SoftHSMKeyManager` gated with `//go:build softhsm`; returns errors without tag
- [x] `EncryptSecret`, `DecryptSecret`, `EncryptedSecret` implemented
- [x] `EncryptedSecret.ExpiresAt` is 90 days after creation
- [x] All 35 packages build successfully
- [x] All tests pass
- [x] REQ-25: `KeyManager` interface with three implementations (soft, vault, softhsm)
