// Package adapter provides a RADIUS client adapter that bridges the layeh API
// to the legacy RADIUS client interface.
//
// The layeh client uses typed AccessRequest/AccessResponse structs, while
// the legacy client uses raw []Attribute slices and []byte responses.
// This adapter allows callers to use the layeh backend through the legacy
// interface without changing their code.
//
// Spec: 3GPP TS 29.561 §16, RFC 2865, RFC 3579.
package adapter

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/operator/nssAAF/internal/radius"
	"github.com/operator/nssAAF/internal/radius/layeh"
	"github.com/operator/nssAAF/internal/radius/layeh/gen"
)

// Client wraps a layeh client and exposes the legacy RADIUS client interface.
// It is returned by the factory when RADIUS_BACKEND=layeh.
type Client struct {
	layeh *layeh.Client
}

// NewClient creates a Client that wraps the layeh client.
func NewClient(l *layeh.Client) *Client {
	return &Client{layeh: l}
}

// SendAccessRequest sends an Access-Request from a legacy-style []Attribute
// slice and returns the raw response bytes. This matches the legacy
// (*radius.Client).SendAccessRequest signature so that aaa/gateway can
// switch backends via RADIUS_BACKEND without changing its call sites.
//
// Supported attribute types:
//   - radius.AttrUserName (string) → layeh.UserName
//   - radius.AttrCallingStationID (string) → layeh.CallingStationID
//   - radius.AttrNASIdentifier (string) → layeh.NASIdentifier
//   - radius.AttrState (octets) → layeh.State
//   - radius.AttrEAPMessage (octets) → layeh.EAPMessage (concatenated)
//   - radius.Attr3GPPSNSSAI (vendor-specific) → layeh.NSSAI
//
// Attributes not listed above are silently ignored.
func (c *Client) SendAccessRequest(ctx context.Context, attrs []radius.Attribute) ([]byte, error) {
	req := &layeh.AccessRequest{}

	for _, attr := range attrs {
		switch attr.Type {
		case radius.AttrUserName:
			req.UserName = string(attr.Value)

		case radius.AttrCallingStationID:
			req.CallingStationID = string(attr.Value)

		case radius.AttrNASIdentifier:
			req.NASIdentifier = string(attr.Value)

		case radius.AttrState:
			req.State = attr.Value

		case radius.AttrEAPMessage:
			req.EAPMessage = append(req.EAPMessage, attr.Value...)

		case radius.Attr3GPPSNSSAI:
			sst, sd, _ := radius.DecodeSnssaiVSA(attr.Value)
			var sdBytes [3]byte
			if sd != "" {
				b, err := hex.DecodeString(sd)
				if err == nil && len(b) >= 3 {
					copy(sdBytes[:], b)
				}
			}
			req.NSSAI = gen.NSSAI{SST: sst, SD: sdBytes}
		}
	}

	resp, err := c.layeh.AccessRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	// Re-encode the layeh response packet back to legacy []byte wire format.
	pkt := resp.Packet()
	if pkt == nil {
		return nil, errors.New("radius/adapter: no underlying packet to re-encode")
	}
	return pkt.Encode()
}

// SendEAP wraps SendAccessRequest with EAP-specific semantics:
// - User-Name is the hashed GPSI
// - Service-Type is Authenticate-Only
// - NAS-Port-Type is Virtual
// - SST and SD from the snssai parameter
//
// This matches the legacy (*radius.Client).SendEAP signature.
func (c *Client) SendEAP(ctx context.Context, gpsi string, eapPayload []byte, snssaiSst uint8, snssaiSd string) ([]byte, error) {
	attrs := []radius.Attribute{
		radius.MakeStringAttribute(radius.AttrUserName, gpsi),
		radius.MakeIntegerAttribute(radius.AttrServiceType, radius.ServiceTypeAuthenticateOnly),
		radius.MakeIntegerAttribute(radius.AttrNASPortType, radius.NASPortTypeVirtual),
	}

	attrs = append(attrs, radius.Make3GPPSNSSAIAttribute(snssaiSst, snssaiSd))

	eapFrags := radius.FragmentEAPMessage(eapPayload, 253)
	for _, frag := range eapFrags {
		attrs = append(attrs, radius.MakeAttribute(radius.AttrEAPMessage, frag))
	}

	return c.SendAccessRequest(ctx, attrs)
}

// Close closes the underlying layeh client.
func (c *Client) Close() error {
	return c.layeh.Close()
}
