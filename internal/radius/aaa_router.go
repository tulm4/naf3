// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"context"

	"github.com/operator/nssAAF/internal/crypto"
	"github.com/operator/nssAAF/internal/eap"
)

// RadiusAAARouter adapts radius.Client to the eap.AAARouter interface.
// It encodes RoutingContext into RADIUS VSAs per RFC 3579 and 3GPP TS 29.561 §16.
type RadiusAAARouter struct {
	client *Client
}

// NewRadiusAAARouter creates a RadiusAAARouter with the given RADIUS client.
func NewRadiusAAARouter(client *Client) *RadiusAAARouter {
	return &RadiusAAARouter{client: client}
}

// RoutingContext implements eap.AAARouter.
// RADIUS requires hashed GPSI per TS 33.501 PII requirements.
func (r *RadiusAAARouter) RoutingContext(session *eap.Session) eap.RoutingContext {
	gpsihash := crypto.HashGPSI(session.Gpsi)
	sst, sd := decodeSnssaiKey(session.SnssaiKey)
	return eap.RoutingContext{
		GPSI:     gpsihash,
		Sst:      sst,
		Sd:       sd,
		AuthCtxID: session.AuthCtxID,
	}
}

// SendEAP implements eap.AAARouter.
// SendEAP forwards an EAP payload to the AAA server via RADIUS.
// Signature verified against internal/radius/client.go:SendEAP.
func (r *RadiusAAARouter) SendEAP(ctx context.Context, session *eap.Session, routing eap.RoutingContext, eapPayload []byte) ([]byte, error) {
	return r.client.SendEAP(ctx, routing.GPSI, eapPayload, routing.Sst, routing.Sd)
}
