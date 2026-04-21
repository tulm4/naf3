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
	// Returns (responseBody, httpStatus, error)
	// - 2xx: success, HTTP Gateway forwards response to AMF/AUSF
	// - 4xx: Biz Pod rejected (validation failure)
	// - 5xx: Biz Pod error; HTTP Gateway may retry if idempotent
	// - context.DeadlineExceeded: all Biz Pods failed; HTTP Gateway returns 503
	ForwardRequest(ctx context.Context, path string, method string, body []byte) ([]byte, int, error)
}

// AaaServerInitiatedResponse is returned by Biz Pod to AAA Gateway after processing
// a server-initiated message (RAR/ASR/CoA).
// The response bytes are forwarded by AAA Gateway to AAA-S.
type AaaServerInitiatedResponse struct {
	Version   string `json:"v"`
	SessionID string `json:"sessionId"`
	AuthCtxID string `json:"authCtxId"`
	Payload   []byte `json:"payload"` // Raw response bytes (RAR-Nak, ASA, etc.)
}
