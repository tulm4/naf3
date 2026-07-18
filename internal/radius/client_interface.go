// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865
package radius

import "context"

// ClientInterface is the unified RADIUS client interface implemented by both
// the legacy and layeh backends. Callers use this interface — they do not need
// to type-assert or branch on the backend.
//
// Both the legacy (*radius.Client) and the adapter (*adapter.Client) implement
// this interface, so callers can work with either backend without changes.
type ClientInterface interface {
	// SendAccessRequest sends a RADIUS Access-Request and returns the raw response.
	SendAccessRequest(ctx context.Context, attrs []Attribute) ([]byte, error)

	// SendEAP sends an Access-Request with EAP payload and NSSAI.
	// User-Name is the hashed GPSI, Service-Type is Authenticate-Only,
	// NAS-Port-Type is Virtual.
	SendEAP(ctx context.Context, gpsi string, eapPayload []byte, snssaiSst uint8, snssaiSd string) ([]byte, error)

	// Close closes the client connection.
	Close() error
}
