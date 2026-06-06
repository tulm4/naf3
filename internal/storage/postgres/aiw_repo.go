// Package postgres provides PostgreSQL data persistence for NSSAAF.
// AIW-specific repository using the aiw_auth_sessions table.
// Spec: TS 29.571 §5.4.4.61, TS 29.526 §7.3
package postgres

import (
	"context"
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
	stateCiphertext, err := encryptState(r.enc, s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("aiw create: encrypt state: %w", err)
	}
	encryptedSUPI, err := encryptField(r.enc, s.Supi)
	if err != nil {
		return fmt.Errorf("aiw create: encrypt supi: %w", err)
	}

	var mskCiphertext []byte
	if len(s.MSK) > 0 {
		mskCiphertext, err = encryptState(r.enc, s.MSK)
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
	stateCiphertext, err := encryptState(r.enc, s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("aiw update: encrypt state: %w", err)
	}
	encryptedSUPI, err := encryptField(r.enc, s.Supi)
	if err != nil {
		return fmt.Errorf("aiw update: encrypt supi: %w", err)
	}

	var mskCiphertext []byte
	if len(s.MSK) > 0 {
		mskCiphertext, err = encryptState(r.enc, s.MSK)
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

	s.Supi, _ = decryptField(r.enc, rawSUPI)
	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if aaaConfigID.Valid {
		idStr := aaaConfigID.String()
		s.AAAConfigID = &idStr
	}
	if len(stateBytes) > 0 {
		s.EAPSessionState, _ = decryptState(r.enc, stateBytes)
	}
	if len(mskBytes) > 0 {
		s.MSK, _ = decryptState(r.enc, mskBytes)
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
		SupportedFeatures:  s.SupportedFeatures,
		Status:            s.NssaaStatus,
		AuthResult:        s.AuthResult,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		ExpiresAt:         s.ExpiresAt,
		CompletedAt:       s.CompletedAt,
	}
}

// sessionToRow converts a domain session to a DB row.
// Sets CreatedAt/UpdatedAt to now if zero.
func (r *AiwRepository) sessionToRow(s *storage.AiwSession) *aiwRow {
	now := time.Now()
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := s.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	expiresAt := s.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(24 * time.Hour)
	}
	return &aiwRow{
		AuthCtxID:         s.AuthCtxID,
		Supi:              s.Supi,
		AusfID:            s.AusfID,
		EAPSessionState:   s.EapPayload,
		TtlsInner:         s.TtlsInner,
		MSK:               s.MSK,
		PvsInfo:           s.PvsInfo,
		SupportedFeatures:  s.SupportedFeatures,
		NssaaStatus:       s.Status,
		AuthResult:        s.AuthResult,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ExpiresAt:         expiresAt,
		CompletedAt:       s.CompletedAt,
	}
}

// Compile-time interface check.
var _ storage.AiwStore = (*AiwRepository)(nil)
