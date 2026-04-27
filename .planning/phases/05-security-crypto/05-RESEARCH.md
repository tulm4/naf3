# Phase 5: Security & Crypto — Research

**Phase:** 5
**Goal:** Implement TLS 1.3, mTLS, JWT validation, AES-256-GCM encryption
**Requirements:** REQ-20, REQ-21, REQ-22, REQ-23, REQ-24, REQ-25
**Modules:** `internal/auth/`, `internal/crypto/`

---

## What I Need to Know to Plan This Phase

### 1. Current State of Each Module

#### `internal/auth/` — Empty stub (1 import line)

- Currently a blank file. Needs everything from scratch:
  - `TokenValidator` struct with NRF public key, JWT RS256/ECDSA parsing, scope/audience/issuer/exp validation
  - `TokenCache` for JWKS caching (TTL TBD in planning)
  - `Middleware` that extracts `Authorization: Bearer` header, validates, and injects claims into context
  - `MiddlewareOption` pattern (like existing handler options)
  - `AuthMiddleware` function returning `func(http.Handler) http.Handler`
  - `AuthClaims` struct holding `Scope`, `ClientId`, `NfType`, `NfId`, `CN`
  - JWT library: use `github.com/golang-jwt/jwt/v5` (most widely used, idiomatic Go)
  - JWKS fetching: NRF public key endpoint, cached

#### `internal/crypto/` — Empty stub (1 import line)

- Currently a blank file. Needs everything from scratch per `docs/design/17_crypto.md`:
  - `encrypt.go` — AES-256-GCM primitives (`Encrypt`, `Decrypt`, `EncryptedData` struct)
  - `envelope.go` — Envelope encryption (`EnvelopeEncrypt`, `EnvelopeDecrypt`, `Envelope` struct with DEK wrapping)
  - `keys.go` — DEK generation (`GenerateDEK`, 32-byte CSPRNG)
  - `hash.go` — SHA-256 hashing for GPSI privacy (`HashGPSI`, `HashSUPI`, `HMACSHA256`)
  - `random.go` — CSPRNG wrappers (`RandomBytes`, `RandomHexString`, `GCMNonce`)
  - `kdf.go` — HKDF key derivation (`DeriveKey`, `SessionKEK`)
  - `kms.go` — **KeyManager interface** + three implementations:
    - `SoftKeyManager` — hex-encoded 32-byte key from `MASTER_KEY_HEX` env var
    - `SoftHSMKeyManager` — PKCS#11 via SoftHSM2 (dev/test only)
    - `VaultKeyManager` — HashiCorp Vault transit engine API (prod on kubeadm)
  - `rotation.go` — KEK rotation manager (`KEKRotator` with 30-day overlap)
  - `session.go` — Session encryption (`EncryptSession`, `DecryptSession`, `EncryptedSession` struct)
  - `secret.go` — Shared secret encryption (`EncryptSecret`, `DecryptSecret`, `EncryptedSecret` struct)
  - Package-level `Init(cfg *Config)` and `KM() KeyManager` singletons

#### `internal/storage/postgres/session.go` — Already has `Encryptor` but wrong model

The current `Encryptor` in `session.go` is a **flat-key AES-GCM encryptor**:
- Single 16/24/32-byte key passed as `[]byte` (stored in memory unencrypted)
- No KEK/DEK hierarchy
- No key version tracking
- No DEK wrapping

This must be **replaced** by wiring `crypto.Envelope` into the repository:
- `Repository` currently takes `*Encryptor`
- Phase 5: Replace with `*crypto.Envelope` (or `KeyManager`) that supports KEK/DEK envelope
- The `EAPSessionState` field storage already uses base64 — can be kept, but encryption changes
- `NewSessionStore(pool, encryptor)` call site in `main.go` (line 108) needs updating

**Key insight**: The current `Encryptor` is NOT exported from `internal/crypto/`. The `crypto/` package is a blank stub. So Phase 5 must build `internal/crypto/` from scratch, then wire it into `internal/storage/postgres/session.go` and `cmd/biz/main.go`.

#### `cmd/biz/main.go` — mTLS scaffolding exists, needs completion

Lines 165–180 already have:
- `tls.Config` built from `cfg.Biz.UseMTLS`, `cfg.Biz.TLSCert`, `cfg.Biz.TLSKey`, `cfg.Biz.TLSCA`
- `mustLoadCertPool()` and `mustLoadCert()` helper functions
- `newHTTPAAAClient()` call with TLS-configured `http.Client`
- `biz.yaml` config has `useMTLS`, `tlsCert`, `tlsKey`, `tlsCa` fields

