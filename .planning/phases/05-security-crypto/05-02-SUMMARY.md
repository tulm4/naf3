---
wave: 2
plan: 05-02-PLAN.md
completed: 2026-04-28
---

# Wave 2 Summary: Session Encryption & Storage Wiring

## Objectives

Implement per-session encryption using the `KeyManager` interface. Wire `crypto.KeyManager` into the storage layer so session state is encrypted at rest in PostgreSQL.

## What Was Done

### Per-Session Encryption — `internal/crypto/session.go`

- `SessionState` struct mirrors the session data (Gpsi, Snssai, AuthStatus, etc.) with JSON tags
- `EncryptedSession` struct holds `Ciphertext`, `Nonce`, `Tag`, `EncryptedDEK`, `DEKVersion`
- `EncryptSession(ctx, state, km)` — serializes `SessionState` to JSON, generates per-session DEK, encrypts, wraps DEK
- `DecryptSession(ctx, encrypted, km)` — unwraps DEK, decrypts ciphertext, unmarshals JSON

### KEK Rotation — `internal/crypto/rotation.go`

- `KEKRotator` struct manages KEK version overlap window
- `NewKEKRotator(km KeyManager, overlapDays int)` — constructor
- `OverlapEndsAt() time.Time` — returns when the old KEK is no longer needed
- Rotation enables zero-downtime KEK updates

### Storage Wiring — `internal/storage/postgres/session_store.go`

- `NewSessionStore(pool, repository, km KeyManager)` — accepts `KeyManager` instead of `*Encryptor`
- `Store.Save(ctx, ...)` — encrypts session state before delegating to repository
- `Store.Load(ctx, ...)` — delegates to repository, then decrypts using `crypto.DecryptSession`
- `AIWStore.Save` / `AIWStore.Load` — same pattern for AIW sessions

### Repository Changes — `internal/storage/postgres/session.go`

- Removed `*Encryptor` from `Repository` struct (decryption moved to store layer)
- `scanSession` / `scanSessionFromRows` return raw base64-decoded bytes (no internal decryption)
- Added `Upsert(ctx, session) error` method for combined insert/update

### Biz Pod Wiring — `cmd/biz/main.go`

- Initializes `github.com/operator/nssAAF/internal/crypto` package
- Calls `crypto.Init(&cfg.Crypto)` using `config.CryptoConfig`
- Passes `crypto.KM()` to session store constructors

## Key Design Decisions

1. **Encryption granularity**: Per-session DEK (unique per session record) wrapped with KEK
2. **State machine**: `SessionState` tracks `AuthStatus` transitions (PENDING → EAP_SUCCESS/EAP_FAILURE)
3. **KEK versioning**: Wrapped DEK carries its version; overlap window allows zero-downtime rotation
4. **Decryption at load**: Store layer decrypts; repository returns raw bytes for SELECT queries
5. **JSON serialization**: `SessionState` marshaled to JSON before encryption for structured storage

## Files Changed

| File | Change |
|---|---|
| `internal/crypto/session.go` | New — `SessionState`, `EncryptedSession`, `EncryptSession`, `DecryptSession` |
| `internal/crypto/rotation.go` | New — `KEKRotator` for KEK version overlap window |
| `internal/storage/postgres/session_store.go` | Accept `KeyManager`, encrypt/decrypt in store layer |
| `internal/storage/postgres/session.go` | Remove `*Encryptor`, add `Upsert`, return raw bytes from scans |
| `cmd/biz/main.go` | `crypto.Init()`, pass `crypto.KM()` to stores |

## Verification

```
go build ./internal/crypto/...     # PASS
go test ./internal/crypto/...       # PASS (all 31 tests)
go build ./internal/storage/postgres/...   # PASS
go test ./internal/storage/postgres/...   # PASS
go build ./cmd/biz/...              # PASS
```

## Acceptance Criteria

- [x] `SessionState` struct with JSON tags for serialization
- [x] `EncryptSession` / `DecryptSession` round-trip
- [x] Storage accepts `KeyManager`, encrypts on Save, decrypts on Load
- [x] `KEKRotator` overlap window implemented
- [x] Biz Pod initializes crypto package before using stores
- [x] All packages build and tests pass
