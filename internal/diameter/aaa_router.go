// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"context"
	"strconv"
	"strings"

	"github.com/operator/nssAAF/internal/eap"
)

// DiameterAAARouter adapts diameter.Client to the eap.AAARouter interface.
// It encodes RoutingContext into Diameter AVPs per RFC 4072 and TS 29.561 §17.
type DiameterAAARouter struct {
	client *Client
}

// NewDiameterAAARouter creates a DiameterAAARouter with the given Diameter client.
func NewDiameterAAARouter(client *Client) *DiameterAAARouter {
	return &DiameterAAARouter{client: client}
}

// RoutingContext implements eap.AAARouter.
// Diameter sends unhashed GPSI as User-Name AVP per RFC 4072.
func (d *DiameterAAARouter) RoutingContext(session *eap.Session) eap.RoutingContext {
	sst, sd := decodeSnssaiKey(session.SnssaiKey)
	return eap.RoutingContext{
		GPSI:     session.Gpsi, // Diameter sends unhashed GPSI
		Sst:      sst,
		Sd:       sd,
		AuthCtxID: session.AuthCtxID,
	}
}

// decodeSnssaiKey parses "sst" or "sst-sd" format.
func decodeSnssaiKey(key string) (sst uint8, sd string) {
	if key == "" {
		return 0, ""
	}
	parts := strings.SplitN(key, "-", 2)
	sstVal, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return 0, ""
	}
	if len(parts) == 1 {
		return uint8(sstVal), ""
	}
	return uint8(sstVal), parts[1]
}

// SendEAP implements eap.AAARouter.
func (d *DiameterAAARouter) SendEAP(ctx context.Context, session *eap.Session, routing eap.RoutingContext, eapPayload []byte) ([]byte, error) {
	return d.client.SendDER(ctx, routing.AuthCtxID, routing.GPSI, eapPayload, routing.Sst, routing.Sd)
}
