// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 4818
package radius

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"time"
)

// UDPConfig holds UDP transport configuration for RADIUS.
type UDPConfig struct {
	// LocalBindAddr is the local address to bind for sending.
	LocalBindAddr string
	// DialTimeout is the timeout for establishing a connection.
	DialTimeout time.Duration
}

// Client is the interface for RADIUS transport clients.
type Client interface {
	// Send sends a RADIUS packet and returns the response.
	Send(ctx context.Context, packet []byte, server string) ([]byte, error)
	// Close closes the client.
	Close() error
	// Addr returns the local address.
	Addr() net.Addr
}

// ErrDTLSNotSupported is returned when DTLS is not available.
var ErrDTLSNotSupported = errors.New("radius: DTLS requires custom implementation or network-layer IPSec")

// DialDTLS establishes a DTLS connection to a RADIUS server.
// Spec: RFC 4818
//
// Note: Go's standard library does not support DTLS natively. This implementation
// provides the RADIUS-over-DTLS architecture. For full DTLS support, either:
// 1. Use a DTLS library like github.com/refraction-networking/utls
// 2. Implement DTLS over a raw socket
// 3. Use IPSec at the network layer
func DialDTLS(cfg DTLSConfig) (*tls.Conn, error) {
	return nil, fmt.Errorf("%w", ErrDTLSNotSupported)
}

// DTLSConfig holds DTLS-specific configuration.
// Spec: RFC 4818
type DTLSConfig struct {
	ServerAddress string
	ServerPort    int
	SharedSecret  string
	CACert        []byte
	ClientCert    []byte
	ClientKey     []byte
	DialTimeout   time.Duration
}

// ParseCertificate parses a PEM-encoded certificate.
func ParseCertificate(pemData []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("radius: no PEM certificate found")
	}
	return x509.ParseCertificate(block.Bytes)
}

// ParsePrivateKey parses a PEM-encoded private key.
// It first attempts PKCS8 (the standard format used by most tools), then falls back to PKCS1.
func ParsePrivateKey(pemData []byte) (any, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("radius: no PEM block found")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// GenerateRandomAuthenticator generates a 16-byte random authenticator.
func GenerateRandomAuthenticator() ([16]byte, error) {
	var authenticator [16]byte
	_, err := rand.Read(authenticator[:])
	if err != nil {
		return authenticator, fmt.Errorf("radius: failed to generate authenticator: %w", err)
	}
	return authenticator, nil
}

// udpClient implements a UDP-based RADIUS client transport.
type udpClient struct {
	conn      *net.UDPConn
	localAddr net.UDPAddr
}

// NewUDPClient creates a UDP-based RADIUS client transport.
func NewUDPClient(cfg UDPConfig) (Client, error) {
	bindAddr := cfg.LocalBindAddr
	if bindAddr == "" {
		bindAddr = ":0"
	}

	local, err := net.ResolveUDPAddr("udp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("radius: failed to resolve bind address: %w", err)
	}

	conn, err := net.ListenUDP("udp", local)
	if err != nil {
		return nil, fmt.Errorf("radius: failed to listen on %s: %w", bindAddr, err)
	}

	return &udpClient{
		conn:      conn,
		localAddr: *local,
	}, nil
}

// Send implements Client.
func (c *udpClient) Send(ctx context.Context, packet []byte, server string) ([]byte, error) {
	addr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return nil, fmt.Errorf("radius: failed to resolve server address: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	} else {
		_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	}

	n, err := c.conn.WriteToUDP(packet, addr)
	if err != nil {
		return nil, fmt.Errorf("radius: failed to send packet: %w", err)
	}
	if n < len(packet) {
		return nil, fmt.Errorf("radius: partial send: %d of %d bytes", n, len(packet))
	}

	buf := make([]byte, 65535)
	m, _, err := c.conn.ReadFromUDP(buf)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("radius: response timeout")
		}
		return nil, fmt.Errorf("radius: failed to receive response: %w", err)
	}

	return buf[:m], nil
}

// Close implements Client.
func (c *udpClient) Close() error {
	return c.conn.Close()
}

// Addr implements Client.
func (c *udpClient) Addr() net.Addr {
	return c.conn.LocalAddr()
}