**Gap**: The Biz Pod server itself (lines 223–237) does NOT use TLS — it's a plain HTTP server. This is intentional: HTTP Gateway handles TLS termination. The mTLS is only for the AAA Gateway HTTP client.

**What needs completion**:
- None for the HTTP server itself (no TLS needed on Biz Pod inbound)
- `internal/auth/` JWT middleware NOT wired (HTTP Gateway validates tokens, not Biz Pod — D-01)
- `internal/crypto/` NOT initialized — Phase 5 needs `crypto.Init(cfg)` and real encryptor

#### `cmd/http-gateway/main.go` — TLS exists, JWT missing

Lines 107–134 already have:
- `ListenAndServeTLS` branch when `cfg.HTTPgw.TLS != nil`
- But `tls.Config` is a bare `MinVersion: tls.VersionTLS12` — **not TLS 1.3**
- No JWT validation middleware
- No auth middleware at all (passes all requests directly to Biz Pod)

**What needs completion**:
- Upgrade TLS config to TLS 1.3 (`tls.VersionTLS13`)
- Add cipher suites per `docs/design/15_sbi_security.md` §2.1
- Add `ISTIO_MTLS=1` env var detection to skip explicit cert loading
- Wire `internal/auth/` middleware (JWT validation) before forwarding to Biz Pod
- The middleware validates NRF-issued Bearer tokens for N58/N60 endpoints

#### `internal/config/config.go` — Missing crypto config

`BizConfig` has `UseMTLS`, `TLSCert`, `TLSKey`, `TLSCA`, `TLS *TLSConfig`.
`HTTPgwConfig` has `TLS *TLSConfig`.

**Missing**:
- `CryptoConfig` struct with `KeyManager string`, `MasterKeyHex string`, `HSM HSMCfg`, `KEKOverlapDays int`
- `VaultConfig` for Vault transit engine URL + auth method
- `SoftHSMConfig` for token slot/label
- Validation of crypto config for each key manager mode
- `ISTIO_MTLS` env var field or detection in `HTTPgwConfig`

### 2. Module Boundaries (Who Does What)

| Concern | Where | Why |
|---------|-------|-----|
| N58/N60 TLS 1.3 server | HTTP Gateway | TLS termination point |
| N58/N60 JWT validation | HTTP Gateway | D-01: GW validates inbound tokens |
| Biz Pod TLS server | None | HTTP GW handles TLS; Biz Pod is internal |
| Biz Pod → AAA GW mTLS | Biz Pod (`http.Client` TLS config) | D-03: explicit cert loading |
| AES-256-GCM session encryption | `internal/crypto/` | Biz Pod only |
| KEK/DEK envelope | `internal/crypto/` | Biz Pod only |
| KeyManager (soft/SoftHSM/Vault) | `internal/crypto/` | Biz Pod only |
| RADIUS shared secret encryption | `internal/crypto/` | Biz Pod only |

### 3. Key Design Decisions to Resolve During Planning

From the 05-CONTEXT.md "Claude's Discretion" section, the following must be decided:

| Item | Options | Impact |
|------|---------|--------|
| TLS cipher suite ordering | Go stdlib defaults vs explicit list | FIPS compliance, performance |
| Token cache TTL | 5min / 15min / 1hr | NRF load vs token freshness |
| Vault transit endpoint path | `/v1/transit/` vs custom | Vault auth method (K8s SA vs token) |
| PostgreSQL encrypted field storage | `BYTEA` vs `TEXT` base64 | Encoding efficiency |
| SoftHSM token slot convention | Slot index vs object label | Dev/test portability |
| RADIUS shared secret in Phase 5? | Encrypt now or defer to Phase 6 | Scope creep risk |
| Audit log TLS cipher field | Populate in Phase 5 or 6 | Depends on TLS being wired |

### 4. VaultKeyManager Specifics (kubeadm deployment)

Key decisions for Vault on kubeadm (not AWS):
- Vault runs as K8s Deployment in `vault` namespace
- Authentication: Kubernetes Service Account (recommended) or Vault token
- Transit engine: `vault transit` for KEK wrap/unwrap
- Endpoint: `http://vault.vault.svc.cluster.local:8200/v1/transit/keys/nssaa-kek`
- KEK rotation via Vault API (`POST /v1/transit/rotate/nssaa-kek`)
- Wrapped DEK stored in PostgreSQL alongside ciphertext

