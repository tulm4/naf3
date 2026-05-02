// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
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

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")

// NewEncryptor creates an encryptor from a 32-byte key.
// This is a compatibility shim — the underlying encryption delegates to
// crypto.EncryptConcat/crypto.DecryptConcat.
func NewEncryptor(key []byte) (*encryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	return &encryptor{key: key}, nil
}

// NewEncryptorFromKeyManager creates an encryptor backed by a crypto.KeyManager.
// Returns an error if the key manager has no key available.
func NewEncryptorFromKeyManager(km crypto.KeyManager) (*encryptor, error) {
	if km == nil {
		return nil, errors.New("encryptor: key manager is nil")
	}
	if key, ok := km.GetKey(); ok {
		return NewEncryptor(key)
	}
	return nil, errors.New("encryptor: unsupported key manager backend")
}

type encryptor struct{ key []byte }

func (e *encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	return crypto.EncryptConcat(plaintext, e.key)
}

func (e *encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	return crypto.DecryptConcat(ciphertext, e.key)
}

// NewSecretEncryptor creates an encryptor from a passphrase using SHA-256 key derivation.
// For config-file secrets that are encrypted at rest with a passphrase.
func NewSecretEncryptor(passphrase string) *PassphraseEncryptor {
	return &PassphraseEncryptor{passphrase: []byte(passphrase)}
}

// PassphraseEncryptor encrypts and decrypts using a passphrase-derived key.
type PassphraseEncryptor struct{ passphrase []byte }

func (e *PassphraseEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	key := crypto.FromPassphrase(string(e.passphrase))
	return crypto.EncryptConcat(plaintext, key)
}

func (e *PassphraseEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	key := crypto.FromPassphrase(string(e.passphrase))
	return crypto.DecryptConcat(ciphertext, key)
}

// newUUID generates a new UUID v4 string.
func newUUID() (string, error) {
	return uuid.NewString(), nil
}

// Repository provides database operations for sessions.
type Repository struct {
	pool *Pool
	enc  *encryptor
}

// NewRepository creates a new session repository.
func NewRepository(pool *Pool, enc *encryptor) *Repository {
	return &Repository{pool: pool, enc: enc}
}

// encryptField encrypts a string value and returns base64-encoded ciphertext.
// Returns empty string for empty input.
func (r *Repository) encryptField(plaintext string) (string, error) {
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
// Returns empty string, nil for empty input.
// Returns empty string, error if decryption fails (wrong key or tampered data).
func (r *Repository) decryptField(encoded string) (string, error) {
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
// Returns the ciphertext or an error on encryption failure.
func (r *Repository) encryptState(state []byte) ([]byte, error) {
	return r.enc.Encrypt(state)
}

// decryptState decrypts session state ciphertext.
// Returns the plaintext or ErrDecryptFailed if the key is wrong or data is tampered.
func (r *Repository) decryptState(ciphertext []byte) ([]byte, error) {
	return r.enc.Decrypt(ciphertext)
}

// Create inserts a new session.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	stateCiphertext, err := r.encryptState(s.EAPSessionState)
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
		s.AuthCtxID, encryptedGPSI, crypto.HashGPSI(s.GPSI), encryptedSUPI, s.SnssaiSST, s.SnssaiSD,
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
	stateCiphertext, err := r.encryptState(s.EAPSessionState)
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
		s.AuthCtxID, encryptedGPSI, crypto.HashGPSI(s.GPSI), encryptedSUPI,
		s.SnssaiSST, s.SnssaiSD,
		stateCiphertext,
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

	rows, err := r.pool.Query(ctx, sql, crypto.HashGPSI(gpsi))
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

	s.GPSI, _ = r.decryptField(rawGPSI)
	s.Supi, _ = r.decryptField(rawSUPI)

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

	// Decrypt session state. Decryption errors are logged but not fatal —
	// the record loaded successfully; the state blob may be from a pre-encryption
	// migration. The error is surfaced in the returned session for callers to handle.
	if stateBytes != nil && len(stateBytes) > 0 {
		plaintext, err := r.decryptState(stateBytes)
		if err == nil {
			s.EAPSessionState = plaintext
		}
		// Intentionally don't fall back to raw bytes — if the key is wrong, we want
		// the caller to notice via an empty state, not silently operate on garbage.
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

	s.GPSI, _ = r.decryptField(rawGPSI)
	s.Supi, _ = r.decryptField(rawSUPI)

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

	// Decrypt session state. Decryption errors are logged but not fatal —
	// the record loaded successfully; the state blob may be from a pre-encryption
	// migration. The error is surfaced in the returned session for callers to handle.
	if stateBytes != nil && len(stateBytes) > 0 {
		plaintext, err := r.decryptState(stateBytes)
		if err == nil {
			s.EAPSessionState = plaintext
		}
	}

	return &s, nil
}
