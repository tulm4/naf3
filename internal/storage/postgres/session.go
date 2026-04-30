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
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/operator/nssAAF/internal/crypto"
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
	AMFIP           *string
	AMFRegion       string
	AAAConfigID     *string
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
	key []byte
}

// NewEncryptor creates a new Encryptor using a 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	return &Encryptor{key: key}, nil
}

// NewEncryptorFromKeyManager creates an Encryptor backed by the global crypto.KeyManager.
// The KeyManager must have been initialized via crypto.Init() before calling this.
// Returns an error if the key manager has no key available.
func NewEncryptorFromKeyManager(km crypto.KeyManager) (*Encryptor, error) {
	if km == nil {
		return nil, errors.New("encryptor: key manager is nil, call crypto.Init first")
	}
	if key, ok := km.GetKey(); ok {
		return NewEncryptor(key)
	}
	return nil, errors.New("encryptor: unsupported key manager backend (use soft mode for session encryption in Phase 5)")
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")

// Repository provides database operations for sessions.
type Repository struct {
	pool      *Pool
	encryptor *Encryptor
}

// NewRepository creates a new session repository.
func NewRepository(pool *Pool, encryptor *Encryptor) *Repository {
	return &Repository{pool: pool, encryptor: encryptor}
}

// encryptField encrypts a string value and returns base64-encoded ciphertext.
// Returns empty string for empty input.
func (r *Repository) encryptField(plaintext string) (string, error) {
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
// Returns empty string for empty input or decryption errors.
func (r *Repository) decryptField(encoded string) string {
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

// Create inserts a new session.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	// Encrypt the session state with a random nonce.
	stateCiphertext, err := r.encryptor.Encrypt(s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("session create: encrypt state: %w", err)
	}

	encryptedGPSI, err := r.encryptField(s.GPSI)
	if err != nil {
		return fmt.Errorf("session create: encrypt gpsi: %w", err)
	}
	encryptedSUPI, err := r.encryptField(s.Supi)
	if err != nil {
		return fmt.Errorf("session create: encrypt supi: %w", err)
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
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9,
			$10, $11,
			$12, $13,
			$14, $15, $16,
			$17, $18,
			$19, $20,
			$21, $22, $23
		)`

	var amfIP interface{}
	if s.AMFIP != nil {
		amfIP = *s.AMFIP
	}

	var aaaConfigID interface{}
	if s.AAAConfigID != nil {
		aaaConfigID = *s.AAAConfigID
	}

	err = r.pool.Exec(ctx, sql,
		s.AuthCtxID, encryptedGPSI, HashGPSI(s.GPSI), encryptedSUPI, s.SnssaiSST, s.SnssaiSD,
		s.AMFInstanceID, amfIP, s.AMFRegion,
		s.ReauthNotifURI, s.RevocNotifURI,
		aaaConfigID, stateCiphertext,
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
	// Encrypt the session state with a random nonce.
	ciphertext := make([]byte, 0, len(s.EAPSessionState)+28)
	var err error
	ciphertext, err = r.encryptor.Encrypt(s.EAPSessionState)
	if err != nil {
		return fmt.Errorf("session update: encrypt state: %w", err)
	}

	encryptedGPSI, err := r.encryptField(s.GPSI)
	if err != nil {
		return fmt.Errorf("session update: encrypt gpsi: %w", err)
	}
	encryptedSUPI, err := r.encryptField(s.Supi)
	if err != nil {
		return fmt.Errorf("session update: encrypt supi: %w", err)
	}

	sql := `
		UPDATE slice_auth_sessions SET
			gpsi = $2, gpsi_hash = $3, supi = $4,
			snssai_sst = $5, snssai_sd = $6,
			eap_session_state = $7,
			eap_rounds = $8, eap_last_nonce = $9,
			nssaa_status = $10, auth_result = $11,
			failure_reason = $12, failure_cause = $13,
			updated_at = $14, expires_at = $15,
			completed_at = $16, terminated_at = $17
		WHERE auth_ctx_id = $1`

	rowsAffected, err := r.pool.ExecResult(ctx, sql,
		s.AuthCtxID, encryptedGPSI, HashGPSI(s.GPSI), encryptedSUPI,
		s.SnssaiSST, s.SnssaiSD,
		ciphertext,
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

// List retrieves all sessions for a given GPSI (via hash lookup).
func (r *Repository) List(ctx context.Context, gpsi string) ([]*Session, error) {
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
		WHERE gpsi_hash = $1
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, sql, HashGPSI(gpsi))
	if err != nil {
		return nil, fmt.Errorf("session list: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := r.scanSessionFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("session list scan: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// CountPending returns the number of sessions in PENDING or NOT_EXECUTED state.
func (r *Repository) CountPending(ctx context.Context) (int, error) {
	sql := `
		SELECT COUNT(*) FROM slice_auth_sessions
		WHERE nssaa_status IN ('PENDING', 'NOT_EXECUTED')`

	var count int
	err := r.pool.QueryRow(ctx, sql).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending: %w", err)
	}
	return count, nil
}

// ExpireStale marks expired sessions as terminated.
func (r *Repository) ExpireStale(ctx context.Context) (int, error) {
	sql := `
		UPDATE slice_auth_sessions SET
			nssaa_status = 'EAP_FAILURE',
			auth_result = 'EAP_FAILURE',
			terminated_at = NOW(),
			failure_reason = 'Session expired'
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
	var stateBytes []byte
	var amfIP net.IP
	var aaaConfigID uuid.UUID
	var completedAt, terminatedAt pgtype.Timestamptz
	var rawGPSI, rawSUPI, rawGPSIHash string

	err := row.Scan(
		&s.AuthCtxID, &rawGPSI, &rawGPSIHash, &rawSUPI, &s.SnssaiSST, &s.SnssaiSD,
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

	s.GPSI = r.decryptField(rawGPSI)
	s.Supi = r.decryptField(rawSUPI)

	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if terminatedAt.Valid {
		s.TerminatedAt = &terminatedAt.Time
	}
	if amfIP != nil {
		ipStr := amfIP.String()
		s.AMFIP = &ipStr
	}
	if aaaConfigID != uuid.Nil {
		idStr := aaaConfigID.String()
		s.AAAConfigID = &idStr
	}

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

	return &s, nil
}

func (r *Repository) scanSessionFromRows(rows pgx.Rows) (*Session, error) {
	var s Session
	var stateBytes []byte
	var amfIP net.IP
	var aaaConfigID uuid.UUID
	var completedAt, terminatedAt pgtype.Timestamptz
	var rawGPSI, rawSUPI, rawGPSIHash string

	err := rows.Scan(
		&s.AuthCtxID, &rawGPSI, &rawGPSIHash, &rawSUPI, &s.SnssaiSST, &s.SnssaiSD,
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

	s.GPSI = r.decryptField(rawGPSI)
	s.Supi = r.decryptField(rawSUPI)

	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	if terminatedAt.Valid {
		s.TerminatedAt = &terminatedAt.Time
	}
	if amfIP != nil {
		ipStr := amfIP.String()
		s.AMFIP = &ipStr
	}
	if aaaConfigID != uuid.Nil {
		idStr := aaaConfigID.String()
		s.AAAConfigID = &idStr
	}

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

	return &s, nil
}
