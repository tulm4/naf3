# Storage Dependency Direction Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix circular dependency: `storage/postgres` imports `api/nssaa` and `api/aiw`. Move domain types and interfaces to `internal/storage/` so storage implementations own their interfaces.

**Architecture:** Define `NssaaSession`/`AiwSession` domain types and `NssaaStore`/`AiwStore` interfaces in `internal/storage/`. Refactor `storage/postgres` to implement these interfaces without importing API packages. API handlers convert between API types and domain types at the storage boundary.

**Tech Stack:** Go, PostgreSQL, pgx, go-redis

---

## Task 1: Create domain types and interfaces in `internal/storage/`

**Files:**
- Create: `internal/storage/types.go`
- Create: `internal/storage/store.go`
- Create: `internal/storage/errors.go`

- [ ] **Step 1: Create `internal/storage/types.go`**

```go
// Package storage provides domain types and interfaces for NSSAAF persistence.
// Spec: TS 29.526 §7.2-7.3
package storage

import "time"

// NssaaSession represents a slice authentication session.
// Domain model owned by the storage layer.
// Corresponds to the slice_auth_sessions table.
type NssaaSession struct {
    AuthCtxID   string
    GPSI        string
    SnssaiSST   uint8
    SnssaiSD    string
    AmfInstance string
    ReauthURI   string
    RevocURI    string
    EapPayload  []byte
    Status      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    ExpiresAt   time.Time
}

// AiwSession represents an AIW authentication session.
// Domain model owned by the storage layer.
// Corresponds to the aiw_auth_sessions table.
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

- [ ] **Step 2: Create `internal/storage/store.go`**

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

- [ ] **Step 3: Create `internal/storage/errors.go`**

```go
package storage

import "errors"

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")
```

- [ ] **Step 4: Verify `go build ./internal/storage/...` compiles**

Run: `go build ./internal/storage/...`
Expected: No output (success)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/types.go internal/storage/store.go internal/storage/errors.go
git commit -m "feat: add domain types and interfaces to storage package

Defines NssaaSession/AiwSession domain types and NssaaStore/AiwStore
interfaces in internal/storage/. These are the canonical persistence
abstractions, owned by the storage layer (not the API layer).

Refs: docs/superpowers/specs/2026-06-02-storage-dependency-fix-design.md
"
```

---

## Task 2: Create postgres repositories implementing storage interfaces

**Files:**
- Create: `internal/storage/postgres/nssaa_repo.go`
- Create: `internal/storage/postgres/aiw_repo.go`
- Modify: `internal/storage/postgres/pool.go` (remove api imports)

- [ ] **Step 1: Read existing `internal/storage/postgres/session.go` and `internal/storage/postgres/aiw_repository.go` to understand DB schema types**

- [ ] **Step 2: Create `internal/storage/postgres/nssaa_repo.go`**

