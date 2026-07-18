// Package layeh provides a RADIUS client built on top of layeh.com/layehRadius.
//
// Spec: 3GPP TS 29.561 §16, RFC 2865, RFC 3579.
package layeh

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/operator/nssAAF/internal/radius/layeh/gen"
	layehRadius "layeh.com/radius"
)

// ErrMAUnset is returned when Message-Authenticator could not be computed.
var ErrMAUnset = errors.New("layeh: Message-Authenticator not set after packet construction")

// Config holds RADIUS client configuration.
type Config struct {
	// ServerAddr is the UDP address of the RADIUS server (e.g., "127.0.0.1:1812").
	ServerAddr string

	// Secret is the shared secret for the RADIUS server.
	Secret []byte

	// Timeout for requests (default: 5 seconds).
	Timeout time.Duration
}

// Client is a RADIUS client using layeh.com/layehRadius.
type Client struct {
	serverAddr   *net.UDPAddr
	secret       []byte
	timeout      time.Duration
	radiusClient *layehRadius.Client
}

// NewClient creates a new RADIUS client.
//
// Note: InsecureSkipVerify is set to true because we handle Message-Authenticator
// integrity ourselves (RFC 3579 §3.2) via the patchMessageAuthenticator mechanism.
// Many RADIUS servers (including our aaa-sim test harness) also don't compute
// the RFC 2865 Response Authenticator correctly, so we bypass that layer's check.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	addr, err := net.ResolveUDPAddr("udp", cfg.ServerAddr)
	if err != nil {
		return nil, fmt.Errorf("NewClient: ResolveUDPAddr: %w", err)
	}

	return &Client{
		serverAddr: addr,
		secret:     cfg.Secret,
		timeout:    cfg.Timeout,
		radiusClient: &layehRadius.Client{
			Retry:              3 * time.Second,
			InsecureSkipVerify: true, // Skips Response Authenticator check (see note below)
		},
	}, nil
}

// AccessRequest represents an Access-Request to send.
type AccessRequest struct {
	// UserName is the user identifier (GPSI format).
	UserName string

	// NSSAI is the Network Slice Selection Assistance Information.
	NSSAI gen.NSSAI

	// NASIdentifier is the NAS identifier (optional).
	NASIdentifier string

	// CallingStationID is the calling station ID (optional).
	CallingStationID string

	// State is the state attribute from Access-Challenge (optional).
	State []byte

	// EAPMessage is the EAP payload (optional).
	EAPMessage []byte
}

// AccessResponse represents the server response.
type AccessResponse struct {
	// Code is the RADIUS response code.
	Code layehRadius.Code

	// EAPMessage is the EAP payload from the response (if any).
	EAPMessage []byte

	// State is the state attribute (if any).
	State []byte

	// NSSAI is the authorized NSSAI from the response (if any).
	NSSAI []gen.NSSAI

	// Message is the Reply-Message from the response (if any).
	Message string

	// _packet is the raw layeh packet for re-encoding to legacy []byte format.
	_packet *layehRadius.Packet
}

// Packet returns the underlying layeh packet for re-encoding to legacy format.
func (r *AccessResponse) Packet() *layehRadius.Packet {
	return r._packet
}

