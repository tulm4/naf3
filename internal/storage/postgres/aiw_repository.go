// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// AIW-specific repository using aiw_auth_sessions table.
// Spec: TS 29.571 §5.4.4.60, TS 29.526 §7.3
// Design: docs/design/04_data_model.md §3.6
package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AIWSession represents an AIW authentication session stored in PostgreSQL.
// Spec: TS 29.526 §7.3
type AIWSession struct {
	AuthCtxID         string
	Supi              string
	SupiHash          string
	AusfID            string
	AAAConfigID       *string
	EAPSessionState   []byte // encrypted
	NssaaStatus       string
	AuthResult        string
	EAPRounds         int
	MaxEAPRounds      int
	EAPLastNonce      string
	MSK               []byte // encrypted
	PvsInfo           []byte // JSONB
	TtlsInner         []byte
	SupportedFeatures string
	FailureReason     string
	FailureCause      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ExpiresAt         time.Time
	CompletedAt       *time.Time
}

// AIWRepository provides database operations for AIW sessions.
type AIWRepository struct {
	pool      *Pool
	encryptor *Encryptor
}

// NewAIWRepository creates a new AIW session repository.
func NewAIWRepository(pool *Pool, encryptor *Encryptor) *AIWRepository {
	return &AIWRepository{pool: pool, encryptor: encryptor}
}

// encryptField encrypts a string value and returns base64-encoded ciphertext.
func (r *AIWRepository) encryptField(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	ciphertext, err := r.encryptor.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptField decrypts a base64-encoded ciphertext back to plaintext.
func (r *AIWRepository) decryptField(encoded string) string {
	if encoded == "" {
		return ""
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	plaintext, err := r.encryptor.Decrypt(ciphertext)
	if err != nil {
		return ""
	}
	return string(plaintext)
}

// Create inserts a new AIW session.
func (r *AIWRepository) Create(ctx context.Context, s *AIWSession) error {
	// Encrypt the session state with a random nonce.
	stateCiphertext, err := r.encryptor.Encrypt(s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("aiw session create: encrypt state: %w", err)
	}

	encryptedSUPI, err := r.encryptField(s.Supi)
	if err != nil {
		return fmt.Errorf("aiw session create: encrypt supi: %w", err)
	}

	// Encrypt MSK if present.
	var mskCiphertext []byte
	if len(s.MSK) > 0 {
		mskCiphertext, err = r.encryptor.Encrypt(s.MSK)
		if err != nil {
			return fmt.Errorf("aiw session create: encrypt msk: %w", err)
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
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			$7, $8, $9,
			$10, $11,
			$12, $13, $14, $15,
			$16, $17,
			$18, $19, $20
		)`

	var aaaConfigID interface{}
	if s.AAAConfigID != nil {
		aaaConfigID = *s.AAAConfigID
	}

	var pvsInfoJSON interface{}
	if len(s.PvsInfo) > 0 {
		pvsInfoJSON = s.PvsInfo
	}

	err = r.pool.Exec(ctx, sql,
		s.AuthCtxID, encryptedSUPI, HashSUPI(s.Supi), s.AusfID,
		aaaConfigID, stateCiphertext,
		s.EAPRounds, s.MaxEAPRounds, s.EAPLastNonce,
		s.NssaaStatus, s.AuthResult,
		mskCiphertext, pvsInfoJSON, s.TtlsInner, s.SupportedFeatures,
		s.FailureReason, s.FailureCause,
		s.CreatedAt, s.UpdatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("aiw session create: %w", err)
	}
	return nil
}

// GetByAuthCtxID retrieves an AIW session by its authCtxID.
func (r *AIWRepository) GetByAuthCtxID(ctx context.Context, authCtxID string) (*AIWSession, error) {
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
	s, err := r.scanAIWSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("aiw session get: %w", err)
	}
	return s, nil
}

// Update updates an existing AIW session.
func (r *AIWRepository) Update(ctx context.Context, s *AIWSession) error {
	// Encrypt the session state.
	ciphertext, err := r.encryptor.Encrypt(s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("aiw session update: encrypt state: %w", err)
	}

	encryptedSUPI, err := r.encryptField(s.Supi)
	if err != nil {
		return fmt.Errorf("aiw session update: encrypt supi: %w", err)
	}

	// Encrypt MSK if present.
	var mskCiphertext []byte
	if len(s.MSK) > 0 {
		mskCiphertext, err = r.encryptor.Encrypt(s.MSK)
		if err != nil {
			return fmt.Errorf("aiw session update: encrypt msk: %w", err)
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
		s.AuthCtxID, encryptedSUPI, HashSUPI(s.Supi), s.AusfID,
		ciphertext,
		s.EAPRounds, s.EAPLastNonce,
		s.NssaaStatus, s.AuthResult,
		mskCiphertext, pvsInfoJSON, s.TtlsInner, s.SupportedFeatures,
		s.FailureReason, s.FailureCause,
		s.UpdatedAt, s.ExpiresAt,
		s.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("aiw session update: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// Delete removes an AIW session.
func (r *AIWRepository) Delete(ctx context.Context, authCtxID string) error {
	sql := `DELETE FROM aiw_auth_sessions WHERE auth_ctx_id = $1`
	rowsAffected, err := r.pool.ExecResult(ctx, sql, authCtxID)
	if err != nil {
		return fmt.Errorf("aiw session delete: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *AIWRepository) scanAIWSession(row pgx.Row) (*AIWSession, error) {
	var s AIWSession
	var stateBytes []byte
	var aaaConfigID pgtype.UUID
	var completedAt pgtype.Timestamptz
	var rawSUPI, rawSupiHash string
	var mskBytes []byte
	var pvsInfoJSON []byte
	var ttlsInner []byte

	err := row.Scan(
		&s.AuthCtxID, &rawSUPI, &rawSupiHash, &s.AusfID,
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

	s.Supi = r.decryptField(rawSUPI)
	s.SupiHash = rawSupiHash

	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}

	if aaaConfigID.Valid {
		idStr := aaaConfigID.String()
		s.AAAConfigID = &idStr
	}

	// Decrypt session state.
	if stateBytes != nil && len(stateBytes) > 0 {
		if r.encryptor != nil && len(stateBytes) > 12 {
			plaintext, err := r.encryptor.Decrypt(stateBytes)
			if err == nil {
				s.EAPSessionState = plaintext
			} else {
				s.EAPSessionState = stateBytes
			}
		} else {
			s.EAPSessionState = stateBytes
		}
	}

	// Decrypt MSK.
	if mskBytes != nil && len(mskBytes) > 0 {
		if r.encryptor != nil && len(mskBytes) > 12 {
			plaintext, err := r.encryptor.Decrypt(mskBytes)
			if err == nil {
				s.MSK = plaintext
			} else {
				s.MSK = mskBytes
			}
		} else {
			s.MSK = mskBytes
		}
	}

	s.PvsInfo = pvsInfoJSON
	s.TtlsInner = ttlsInner

	return &s, nil
}

// HashSUPI computes SHA-256 hash of SUPI for lookups.
// Spec: TS 29.571 §5.4.4.60
func HashSUPI(supi string) string {
	return HashGPSI(supi) // Same hash function for both
}
