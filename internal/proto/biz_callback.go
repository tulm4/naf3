// Package proto defines the wire protocol between NSSAAF components.
package proto

import "time"

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

// Redis key constants.
// Spec: PHASE §1.2
const (
	// SessionCorrKeyPrefix is the Redis key prefix for session correlation.
	// Full key: "nssaa:session:{sessionId}" → SessionCorrEntry (JSON), TTL = DefaultPayloadTTL
	SessionCorrKeyPrefix = "nssaa:session:"
	// PodsKey is the Redis SET containing IDs of live Biz Pod instances.
	// Updated on Biz Pod startup/shutdown and refreshed on heartbeat.
	PodsKey = "nssaa:pods"
)

// SessionCorrKey builds the full Redis key for a given sessionId.
// Spec: PHASE §1.2
func SessionCorrKey(sessionID string) string {
	return SessionCorrKeyPrefix + sessionID
}

// BizPodsHash is the Redis HASH storing live Biz Pod URLs keyed by PodID.
// Key: "nssaa:biz:pods"
// Field: podID → BizPodEntry JSON
// TTL: managed by per-field TTL (not natively supported by Redis HASH, use separate per-pod keys)
// See BizPodEntryTTL.
const BizPodsHash = "nssaa:biz:pods"

// BizPodEntryTTL is the TTL for the per-pod key in BizPodsKey.
// If a pod does not refresh within this window, it is considered dead.
const BizPodEntryTTL = 60 * time.Second

// BizPodsKey builds the Redis key for a specific pod's entry.
// Key: "nssaa:biz:pod:{podID}" → BizPodEntry JSON, TTL = BizPodEntryTTL
func BizPodsKey(podID string) string {
	return "nssaa:biz:pod:" + podID
}

// BizPodEntry represents a registered Biz Pod in Redis.
// Written by Biz Pod on startup and refreshed every 30s via heartbeat.
type BizPodEntry struct {
	URL      string `json:"url"`       // e.g. "http://biz-pod-a:8080"
	LastSeen int64  `json:"lastSeen"` // Unix timestamp of last heartbeat
}

// DLQKey is the Redis LIST used as a dead-letter queue for failed server-initiated messages.
const DLQKey = "nssaa:dlq:server-initiated"
