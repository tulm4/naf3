---
spec: internal
phase: 05-security-crypto
status: issues-found
files_reviewed: 31
depth: standard
review_date: 2026-04-28
critical: 2
warning: 8
info: 7
total: 17
---

# Code Review: Phase 5 — Security & Crypto

## Executive Summary

Phase 5 introduces a well-structured cryptographic foundation with AES-256-GCM envelope encryption, a KeyManager interface with three backends (Soft, Vault, SoftHSM), JWT Bearer authentication for N58/N60, and TLS 1.3 support. The implementation is generally sound with proper use of `crypto/rand` for nonces, HKDF for key derivation, HMAC constant-time comparison, and AAD support in GCM. However, two critical issues undermine the security posture: (1) the crypto package is never wired into `cmd/biz/main.go`, leaving the session encryptor with an empty key and silently discarded error; and (2) the `expandEnv` function silently strips `${VAR:-default}` syntax used per its own docstring. A thorough cross-cutting review also surfaced locking inefficiency in JWKSFetcher, hardcoded Vault token in config, missing auth init validation in the gateway, and several other concerns.

---

## CRITICAL

### CR-01: Crypto package never initialized — session data stored with empty key

**File:** `cmd/biz/main.go:104-105`

```go
var encryptor *postgres.Encryptor
encryptor, _ = postgres.NewEncryptor([]byte{}) // empty key = no encryption for now
```

The Phase 5 `internal/crypto/` package (envelope encryption, KEK/DEK, KeyManager) is defined but never wired into `cmd/biz/main.go`. No `crypto.Init` call, no import of `crypto` package. Instead, a hardcoded empty 32-byte slice is passed to `postgres.NewEncryptor`, and the error return is silently discarded with `_`. Session data (GPSI, auth results) is stored in plaintext in PostgreSQL.

**Severity:** Critical — data at rest is unencrypted despite Phase 5's mandate to add envelope encryption. The empty-key initialization bypasses all Phase 5 crypto work.

**Recommendation:** Wire `crypto.Init` from `internal/crypto/config.go` into the biz pod startup, use the resulting `KeyManager` to wrap session keys via `EnvelopeEncrypt`, and surface the error rather than discarding it:

```go
if err := crypto.Init(&crypto.Config{
    KeyManager:     cfg.Crypto.KeyManager,
    MasterKeyHex:   cfg.Crypto.MasterKeyHex,
    KEKOverlapDays: cfg.Crypto.KEKOverlapDays,
    Vault:          vaultCfg,
}); err != nil {
    slog.Error("crypto.Init failed", "error", err)
    os.Exit(1)
}
```

---

### CR-02: `expandEnv` silently breaks `${VAR:-default}` syntax

**File:** `internal/config/config.go:344-350`

```go
func expandEnv(s string) string {
    result := os.Expand(s, func(key string) string {
        return os.Getenv(key)
    })
    return result
}
```

The docstring on line 344 advertises support for `${VAR:-default}`, but `os.Expand` with a custom function only handles `${VAR}` — it never evaluates the `:-default` suffix. A config value of `"${MISSING_VAR:-fallback}"` expands to an empty string rather than `"fallback"`. The `biz.yaml` template uses `"${MASTER_KEY_HEX}"` without a default, but any future use of the `:-default` pattern will silently fail.

**Severity:** Critical — config schemas that rely on `:-default` for optional fields will get empty values at runtime with no error signal.

**Recommendation:** Implement proper brace-enclosed syntax with default value support:

```go
func expandEnv(s string) string {
    re := regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)
    return re.ReplaceAllStringFunc(s, func(match string) string {
        parts := re.FindStringSubmatch(match)
        if val := os.Getenv(parts[1]); val != "" {
            return val
        }
        return parts[2] // default or empty string
    })
}
```

---

## WARNINGS

### WR-01: JWKSFetcher holds mutex across HTTP fetch — blocks all token validation

**File:** `internal/auth/cache.go:72-90`

```go
func (f *JWKSFetcher) GetKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
    f.mu.Lock()
    defer f.mu.Unlock() // Lock held during HTTP I/O below
    // ...
    if err := f.refreshLocked(ctx); err != nil { // HTTP call here
        return nil, err
    }
```

`GetKey` acquires a write lock and holds it across the HTTP request to the NRF JWKS endpoint. Any concurrent call to `GetKey` (even for a cached key) blocks until the fetch completes. Under NRF unavailability, every token validation request blocks for 10 seconds.

