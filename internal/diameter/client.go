// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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
	OriginHost       string
	OriginRealm      string
	DestinationHost  string
	DestinationRealm string

	// Connection
	ServerAddress string
	Network       string // "tcp", "sctp", or "tcp+tls"

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
	pending   map[uint32]chan *diam.Message
	pendingMu sync.RWMutex

	// Atomic counter for generating unique hop-by-hop IDs.
	hopByHopSeq uint64
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

// buildDERMessage constructs a Diameter-EAP-Request message with all required AVPs.
// Spec: RFC 4072, RFC 6733 §8.8, TS 29.561 §17
func (c *Client) buildDERMessage(conn diam.Conn, hopByHop uint32, sessionID, userName string, eapPayload []byte, sst uint8, sd string) (*diam.Message, error) {
	m := diam.NewRequest(CmdDER, AppIDAAP, conn.Dictionary())
	m.Header.HopByHopID = hopByHop

	addAVP := func(code interface{}, flags uint8, _ uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, flags, 0, data)
		return err
	}

	if err := addAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.OriginHost, avp.Mbit, 0, c.settings.OriginHost); err != nil {
		return nil, err
	}
	if err := addAVP(avp.OriginRealm, avp.Mbit, 0, c.settings.OriginRealm); err != nil {
		return nil, err
	}
	if err := addAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.DestinationHost, avp.Mbit, 0, datatype.DiameterIdentity(c.cfg.DestinationHost)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.DestinationRealm, avp.Mbit, 0, datatype.DiameterIdentity(c.cfg.DestinationRealm)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(userName)); err != nil {
		return nil, err
	}
	if err := addAVP(209, 0, 0, datatype.OctetString(eapPayload)); err != nil {
		return nil, err
	}

	snssaiAVP, err := EncodeSnssaiAVP(sst, sd)
	if err != nil {
		return nil, fmt.Errorf("diameter: failed to encode SNSSAI AVP: %w", err)
	}
	m.AddAVP(snssaiAVP)

	return m, nil
}

// SendDER sends a Diameter-EAP-Request and waits for a DEA response.
// Spec: RFC 4072, RFC 6733 §8.8, TS 29.561 §17
func (c *Client) SendDER(ctx context.Context, sessionID, userName string, eapPayload []byte, sst uint8, sd string) ([]byte, error) {
	conn, err := c.getConn()
	if err != nil {
		return nil, err
	}

	respCh := make(chan *diam.Message, 1)
	hopByHop := c.nextHopByHopID()
	c.addPending(hopByHop, respCh)
	defer c.removePending(hopByHop)

	m, err := c.buildDERMessage(conn, hopByHop, sessionID, userName, eapPayload, sst, sd)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		if dc, ok := conn.(interface {
			SetWriteDeadline(t time.Time) error
			SetReadDeadline(t time.Time) error
		}); ok {
			_ = dc.SetWriteDeadline(deadline)
			_ = dc.SetReadDeadline(deadline)
		}
	}

	_, err = m.WriteTo(conn)
	if err != nil {
		c.removePending(hopByHop)
		return nil, fmt.Errorf("diameter: failed to send DER: %w", err)
	}

	c.logger.Debug("diameter_der_sent",
		"session_id", sessionID,
		"hop_by_hop", hopByHop,
		"eap_len", len(eapPayload),
	)

	select {
	case <-ctx.Done():
		c.removePending(hopByHop)
		return nil, ctx.Err()
	case resp := <-respCh:
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

// nextHopByHopID returns the next unique hop-by-hop ID using an atomic counter.
func (c *Client) nextHopByHopID() uint32 {
	return uint32(atomic.AddUint64(&c.hopByHopSeq, 1))
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
