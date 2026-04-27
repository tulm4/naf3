// Package proto defines the wire protocol between NSSAAF components.
// It must have zero dependencies on internal/radius/, internal/diameter/,
// internal/eap/, or internal/aaa/.
// Spec: TS 29.526 v18.7.0
package proto

import (
	"context"
	"time"
)

// TransportType identifies the AAA transport protocol.
type TransportType string

const (
	TransportRADIUS   TransportType = "RADIUS"
	TransportDIAMETER TransportType = "DIAMETER"
)

// Direction indicates who initiated the exchange.
type Direction string

const (
	DirectionClientInitiated Direction = "CLIENT_INITIATED"
	DirectionServerInitiated Direction = "SERVER_INITIATED"
)

// MessageType identifies a server-initiated message type.
type MessageType string

const (
	MessageTypeRAR MessageType = "RAR" // RADIUS Re-Auth-Request (RFC 5176)
	MessageTypeASR MessageType = "ASR" // Diameter Abort-Session-Request (RFC 6733)
	MessageTypeCoA MessageType = "COA" // RADIUS Change-of-Authorization (RFC 5176)
)

// AaaForwardRequest is the body of POST /aaa/forward from Biz Pod to AAA Gateway.
// AAA Gateway forwards raw RADIUS/Diameter transport bytes without modification.
// Spec: docs/design/01_service_model.md §5.4.3
type AaaForwardRequest struct {
	Version       string        `json:"v"`             // Schema version, e.g. "1.0"
	SessionID     string        `json:"sessionId"`     // Unique per EAP round-trip
	AuthCtxID     string        `json:"authCtxId"`     // NSSAAF auth context ID
	TransportType TransportType `json:"transportType"` // RADIUS or DIAMETER
	Sst           uint8         `json:"sst"`           // S-NSSAI SST (0-255)
	Sd            string        `json:"sd"`            // S-NSSAI SD (6 hex, "FFFFFF" if none)
	Direction     Direction     `json:"direction"`     // CLIENT_INITIATED or SERVER_INITIATED
	Payload       []byte        `json:"payload"`       // Raw EAP bytes (already-encoded RADIUS/Diameter)
}

// AaaForwardResponse is the response from AAA Gateway back to Biz Pod.
type AaaForwardResponse struct {
	Version   string `json:"v"`
	SessionID string `json:"sessionId"`
	AuthCtxID string `json:"authCtxId"`
	Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// AaaServerInitiatedRequest is sent by AAA Gateway to Biz Pod when AAA-S initiates
// a Re-Auth, Revocation, or CoA request.
// Spec: PHASE §6.4
type AaaServerInitiatedRequest struct {
	Version       string        `json:"v"`
	SessionID     string        `json:"sessionId"` // RADIUS State / Diameter Session-Id
	AuthCtxID     string        `json:"authCtxId"` // From Redis lookup
	TransportType TransportType `json:"transportType"`
	MessageType   MessageType   `json:"messageType"` // RAR, ASR, CoA
	Payload       []byte        `json:"payload"`     // Raw RAR/ASR/CoA bytes
}

// BizAAAClient is the interface the Biz Pod uses to talk to the AAA Gateway.
// Replaces direct radius.Client / diameter.Client calls in internal/aaa/router.go.
// Spec: PHASE §1.1
type BizAAAClient interface {
	ForwardEAP(ctx context.Context, req *AaaForwardRequest) (*AaaForwardResponse, error)
}

// DefaultPayloadTTL is the TTL for nssaa:session:{sessionId} Redis keys (10 minutes).
const DefaultPayloadTTL = 10 * time.Minute