**Severity:** Warning — high latency under JWKS cache miss or NRF unavailability. Affects all N58/N60 traffic.

**Recommendation:** Move the HTTP fetch out of the lock. Use a separate goroutine or a per-fetch lock to avoid blocking cached lookups:

```go
func (f *JWKSFetcher) GetKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
    // Fast path: check cache without write lock
    f.mu.RLock()
    if entry, ok := f.entries[kid]; ok && time.Since(f.fetchedAt) <= f.ttl {
        f.mu.RUnlock()
        return entry, nil
    }
    f.mu.RUnlock()

    // Slow path: refresh if needed (under write lock)
    f.mu.Lock()
    defer f.mu.Unlock()
    // Double-check after acquiring write lock
    if entry, ok := f.entries[kid]; ok && time.Since(f.fetchedAt) <= f.ttl {
        return entry, nil
    }
    if err := f.refreshLocked(ctx); err != nil {
        return nil, err
    }
    return f.entries[kid], nil
}
```

---

### WR-02: Vault token stored as plain string in config struct

**File:** `internal/config/config.go:70-81`

```go
type VaultConfig struct {
    Address    string
    KeyName    string
    AuthMethod string
    K8sRole    string
    Token      string `yaml:"token"` // plain text in memory
}
```

The `Token` field holds the HashiCorp Vault token in plaintext in a Go struct that lives for the lifetime of the process. Even with env-var expansion (`${VAULT_TOKEN}`), the value is fully readable in memory via heap inspection or core dumps.

**Severity:** Warning — sensitive credential lives in plaintext in process memory with no option for HSM-backed or external secret injection.

**Recommendation:** Add support for reading the Vault token from a file path (e.g., `tokenFile: /var/run/secrets/vault/token`) and deprecate the plain `token` field. Alternatively, document the risk and recommend Kubernetes secret injection via env vars with short process lifetime.

---

### WR-03: HTTP Gateway silently proceeds if `auth.Init` fails

**File:** `cmd/http-gateway/main.go:116-125`

```go
if err := auth.Init(auth.TokenValidatorConfig{
    // ...
}); err != nil {
    slog.Error("auth.Init failed", "error", err)
    os.Exit(1)
}
```

The `os.Exit(1)` does halt startup, which is correct. However, the `slog.Error` call writes to stdout before `slog.SetDefault` has been called (line 81-82), so the error message may be lost or misformatted in some log configurations. More importantly, there is no test coverage for the auth initialization path, so this safety check is not validated.

**Severity:** Warning — the behavior is correct but fragile; a future refactor that moves `slog.SetDefault` after `auth.Init` would silently swallow the error.

**Recommendation:** Add a test that `auth.Init` failure causes exit, and ensure `slog.SetDefault` is called before any error logging.

---

### WR-04: `tls.VersionTLS13` not enforced as minimum

**File:** `cmd/http-gateway/main.go:166-179`

```go
tlsConfig = &tls.Config{
    MinVersion: tls.VersionTLS13, // ← this is good
    // ...
}
```

The `MinVersion: tls.VersionTLS13` is set correctly, but there is no corresponding `MaxVersion` to prevent TLS 1.2 fallback if a future change sets `MinVersion` lower. The cipher suites are also hardcoded as a list rather than derived from the `tls` package constants, which could drift from the spec.

**Severity:** Warning — if the TLS config is modified in the future, the minimum version constraint could be accidentally weakened.

**Recommendation:** Add `MaxVersion: tls.VersionTLS13` to enforce a ceiling, and add a compile-time assertion or comment linking the cipher suite list to the design doc (`docs/design/15_sbi_security.md §2.1`).

---

### WR-05: `handleAaaForward` is a stub that returns `501 Not Implemented`

**File:** `cmd/biz/main.go:268-270`

```go
func handleAaaForward(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "not implemented", http.StatusNotImplemented)
}
```

This function is registered in the HTTP mux at line 208 (`mux.HandleFunc("/aaa/forward", handleAaaForward)`) but always returns 501. The comment "not implemented" suggests it was planned but deferred. If the AAA Gateway ever attempts to forward requests to the Biz Pod via this endpoint, it will receive 501.

**Severity:** Warning — this endpoint exists in the routing table but has no implementation. Either implement it or remove the route and add a comment explaining why it is not needed.

---

### WR-06: Silent error discard in `NewEncryptor` call

