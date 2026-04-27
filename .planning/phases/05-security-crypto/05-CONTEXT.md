# Phase 5: Security & Crypto - Context

**Gathered:** 2026-04-27
**Status:** Ready for planning

<domain>
## Phase Boundary

Implement TLS 1.3 for all external SBI interfaces (N58, N60), Go stdlib mTLS between Biz Pod and AAA Gateway, JWT Bearer token validation on the HTTP Gateway, and AES-256-GCM session state encryption with a KEK/DEK envelope hierarchy backed by Vault transit (prod) / SoftHSM (dev) / soft mode (dev-only env var).

This phase makes NSSAAF ready for production deployment with proper transport security and data-at-rest encryption.

Not this phase: Kubernetes manifests for TLS cert management (Phase 7), cert-manager resources, ArgoCD TLS policies.
</domain>

<decisions>
## Implementation Decisions

### JWT validation boundary
- **D-01:** HTTP Gateway validates all inbound N58/N60 Bearer tokens (from AMF/AUSF) — struct: issuer, audience, expiry, NF type, scope
- Biz Pod does NOT re-validate inbound tokens — trusts gateway
- Biz Pod still needs its own token infrastructure for outbound NF calls (NRF discovery, UDM `Nudm_UECM_Get`, AUSF)
- Token validation: RSA + ECDSA signing methods accepted; `nnssaaf-nssaa` / `nnssaaf-aiw` audiences; AMF/AUSF NF types; `scope=nnssaaf-nssaa` / `scope=nnssaaf-aiw`
- See `docs/design/15_sbi_security.md` §3.2 for TokenValidator struct

### mTLS approach
- **D-02:** Go stdlib throughout — HTTP Gateway and Biz Pod both use explicit cert loading
- Config-driven enable/disable for both HTTP Gateway TLS server and Biz Pod AAA Gateway mTLS client
- HTTP Gateway Istio mTLS is optional: stdlib TLS active by default; `ISTIO_MTLS=1` env var enables Istio sidecar mode (no explicit cert loading in app)
- **D-03:** Biz Pod → AAA Gateway mTLS already has scaffolding in `cmd/biz/main.go` (`UseMTLS`, `tls.Config`, `CertFile`/`KeyFile`/`CAFile`) — needs completion of `internal/auth/` and wiring
- HTTP Gateway `cmd/http-gateway/main.go` currently has no TLS config — needs `internal/auth/` integration for TLS server setup
- See `docs/design/15_sbi_security.md` §2 and §4 for TLS config and mTLS config schemas