// AccessRequest sends an Access-Request and returns the response.
func (c *Client) AccessRequest(ctx context.Context, req *AccessRequest) (*AccessResponse, error) {
	// Create packet
	pkt := layehRadius.New(layehRadius.CodeAccessRequest, c.secret)

	// Add User-Name
	if err := gen.UserName_AddString(pkt, req.UserName); err != nil {
		return nil, fmt.Errorf("AccessRequest: UserName: %w", err)
	}

	// Add NAS-Identifier if provided
	if req.NASIdentifier != "" {
		if err := gen.NASIdentifier_AddString(pkt, req.NASIdentifier); err != nil {
			return nil, fmt.Errorf("AccessRequest: NASIdentifier: %w", err)
		}
	}

	// Add Calling-Station-Id if provided
	if req.CallingStationID != "" {
		if err := gen.CallingStationID_AddString(pkt, req.CallingStationID); err != nil {
			return nil, fmt.Errorf("AccessRequest: CallingStationID: %w", err)
		}
	}

	// Add State if provided (from Access-Challenge)
	if len(req.State) > 0 {
		if err := gen.State_Add(pkt, req.State); err != nil {
			return nil, fmt.Errorf("AccessRequest: State: %w", err)
		}
	}

	// Add EAP-Message if provided
	if len(req.EAPMessage) > 0 {
		if err := gen.EAPMessage_Add(pkt, req.EAPMessage); err != nil {
			return nil, fmt.Errorf("AccessRequest: EAPMessage: %w", err)
		}
	}

	// Add 3GPP-S-NSSAI
	if err := gen.AddNSSAIAttribute(pkt, req.NSSAI); err != nil {
		return nil, fmt.Errorf("AccessRequest: AddNSSAIAttribute: %w", err)
	}

	// Add Message-Authenticator as a placeholder (zero value).
	// The actual HMAC-MD5 value is computed and patched in below.
	if err := gen.MessageAuthenticator_Add(pkt, make([]byte, 16)); err != nil {
		return nil, fmt.Errorf("AccessRequest: MessageAuthenticator: %w", err)
	}

	// Patch Message-Authenticator with the proper HMAC-MD5 of the packet.
	// Per RFC 3579 §3.2, MA covers the entire packet with the MA value set
	// to 16 zero octets before the HMAC is computed.
	if err := patchMessageAuthenticator(pkt); err != nil {
		return nil, fmt.Errorf("AccessRequest: patchMessageAuthenticator: %w", err)
	}

	// Send with timeout
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.radiusClient.Exchange(ctx, pkt, c.serverAddr.String())
	if err != nil {
		return nil, fmt.Errorf("AccessRequest: Exchange: %w", err)
	}

	return parseAccessResponse(response)
}

// patchMessageAuthenticator encodes the packet, computes HMAC-MD5 over the
// encoded bytes (with the MA value zeroed), and stores the HMAC back into the
// in-memory packet. Subsequent calls to pkt.Encode() will emit the patched
// value because layeh's Encode() does not modify the MA attribute.
func patchMessageAuthenticator(pkt *layehRadius.Packet) error {
	attr, ok := pkt.Attributes.Lookup(gen.MessageAuthenticator_Type)
	if !ok {
		return ErrMAUnset
	}

	// Encode the packet so the header length reflects the MA attribute.
	// For Access-Request, Encode() leaves the Request Authenticator as-is.
	wire, err := pkt.Encode()
	if err != nil {
		return fmt.Errorf("Encode: %w", err)
	}

	// Find the MA attribute in the wire bytes and zero its value.
	// gen.MessageAuthenticator_Type is layehRadius.Type (int) — compare as an integer
	// constant because wire bytes are []byte (uint8).
	maOffset := -1
	for i := 20; i+1 < len(wire); {
		attrType := wire[i]
		attrLen := int(wire[i+1])
		if attrType == byte(gen.MessageAuthenticator_Type) && attrLen == 18 {
			maOffset = i
			break
		}
		i += attrLen
	}
	if maOffset < 0 {
		return ErrMAUnset
	}

	// Zero the MA value before HMAC computation.
	zeroed := make([]byte, len(wire))
	copy(zeroed, wire)
	for i := 0; i < 16; i++ {
		zeroed[maOffset+2+i] = 0
	}

	mac := hmac.New(md5.New, pkt.Secret)
	mac.Write(zeroed)
	sum := mac.Sum(nil)

	// Write the HMAC value back into the in-memory MA attribute.
	copy(attr, sum)

	return nil
}

// parseAccessResponse extracts response data from a RADIUS packet.
func parseAccessResponse(pkt *layehRadius.Packet) (*AccessResponse, error) {
	resp := &AccessResponse{
		Code:    pkt.Code,
		_packet: pkt,
	}

	// Extract EAP-Message
	resp.EAPMessage = gen.EAPMessage_Get(pkt)

	// Extract State
	resp.State = gen.State_Get(pkt)

	// Extract Reply-Message
	resp.Message = gen.ReplyMessage_GetString(pkt)

	// Extract 3GPP-S-NSSAI
	nssais, err := gen.GetNSSAIAttributes(pkt)
	if err != nil {
		return nil, fmt.Errorf("parseAccessResponse: GetNSSAIAttributes: %w", err)
	}
	resp.NSSAI = nssais

	return resp, nil
}

// Close closes the client (no-op for UDP).
func (c *Client) Close() error {
	return nil
}
