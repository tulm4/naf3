// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/operator/nssAAF/internal/types"
)

// Session represents a slice authentication session stored in PostgreSQL.
// Spec: TS 29.571 §5.4.4.60, TS 29.526 §7
type Session struct {
	AuthCtxID       string
	GPSI            string
	Supi            string
	SnssaiSST       uint8
	SnssaiSD        string
	AMFInstanceID   string
	AMFIP           string
	AMFRegion       string
	AAAConfigID     string
	EAPSessionState []byte // encrypted
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

// Encryptor handles encryption and decryption of sensitive session data.
type Encryptor struct {
	key []byte // 16, 24, or 32 bytes for AES-128/192/256
}

// NewEncryptor creates an encryptor with a 32-byte key (AES-256).
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, errors.New("encryptor: key must be 16, 24, or 32 bytes")
	}
	return &Encryptor{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("encrypt: nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("decrypt: ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ErrSessionNotFound is returned when a session does not exist.
var ErrSessionNotFound = errors.New("postgres: session not found")

// ErrEncryptionFailed is returned when encryption fails.
var ErrEncryptionFailed = errors.New("postgres: encryption failed")

// Repository provides session persistence operations.
type Repository struct {
	pool      *Pool
	encryptor *Encryptor
}

// NewRepository creates a new session repository.
func NewRepository(pool *Pool, encryptor *Encryptor) *Repository {
	return &Repository{pool: pool, encryptor: encryptor}
}

// Create inserts a new session.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	stateB64 := base64.StdEncoding.EncodeToString(s.EAPSessionState)

	sql := `
		INSERT INTO slice_auth_sessions (
			auth_ctx_id, gpsi, supi, snssai_sst, snssai_sd,
			amf_instance_id, amf_ip, amf_region,
			reauth_notif_uri, revoc_notif_uri,
			aaa_config_id, eap_session_state,
			eap_rounds, max_eap_rounds, eap_last_nonce,
			nssaa_status, auth_result,
			failure_reason, failure_cause,
			created_at, updated_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12,
			$13, $14, $15,
			$16, $17,
			$18, $19,
			$20, $21, $22
		)`

	err := r.pool.Exec(ctx, sql,
		s.AuthCtxID, s.GPSI, s.Supi, s.SnssaiSST, s.SnssaiSD,
		s.AMFInstanceID, s.AMFIP, s.AMFRegion,
		s.ReauthNotifURI, s.RevocNotifURI,
		s.AAAConfigID, stateB64,
		s.EAPRounds, s.MaxEAPRounds, s.EAPLastNonce,
		s.NssaaStatus, s.AuthResult,
		s.FailureReason, s.FailureCause,
		s.CreatedAt, s.UpdatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("session create: %w", err)
	}
	return nil
}

// GetByAuthCtxID retrieves a session by its authCtxID.
func (r *Repository) GetByAuthCtxID(ctx context.Context, authCtxID string) (*Session, error) {
	sql := `
		SELECT
			auth_ctx_id, gpsi, supi, snssai_sst, snssai_sd,
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
	s, err := r.scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("session get: %w", err)
	}
	return s, nil
}

// Update updates an existing session.
func (r *Repository) Update(ctx context.Context, s *Session) error {
	stateB64 := base64.StdEncoding.EncodeToString(s.EAPSessionState)

	sql := `
		UPDATE slice_auth_sessions SET
			gpsi = $2, supi = $3,
			snssai_sst = $4, snssai_sd = $5,
			eap_session_state = $6,
			eap_rounds = $7, eap_last_nonce = $8,
			nssaa_status = $9, auth_result = $10,
			failure_reason = $11, failure_cause = $12,
			updated_at = $13, expires_at = $14,
			completed_at = $15, terminated_at = $16
		WHERE auth_ctx_id = $1`

	rowsAffected, err := r.pool.ExecResult(ctx, sql,
		s.AuthCtxID, s.GPSI, s.Supi,
		s.SnssaiSST, s.SnssaiSD,
		stateB64,
		s.EAPRounds, s.EAPLastNonce,
		s.NssaaStatus, s.AuthResult,
		s.FailureReason, s.FailureCause,
		s.UpdatedAt, s.ExpiresAt,
		s.CompletedAt, s.TerminatedAt,
	)
	if err != nil {
		return fmt.Errorf("session update: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// Delete removes a session.
func (r *Repository) Delete(ctx context.Context, authCtxID string) error {
	sql := `DELETE FROM slice_auth_sessions WHERE auth_ctx_id = $1`
	rowsAffected, err := r.pool.ExecResult(ctx, sql, authCtxID)
	if err != nil {
		return fmt.Errorf("session delete: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// ListPending returns all sessions with PENDING or NOT_EXECUTED status.
func (r *Repository) ListPending(ctx context.Context) ([]*Session, error) {
	sql := `
		SELECT
			auth_ctx_id, gpsi, supi, snssai_sst, snssai_sd,
			amf_instance_id, amf_ip, amf_region,
			reauth_notif_uri, revoc_notif_uri,
			aaa_config_id, eap_session_state,
			eap_rounds, max_eap_rounds, eap_last_nonce,
			nssaa_status, auth_result,
			failure_reason, failure_cause,
			created_at, updated_at, expires_at,
			completed_at, terminated_at
		FROM slice_auth_sessions
		WHERE nssaa_status IN ('PENDING', 'NOT_EXECUTED')
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("session list pending: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := r.scanSessionFromRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ExpireOld deletes sessions past their expires_at that are not PENDING.
func (r *Repository) ExpireOld(ctx context.Context) (int, error) {
	sql := `
		DELETE FROM slice_auth_sessions
		WHERE expires_at < NOW()
		AND nssaa_status NOT IN ('PENDING', 'NOT_EXECUTED')`

	rowsAffected, err := r.pool.ExecResult(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("session expire: %w", err)
	}
	return int(rowsAffected), nil
}

func (r *Repository) scanSession(row pgx.Row) (*Session, error) {
	var s Session
	var stateB64 string
	var completedAt, terminatedAt pgtype.Timestamptz

	err := row.Scan(
		&s.AuthCtxID, &s.GPSI, &s.Supi, &s.SnssaiSST, &s.SnssaiSD,
		&s.AMFInstanceID, &s.AMFIP, &s.AMFRegion,
		&s.ReauthNotifURI, &s.RevocNotifURI,
		&s.AAAConfigID, &stateB64,
		&s.EAPRounds, &s.MaxEAPRounds, &s.EAPLastNonce,
		&s.NssaaStatus, &s.AuthResult,
		&s.FailureReason, &s.FailureCause,
		&s.CreatedAt, &s.UpdatedAt, &s.ExpiresAt,
		&completedAt, &terminatedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if terminatedAt.Valid {
		s.TerminatedAt = &terminatedAt.Time
	}

	if stateB64 != "" {
		stateBytes, err := base64.StdEncoding.DecodeString(stateB64)
		if err != nil {
			return nil, fmt.Errorf("decode session state: %w", err)
		}
		if r.encryptor != nil {
			s.EAPSessionState, err = r.encryptor.Decrypt(stateBytes)
			if err != nil {
				return nil, fmt.Errorf("decrypt session state: %w", err)
			}
		} else {
			s.EAPSessionState = stateBytes
		}
	}

	return &s, nil
}

func (r *Repository) scanSessionFromRows(rows pgx.Rows) (*Session, error) {
	var s Session
	var stateB64 string
	var completedAt, terminatedAt pgtype.Timestamptz

	err := rows.Scan(
		&s.AuthCtxID, &s.GPSI, &s.Supi, &s.SnssaiSST, &s.SnssaiSD,
		&s.AMFInstanceID, &s.AMFIP, &s.AMFRegion,
		&s.ReauthNotifURI, &s.RevocNotifURI,
		&s.AAAConfigID, &stateB64,
		&s.EAPRounds, &s.MaxEAPRounds, &s.EAPLastNonce,
		&s.NssaaStatus, &s.AuthResult,
		&s.FailureReason, &s.FailureCause,
		&s.CreatedAt, &s.UpdatedAt, &s.ExpiresAt,
		&completedAt, &terminatedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if terminatedAt.Valid {
		s.TerminatedAt = &terminatedAt.Time
	}

	if stateB64 != "" {
		stateBytes, err := base64.StdEncoding.DecodeString(stateB64)
		if err != nil {
			return nil, fmt.Errorf("decode session state: %w", err)
		}
		if r.encryptor != nil {
			s.EAPSessionState, err = r.encryptor.Decrypt(stateBytes)
			if err != nil {
				return nil, fmt.Errorf("decrypt session state: %w", err)
			}
		} else {
			s.EAPSessionState = stateBytes
		}
	}

	return &s, nil
}
