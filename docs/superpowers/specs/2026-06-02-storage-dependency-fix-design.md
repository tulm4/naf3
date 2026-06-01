# Storage Dependency Direction Fix — Design Spec

**Date:** 2026-06-02
**Status:** Approved

## Problem Statement

The current dependency graph creates a circular dependency:

```
storage/postgres/session_store.go
    ↓ imports
api/nssaa/AuthCtx, api/aiw/AuthContext (types + interfaces)
    ↓ used by
api/nssaa/handler.go, api/aiw/handler.go
    ↓ calls
storage/postgres/session_store.go
```

This violates the dependency inversion principle. The `storage` package (infrastructure) depends on `api` packages (consumers).

Additionally, `internal/session/adapter.go` was intended to fix this but is dead code because `storage/postgres` bypasses it entirely.

## Target Architecture

```
api/nssaa/handler.go  ──depends on──►  storage.NssaaStore (interface)
api/aiw/handler.go    ──depends on──►  storage.AiwStore (interface)
                                          ↑
                                          │
internal/eap/              ──depends on──►  storage.NssaaSession (domain)
                                          │
storage/postgres/           ──implements──┘
```

**Dependency direction:** Consumers depend on abstractions in `internal/storage/`. Implementations (`postgres`) depend on the same abstractions.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Interface location | `internal/storage/` | Natural home for persistence interfaces |
| Number of interfaces | Two separate | `NssaaStore` and `AiwStore` — different data models |
| Type ownership | Domain types in storage | API types in api/, domain types in storage |
| Session package | Merge into storage | Session models are storage-related |
| Adapter file | Delete | Dead code, superseded by domain approach |
| Migration strategy | Big bang | Clean break, no dual-interface complexity |

## New Package Structure

```
internal/storage/
├── types.go           # Domain types: NssaaSession, AiwSession
├── store.go           # Interfaces: NssaaStore, AiwStore
├── errors.go          # Shared errors
├── postgres/
│   ├── pool.go
│   ├── session.go     # DB schema types
│   ├── nssaa_repo.go  # Implements storage.NssaaStore
│   ├── aiw_repo.go   # Implements storage.AiwStore
│   └── ...
└── memory/
    └── store.go      # In-memory implementation for tests
```

## Domain Types (`internal/storage/types.go`)

```go
package storage

import "time"

// NssaaSession represents a slice authentication session.
// Domain model owned by the storage layer.
type NssaaSession struct {
    AuthCtxID   string
    GPSI        string
    SnssaiSST   uint8
    SnssaiSD    string
    AmfInstance string
    ReauthURI   string
    RevocURI    string
    EapPayload  []byte
    CreatedAt   time.Time
    UpdatedAt   time.Time
    ExpiresAt   time.Time
}

// AiwSession represents an AIW authentication session.
// Domain model owned by the storage layer.
type AiwSession struct {
    AuthCtxID         string
    Supi              string
    EapPayload        []byte
    TtlsInner         []byte
    MSK               []byte
    PvsInfo           []byte
    AusfID            string
    SupportedFeatures string
    Status            string
    AuthResult        string
    CreatedAt         time.Time
    UpdatedAt         time.Time
    ExpiresAt         time.Time
    CompletedAt       *time.Time
}
```

## Interfaces (`internal/storage/store.go`)

```go
package storage

import "context"

// NssaaStore manages NSSAA slice authentication sessions.
type NssaaStore interface {
    Load(ctx context.Context, id string) (*NssaaSession, error)
    Save(ctx context.Context, session *NssaaSession) error
    Delete(ctx context.Context, id string) error
    Close() error
}

// AiwStore manages AIW authentication sessions.
type AiwStore interface {
    Load(ctx context.Context, id string) (*AiwSession, error)
    Save(ctx context.Context, session *AiwSession) error
    Delete(ctx context.Context, id string) error
    Close() error
}
```

## Errors (`internal/storage/errors.go`)

```go
package storage

import "errors"

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")
```

## API Handler Changes

### NSSAA Handler

```go
// internal/api/nssaa/handler.go

// NssaaStore is the interface for NSSAA session persistence.
// Aliased from storage.NssaaStore for API convenience.
type NssaaStore = storage.NssaaStore
```

