// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Client lifecycle errors.
var (
	ErrTimeout         = errors.New("radius: response timeout")
	ErrInvalidResponse = errors.New("radius: invalid response")
	ErrIDMismatch      = errors.New("radius: response ID mismatch")
)

// Default configuration values.
const (
	DefaultServerPort     = 1812
	DefaultResponseWindow = 10 * time.Second
	DefaultMaxRetries     = 3
)

// Config holds RADIUS client configuration.
type Config struct {
	ServerAddress  string
	ServerPort     int
	SharedSecret   string
	Timeout        time.Duration
	MaxRetries     int
	ResponseWindow time.Duration
	Transport      string // "UDP" or "DTLS"
	LocalBindAddr  string
}

// Client is the main RADIUS client for NSSAAF.
type Client struct {
	config    Config
	transport Client
	packetID  uint8
	mu        sync.Mutex
	logger    *slog.Logger
}

// NewRadiusClient creates a new RADIUS client.
func NewRadiusClient(cfg Config, logger *slog.Logger) (*Client, error) {
	if cfg.ServerPort == 0 {
		cfg.ServerPort = DefaultServerPort
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultResponseWindow
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.ResponseWindow == 0 {
		cfg.ResponseWindow = DefaultResponseWindow
	}

	transport, err := NewUDPClient(UDPConfig{
		LocalBindAddr: cfg.LocalBindAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("radius: failed to create UDP transport: %w", err)
	}

	return &Client{
		config:    cfg,
		transport: transport,
		logger:    logger,
	}, nil
}

// Close shuts down the client.
func (c *Client) Close() error {
	return c.transport.Close()
}

// SendAccessRequest sends an Access-Request and waits for a response.
// Spec: RFC 2865 §3.1
func (c *Client) SendAccessRequest(ctx context.Context, attrs []Attribute) ([]byte, error) {
	// Acquire next packet ID.
	c.mu.Lock()
	id := c.packetID
	c.packetID++
	c.mu.Unlock()

	// Generate authenticator.
	authenticator, err := GenerateRandomAuthenticator()
	if err != nil {
		return nil, fmt.Errorf("radius: failed to generate authenticator: %w", err)
	}

	// Build packet.
	packet := BuildAccessRequest(id, authenticator, attrs)
	rawPacket := packet.Encode()

	// Add Message-Authenticator.
	rawPacket = AddMessageAuthenticator(rawPacket, c.config.SharedSecret)

	serverAddr := fmt.Sprintf("%s:%d", c.config.ServerAddress, c.config.ServerPort)

	// Retry loop.
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			c.logger.Debug("radius_retry",
				"id", id,
				"attempt", attempt,
				"server", serverAddr,
			)
		}

		sendCtx, cancel := context.WithTimeout(ctx, c.config.ResponseWindow)
		response, err := c.transport.Send(sendCtx, rawPacket, serverAddr)
		cancel()

		if err != nil {
			lastErr = err
			c.logger.Warn("radius_send_error",
				"id", id,
				"attempt", attempt,
				"error", err,
			)
			continue
		}

		if err := c.validateResponse(response, id); err != nil {
			lastErr = err
			c.logger.Warn("radius_response_invalid",
				"id", id,
				"error", err,
			)
			continue
		}

		return response, nil
	}

	return nil, fmt.Errorf("radius: all retries exhausted: %w", lastErr)
}

// validateResponse validates a RADIUS response packet.
func (c *Client) validateResponse(data []byte, requestID uint8) error {
	if len(data) < 20 {
		return fmt.Errorf("radius: response too short: %d bytes", len(data))
	}

	if data[1] != requestID {
		return fmt.Errorf("%w: expected %d, got %d", ErrIDMismatch, requestID, data[1])
	}

	code := data[0]
	switch code {
	case CodeAccessAccept, CodeAccessReject, CodeAccessChallenge:
		// Valid response codes.
	default:
		return fmt.Errorf("radius: unexpected response code: %d", code)
	}

	if hasMessageAuthenticator(data) {
		if !VerifyMessageAuthenticator(data, c.config.SharedSecret) {
			return ErrInvalidMessageAuth
		}
	}

	return nil
}

// SendEAP forwards an EAP payload within a RADIUS Access-Request.
// This is the primary method used by the EAP engine.
func (c *Client) SendEAP(ctx context.Context, gpsi string, eapPayload []byte, snssaiSst uint8, snssaiSd string) ([]byte, error) {
	attrs := []Attribute{
		MakeStringAttribute(AttrUserName, gpsi),
		MakeStringAttribute(AttrCallingStationID, gpsi),
		MakeIntegerAttribute(AttrServiceType, ServiceTypeAuthenticateOnly),
		MakeIntegerAttribute(AttrNASPortType, NASPortTypeVirtual),
		Make3GPPSNSSAIAttribute(snssaiSst, snssaiSd),
	}

	// Fragment EAP payload if necessary.
	eapFrags := FragmentEAPMessage(eapPayload, 253)
	for _, frag := range eapFrags {
		attrs = append(attrs, MakeAttribute(AttrEAPMessage, frag))
	}

	return c.SendAccessRequest(ctx, attrs)
}

// Stats returns client statistics.
func (c *Client) Stats() ClientStats {
	c.mu.Lock()
	id := c.packetID
	c.mu.Unlock()

	return ClientStats{
		PacketIDNext:  id,
		ServerAddress: c.config.ServerAddress,
		ServerPort:    c.config.ServerPort,
	}
}

// ClientStats holds operational statistics.
type ClientStats struct {
	PacketIDNext  uint8
	ServerAddress string
	ServerPort    int
}

// FragmentEAPMessage fragments an EAP message into chunks suitable for RADIUS.
// RFC 3579 §3.2: Multiple EAP-Message attributes may be used.
func FragmentEAPMessage(payload []byte, maxSize int) [][]byte {
	if len(payload) <= maxSize {
		return [][]byte{payload}
	}

	frags := make([][]byte, 0, (len(payload)+maxSize-1)/maxSize)
	for i := 0; i < len(payload); i += maxSize {
		end := i + maxSize
		if end > len(payload) {
			end = len(payload)
		}
		frags = append(frags, payload[i:end])
	}
	return frags
}

// AssembleEAPMessage reassembles EAP fragments from RADIUS attributes.
func AssembleEAPMessage(attrs []Attribute) []byte {
	var payload []byte
	for _, attr := range attrs {
		if attr.Type == AttrEAPMessage {
			payload = append(payload, attr.Value...)
		}
	}
	return payload
}

// hasMessageAuthenticator checks if a packet contains a Message-Authenticator attribute.
func hasMessageAuthenticator(data []byte) bool {
	return findMessageAuthenticator(data) >= 0
}
