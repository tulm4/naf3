// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"fmt"
	"time"
)

// AuditAction represents an auditable action in the system.
type AuditAction string

// Audit actions as defined in the schema.
const (
	AuditActionSessionCreated    AuditAction = "SESSION_CREATED"
	AuditActionEAPRoundAdvanced  AuditAction = "EAP_ROUND_ADVANCED"
	AuditActionEAPSuccess        AuditAction = "EAP_SUCCESS"
	AuditActionEAPFailure        AuditAction = "EAP_FAILURE"
	AuditActionSessionExpired    AuditAction = "SESSION_EXPIRED"
	AuditActionSessionTerminated AuditAction = "SESSION_TERMINATED"
	AuditActionNotifReauthSent   AuditAction = "NOTIF_REAUTH_SENT"
	AuditActionNotifReauthAck    AuditAction = "NOTIF_REAUTH_ACK"
	AuditActionNotifReauthFailed AuditAction = "NOTIF_REAUTH_FAILED"
	AuditActionNotifRevocSent    AuditAction = "NOTIF_REVOC_SENT"
	AuditActionNotifRevocAck     AuditAction = "NOTIF_REVOC_ACK"
	AuditActionNotifRevocFailed  AuditAction = "NOTIF_REVOC_FAILED"
	AuditActionAAAConnected      AuditAction = "AAA_CONNECTED"
	AuditActionAAAFailed         AuditAction = "AAA_FAILED"
)

// AuditEntry represents an immutable audit log entry.
type AuditEntry struct {
	ID            int64
	AuthCtxID     string
	GPSIHash      string // SHA-256 first 16 bytes hex
	SnssaiSST     int
	SnssaiSD      string
	AMFInstanceID string
	AMFIP         string
	Action        AuditAction
	NssaaStatus   string
	ErrorCode     int
	ErrorMessage  string
	RequestID     string
	CorrelationID string
	ClientIP      string
	UserAgent     string
	CreatedAt     time.Time
}

// AuditRepository provides audit log persistence.
type AuditRepository struct {
	pool *Pool
}

// NewAuditRepository creates a new audit repository.
func NewAuditRepository(pool *Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// Append inserts a new immutable audit entry.
func (r *AuditRepository) Append(ctx context.Context, e *AuditEntry) error {
	sql := `
		INSERT INTO nssaa_audit_log (
			auth_ctx_id, gpsi_hash, snssai_sst, snssai_sd,
			amf_instance_id, amf_ip,
			action, nssaa_status,
			error_code, error_message,
			request_id, correlation_id,
			client_ip, user_agent
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	err := r.pool.Exec(ctx, sql,
		e.AuthCtxID, e.GPSIHash, e.SnssaiSST, e.SnssaiSD,
		e.AMFInstanceID, e.AMFIP,
		e.Action, e.NssaaStatus,
		e.ErrorCode, e.ErrorMessage,
		e.RequestID, e.CorrelationID,
		e.ClientIP, e.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("audit append: %w", err)
	}
	return nil
}

// ListByAuthCtxID returns audit entries for a session.
func (r *AuditRepository) ListByAuthCtxID(ctx context.Context, authCtxID string, limit int) ([]*AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	sql := `
		SELECT
			id, auth_ctx_id, gpsi_hash, snssai_sst, snssai_sd,
			amf_instance_id, amf_ip,
			action, nssaa_status,
			error_code, error_message,
			request_id, correlation_id,
			client_ip, user_agent,
			created_at
		FROM nssaa_audit_log
		WHERE auth_ctx_id = $1
		ORDER BY created_at DESC
		LIMIT $2`

	rows, err := r.pool.Query(ctx, sql, authCtxID, limit)
	if err != nil {
		return nil, fmt.Errorf("audit list: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListByGPSIHash returns audit entries for a GPSI (using hash for privacy).
func (r *AuditRepository) ListByGPSIHash(ctx context.Context, gpsiHash string, since time.Time, limit int) ([]*AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	sql := `
		SELECT
			id, auth_ctx_id, gpsi_hash, snssai_sst, snssai_sd,
			amf_instance_id, amf_ip,
			action, nssaa_status,
			error_code, error_message,
			request_id, correlation_id,
			client_ip, user_agent,
			created_at
		FROM nssaa_audit_log
		WHERE gpsi_hash = $1 AND created_at >= $2
		ORDER BY created_at DESC
		LIMIT $3`

	rows, err := r.pool.Query(ctx, sql, gpsiHash, since, limit)
	if err != nil {
		return nil, fmt.Errorf("audit list by gpsi: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *AuditRepository) scanEntry(rows interface{ Scan(...any) error }) (*AuditEntry, error) {
	var e AuditEntry
	var amfIP, errorMsg, requestID, correlationID, clientIP, userAgent *string

	err := rows.Scan(
		&e.ID, &e.AuthCtxID, &e.GPSIHash, &e.SnssaiSST, &e.SnssaiSD,
		&e.AMFInstanceID, &amfIP,
		&e.Action, &e.NssaaStatus,
		&e.ErrorCode, &errorMsg,
		&requestID, &correlationID,
		&clientIP, &userAgent,
		&e.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan audit entry: %w", err)
	}

	if amfIP != nil {
		e.AMFIP = *amfIP
	}
	if errorMsg != nil {
		e.ErrorMessage = *errorMsg
	}
	if requestID != nil {
		e.RequestID = *requestID
	}
	if correlationID != nil {
		e.CorrelationID = *correlationID
	}
	if clientIP != nil {
		e.ClientIP = *clientIP
	}
	if userAgent != nil {
		e.UserAgent = *userAgent
	}

	return &e, nil
}
