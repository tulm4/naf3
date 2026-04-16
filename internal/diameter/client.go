// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

// Application IDs for NSSAA.
const (
	// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
	AppIDAAP = 5
)

// Command codes for NSSAA.
const (
	CmdDER = 268 // Diameter-EAP-Request
	CmdDEA = 268 // Diameter-EAP-Answer
	CmdSTR = 275 // Session-Termination-Request
	CmdSTA = 275 // Session-Termination-Answer
)

// Config holds Diameter client configuration.
type Config struct {
	OriginHost  string
	OriginRealm string

	// Connection
	ServerAddress string
	Network      string // "tcp", "sctp", or "tcp+tls"

	// TLS
	UseTLS  bool
	TLSCert string
	TLSKey  string
	TLSCA   string

	// Transport
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client is the Diameter client for NSSAAF.
// It wraps go-diameter's state machine and provides a synchronous
// request-response interface for DER/DEA messages.
type Client struct {
	cfg      Config
	settings *sm.Settings
	machine  *sm.StateMachine
	smClient *sm.Client
	conn     diam.Conn
	mu       sync.RWMutex
	logger   *slog.Logger

	// Pending requests: hop-by-hop ID → result channel.
	pending    map[uint32]chan *diam.Message
	pendingMu  sync.RWMutex
}

// NewClient creates a new Diameter client.
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	c := &Client{
		cfg:     cfg,
		logger:  logger,
		pending: make(map[uint32]chan *diam.Message),
	}

	c.settings = &sm.Settings{
		OriginHost:  datatype.DiameterIdentity(cfg.OriginHost),
		OriginRealm: datatype.DiameterIdentity(cfg.OriginRealm),
		VendorID:    datatype.Unsigned32(VendorID3GPP),
		ProductName: "NSSAAF",
	}

	c.machine = sm.New(c.settings)

	c.smClient = &sm.Client{
		Dict:               dict.Default,
		Handler:            c.machine,
		MaxRetransmits:     3,
		RetransmitInterval: 5 * time.Second,
		EnableWatchdog:     true,
		WatchdogInterval:   30 * time.Second,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)),
		},
	}

	// Register handler for DEA responses.
	c.machine.Handle("DEA", c.handleDEA())
	c.machine.Handle("STA", c.handleDEA())

	return c, nil
}

// Connect establishes a connection to the Diameter server.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var conn diam.Conn
	var err error

	network := c.cfg.Network
	if network == "" {
		network = "tcp"
	}

	if c.cfg.UseTLS {
		conn, err = c.smClient.DialNetworkTLS(network, c.cfg.ServerAddress, c.cfg.TLSCert, c.cfg.TLSKey, nil)
	} else {
		conn, err = c.smClient.DialNetwork(network, c.cfg.ServerAddress)
	}

	if err != nil {
		return fmt.Errorf("diameter: failed to connect to %s: %w", c.cfg.ServerAddress, err)
	}

	c.conn = conn
	c.logger.Info("diameter_connected",
		"server", c.cfg.ServerAddress,
		"network", network,
	)

	return nil
}

// Close closes the Diameter connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	return nil
}

// SendDER sends a Diameter-EAP-Request and waits for a DEA response.
// Spec: RFC 4072, TS 29.561 §17
func (c *Client) SendDER(ctx context.Context, sessionId, userName string, eapPayload []byte, sst uint8, sd string) ([]byte, error) {
	conn, err := c.getConn()
	if err != nil {
		return nil, err
	}

	// Create channel for response.
	respCh := make(chan *diam.Message, 1)
	hopByHop := c.addPending(hopByHopID(conn), respCh)
	defer c.removePending(hopByHop)

	// Build DER message.
	m := diam.NewRequest(CmdDER, AppIDAAP, conn.Dictionary())
	m.Header.HopByHopID = hopByHop

	m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionId))
	m.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP))
	m.NewAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1))
	m.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Unsigned32(1))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, c.settings.OriginHost)
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, c.settings.OriginRealm)
	m.NewAVP(avp.DestinationHost, avp.Mbit, 0, datatype.DiameterIdentity(c.cfg.OriginHost))
	m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, c.settings.OriginRealm)
	m.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(userName))
	m.NewAVP(209, 0, 0, datatype.OctetString(eapPayload)) // EAP-Payload AVP
	m.AddAVP(EncodeSnssaiAVP(sst, sd))

	// Set timeout.
	deadline, ok := ctx.Deadline()
	if ok {
		conn.(interface {
			SetWriteDeadline(t time.Time) error
			SetReadDeadline(t time.Time) error
		}).SetWriteDeadline(deadline)
		conn.(interface {
			SetWriteDeadline(t time.Time) error
			SetReadDeadline(t time.Time) error
		}).SetReadDeadline(deadline)
	}

	_, err = m.WriteTo(conn)
	if err != nil {
		c.removePending(hopByHop)
		return nil, fmt.Errorf("diameter: failed to send DER: %w", err)
	}

	c.logger.Debug("diameter_der_sent",
		"session_id", sessionId,
		"hop_by_hop", hopByHop,
		"eap_len", len(eapPayload),
	)

	// Wait for response or context cancellation.
	select {
	case <-ctx.Done():
		c.removePending(hopByHop)
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("diameter: DEA response was nil")
		}
		// Serialize to bytes for caller.
		data, err := resp.Serialize()
		if err != nil {
			return nil, fmt.Errorf("diameter: failed to serialize DEA: %w", err)
		}
		return data, nil
	}
}

// handleDEA is the handler for DEA (and STA) responses.
func (c *Client) handleDEA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		hopByHop := m.Header.HopByHopID
		c.pendingMu.RLock()
		ch, ok := c.pending[hopByHop]
		c.pendingMu.RUnlock()
		if !ok {
			c.logger.Warn("diameter_unexpected_responser",
				"hop_by_hop", hopByHop,
			)
			return
		}
		ch <- m
	}
}

// getConn returns the current connection, connecting if necessary.
func (c *Client) getConn() (diam.Conn, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn != nil {
		return conn, nil
	}

	if err := c.Connect(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	conn = c.conn
	c.mu.RUnlock()
	return conn, nil
}

// hopByHopID returns the next hop-by-hop ID for the connection.
// Uses a simple counter for uniqueness.
func hopByHopID(conn diam.Conn) uint32 {
	return uint32(time.Now().UnixNano() & 0xFFFFFFFF)
}

// addPending registers a pending request and returns the hop-by-hop ID.
func (c *Client) addPending(hopByHop uint32, ch chan *diam.Message) uint32 {
	c.pendingMu.Lock()
	c.pending[hopByHop] = ch
	c.pendingMu.Unlock()
	return hopByHop
}

// removePending removes a pending request.
func (c *Client) removePending(hopByHop uint32) {
	c.pendingMu.Lock()
	delete(c.pending, hopByHop)
	c.pendingMu.Unlock()
}

// PeerMetadata returns the peer's metadata from the CER/CEA handshake.
func (c *Client) PeerMetadata() (*smpeer.Metadata, error) {
	conn, err := c.getConn()
	if err != nil {
		return nil, err
	}

	meta, ok := smpeer.FromContext(conn.Context())
	if !ok {
		return nil, fmt.Errorf("diameter: no peer metadata available")
	}
	return meta, nil
}
