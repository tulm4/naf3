// Package proto defines the wire protocol between NSSAAF components.
package proto

// AaaResponseEvent is published to Redis channel nssaa:aaa-response when the
// AAA Gateway receives a response from AAA-S.
// All Biz Pods receive every event; each discards events not matching its in-flight sessions.
type AaaResponseEvent struct {
	Version   string `json:"v"`
	SessionID string `json:"sessionId"`
	AuthCtxID string `json:"authCtxId"`
	Payload   []byte `json:"payload"` // Raw response bytes from AAA-S
}

// SessionCorrEntry is stored at nssaa:session:{sessionId} in Redis.
// Correlates a RADIUS/Diameter session ID with the NSSAAF authCtxId and the
// Biz Pod that initiated the request. Written by AAA Gateway before forwarding
// to AAA-S; read by AAA Gateway on response arrival or server-initiated routing.
type SessionCorrEntry struct {
	AuthCtxID string `json:"authCtxId"` // NSSAAF auth context ID
	PodID     string `json:"podId"`     // Biz Pod hostname/UID (observability only; NOT used for routing)
	Sst       uint8  `json:"sst"`       // S-NSSAI SST
	Sd        string `json:"sd"`        // S-NSSAI SD
	CreatedAt int64  `json:"createdAt"` // Unix timestamp
}

// Redis key and channel constants.
// Spec: PHASE §1.2
const (
	// SessionCorrKeyPrefix is the Redis key prefix for session correlation.
	// Full key: "nssaa:session:{sessionId}" → SessionCorrEntry (JSON), TTL = DefaultPayloadTTL
	SessionCorrKeyPrefix = "nssaa:session:"
	// PodsKey is the Redis SET containing IDs of live Biz Pod instances.
	// Updated on Biz Pod startup/shutdown and refreshed on heartbeat.
	PodsKey = "nssaa:pods"
	// AaaResponseChannel is the Redis pub/sub channel for AAA responses.
	// Publisher: AAA Gateway. Subscribers: all Biz Pods.
	AaaResponseChannel = "nssaa:aaa-response"
)

// SessionCorrKey builds the full Redis key for a given sessionId.
// Spec: PHASE §1.2
func SessionCorrKey(sessionID string) string {
	return SessionCorrKeyPrefix + sessionID
}
