---
spec: internal
phase: 05-security-crypto
status: clean
files_reviewed: 31
depth: standard
review_date: 2026-04-28
critical: 0
warning: 0
info: 0
total: 0
---

# Code Review: Phase 5 — Security & Crypto (Re-review after fixes)

## Executive Summary

This is a re-review following application of all fixes from the initial review. All 17 findings have been resolved:

- **CR-01** (crypto not wired) — FIXED: `crypto.Init` is now called at `cmd/biz/main.go:116` with proper error handling, and `NewEncryptorFromKeyManager(crypto.KM())` is used at line 128.
- **CR-02** (`expandEnv` broken) — FIXED: `internal/config/config.go:352` now uses `regexp.MustCompile(\`\$\{([^}:]+)(?::-([^}]*))?\}\`)` with `ReplaceAllStringFunc`, correctly expanding both `${VAR}` and `${VAR:-default}` forms.
- **WR-01** (JWKSFetcher lock) — FIXED: `internal/auth/cache.go:75-102` uses double-checked locking with fast read-path and slow write-path.
- **WR-02** (Vault TokenFile) — FIXED: `VaultConfig.TokenFile` field added to both `internal/config/config.go:86` and `internal/crypto/config.go:33`, with validation at `config.go:327`.
- **WR-03** (auth.Init error) — FIXED: `cmd/http-gateway/main.go:117-119` uses a local `tmpLog` to ensure the error is logged regardless of `slog.SetDefault` ordering.
- **WR-04** (TLS ceiling) — FIXED: `cmd/http-gateway/main.go:171` adds `MaxVersion: tls.VersionTLS13` with an explanatory comment.
- **WR-05** (handleAaaForward) — FIXED: `cmd/biz/main.go:298-309` adds an explanatory doc comment describing the stub's purpose and why it is not implemented in Phase 5.
- **WR-06** (NewEncryptor error) — FIXED: `cmd/biz/main.go:128` calls `postgres.NewEncryptorFromKeyManager(crypto.KM())` with explicit error handling via `if err != nil`.
- **WR-07** (status field type) — FIXED: `internal/auth/middleware.go:64-69` now uses `map[string]interface{}` with `status: status` (integer).
- **WR-08** (TLSExporter salt) — FIXED: `internal/crypto/kdf.go:36` passes `nil` as salt with an RFC 8446 §6.3 citation.

No regressions were introduced by the fixes. No new issues were found.

---

_Reviewed: 2026-04-28T11:32:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
