// Package postgres provides PostgreSQL data persistence for NSSAAF.
// Spec: TS 29.571 §5.4.4.60, TS 29.526 §7.2
package postgres

import (
	"context"
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
	stateCiphertext, err := encryptState(r.enc, s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("nssaa create: encrypt state: %w", err)
	}
	encryptedGPSI, err := encryptField(r.enc, s.GPSI)
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
	stateCiphertext, err := encryptState(r.enc, s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("nssaa update: encrypt state: %w", err)
	}
	encryptedGPSI, err := encryptField(r.enc, s.GPSI)
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

	s.GPSI, _ = decryptField(r.enc, rawGPSI)
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
		s.EAPSessionState, _ = decryptState(r.enc, stateBytes)
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
// Sets CreatedAt/UpdatedAt to now if zero (handles both create and update paths).
func (r *NssaaRepository) sessionToRow(s *storage.NssaaSession) *nssaaRow {
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
		expiresAt = now.Add(5 * time.Minute)
	}
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
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		ExpiresAt:       expiresAt,
	}
}

// Compile-time interface check.
var _ storage.NssaaStore = (*NssaaRepository)(nil)