### HSM/KMS scope
- **D-04:** Three-layer key management:
  - `KeyManager` interface defined in `internal/crypto/kms.go`
  - Three implementations: `SoftKeyManager` (env var `MASTER_KEY_HEX`, 32-byte hex), `SoftHSMKeyManager` (PKCS#11 via SoftHSM2, dev/test), `VaultKeyManager` (HashiCorp Vault transit engine API, production)
- Vault runs as Kubernetes deployment on kubeadm (not AWS); transit engine for KEK wrap/unwrapping
- DEK generated per session/data item with `crypto/rand`; KEK never leaves HSM/Vault
- 30-day KEK overlap window for rotation
- KEK rotation: new KEK generated in Vault → old KEK retained for overlap window → background re-encryption of existing DEKs
- See `docs/design/17_crypto.md` §5 for KeyManager interface and Vault transit integration

### Session state encryption
- **D-05:** `postgres.NewSessionStore` currently accepts `*postgres.Encryptor` with empty key — needs real `*crypto.Envelope` wiring
- `EncryptedSession` struct: ciphertext + nonce + tag + encrypted DEK + KEK version
- PostgreSQL session store reads/writes encrypted session state via crypto package
- See `docs/design/17_crypto.md` §7 for EncryptedSession and session encryption flow

### RADIUS shared secret (AAA security)
- **D-06:** RADIUS shared secrets stored encrypted in PostgreSQL via crypto envelope (DEK per secret, KEK from Vault/SoftHSM)
- Shared secret rotation: dual-secret mode (current + previous valid during overlap)
- Secret rotation triggered manually or via cron; not automatic within Phase 5
- See `docs/design/16_aaa_security.md` §2.2-2.3 for storage schema and rotation manager

### Claude's Discretion
- TLS cipher suite ordering and exact preference list
- Token cache TTL and eviction policy
- Vault transit engine endpoint path and auth method (Kubernetes auth vs token)
- Exact PostgreSQL column types for encrypted fields (BYTEA vs TEXT with base64)
- SoftHSM token slot / object label conventions
- Whether to encrypt RADIUS shared secrets in Phase 5 or defer to Phase 6
- Audit log TLS cipher field population timing (Phase 5 or Phase 6)
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Security Design
- `docs/design/15_sbi_security.md` — TLS 1.3 config, OAuth2 token validation, Istio mTLS, IP allowlist, audit logging
- `docs/design/16_aaa_security.md` — RADIUS shared secret storage/rotation, RADIUS DTLS, Diameter IPSec, AAA IP allowlist
- `docs/design/17_crypto.md` — KeyManager interface, SoftKeyManager, VaultKeyManager, SoftHSMKeyManager, AES-256-GCM, envelope encryption, KEK/DEK hierarchy, session encryption

### Architecture
- `docs/design/01_service_model.md` §5.4 — 3-component responsibilities (HTTP GW: TLS termination; Biz Pod: EAP + crypto; AAA GW: raw transport)
- `docs/design/10_ha_architecture.md` — HA patterns, circuit breaker (security events from Phase 4)

### Implementation Patterns (from existing code)
- `cmd/biz/main.go` — Existing mTLS scaffolding (UseMTLS, tls.Config, CertFile/KeyFile/CAFile); `nssaaStore`/`aiwStore` with encryptor
- `cmd/http-gateway/main.go` — Needs TLS server setup; currently has no TLS config
- `internal/auth/auth.go` — 5-line stub, needs full JWT validator + middleware
- `internal/crypto/crypto.go` — 5-line stub, needs full crypto package
- `internal/config/config.go` — `TLSConfig`, `BizConfig`, existing config structure
- `internal/storage/postgres/session_store.go` — `NewSessionStore` with encryptor parameter (exists from Phase 4)

### 3GPP Specifications
- TS 29.500 §5 — SBI TLS requirements
- TS 33.310 — NF TLS profile
- RFC 5246 / RFC 8446 — TLS 1.2 / TLS 1.3
- RFC 5216 — EAP-TLS MSK derivation (for session key handling)
- RFC 2865 / RFC 3579 — RADIUS security (shared secret, Message-Authenticator)

</canonical_refs>

<codebase_context>
## Existing Code Insights

### Reusable Assets
- `cmd/biz/main.go` — mTLS scaffolding already exists; `mustLoadCert()`, `mustLoadCertPool()`, `tls.Config` for AAA Gateway HTTP client
- `internal/config/config.go` — `TLSConfig`, `BizConfig` with `UseMTLS`, `TLSCert`, `TLSKey`, `TLSCA` fields
- `internal/storage/postgres/session_store.go` — `NewSessionStore(*Pool, *Encryptor)` already accepts an encryptor
- `internal/logging/gpsi.go` — GPSI hashing pattern (SHA-256, first 16 bytes) — reuse for identity hashing

### Established Patterns
- Option function pattern (`WithAAA`, `WithAPIRoot`) from handler packages — apply to auth middleware
- `slog.NewJSONHandler` for structured logging — extend with TLS cipher and auth events
- Environment variable detection pattern (e.g., `ISTIO_MTLS` env var) for feature flags
- TLS cipher suites: Go stdlib default is TLS 1.3-first — align with design doc

### Integration Points
- `cmd/http-gateway/main.go` — Central point for TLS server setup; add `TLSConfig` loading and `ListenAndServeTLS`
- `cmd/biz/main.go` — mTLS config already wired; complete `internal/auth/` integration
- `internal/api/nssaa/handler.go` — Trust gateway-validated token; no re-validation needed in handler
- `internal/api/aiw/handler.go` — Same as NSSAA handler
- `internal/storage/postgres/session_store.go` — Wire real `crypto.Envelope` instead of empty encryptor

### Known Gaps
- `internal/auth/auth.go` — stub only; needs TokenValidator struct, middleware, JWKS fetching
- `internal/crypto/crypto.go` — stub only; needs entire `internal/crypto/` package per design doc
- `cmd/http-gateway/main.go` — no TLS server config; no JWT middleware
- Vault transit engine client not yet designed (part of `internal/crypto/kms.go`)
- `postgres.Encryptor` is not yet a real implementation — uses empty key

</codebase_context>

<deferred>
## Deferred Ideas

None — all security requirements discussed within scope.

### Reviewed Todos (not folded)
None.

</deferred>

---

*Phase: 05-security-crypto*
*Context gathered: 2026-04-27*
