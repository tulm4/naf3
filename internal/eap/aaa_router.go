// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import "context"

// RoutingContext is the structured metadata needed to route and encode an EAP message
// to AAA-S. It crosses the AAARouter seam so that tests can assert on routing
// decisions without parsing opaque wire bytes.
//
// Key differences from raw Session fields:
//   - GPSI is hashed before RADIUS encoding (per TS 33.501 PII requirements).
//     Diameter sends unhashed GPSI as User-Name AVP.
//   - S-NSSAI is decoded from the Session.SnssaiKey composite key.
type RoutingContext struct {
	GPSI     string // subscriber identity (protocol-dependent: hashed for RADIUS, raw for Diameter)
	Sst      uint8  // S-NSSAI Slice Service Type
	Sd       string // S-NSSAI Slice Differentiator (empty string if not configured)
	AuthCtxID string // NSSAAF auth context ID (for correlation)
}

// AAARouter is the seam between the EAP engine and the AAA protocol.
// Protocol adapters (RADIUS, Diameter) implement this by encoding RoutingContext
// into protocol-specific attributes/AVPs.
type AAARouter interface {
	// RoutingContext extracts the structured routing metadata from a session.
	// Extracted once per call site, passed to SendEAP so the adapter can
	// encode it without re-extracting from the session.
	RoutingContext(session *Session) RoutingContext

	// SendEAP forwards an EAP payload to AAA-S and returns the response.
	// The routing parameter carries all context needed for attribute/AVP encoding,
	// so the adapter can be tested on message structure without a live AAA-S.
	SendEAP(ctx context.Context, session *Session, routing RoutingContext, eapPayload []byte) ([]byte, error)
}