The handler continues to use `nssaa.AuthCtx` internally. Conversion happens at the storage boundary:

```go
func (h *Handler) CreateSliceAuthenticationContext(...) {
    // ... validation ...
    
    // Convert API type to domain type
    domainSession := &storage.NssaaSession{
        AuthCtxID:   authCtxID,
        GPSI:        string(body.Gpsi),
        SnssaiSST:   sst,
        SnssaiSD:    sd,
        // ... other fields ...
    }
    
    if err := h.store.Save(r.Context(), domainSession); err != nil {
        // error handling
    }
}
```

### AIW Handler

```go
// internal/api/aiw/handler.go

// AiwStore is the interface for AIW session persistence.
type AiwStore = storage.AiwStore
```

## PostgreSQL Implementation

### Renamed Repository Files

- `session_store.go` → `nssaa_repo.go` (implements `storage.NssaaStore`)
- `aiw_repository.go` → `aiw_repo.go` (implements `storage.AiwStore`)

### Updated Signatures

```go
// internal/storage/postgres/nssaa_repo.go

type NssaaRepository struct {
    pool *Pool
    enc  *encryptor
}

func NewNssaaRepository(pool *Pool, enc *encryptor) *NssaaRepository

// Load implements storage.NssaaStore.
func (r *NssaaRepository) Load(ctx context.Context, id string) (*storage.NssaaSession, error)

// Save implements storage.NssaaStore.
func (r *NssaaRepository) Save(ctx context.Context, s *storage.NssaaSession) error

// Delete implements storage.NssaaStore.
func (r *NssaaRepository) Delete(ctx context.Context, id string) error

// Close implements storage.NssaaStore.
func (r *NssaaRepository) Close() error
```

## Migration Steps

### Step 1: Create new storage types and interfaces

Create `internal/storage/types.go`, `internal/storage/store.go`, `internal/storage/errors.go`.

### Step 2: Verify domain types align with existing session models

Review `internal/session/session.go` to ensure `storage.NssaaSession` and `storage.AiwSession` fields match. The session types already exist in `session/` and should inform the domain types.

### Step 3: Create postgres repositories

Create `internal/storage/postgres/nssaa_repo.go` and `internal/storage/postgres/aiw_repo.go` implementing the new interfaces.

### Step 4: Update API handlers

Update `internal/api/nssaa/handler.go` and `internal/api/aiw/handler.go`:
- Import `internal/storage`
- Add conversion functions between API types and domain types
- Update `NewHandler` to accept `storage.NssaaStore` / `storage.AiwStore`

### Step 5: Delete dead code

- Delete `internal/session/` directory
- Delete `internal/storage/postgres/session_store.go`
- Delete `internal/storage/postgres/aiw_repository.go`

### Step 6: Update wiring

Update `cmd/biz/main.go` and any other files that construct handlers with stores.

### Step 7: Update tests

- Update unit tests in `internal/storage/postgres/`
- Update handler tests in `internal/api/nssaa/`, `internal/api/aiw/`
- Create conversion function tests if needed

## Verification

- [ ] `go build ./...` compiles without errors
- [ ] `go test ./...` passes
- [ ] No circular imports (verify with `go mod why` or import graph)
- [ ] `golangci-lint run ./...` passes

## Files to Delete

- `internal/session/adapter.go`
- `internal/session/session.go`
- `internal/session/memory.go`
- `internal/session/store.go`
- `internal/storage/postgres/session_store.go`
- `internal/storage/postgres/aiw_repository.go`

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/storage/types.go` | Create |
| `internal/storage/store.go` | Create |
| `internal/storage/errors.go` | Create |
| `internal/storage/postgres/nssaa_repo.go` | Create |
| `internal/storage/postgres/aiw_repo.go` | Create |
| `internal/storage/postgres/migrate.go` | Modify (remove api imports) |
| `internal/api/nssaa/handler.go` | Modify (use storage interfaces, add conversions) |
| `internal/api/aiw/handler.go` | Modify (use storage interfaces, add conversions) |
| `internal/api/nssaa/redis_store.go` | Modify (implement storage.NssaaStore) |
| `cmd/biz/main.go` | Modify (wiring) |
| `internal/storage/postgres/pool.go` | Modify (remove api imports) |
