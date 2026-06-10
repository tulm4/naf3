// Package storage provides domain types and interfaces for NSSAAF persistence.
// Spec: TS 29.526 §7.2-7.3
package storage

import "time"

// NssaaSession represents a slice authentication session.
// Domain model owned by the storage layer.
// Corresponds to the slice_auth_sessions table.
type NssaaSession struct {
	AuthCtxID      string
	GPSI           string
	SnssaiSST      uint8
	SnssaiSD       string
	AmfInstance    string
	ReauthURI      string
	RevocURI       string
	EapPayload     []byte
	Status         string
	CallbackOwner  string
	HasAIWContext  bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
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
