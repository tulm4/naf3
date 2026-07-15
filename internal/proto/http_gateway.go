// Package proto defines the wire protocol between NSSAAF components.
package proto

import "context"

// BizServiceClient is the interface HTTP Gateway uses to forward N58/N60 requests
// to Biz Pods. It handles load balancing across Biz Pod replicas.
// Spec: docs/design/01_service_model.md §5.4.6, PHASE §1.3
type BizServiceClient interface {
	// ForwardRequest forwards an HTTP request to a Biz Pod and returns the response.
	// - path: original request path (e.g. "/nnssaaf-nssaa/v1/slice-authentications")
	// - method: HTTP method (GET, POST, PUT, DELETE)
	// - body: request body bytes
	// - requestID: correlation ID for tracing (optional, forwarded as X-Request-ID header)
	// - gpsi: GPSI for per-UE debug tracing (optional, forwarded as X-NSSAA-GPSI header)
	// - supi: SUPI for per-UE debug tracing (optional, forwarded as X-NSSAA-SUPI header)
	// Returns (responseBody, httpStatus, error)
	// - 2xx: success, HTTP Gateway forwards response to AMF/AUSF
	// - 4xx: Biz Pod rejected (validation failure)
	// - 5xx: Biz Pod error; HTTP Gateway may retry if idempotent
	// - context.DeadlineExceeded: all Biz Pods failed; HTTP Gateway returns 503
	ForwardRequest(ctx context.Context, path string, method string, body []byte, requestID string, gpsi string, supi string) ([]byte, int, error)
}

// AaaServerInitiatedResponse is returned by Biz Pod to AAA Gateway after processing
// a server-initiated message (RAR/ASR/CoA).
// The response bytes are forwarded by AAA Gateway to AAA-S.
type AaaServerInitiatedResponse struct {
	Version     string      `json:"v"`
	SessionID   string      `json:"sessionId"`
	AuthCtxID   string      `json:"authCtxId"`
	MessageType MessageType `json:"messageType"` // RAR | ASR | COA | DM | STR
	ResultCode  uint32      `json:"resultCode"` // 0=success, 2001=DIAMETER_SUCCESS, 5002=UNKNOWN_SESSION_ID, etc.
	Payload     []byte      `json:"payload,omitempty"`
	ErrorCause  string      `json:"errorCause,omitempty"`
}

// ServerInitiatedHandler processes server-initiated messages from AAA GW.
// Spec: docs/superpowers/specs/... §4.5
type ServerInitiatedHandler interface {
	HandleReAuth(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
	HandleRevocation(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
	HandleCoA(ctx context.Context, req *AaaServerInitiatedRequest) (*AaaServerInitiatedResponse, error)
}

// Result codes for AAA server-initiated responses (Diameter base protocol).
const (
	ResultCodeSuccess             uint32 = 2001 // DIAMETER_SUCCESS
	ResultCodeAuthRejected        uint32 = 5001 // DIAMETER_AUTHORIZATION_REJECTED
	ResultCodeUnknownSessionID    uint32 = 5002 // DIAMETER_UNKNOWN_SESSION_ID
	ResultCodeInvalidAvpBits      uint32 = 5003 // DIAMETER_INVALID_AVP_BITS
	ResultCodeAuthExpired         uint32 = 5004 // DIAMETER_AUTHORIZATION_EXPIRED
	ResultCodeAdminProhibited     uint32 = 5005 // DIAMETER_ADMIN_PROHIBITED
	ResultCodeAuthenticationError uint32 = 5012 // DIAMETER_AUTHENTICATION_ERROR
)
