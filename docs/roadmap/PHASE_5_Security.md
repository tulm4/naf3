# Phase 5: Security — Authentication & Encryption

## Overview

Phase 5 thêm security layer: TLS, OAuth2, và encryption.

## Modules to Implement

### 1. `internal/auth/` — Authentication & Authorization

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/15_sbi_security.md`

**Deliverables:**
- [ ] `token.go` — JWT token validation (NRF)
- [ ] `middleware.go` — Auth middleware
- [ ] `scope.go` — Scope validation
- [ ] `mtls.go` — mTLS configuration
- [ ] `auth_test.go` — Unit tests

### 2. `internal/crypto/` — Cryptography

**Priority:** P0
**Dependencies:** None (standalone)
**Design Doc:** `docs/design/17_crypto.md`

**Deliverables:**
- [ ] `encrypt.go` — AES-256-GCM encryption
- [ ] `envelope.go` — Envelope encryption (DEK wrapping)
- [ ] `kdf.go` — HKDF key derivation
- [ ] `hash.go` — SHA-256 hashing, HMAC
- [ ] `random.go` — Secure random generation
- [ ] `kms.go` — HSM interface (AWS CloudHSM, SoftHSM)
- [ ] `rotation.go` — KEK rotation with overlap window
- [ ] `session.go` — Session state encryption
- [ ] `secret.go` — Shared secret encryption
- [ ] `crypto_test.go` — Unit tests

**Key Design:**
```go
// Key hierarchy: MEK (HSM) → KEK → DEK (per data item)
type EncryptedData struct {
    Ciphertext   []byte  // encrypted with DEK
    EncryptedDEK []byte  // DEK encrypted with KEK
    KeyVersion  int     // KEK version for rotation
}

// Session state encrypted with per-session DEK
func EncryptSession(state *EapSessionState, kek []byte) (*EncryptedSession, error)
```

## Validation Checklist

- [ ] TLS 1.3 configured
- [ ] JWT token validation with NRF public key
- [ ] OAuth2 scopes: `nnssaaf-nssaa`, `nnssaaf-aiw`
- [ ] AES-256-GCM encryption for session state
- [ ] Shared secret rotation support
- [ ] KEK/DEK envelope encryption hierarchy
- [ ] HSM/KMS integration (CloudHSM or SoftHSM)
- [ ] GPSI hashed in audit log
- [ ] Unit test coverage >80%