**File:** `cmd/biz/main.go:104-105`

```go
encryptor, _ = postgres.NewEncryptor([]byte{}) // empty key = no encryption for now
```

The underscore-discard pattern hides whatever error `NewEncryptor` returns. If `NewEncryptor` returns `nil, errors.New("key too short")`, the biz pod starts with a `nil` encryptor and will panic at first use — or worse, silently skip encryption.

**Severity:** Warning — silent error discard masks initialization failures.

**Recommendation:** Either log and exit, or explicitly handle the nil case with a comment documenting the risk:

```go
encryptor, err := postgres.NewEncryptor([]byte{})
if err != nil {
    slog.Warn("session encryption disabled: empty key", "error", err)
    encryptor = nil // documented: no encryption with nil encryptor
}
```

---

### WR-07: `writeError` serializes HTTP status as string in JSON body

**File:** `internal/auth/middleware.go:64-71`

```go
func writeError(w http.ResponseWriter, status int, message string) {
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "type":   "https://tools.ietf.org/html/rfc9110#section-15.5.2",
        "title":  "Unauthorized",
        "status": fmt.Sprintf("%d", status), // ← should be int
        "detail": message,
    })
}
```

The `status` field in a ProblemDetails response (RFC 7807) should be an integer, not a string. While RFC 7807 allows any value, 3GPP APIs expect `"status": 401` (integer). The `common.WriteProblem` function in `internal/api/common/middleware.go:175-179` correctly uses `problem.Status` (int), but the auth middleware uses its own `writeError` with the wrong type.

**Severity:** Warning — inconsistent ProblemDetails schema between auth errors (string status) and all other errors (int status). AMF/AUSF clients may fail to parse auth error responses.

---

### WR-08: `TLSExporter` uses `masterSecret` as both IKM and salt

**File:** `internal/crypto/kdf.go:34-40`

```go
func TLSExporter(masterSecret []byte, label string, context []byte, length int) ([]byte, error) {
    prk, err := hkdf.Extract(sha256.New, masterSecret, nil) // masterSecret used as both IKM and salt
    if err != nil {
        return nil, errors.New("TLSExporter: extract failed: " + err.Error())
    }
    return hkdf.Expand(sha256.New, prk, label, length)
}
```

Per RFC 8446 §6.3, `TLS-Exporter` uses an empty salt. The current code passes `masterSecret` as the salt (second argument), making the PRK `HMAC-SHA256(masterSecret, masterSecret)` instead of `HMAC-SHA256(Zero, masterSecret)`. This is a non-standard derivation.

**Severity:** Warning — deviates from RFC 8446 §6.3 which mandates an empty salt. Keys derived with this function may not be compatible with external TLS exporters.