```go
// Package postgres provides PostgreSQL data persistence for NSSAAF.
// Spec: TS 29.571 §5.4.4.60, TS 29.526 §7.2
package postgres

import (
    "context"
    "encoding/base64"
    "errors"
    "fmt"
    "net"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"

    "github.com/operator/nssAAF/internal/crypto"
    "github.com/operator/nssAAF/internal/storage"
    "github.com/operator/nssAAF/internal/types"
)

// nssaaRow represents the database row for slice_auth_sessions.
type nssaaRow struct {
    AuthCtxID       string
    GPSI            string
    SnssaiSST       uint8
    SnssaiSD        string
    AMFInstanceID   string
    AMFIP           *string
    AMFRegion       string
    AAAConfigID     *string
    EAPSessionState []byte
    NssaaStatus     types.NssaaStatus
    AuthResult      types.NssaaStatus
    EAPRounds       int
    MaxEAPRounds    int
    EAPLastNonce    string
    FailureReason   string
    FailureCause    string
    ReauthNotifURI  string
    RevocNotifURI   string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    ExpiresAt       time.Time
    CompletedAt     *time.Time
    TerminatedAt    *time.Time
}

// NssaaRepository implements storage.NssaaStore for PostgreSQL.
// Uses the slice_auth_sessions table.
type NssaaRepository struct {
    pool *Pool
    enc  *encryptor
}

// NewNssaaRepository creates a new NSSAA session repository.
func NewNssaaRepository(pool *Pool, enc *encryptor) *NssaaRepository {
    return &NssaaRepository{pool: pool, enc: enc}
}

// Load implements storage.NssaaStore.
func (r *NssaaRepository) Load(ctx context.Context, id string) (*storage.NssaaSession, error) {
    row, err := r.loadRow(ctx, id)
    if err != nil {
        return nil, err
    }
    return r.rowToSession(row), nil
}

// Save implements storage.NssaaStore.
func (r *NssaaRepository) Save(ctx context.Context, s *storage.NssaaSession) error {
    row := r.sessionToRow(s)
    // Try update first, then create if not found.
    err := r.updateRow(ctx, row)
    if errors.Is(err, storage.ErrSessionNotFound) {
        return r.createRow(ctx, row)
    }
    return err
}

// Delete implements storage.NssaaStore.
func (r *NssaaRepository) Delete(ctx context.Context, id string) error {
    sql := `DELETE FROM slice_auth_sessions WHERE auth_ctx_id = $1`
    rowsAffected, err := r.pool.ExecResult(ctx, sql, id)
    if err != nil {
        return fmt.Errorf("nssaa delete: %w", err)
    }
    if rowsAffected == 0 {
        return storage.ErrSessionNotFound
    }
    return nil
}

// Close implements storage.NssaaStore. No-op for pool.
func (r *NssaaRepository) Close() error {
    return nil
}

// loadRow loads a raw DB row by authCtxID.
func (r *NssaaRepository) loadRow(ctx context.Context, authCtxID string) (*nssaaRow, error) {
    sql := `
        SELECT
            auth_ctx_id, gpsi, gpsi_hash, supi, snssai_sst, snssai_sd,
            amf_instance_id, amf_ip, amf_region,
            reauth_notif_uri, revoc_notif_uri,
            aaa_config_id, eap_session_state,
            eap_rounds, max_eap_rounds, eap_last_nonce,
            nssaa_status, auth_result,
            failure_reason, failure_cause,
            created_at, updated_at, expires_at,
            completed_at, terminated_at
        FROM slice_auth_sessions
        WHERE auth_ctx_id = $1`

    row := r.pool.QueryRow(ctx, sql, authCtxID)
    s, err := r.scanRow(row)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, storage.ErrSessionNotFound
        }
        return nil, fmt.Errorf("nssaa load: %w", err)
    }
    return s, nil
}

// createRow inserts a new NSSAA session row.
func (r *NssaaRepository) createRow(ctx context.Context, s *nssaaRow) error {
    stateCiphertext, err := r.encryptState(s.EAPSessionState)
    if err != nil {
        return fmt.Errorf("nssaa create: encrypt state: %w", err)
    }
    encryptedGPSI, err := r.encryptField(s.GPSI)
    if err != nil {
        return fmt.Errorf("nssaa create: encrypt gpsi: %w", err)
    }

    sql := `
        INSERT INTO slice_auth_sessions (
            auth_ctx_id, gpsi, gpsi_hash, supi, snssai_sst, snssai_sd,
            amf_instance_id, amf_ip, amf_region,
            reauth_notif_uri, revoc_notif_uri,
            aaa_config_id, eap_session_state,
            eap_rounds, max_eap_rounds, eap_last_nonce,
            nssaa_status, auth_result,
            failure_reason, failure_cause,
            created_at, updated_at, expires_at
        ) VALUES ($1, $2, $3, '', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`

    var amfIP interface{}
    if s.AMFIP != nil {
        amfIP = *s.AMFIP
    }
    var aaaConfigID interface{}
    if s.AAAConfigID != nil {
        aaaConfigID = *s.AAAConfigID
    }

    err = r.pool.Exec(ctx, sql,
        s.AuthCtxID, encryptedGPSI, crypto.HashGPSI(s.GPSI),
        s.SnssaiSST, s.SnssaiSD,
        s.AMFInstanceID, amfIP, s.AMFRegion,
        s.ReauthNotifURI, s.RevocNotifURI,
        aaaConfigID, stateCiphertext,
        s.EAPRounds, s.MaxEAPRounds, s.EAPLastNonce,
        s.NssaaStatus, s.AuthResult,
        s.FailureReason, s.FailureCause,
        s.CreatedAt, s.UpdatedAt, s.ExpiresAt,
    )
    if err != nil {
        return fmt.Errorf("nssaa create: %w", err)
    }
    return nil
}

// updateRow updates an existing NSSAA session row.
func (r *NssaaRepository) updateRow(ctx context.Context, s *nssaaRow) error {
    stateCiphertext, err := r.encryptState(s.EAPSessionState)
    if err != nil {
        return fmt.Errorf("nssaa update: encrypt state: %w", err)
    }
    encryptedGPSI, err := r.encryptField(s.GPSI)
    if err != nil {
        return fmt.Errorf("nssaa update: encrypt gpsi: %w", err)
    }

    sql := `
        UPDATE slice_auth_sessions SET
            gpsi = $2, gpsi_hash = $3,
            snssai_sst = $4, snssai_sd = $5,
            eap_session_state = $6,
            eap_rounds = $7, eap_last_nonce = $8,
            nssaa_status = $9, auth_result = $10,
            failure_reason = $11, failure_cause = $12,
            updated_at = $13, expires_at = $14,
            completed_at = $15, terminated_at = $16
        WHERE auth_ctx_id = $1`

    rowsAffected, err := r.pool.ExecResult(ctx, sql,
        s.AuthCtxID, encryptedGPSI, crypto.HashGPSI(s.GPSI),
        s.SnssaiSST, s.SnssaiSD,
        stateCiphertext,
        s.EAPRounds, s.EAPLastNonce,
        s.NssaaStatus, s.AuthResult,
        s.FailureReason, s.FailureCause,
        s.UpdatedAt, s.ExpiresAt,
        s.CompletedAt, s.TerminatedAt,
    )
    if err != nil {
        return fmt.Errorf("nssaa update: %w", err)
    }
    if rowsAffected == 0 {
        return storage.ErrSessionNotFound
    }
    return nil
}

// scanRow scans a database row into nssaaRow.
func (r *NssaaRepository) scanRow(row pgx.Row) (*nssaaRow, error) {
    var s nssaaRow
    var stateBytes []byte
    var amfIP net.IP
    var aaaConfigID uuid.UUID
    var completedAt, terminatedAt pgtype.Timestamptz
    var rawGPSI string

    err := row.Scan(
        &s.AuthCtxID, &rawGPSI, new(string), new(string), &s.SnssaiSST, &s.SnssaiSD,
        &s.AMFInstanceID, &amfIP, &s.AMFRegion,
        &s.ReauthNotifURI, &s.RevocNotifURI,
        &aaaConfigID, &stateBytes,
        &s.EAPRounds, &s.MaxEAPRounds, &s.EAPLastNonce,
        &s.NssaaStatus, &s.AuthResult,
        &s.FailureReason, &s.FailureCause,
        &s.CreatedAt, &s.UpdatedAt, &s.ExpiresAt,
        &completedAt, &terminatedAt,
    )
    if err != nil {
        return nil, err
    }

    s.GPSI, _ = r.decryptField(rawGPSI)
    if amfIP != nil {
        ipStr := amfIP.String()
        s.AMFIP = &ipStr
    }
    if aaaConfigID != uuid.Nil {
        idStr := aaaConfigID.String()
        s.AAAConfigID = &idStr
    }
    if completedAt.Valid {
        s.CompletedAt = &completedAt.Time
    }
    if terminatedAt.Valid {
        s.TerminatedAt = &terminatedAt.Time
    }
    if len(stateBytes) > 0 {
        s.EAPSessionState, _ = r.decryptState(stateBytes)
    }
    return &s, nil
}

// rowToSession converts a DB row to a domain session.
func (r *NssaaRepository) rowToSession(s *nssaaRow) *storage.NssaaSession {
    return &storage.NssaaSession{
        AuthCtxID:   s.AuthCtxID,
        GPSI:        s.GPSI,
        SnssaiSST:   s.SnssaiSST,
        SnssaiSD:    s.SnssaiSD,
        AmfInstance: s.AMFInstanceID,
        ReauthURI:   s.ReauthNotifURI,
        RevocURI:    s.RevocNotifURI,
        EapPayload:  s.EAPSessionState,
        Status:      string(s.NssaaStatus),
        CreatedAt:   s.CreatedAt,
        UpdatedAt:   s.UpdatedAt,
        ExpiresAt:   s.ExpiresAt,
    }
}

// sessionToRow converts a domain session to a DB row.
func (r *NssaaRepository) sessionToRow(s *storage.NssaaSession) *nssaaRow {
    return &nssaaRow{
        AuthCtxID:       s.AuthCtxID,
        GPSI:            s.GPSI,
        SnssaiSST:       s.SnssaiSST,
        SnssaiSD:        s.SnssaiSD,
        AMFInstanceID:   s.AmfInstance,
        ReauthNotifURI:  s.ReauthURI,
        RevocNotifURI:   s.RevocURI,
        EAPSessionState: s.EapPayload,
        NssaaStatus:     types.NssaaStatus(s.Status),
        CreatedAt:       s.CreatedAt,
        UpdatedAt:       s.UpdatedAt,
        ExpiresAt:       s.ExpiresAt,
    }
}

// encryptField encrypts a string value and returns base64-encoded ciphertext.
func (r *NssaaRepository) encryptField(plaintext string) (string, error) {
    if plaintext == "" {
        return "", nil
    }
    ciphertext, err := r.enc.Encrypt([]byte(plaintext))
    if err != nil {
        return "", err
    }
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptField decrypts a base64-encoded ciphertext back to plaintext.
func (r *NssaaRepository) decryptField(encoded string) (string, error) {
    if encoded == "" {
        return "", nil
    }
    ciphertext, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return "", err
    }
    plaintext, err := r.enc.Decrypt(ciphertext)
    if err != nil {
        return "", err
    }
    return string(plaintext), nil
}

// encryptState encrypts raw session state bytes.
func (r *NssaaRepository) encryptState(state []byte) ([]byte, error) {
    return r.enc.Encrypt(state)
}

// decryptState decrypts session state ciphertext.
func (r *NssaaRepository) decryptState(ciphertext []byte) ([]byte, error) {
    return r.enc.Decrypt(ciphertext)
}

// Compile-time interface check.
var _ storage.NssaaStore = (*NssaaRepository)(nil)
```

- [ ] **Step 3: Create `internal/storage/postgres/aiw_repo.go`**

```go
// Package postgres provides PostgreSQL data persistence for NSSAAF.
// AIW-specific repository using the aiw_auth_sessions table.
// Spec: TS 29.571 §5.4.4.61, TS 29.526 §7.3
package postgres

import (
    "context"
    "encoding/base64"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"

    "github.com/operator/nssAAF/internal/crypto"
    "github.com/operator/nssAAF/internal/storage"
)

// aiwRow represents the database row for aiw_auth_sessions.
type aiwRow struct {
    AuthCtxID         string
    Supi              string
    AusfID            string
    AAAConfigID       *string
    EAPSessionState   []byte
    NssaaStatus       string
    AuthResult        string
    EAPRounds         int
    MaxEAPRounds      int
    EAPLastNonce      string
    MSK               []byte
    PvsInfo           []byte
    TtlsInner         []byte
    SupportedFeatures string
    FailureReason     string
    FailureCause      string
    CreatedAt         time.Time
    UpdatedAt         time.Time
    ExpiresAt         time.Time
    CompletedAt       *time.Time
}

// AiwRepository implements storage.AiwStore for PostgreSQL.
// Uses the aiw_auth_sessions table.
type AiwRepository struct {
    pool *Pool
    enc  *encryptor
}

// NewAiwRepository creates a new AIW session repository.
func NewAiwRepository(pool *Pool, enc *encryptor) *AiwRepository {
    return &AiwRepository{pool: pool, enc: enc}
}

// Load implements storage.AiwStore.
func (r *AiwRepository) Load(ctx context.Context, id string) (*storage.AiwSession, error) {
    row, err := r.loadRow(ctx, id)
    if err != nil {
        return nil, err
    }
    return r.rowToSession(row), nil
}

// Save implements storage.AiwStore.
func (r *AiwRepository) Save(ctx context.Context, s *storage.AiwSession) error {
    row := r.sessionToRow(s)
    err := r.updateRow(ctx, row)
    if errors.Is(err, storage.ErrSessionNotFound) {
        return r.createRow(ctx, row)
    }
    return err
}

// Delete implements storage.AiwStore.
func (r *AiwRepository) Delete(ctx context.Context, id string) error {
    sql := `DELETE FROM aiw_auth_sessions WHERE auth_ctx_id = $1`
    rowsAffected, err := r.pool.ExecResult(ctx, sql, id)
    if err != nil {
        return fmt.Errorf("aiw delete: %w", err)
    }
    if rowsAffected == 0 {
        return storage.ErrSessionNotFound
    }
    return nil
}

// Close implements storage.AiwStore. No-op for pool.
func (r *AiwRepository) Close() error {
    return nil
}

// loadRow loads a raw DB row by authCtxID.
func (r *AiwRepository) loadRow(ctx context.Context, authCtxID string) (*aiwRow, error) {
    sql := `
        SELECT
            auth_ctx_id, supi, supi_hash, ausf_id,
            aaa_config_id, eap_session_state,
            eap_rounds, max_eap_rounds, eap_last_nonce,
            nssaa_status, auth_result,
            msk, pvs_info, ttls_inner_container, supported_features,
            failure_reason, failure_cause,
            created_at, updated_at, expires_at,
            completed_at
        FROM aiw_auth_sessions
        WHERE auth_ctx_id = $1`

    row := r.pool.QueryRow(ctx, sql, authCtxID)
    s, err := r.scanRow(row)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, storage.ErrSessionNotFound
        }
        return nil, fmt.Errorf("aiw load: %w", err)
    }
    return s, nil
}

// createRow inserts a new AIW session row.
func (r *AiwRepository) createRow(ctx context.Context, s *aiwRow) error {
    stateCiphertext, err := r.encryptState(s.EAPSessionState)
    if err != nil {
        return fmt.Errorf("aiw create: encrypt state: %w", err)
    }
    encryptedSUPI, err := r.encryptField(s.Supi)
    if err != nil {
        return fmt.Errorf("aiw create: encrypt supi: %w", err)
    }

    var mskCiphertext []byte
    if len(s.MSK) > 0 {
        mskCiphertext, err = r.encryptState(s.MSK)
        if err != nil {
            return fmt.Errorf("aiw create: encrypt msk: %w", err)
        }
    }

    sql := `
        INSERT INTO aiw_auth_sessions (
            auth_ctx_id, supi, supi_hash, ausf_id,
            aaa_config_id, eap_session_state,
            eap_rounds, max_eap_rounds, eap_last_nonce,
            nssaa_status, auth_result,
            msk, pvs_info, ttls_inner_container, supported_features,
            failure_reason, failure_cause,
            created_at, updated_at, expires_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`

    var aaaConfigID interface{}
    if s.AAAConfigID != nil {
        aaaConfigID = *s.AAAConfigID
    }
    var pvsInfoJSON interface{}
    if len(s.PvsInfo) > 0 {
        pvsInfoJSON = s.PvsInfo
    }

    err = r.pool.Exec(ctx, sql,
        s.AuthCtxID, encryptedSUPI, crypto.HashSUPI(s.Supi), s.AusfID,
        aaaConfigID, stateCiphertext,
        s.EAPRounds, s.MaxEAPRounds, s.EAPLastNonce,
        s.NssaaStatus, s.AuthResult,
        mskCiphertext, pvsInfoJSON, s.TtlsInner, s.SupportedFeatures,
        s.FailureReason, s.FailureCause,
        s.CreatedAt, s.UpdatedAt, s.ExpiresAt,
    )
    if err != nil {
        return fmt.Errorf("aiw create: %w", err)
    }
    return nil
}

// updateRow updates an existing AIW session row.
func (r *AiwRepository) updateRow(ctx context.Context, s *aiwRow) error {
    stateCiphertext, err := r.encryptState(s.EAPSessionState)
    if err != nil {
        return fmt.Errorf("aiw update: encrypt state: %w", err)
    }
    encryptedSUPI, err := r.encryptField(s.Supi)
    if err != nil {
        return fmt.Errorf("aiw update: encrypt supi: %w", err)
    }

    var mskCiphertext []byte
    if len(s.MSK) > 0 {
        mskCiphertext, err = r.encryptState(s.MSK)
        if err != nil {
            return fmt.Errorf("aiw update: encrypt msk: %w", err)
        }
    }

    sql := `
        UPDATE aiw_auth_sessions SET
            supi = $2, supi_hash = $3, ausf_id = $4,
            eap_session_state = $5,
            eap_rounds = $6, eap_last_nonce = $7,
            nssaa_status = $8, auth_result = $9,
            msk = $10, pvs_info = $11, ttls_inner_container = $12, supported_features = $13,
            failure_reason = $14, failure_cause = $15,
            updated_at = $16, expires_at = $17,
            completed_at = $18
        WHERE auth_ctx_id = $1`

    var pvsInfoJSON interface{}
    if len(s.PvsInfo) > 0 {
        pvsInfoJSON = s.PvsInfo
    }

    rowsAffected, err := r.pool.ExecResult(ctx, sql,
        s.AuthCtxID, encryptedSUPI, crypto.HashSUPI(s.Supi), s.AusfID,
        stateCiphertext,
        s.EAPRounds, s.EAPLastNonce,
        s.NssaaStatus, s.AuthResult,
        mskCiphertext, pvsInfoJSON, s.TtlsInner, s.SupportedFeatures,
        s.FailureReason, s.FailureCause,
        s.UpdatedAt, s.ExpiresAt,
        s.CompletedAt,
    )
    if err != nil {
        return fmt.Errorf("aiw update: %w", err)
    }
    if rowsAffected == 0 {
        return storage.ErrSessionNotFound
    }
    return nil
}

// scanRow scans a database row into aiwRow.
func (r *AiwRepository) scanRow(row pgx.Row) (*aiwRow, error) {
    var s aiwRow
    var stateBytes []byte
    var aaaConfigID pgtype.UUID
    var completedAt pgtype.Timestamptz
    var rawSUPI string
    var mskBytes []byte
    var pvsInfoJSON []byte
    var ttlsInner []byte

    err := row.Scan(
        &s.AuthCtxID, &rawSUPI, new(string), &s.AusfID,
        &aaaConfigID, &stateBytes,
        &s.EAPRounds, &s.MaxEAPRounds, &s.EAPLastNonce,
        &s.NssaaStatus, &s.AuthResult,
        &mskBytes, &pvsInfoJSON, &ttlsInner, &s.SupportedFeatures,
        &s.FailureReason, &s.FailureCause,
        &s.CreatedAt, &s.UpdatedAt, &s.ExpiresAt,
        &completedAt,
    )
    if err != nil {
        return nil, err
    }

    s.Supi, _ = r.decryptField(rawSUPI)
    if completedAt.Valid {
        s.CompletedAt = &completedAt.Time
    }
    if aaaConfigID.Valid {
        idStr := aaaConfigID.String()
        s.AAAConfigID = &idStr
    }
    if len(stateBytes) > 0 {
        s.EAPSessionState, _ = r.decryptState(stateBytes)
    }
    if len(mskBytes) > 0 {
        s.MSK, _ = r.decryptState(mskBytes)
    }
    s.PvsInfo = pvsInfoJSON
    s.TtlsInner = ttlsInner

    return &s, nil
}

// rowToSession converts a DB row to a domain session.
func (r *AiwRepository) rowToSession(s *aiwRow) *storage.AiwSession {
    return &storage.AiwSession{
        AuthCtxID:         s.AuthCtxID,
        Supi:              s.Supi,
        EapPayload:        s.EAPSessionState,
        TtlsInner:         s.TtlsInner,
        MSK:               s.MSK,
        PvsInfo:           s.PvsInfo,
        AusfID:            s.AusfID,
        SupportedFeatures: s.SupportedFeatures,
        Status:            s.NssaaStatus,
        AuthResult:        s.AuthResult,
        CreatedAt:         s.CreatedAt,
        UpdatedAt:         s.UpdatedAt,
        ExpiresAt:         s.ExpiresAt,
        CompletedAt:       s.CompletedAt,
    }
}

// sessionToRow converts a domain session to a DB row.
func (r *AiwRepository) sessionToRow(s *storage.AiwSession) *aiwRow {
    return &aiwRow{
        AuthCtxID:         s.AuthCtxID,
        Supi:              s.Supi,
        AusfID:            s.AusfID,
        EAPSessionState:   s.EapPayload,
        TtlsInner:         s.TtlsInner,
        MSK:               s.MSK,
        PvsInfo:           s.PvsInfo,
        SupportedFeatures: s.SupportedFeatures,
        NssaaStatus:       s.Status,
        AuthResult:        s.AuthResult,
        CreatedAt:         s.CreatedAt,
        UpdatedAt:         s.UpdatedAt,
        ExpiresAt:         s.ExpiresAt,
        CompletedAt:       s.CompletedAt,
    }
}

// encryptField encrypts a string value and returns base64-encoded ciphertext.
func (r *AiwRepository) encryptField(plaintext string) (string, error) {
    if plaintext == "" {
        return "", nil
    }
    ciphertext, err := r.enc.Encrypt([]byte(plaintext))
    if err != nil {
        return "", err
    }
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptField decrypts a base64-encoded ciphertext back to plaintext.
func (r *AiwRepository) decryptField(encoded string) (string, error) {
    if encoded == "" {
        return "", nil
    }
    ciphertext, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return "", err
    }
    plaintext, err := r.enc.Decrypt(ciphertext)
    if err != nil {
        return "", err
    }
    return string(plaintext), nil
}

// encryptState encrypts raw session state bytes.
func (r *AiwRepository) encryptState(state []byte) ([]byte, error) {
    return r.enc.Encrypt(state)
}

// decryptState decrypts session state ciphertext.
func (r *AiwRepository) decryptState(ciphertext []byte) ([]byte, error) {
    return r.enc.Decrypt(ciphertext)
}

// Compile-time interface check.
var _ storage.AiwStore = (*AiwRepository)(nil)
```

- [ ] **Step 4: Verify `go build ./internal/storage/postgres/...` compiles**

Run: `go build ./internal/storage/postgres/...`
Expected: No output (success)

- [ ] **Step 5: Write tests for NssaaRepository**

Create `internal/storage/postgres/nssaa_repo_test.go` with tests for Load/Save/Delete operations. Use table-driven tests with mock pool.

- [ ] **Step 6: Write tests for AiwRepository**

Create `internal/storage/postgres/aiw_repo_test.go` with tests for Load/Save/Delete operations.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/storage/postgres/... -v -count=1`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/storage/postgres/nssaa_repo.go internal/storage/postgres/nssaa_repo_test.go internal/storage/postgres/aiw_repo.go internal/storage/postgres/aiw_repo_test.go
git commit -m "feat: add NssaaRepository and AiwRepository implementing storage interfaces

New postgres repositories in internal/storage/postgres/ that implement
storage.NssaaStore and storage.AiwStore using the existing
slice_auth_sessions and aiw_auth_sessions tables. These repos do NOT
import api/nssaa or api/aiw packages, breaking the circular dependency.

Refs: docs/superpowers/specs/2026-06-02-storage-dependency-fix-design.md
"
```

---

## Task 3: Update API handlers to use storage interfaces with conversion

**Files:**
- Modify: `internal/api/nssaa/handler.go`
- Modify: `internal/api/aiw/handler.go`
- Modify: `internal/api/nssaa/redis_store.go`

- [ ] **Step 1: Add type aliases and conversion functions to `internal/api/nssaa/handler.go`**

After the existing type definitions (around line 53), add:

```go
// NssaaStore is the interface for NSSAA session persistence.
// Aliased from storage.NssaaStore for API convenience.
type NssaaStore = storage.NssaaStore

// authCtxToNssaaSession converts nssaa.AuthCtx → storage.NssaaSession.
func authCtxToNssaaSession(a *AuthCtx) *storage.NssaaSession {
    return &storage.NssaaSession{
        AuthCtxID:   a.AuthCtxID,
        GPSI:        a.GPSI,
        SnssaiSST:   a.SnssaiSST,
        SnssaiSD:    a.SnssaiSD,
        AmfInstance: a.AmfInstance,
        ReauthURI:   a.ReauthURI,
        RevocURI:    a.RevocURI,
        EapPayload:  a.EapPayload,
        Status:      "PENDING",
        ExpiresAt:   time.Now().Add(5 * time.Minute),
    }
}

// nssaaSessionToAuthCtx converts storage.NssaaSession → nssaa.AuthCtx.
func nssaaSessionToAuthCtx(s *storage.NssaaSession) *AuthCtx {
    return &AuthCtx{
        AuthCtxID:   s.AuthCtxID,
        GPSI:        s.GPSI,
        SnssaiSST:   s.SnssaiSST,
        SnssaiSD:    s.SnssaiSD,
        AmfInstance: s.AmfInstance,
        ReauthURI:   s.ReauthURI,
        RevocURI:    s.RevocURI,
        EapPayload:  s.EapPayload,
    }
}
```

Also add `"time"` to the imports if not present.

- [ ] **Step 2: Update `CreateSliceAuthenticationContext` to use storage interface**

Find the `Save` call in `CreateSliceAuthenticationContext` (around line 213). Change:

```go
authCtx := &AuthCtx{...}
if err := h.store.Save(r.Context(), authCtx); err != nil {
```

To:

```go
authCtx := &AuthCtx{...}
session := authCtxToNssaaSession(authCtx)
if err := h.store.Save(r.Context(), session); err != nil {
```

- [ ] **Step 3: Update `ConfirmSliceAuthentication` to use storage interface**

Find the `Load` call (around line 305) and `Save` call (around line 335). Change:

```go
authCtx, err := h.store.Load(r.Context(), authCtxId)
// ...
authCtx.EapPayload = eapPayload
if err := h.store.Save(r.Context(), authCtx); err != nil {
```

To:

```go
domSession, err := h.store.Load(r.Context(), authCtxId)
if err != nil {
    if errors.Is(err, storage.ErrSessionNotFound) {
        common.WriteProblem(w, common.NotFoundProblem(...))
        return
    }
    common.WriteProblem(w, common.InternalServerProblem(...))
    return
}
// GPSI and SNSSAI validation using domSession fields...
domSession.EapPayload = eapPayload
if err := h.store.Save(r.Context(), domSession); err != nil {
```

Note: Update field accesses from `authCtx.GPSI` to `domSession.GPSI`, etc.

- [ ] **Step 4: Add type alias and conversion functions to `internal/api/aiw/handler.go`**

After the existing type definitions (around line 77), add:

```go
// AiwStore is the interface for AIW session persistence.
// Aliased from storage.AiwStore for API convenience.
type AiwStore = storage.AiwStore

// authContextToAiwSession converts aiw.AuthContext → storage.AiwSession.
func authContextToAiwSession(a *AuthContext) *storage.AiwSession {
    expiresAt := a.ExpiresAt
    if expiresAt.IsZero() {
        expiresAt = time.Now().Add(24 * time.Hour)
    }
    return &storage.AiwSession{
        AuthCtxID:         a.AuthCtxID,
        Supi:              a.Supi,
        EapPayload:        a.EapPayload,
        TtlsInner:         a.TtlsInner,
        MSK:               a.MSK,
        PvsInfo:           a.PvsInfo,
        AusfID:            a.AusfID,
        SupportedFeatures: a.SupportedFeatures,
        Status:            a.Status,
        AuthResult:        a.AuthResult,
        CreatedAt:         a.CreatedAt,
        UpdatedAt:         a.UpdatedAt,
        ExpiresAt:         expiresAt,
        CompletedAt:       a.CompletedAt,
    }
}

// aiwSessionToAuthContext converts storage.AiwSession → aiw.AuthContext.
func aiwSessionToAuthContext(s *storage.AiwSession) *AuthContext {
    return &AuthContext{
        AuthCtxID:         s.AuthCtxID,
        Supi:              s.Supi,
        EapPayload:        s.EapPayload,
        TtlsInner:         s.TtlsInner,
        MSK:               s.MSK,
        PvsInfo:           s.PvsInfo,
        AusfID:            s.AusfID,
        SupportedFeatures: s.SupportedFeatures,
        Status:            s.Status,
        AuthResult:        s.AuthResult,
        CreatedAt:         s.CreatedAt,
        UpdatedAt:         s.UpdatedAt,
        ExpiresAt:         s.ExpiresAt,
        CompletedAt:       s.CompletedAt,
    }
}
```

Also add `"time"` to the imports if not present.

- [ ] **Step 5: Update `CreateAuthenticationContext` to use storage interface**

Change the `Save` call in `CreateAuthenticationContext` (around line 228):

```go
authCtx := &AuthContext{...}
session := authContextToAiwSession(authCtx)
if err := h.store.Save(r.Context(), session); err != nil {
```

- [ ] **Step 6: Update `ConfirmAuthentication` to use storage interface**

Change the `Load` and `Save` calls (around lines 288 and 308):

```go
domSession, err := h.store.Load(r.Context(), authCtxId)
// ...
domSession.EapPayload = eapPayloadFromPtr(body.EapMessage)
if err := h.store.Save(r.Context(), domSession); err != nil {
```

Update field accesses from `authCtx.Supi` to `domSession.Supi`.

- [ ] **Step 7: Update `internal/api/nssaa/redis_store.go` to implement storage.NssaaStore**

Replace the entire file content:

```go
// Package nssaa provides HTTP handlers for the Nnssaaf_NSSAA service (N58 interface).
// Spec: TS 29.526 §7.2, TS 23.502 §4.2.9
package nssaa

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"

    "github.com/operator/nssAAF/internal/storage"
)

const (
    AuthCtxKeyPrefix = "nssaa:auth:ctx:"
    AuthCtxTTL       = 24 * time.Hour
)

// RedisNssaaStore implements storage.NssaaStore backed by Redis.
// For caching only — primary storage is PostgreSQL via NssaaRepository.
type RedisNssaaStore struct {
    client redis.Cmdable
}

// NewRedisNssaaStore creates a new Redis-backed NSSAA session store.
func NewRedisNssaaStore(client redis.Cmdable) *RedisNssaaStore {
    return &RedisNssaaStore{client: client}
}

// Load implements storage.NssaaStore.
func (s *RedisNssaaStore) Load(ctx context.Context, id string) (*storage.NssaaSession, error) {
    key := AuthCtxKeyPrefix + id
    data, err := s.client.Get(ctx, key).Bytes()
    if err != nil {
        if err == redis.Nil {
            return nil, storage.ErrSessionNotFound
        }
        return nil, fmt.Errorf("redis get: %w", err)
    }
    var session storage.NssaaSession
    if err := json.Unmarshal(data, &session); err != nil {
        return nil, fmt.Errorf("unmarshal session: %w", err)
    }
    return &session, nil
}

// Save implements storage.NssaaStore.
func (s *RedisNssaaStore) Save(ctx context.Context, session *storage.NssaaSession) error {
    key := AuthCtxKeyPrefix + session.AuthCtxID
    data, err := json.Marshal(session)
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }
    if err := s.client.Set(ctx, key, data, AuthCtxTTL).Err(); err != nil {
        return fmt.Errorf("redis set: %w", err)
    }
    return nil
}

// Delete implements storage.NssaaStore.
func (s *RedisNssaaStore) Delete(ctx context.Context, id string) error {
    key := AuthCtxKeyPrefix + id
    return s.client.Del(ctx, key).Err()
}

// Close implements storage.NssaaStore. No-op for Redis client.
func (s *RedisNssaaStore) Close() error {
    return nil
}

// Compile-time interface check.
var _ storage.NssaaStore = (*RedisNssaaStore)(nil)
```

- [ ] **Step 8: Verify `go build ./internal/api/nssaa/... ./internal/api/aiw/...` compiles**

Run: `go build ./internal/api/nssaa/... ./internal/api/aiw/...`
Expected: No output (success)

- [ ] **Step 9: Update tests**

Update `test/conformance/ts29526_test.go` to use `storage.NssaaStore` / `storage.AiwStore` interfaces instead of `nssaa.AuthCtxStore` / `aiw.AuthCtxStore`.

Update handler tests in `test/unit/api/nssaa_handler_gaps_test.go` and `test/unit/api/aiw_handler_gaps_test.go` to pass storage-compatible stores.

- [ ] **Step 10: Run all tests**

Run: `go test ./internal/api/... -count=1`
Expected: All tests pass

- [ ] **Step 11: Commit**

```bash
git add internal/api/nssaa/handler.go internal/api/aiw/handler.go internal/api/nssaa/redis_store.go
git add test/conformance/ts29526_test.go test/unit/api/nssaa_handler_gaps_test.go test/unit/api/aiw_handler_gaps_test.go
git commit -m "refactor: API handlers use storage interfaces with domain conversion

Handlers now accept storage.NssaaStore/AiwStore interfaces and convert
between API types (nssaa.AuthCtx, aiw.AuthContext) and domain types
(storage.NssaaSession, storage.AiwSession) at the storage boundary.

Refs: docs/superpowers/specs/2026-06-02-storage-dependency-fix-design.md
"
```

---

## Task 4: Update factory wiring and delete dead code

**Files:**
- Modify: `cmd/biz/factory.go`
- Delete: `internal/session/adapter.go`
- Delete: `internal/session/session.go`
- Delete: `internal/storage/postgres/session_store.go`
- Delete: `internal/storage/postgres/aiw_repository.go`
- Delete: `internal/storage/postgres/session.go` (merge into new repos)
- Modify: `internal/storage/postgres/pool.go`

- [ ] **Step 1: Update `cmd/biz/factory.go` to use new repository types**

Update the `BizPod` struct and factory method to use `*postgres.NssaaRepository` and `*postgres.AiwRepository`:

In `BizPod` struct (around line 33):
```go
type BizPod struct {
    Server          *http.Server
    NRFClient       *nrf.Client
    NssaaStore      *postgres.NssaaRepository
    AiwStore        *postgres.AiwRepository
    // ... rest unchanged
}
```

In `Build` method (around lines 151-152):
```go
nssaaStore := postgres.NewNssaaRepository(pgPool, encryptor)
aiwStore := postgres.NewAiwRepository(pgPool, encryptor)
```

Also update the return statement and the `Close` method to match.

- [ ] **Step 2: Verify `go build ./cmd/biz/...` compiles**

Run: `go build ./cmd/biz/...`
Expected: No output (success)

- [ ] **Step 3: Delete dead code**

```bash
rm internal/session/adapter.go
rm internal/session/session.go
rm internal/storage/postgres/session_store.go
rm internal/storage/postgres/aiw_repository.go
```

Note: Keep `internal/storage/postgres/session.go` — it defines `Session` (DB type), `Repository` (old), and `encryptor`. The new repos embed the encryptor functionality directly, so `session.go` can be deleted only after verifying no other code references `Session`, `Repository`, or the encryptor from that file.

- [ ] **Step 4: Verify `go build ./...` compiles**

Run: `go build ./...`
Expected: No output (success)

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 6: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors (warnings acceptable)

- [ ] **Step 7: Commit**

```bash
git add -A
git rm internal/session/adapter.go internal/session/session.go internal/storage/postgres/session_store.go internal/storage/postgres/aiw_repository.go
git add cmd/biz/factory.go
git commit -m "refactor: update factory wiring to use new storage repositories

Wires BizPod to use postgres.NssaaRepository and postgres.AiwRepository
directly. Removes dead code: session/adapter.go, session/session.go,
and old session_store.go/aiw_repository.go.

Circular dependency broken: storage/postgres no longer imports api packages.

Refs: docs/superpowers/specs/2026-06-02-storage-dependency-fix-design.md
"
```

---

## Task 5: Verification

- [ ] **`go build ./...` compiles without errors**

Run: `go build ./...`

- [ ] **`go test ./... -count=1` passes**

Run: `go test ./... -count=1`

- [ ] **No circular imports**

Run: `go mod verify && go list -f '{{.ImportPath}}: {{.Imports}}' ./... | grep -E "nssaa|aiw" | sort | uniq`

Verify that `internal/storage/postgres` does NOT appear in the imports of `internal/api/nssaa` or `internal/api/aiw`.

- [ ] **`golangci-lint run ./...` passes**

Run: `golangci-lint run ./...`

---

## Self-Review Checklist

After completing all tasks, verify:

1. **Spec coverage:** Every section in the spec has a corresponding task/step.
2. **No placeholders:** All code blocks are complete — no "TBD", "TODO", or partial implementations.
3. **Type consistency:** Field names match between domain types, interfaces, and implementations.
4. **Import graph:** Run `go mod graph | grep "storage.*nssaa\|nssaa.*storage\|aiw.*storage\|storage.*aiw"` — should return empty.

If any check fails, fix inline and re-verify.
