// Package session provides shared session types and storage interfaces for NSSAA and AIW.
// These types are the canonical session representation used by both API handlers and storage adapters.
package session

import (
	"errors"
	"time"
)

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session: not found")

// ErrSessionExpired is returned when a session has passed its expiry time.
var ErrSessionExpired = errors.New("session: expired")

// Session is the core slice authentication session.
// It contains fields common to both NSSAA and AIW authentication contexts.
// NSSAA-specific fields (S-NSSAI, GPSI, AMF instance) are in NSSAASession.
// AIW-specific fields (SUPI, MSK, AUSF ID) are in AIWSession.
// Extension data is stored in Ext to avoid duplicating struct fields.
type Session struct {
	AuthCtxID   string
	EapPayload  []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   time.Time
	CompletedAt *time.Time

	// Ext holds protocol-specific session data.
	// Use type assertions: *NSSAASession or *AIWSession.
	Ext any
}

// NSSAASession holds NSSAA-specific fields for a session.
// It is stored in Session.Ext as *NSSAASession.
type NSSAASession struct {
	// GPSI is the Generic Public Subscription Identifier.
	GPSI string

	// SnssaiSST is the Slice/Service Type.
	SnssaiSST uint8

	// SnssaiSD is the Slice Differentiator (optional, 6 hex chars).
	SnssaiSD string

	// AmfInstance is the AMF instance ID that initiated this session.
	AmfInstance string

	// ReauthURI is the URI for re-authentication notification (TS 29.526 §7.2.4).
	ReauthURI string

	// RevocURI is the URI for revocation notification (TS 29.526 §7.2.5).
	RevocURI string
}

// AIWSession holds AIW-specific fields for a session.
// It is stored in Session.Ext as *AIWSession.
type AIWSession struct {
	// Supi is the Subscription Permanent Identifier (IMSI format).
	Supi string

	// MSK is the Master Session Key derived from EAP-TLS (RFC 5216 §2.1.4).
	MSK []byte

	// PvsInfo is the Privacy-Violating Servers info from AAA-S (TS 29.526 §7.3.3).
	PvsInfo []byte

	// AusfID is the AUSF instance that triggered this authentication.
	AusfID string

	// SupportedFeatures echo from the request (TS 29.526 §7.3.2).
	SupportedFeatures string

	// Status is the session status (PENDING, EAP_SUCCESS, EAP_FAILURE, NOT_EXECUTED).
	Status string

	// AuthResult is the final authentication result.
	AuthResult string
}