**Vault API calls needed**:
- `POST /v1/transit/encrypt/<key_name>` — encrypt DEK with KEK
- `POST /v1/transit/decrypt/<key_name>` — decrypt DEK
- `POST /v1/transit/rotate/<key_name>` — rotate KEK (generates new version)
- `GET /v1/transit/keys/<key_name>` — get current key version

### 5. Migration Path for Existing Encryptor

The current `session.go` `Encryptor` must be replaced. Migration path:
1. Build `internal/crypto/` package with full KEK/DEK envelope
2. Add `crypto` config to `BizConfig` in `internal/config/config.go`
3. In `main.go`: initialize `crypto.Init(cfg)` and get `crypto.KM()`
4. Change `NewSessionStore(pool, encryptor)` → `NewSessionStore(pool, keyManager)` or `NewSessionStore(pool, kek)`
5. Update `Repository` to use `crypto.Envelope` instead of `*Encryptor`
6. The existing `Encryptor` in `session.go` can be kept as `SoftEncryptor` (single-key, no KEK) for the AAA Gateway (which doesn't need HSM-level security)

### 6. JWT Validation Details

From `docs/design/15_sbi_security.md` §3.2:
- Accept RS256, RS384, RS512, ES256, ES384, ES512 signing methods
- Issuer: NRF FQDN (configurable)
- Audiences: `nnssaaf-nssaa`, `nnssaaf-aiw`
- NF types: `AMF`, `AUSF` (from `nf_type` claim)
- Scopes: `nnssaaf-nssaa`, `nnssaaf-aiw`
- JWKS URL: NRF `/.well-known/jwks.json` or per-NF-type endpoint

**HTTP Gateway middleware flow**:
1. Extract `Authorization: Bearer <token>` header
2. If missing → 401 Unauthorized
3. Parse and validate JWT (sig, expiry, issuer, audience, scope)
4. If invalid → 401 Unauthorized
5. Inject claims into request context (`r.Context()` with `WithValue`)
6. Biz Pod handlers read from context (trust gateway)

### 7. TLS 1.3 Configuration for HTTP Gateway

From `docs/design/15_sbi_security.md` §2.1:

```go
&tls.Config{
    MinVersion: tls.VersionTLS13,
    CurvePreferences: []tls.CurveID{
        tls.X25519,
        tls.secp384r1,
        tls.secp256r1,
    },
    CipherSuites: []uint16{
        tls.TLS_AES_256_GCM_SHA384,
        tls.TLS_AES_128_GCM_SHA256,
        tls.TLS_CHACHA20_POLY1305_SHA256,
    },
    PreferServerCipherSuites: true,
}
```

**Istio mode**: `ISTIO_MTLS=1` env var → skip explicit TLS config, let Istio sidecar handle mTLS.

### 8. Package Structure to Create

```
internal/auth/
├── auth.go           # TokenValidator, TokenClaims, Init, validator singleton
├── cache.go          # JWKS cache, token cache with TTL
├── middleware.go     # AuthMiddleware, context extraction helpers
├── middleware_test.go
├── auth_test.go
└── errors.go         # Sentinel errors

internal/crypto/
├── encrypt.go        # AES-256-GCM Encrypt/Decrypt, EncryptedData
├── envelope.go       # EnvelopeEncrypt/Decrypt, Envelope struct
├── keys.go           # GenerateDEK
├── hash.go           # HashGPSI, HashSUPI, HMACSHA256
├── random.go         # RandomBytes, RandomHexString, GCMNonce
├── kdf.go            # DeriveKey (HKDF-SHA256), SessionKEK
├── kms.go            # KeyManager interface + SoftKeyManager + SoftHSMKeyManager + VaultKeyManager
├── rotation.go       # KEKRotator
├── session.go        # EncryptSession, DecryptSession, EncryptedSession
├── secret.go         # EncryptSecret, DecryptSecret, EncryptedSecret
├── config.go         # CryptoConfig, Validate, Init, KM() singleton
├── crypto_test.go
└── integration_test.go

internal/config/
├── config.go         # Add CryptoConfig, VaultConfig, SoftHSMConfig, validate crypto fields
```

### 9. Wave Structure Recommendation

**Wave 1 — Foundation (internal/crypto/ primitives)**
- `internal/crypto/encrypt.go`, `keys.go`, `random.go`, `hash.go`, `kdf.go` (no dependencies)
- `internal/config/config.go` — add CryptoConfig, validate key manager settings
- `internal/crypto/config.go` — Init, KM() singleton
- `internal/crypto/kms.go` — KeyManager interface + SoftKeyManager (env var mode)
- `internal/crypto/envelope.go` — envelope encryption using SoftKeyManager
- Unit tests for all crypto primitives

**Wave 2 — Session Encryption (wiring to storage)**
- `internal/crypto/session.go` — session encryption
- `internal/storage/postgres/session.go` — replace flat Encryptor with crypto.Envelope
- `cmd/biz/main.go` — initialize crypto.Init(), wire real encryptor
- `internal/crypto/rotation.go` — KEK rotation manager
- Integration test: encrypt/decrypt session round-trip

**Wave 3 — TLS + mTLS (HTTP Gateway + Biz Pod)**
- `cmd/http-gateway/main.go` — upgrade to TLS 1.3 config
- `cmd/http-gateway/main.go` — add ISTIO_MTLS=1 detection
- `cmd/biz/main.go` — add mTLS health check (TLSConfig check)
- Update `compose/configs/http-gateway.yaml` and `biz.yaml` with TLS settings
- Unit test: TLS config generation

**Wave 4 — JWT Validation (HTTP Gateway middleware)**
- `internal/auth/auth.go` — TokenValidator, TokenClaims, Init
- `internal/auth/cache.go` — JWKS cache, token cache
- `internal/auth/middleware.go` — AuthMiddleware, claims context helpers
- `internal/auth/errors.go` — sentinel errors
- Wire middleware into HTTP Gateway router
- Unit tests: JWT parsing, validation, middleware

**Wave 5 — Advanced KMS + Shared Secrets**
- `internal/crypto/kms.go` — complete SoftHSMKeyManager (PKCS#11)
- `internal/crypto/kms.go` — VaultKeyManager (kubeadm transit engine)
- `internal/crypto/secret.go` — shared secret encryption
- `internal/storage/postgres/aaa_config.go` — integrate crypto for RADIUS secrets
- `internal/config/config.go` — add VaultConfig, SoftHSMConfig
- Unit tests for all KeyManager implementations

### 10. Testability

| Component | Test Strategy |
|-----------|---------------|
| AES-256-GCM | Golden vectors from NIST SP 800-38D |
| Envelope encryption | Property-based: encrypt then decrypt = original |
| JWT validation | Mock NRF JWKS endpoint with known keys |
| VaultKeyManager | Mock HTTP server returning transit API responses |
| SoftHSMKeyManager | SoftHSM2 running in test (or skip if unavailable) |
| Middleware | httptest.NewRequest with valid/invalid/missing tokens |
| TLS config | Verify TLS version and cipher suite on test server |

### 11. Spec References for Implementation

| What | Spec | Section |
|------|------|---------|
| TLS 1.3 mandatory | RFC 8446 | §1.2 |
| TLS cipher suites | RFC 8446 | §4.1.2 |
| OAuth2 token | RFC 6749 | §4.4 |
| JWT Bearer token | RFC 7523 | §2.2 |
| AES-256-GCM | NIST SP 800-38D | Pass |
| HKDF | RFC 5869 | §2 |
| KEK/DEK hierarchy | AWS CSEC | Best practices |
| RADIUS shared secret | RFC 2865 | §5.2 |
| Message-Authenticator | RFC 3579 | §3.2 |

### 12. Known Constraints

- **No external network calls during tests**: VaultKeyManager needs a mock or interface-based testing approach
- **SoftHSM2 dependency**: SoftHSMKeyManager needs `libsofthsm2.so` and tokens initialized — may need conditional compilation or build tags
- **Vault on kubeadm**: No cloud provider SDK; use raw HTTP calls to Vault REST API
- **Key material in memory**: Go's GC may leave key material in memory — acceptable for Phase 5, document as a known limitation
- **Phase 7 owns cert-manager**: TLS certificates are provisioned by cert-manager in Phase 7. Phase 5 configures the TLS server to use whatever certs are available on disk.

---

*Research completed: 2026-04-28*
*Source: docs/design/{15,16,17}_*.md, cmd/{biz,http-gateway}/main.go, internal/{auth,crypto,config}/, internal/storage/postgres/session.go*
