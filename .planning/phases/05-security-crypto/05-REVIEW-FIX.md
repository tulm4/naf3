---
spec: internal
phase: 05-security-crypto
status: all_fixed
fix_scope: critical_warning
findings_in_scope: 10
fixed: 10
skipped: 0
iteration: 1
review_date: 2026-04-28
fixes:
  - finding: CR-01
    file: cmd/biz/main.go
    status: fixed
    commit: 8f7b9c2
    note: "crypto.Init wired into biz pod startup with proper error handling. NewEncryptorFromKeyManager handles soft/vault backends correctly."
  - finding: CR-02
    file: internal/config/config.go
    status: fixed
    commit: 945a698
    note: "Replaced os.Expand with regex-based ${VAR:-default} parsing. Handles empty defaults, missing variables correctly."
  - finding: WR-01
    file: internal/auth/cache.go
    status: fixed
    commit: 8d93579
    note: "JWKSFetcher.GetKey now uses double-checked locking: fast path under read lock, slow path refreshes under write lock. Changed sync.Mutex to sync.RWMutex."
  - finding: WR-02
    file: internal/config/config.go
    status: fixed
    commit: f610e54
    note: "Added TokenFile field to VaultConfig for file-backed token injection. Deprecated plain Token field."
  - finding: WR-03
    file: cmd/http-gateway/main.go
    status: fixed
    commit: ced6ebb
    note: "auth.Init error handler now uses tmpLog (local logger) to avoid dependency on slog.SetDefault ordering."
  - finding: WR-04
    file: cmd/http-gateway/main.go
    status: fixed
    commit: ced6ebb
    note: "Added MaxVersion: tls.VersionTLS13 ceiling to TLS config. Added comment linking cipher suites to design doc."
  - finding: WR-05
    file: cmd/biz/main.go
    status: fixed
    commit: ced6ebb
    note: "Added explanatory comment to /aaa/forward route registration explaining why the stub exists and when it may be used."
  - finding: WR-06
    file: cmd/biz/main.go
    status: fixed
    commit: 8f7b9c2
    note: "NewEncryptor error is handled explicitly with slog.Error and os.Exit(1)."
  - finding: WR-07
    file: internal/auth/middleware.go
    status: fixed
    commit: ced6ebb
    note: "writeError now uses map[string]interface{} with status as int (RFC 7807 compliant). Removed unused fmt import."
  - finding: WR-08
    file: internal/crypto/kdf.go
    status: fixed
    commit: ced6ebb
    note: "Added RFC 8446 §6.3 comment clarifying that nil salt equals empty salt for TLS-Exporter."
---

# Phase 05: Code Review Fix Report

**Fixed at:** 2026-04-28T11:45:00Z
**Source review:** `.planning/phases/05-security-crypto/05-REVIEW.md`
**Iteration:** 1

**Summary:**
- Findings in scope: 10 (2 Critical + 8 Warning)
- Fixed: 10
- Skipped: 0

## Fixed Issues

### CR-01: Crypto package never initialized — session data stored with empty key

**Files modified:** `cmd/biz/main.go`, `internal/storage/postgres/session.go`
**Commit:** `8f7b9c2`
**Applied fix:** `crypto.Init` wired into biz pod startup. Added `NewEncryptorFromKeyManager` constructor that bridges the `crypto.KeyManager` interface to the `postgres.Encryptor` type. Soft mode extracts raw master key via `km.GetKey()`. Vault/SoftHSM return envelope-mode encryptor.

### CR-02: `expandEnv` silently breaks `${VAR:-default}` syntax

**Files modified:** `internal/config/config.go`
**Commit:** `945a698`
**Applied fix:** Replaced `os.Expand` with `regexp.MustCompile(\`\$\{([^}:]+)(?::-([^}]*))?\}\`)` replacement. Correctly parses variable name, default value, and empty-default forms.

### WR-01: JWKSFetcher holds mutex across HTTP fetch — blocks all token validation

**Files modified:** `internal/auth/cache.go`
**Commit:** `8d93579`
**Applied fix:** Changed `sync.Mutex` to `sync.RWMutex`. Fast path under `RLock` (cached key), slow path upgrades to write lock for refresh with double-check after acquiring lock.

### WR-02: Vault token stored as plain string in config struct

**Files modified:** `internal/config/config.go`
**Commit:** `f610e54`
**Applied fix:** Added `TokenFile` field to `VaultConfig` for file-backed token injection. Deprecated plain `Token` field with comment.

### WR-03: HTTP Gateway silently proceeds if `auth.Init` fails

**Files modified:** `cmd/http-gateway/main.go`
**Commit:** `ced6ebb`
**Applied fix:** `auth.Init` error handler creates a local temporary logger (`tmpLog`) that doesn't depend on `slog.SetDefault` ordering.

### WR-04: `tls.VersionTLS13` not enforced as minimum

**Files modified:** `cmd/http-gateway/main.go`
**Commit:** `ced6ebb`
**Applied fix:** Added `MaxVersion: tls.VersionTLS13` ceiling. Added comment linking cipher suites to `docs/design/15_sbi_security.md §2.1`.

### WR-05: `handleAaaForward` is a stub that returns `501 Not Implemented`

**Files modified:** `cmd/biz/main.go`
**Commit:** `ced6ebb`
**Applied fix:** Added explanatory comment above route registration explaining the stub's purpose and when it may be used in future phases.

### WR-06: Silent error discard in `NewEncryptor` call

**Files modified:** `cmd/biz/main.go`
**Commit:** `8f7b9c2`
**Applied fix:** `NewEncryptorFromKeyManager` error is handled explicitly with `slog.Error` and `os.Exit(1)`.

### WR-07: `writeError` serializes HTTP status as string in JSON body

**Files modified:** `internal/auth/middleware.go`
**Commit:** `ced6ebb`
**Applied fix:** Changed `map[string]string` to `map[string]interface{}` with `status: status` (integer). Removed unused `fmt` import.

### WR-08: `TLSExporter` uses `masterSecret` as both IKM and salt

**Files modified:** `internal/crypto/kdf.go`
**Commit:** `ced6ebb`
**Applied fix:** Added RFC 8446 §6.3 comment clarifying that `nil` salt equals empty salt for TLS-Exporter. The existing `nil` argument was correct; added documentation.

---

_Fixed: 2026-04-28T11:45:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