**Recommendation:** Pass `nil` as the salt (Go's HKDF uses `nil` as the zero-length salt):

```go
prk, err := hkdf.Extract(sha256.New, masterSecret, nil) // nil = empty salt per RFC 8446
```

---

## INFORMATIONAL

### IN-01: `TokenCache` is defined but never used

**File:** `internal/auth/auth.go:183-218`

The `TokenCache` struct with `Get`, `Set`, `Remove` methods is defined and fully implemented but is not used anywhere. The comment on line 184 says "Not used in Phase 5 HTTP Gateway middleware (stateless per request). Available for future use." This is acceptable but adds dead weight to the codebase.

**Recommendation:** Keep it if the Phase 6 roadmap includes token caching. Otherwise, remove to reduce maintenance surface.

---

### IN-02: Pod heartbeat Redis client not tracked for cleanup

**File:** `cmd/biz/main.go:336-356`

`podHeartbeat` creates its own `goredis.NewClient` without storing it for `Close()` during shutdown. The `defer func() { _ = rdb.Close() }` inside the goroutine fires when the context is cancelled, but there is no explicit shutdown synchronization — the goroutine may outlive the main shutdown sequence.

**Recommendation:** Pass the `redisPool` pool to `podHeartbeat` instead of creating a separate client, or track the client for deferred cleanup.

---

### IN-03: `handleServerInitiated` discards JSON decode errors in error response

**File:** `cmd/biz/main.go:283-286`

```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest) // exposes internal error detail
    return
}
```

`err.Error()` may contain implementation details (e.g., "expected object, got string"). This leaks internal state in the HTTP response.

**Recommendation:** Return a generic error message in the 400 response body:

```go
http.Error(w, "invalid request body", http.StatusBadRequest)
```

---

### IN-04: `biz.yaml` contains empty `MASTER_KEY_HEX` env default

**File:** `compose/configs/biz.yaml:101`

```yaml
env:
  MASTER_KEY_HEX: ""  # 64-char hex string; REQUIRED for soft mode
```

The empty string is the documented "unset" state, which is correct. However, the `expandEnv` bug (CR-02) means that if someone adds `:-default` for local dev convenience, it silently fails.

**Recommendation:** Once CR-02 is fixed, add `MASTER_KEY_HEX: "${MASTER_KEY_HEX:-000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f}"` as a local dev convenience (documented as insecure).

---

### IN-05: `MiddlewareWithOptions` creates a new Middleware on every request

**File:** `internal/auth/middleware.go:125-132`

```go
return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if skipSet[r.URL.Path] {
            next.ServeHTTP(w, r)
            return
        }
        Middleware(cfg.requiredScope)(next).ServeHTTP(w, r) // ← new closure each time
    })
}
```

`Middleware(cfg.requiredScope)` returns a new `http.HandlerFunc` closure on every incoming request. While the closure is lightweight, this is unnecessary allocation. The `Middleware` function itself is a factory, so calling it per request is correct usage, but it could be cached at construction time.

**Recommendation:** Cache the result of `Middleware(cfg.requiredScope)` in the returned closure instead of re-calling it per request.

---

### IN-06: `ForwardRequest` silently discards response body on non-200

**File:** `cmd/http-gateway/main.go:52-53`

```go
respBody, _ := io.ReadAll(resp.Body)
return respBody, resp.StatusCode, nil
```

If the Biz Pod returns a non-200 with a ProblemDetails body (e.g., 403, 404), the body is read into `respBody` but the error return is `nil`. The caller in `forwardToBiz` then writes `respBody` to the client but has no way to know if it was an error body vs. a success body, because the HTTP status code is returned separately from the error.

**Recommendation:** Return an error for non-2xx status codes, with the body included in the error message:

```go
if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return nil, resp.StatusCode, fmt.Errorf("biz returned %d: %s", resp.StatusCode, string(respBody))
}
```

---

### IN-07: Missing tests for `VaultKeyManager.Wrap` with auth failure

**File:** `internal/crypto/kms.go:150-183`

`VaultKeyManager.Wrap` handles HTTP success but the `setAuthHeader` method has multiple branches (`kubernetes`, `token`, default error). There is no test coverage for auth failures (missing token file, invalid Vault token, wrong auth method).

**Recommendation:** Add unit tests for `VaultKeyManager` with a mock `HTTPDoer` that returns auth errors.

---

## Summary by File

| File | Critical | Warnings | Info |
|---|---|---|---|
| `cmd/biz/main.go` | 1 | 2 | 1 |
| `cmd/http-gateway/main.go` | — | 2 | 1 |
| `internal/auth/auth.go` | — | — | 1 |
| `internal/auth/cache.go` | — | 1 | — |
| `internal/auth/middleware.go` | — | 1 | — |
| `internal/config/config.go` | 1 | — | — |
| `internal/crypto/kdf.go` | — | 1 | — |
| `internal/crypto/kms.go` | — | — | 1 |
| `compose/configs/biz.yaml` | — | 1 | 1 |
| All other files | — | — | — |

---

## Recommended Fix Priority

| ID | Severity | Fix | Effort |
|---|---|---|---|
| CR-01 | Critical | Wire `crypto.Init` into biz main; surface `NewEncryptor` error | High |
| CR-02 | Critical | Implement `${VAR:-default}` in `expandEnv` | Low |
| WR-01 | Warning | Move HTTP fetch out of JWKSFetcher lock | Medium |
| WR-02 | Warning | Add `tokenFile` field for Vault; deprecate plain `Token` | Medium |
| WR-03 | Warning | Add test for `auth.Init` failure path | Low |
| WR-04 | Warning | Add `MaxVersion: tls.VersionTLS13` ceiling | Low |
| WR-05 | Warning | Implement or remove `/aaa/forward` stub route | Low |
| WR-06 | Warning | Remove `_` discard on `NewEncryptor` | Low |
| WR-07 | Warning | Fix `writeError` status field type to int | Low |
| WR-08 | Warning | Fix `TLSExporter` salt to `nil` per RFC 8446 | Low |

---

_Reviewed: 2026-04-28T11:09:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
