// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 6733, RFC 4072, TS 29.561 Ch.17
package gateway

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

const (
	// VendorID3GPP is the 3GPP vendor ID (10415).
	// Spec: TS 29161; same value as internal/diameter.VendorID3GPP.
	VendorID3GPP uint32 = 10415
	// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
	AppIDAAP = 5
)

// diamForwarder manages a persistent TCP/SCTP connection to AAA-S for the
// client-initiated path (DER/DEA). It uses go-diameter/v4/sm for CER/CEA
// handshake, DWR/DWA watchdog, and DER encoding.
// Spec: RFC 6733 §5.3 (CER/CEA), RFC 6733 §5.5 (DWR/DWA), RFC 4072 (DER/DEA)
type diamForwarder struct {
	addr        string
	network     string // "tcp" or "sctp"
	originHost  string
	originRealm string
	destHost    string
	destRealm   string
	settings    *sm.Settings
	machine     *sm.StateMachine
	smClient    *sm.Client
	conn        diam.Conn
	mu          sync.RWMutex
	logger      *slog.Logger
	connected   bool

	// Pending requests: hop-by-hop ID → result channel.
	pending   map[uint32]chan *diam.Message
	pendingMu sync.RWMutex

	// Atomic counter for generating unique hop-by-hop IDs.
	hopByHopSeq uint64
}

// newDiamForwarder creates a new Diameter forwarder.
// addr is the AAA-S address (e.g. "nss-aaa-server:3868").
// network is "tcp" or "sctp".
// originHost/originRealm are the AAA Gateway's identity (Origin-Host in CER).
// destHost/destRealm are the AAA-S identity (Destination-Host in DER).
func newDiamForwarder(addr, network, originHost, originRealm, destHost, destRealm string, logger *slog.Logger) *diamForwarder {
	df := &diamForwarder{
		addr:        addr,
		network:     network,
		originHost:  originHost,
		originRealm: originRealm,
		destHost:    destHost,
		destRealm:   destRealm,
		logger:      logger,
		pending:     make(map[uint32]chan *diam.Message),
	}

	df.settings = &sm.Settings{
		OriginHost:  datatype.DiameterIdentity(originHost),
		OriginRealm: datatype.DiameterIdentity(originRealm),
		VendorID:    datatype.Unsigned32(VendorID3GPP),
		ProductName: "NSSAAF-GW",
	}

	df.machine = sm.New(df.settings)

	df.smClient = &sm.Client{
		Dict:               dict.Default,
		Handler:            df.machine,
		MaxRetransmits:     3,
		RetransmitInterval: 5 * time.Second,
		EnableWatchdog:     true, // DWR/DWA per RFC 6733 §5.5
		WatchdogInterval:   30 * time.Second,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)),
		},
	}

	// Register handler for DEA (and STA) responses.
	df.machine.Handle("DEA", df.handleDEA())
	df.machine.Handle("STA", df.handleDEA())

	return df
}

// Connect establishes and maintains a persistent connection to AAA-S.
// It performs CER/CEA handshake automatically via sm.Client.
// After connecting, a monitor goroutine watches for disconnection and reconnects.
func (df *diamForwarder) Connect(ctx context.Context) error {
	conn, err := df.smClient.DialNetwork(df.network, df.addr)
	if err != nil {
		return fmt.Errorf("diameter_forward: failed to connect to %s: %w", df.addr, err)
	}

	df.mu.Lock()
	df.conn = conn
	df.connected = true
	df.mu.Unlock()

	df.logger.Info("diameter_forward_connected",
		"server", df.addr,
		"network", df.network,
		"origin_host", df.originHost,
	)

	// Monitor connection in background
	go df.monitorConnection(ctx)

	return nil
}

// monitorConnection watches for disconnection and reconnects with exponential backoff.
func (df *diamForwarder) monitorConnection(ctx context.Context) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		df.mu.RLock()
		conn := df.conn
		df.mu.RUnlock()

		if conn == nil {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Attempt reconnect
			df.mu.Lock()
			newConn, err := df.smClient.DialNetwork(df.network, df.addr)
			if err != nil {
				df.mu.Unlock()
				df.logger.Error("diameter_forward_reconnect_failed",
					"error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			df.conn = newConn
			df.connected = true
			df.mu.Unlock()

			df.logger.Info("diameter_forward_reconnected", "server", df.addr)
			backoff = 1 * time.Second

			// Clear stale pending entries from the lost connection.
			df.clearPending()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// clearPending sends nil to all pending channels (signals connection loss) and clears the map.
func (df *diamForwarder) clearPending() {
	df.pendingMu.Lock()
	defer df.pendingMu.Unlock()
	for _, ch := range df.pending {
		select {
		case ch <- nil:
		default:
		}
	}
	for id := range df.pending {
		delete(df.pending, id)
	}
}

// Close closes the Diameter connection.
func (df *diamForwarder) Close() error {
	df.mu.Lock()
	defer df.mu.Unlock()
	if df.conn != nil {
		df.conn.Close()
		df.conn = nil
		df.connected = false
	}
	return nil
}

// Forward encodes raw EAP payload into a DER message, sends it to AAA-S,
// and waits for the DEA response.
// Spec: RFC 4072 (Diameter EAP), RFC 6733 §8.8, TS 29.561 §17
func (df *diamForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string, sst uint8, sd string) ([]byte, error) {
	conn, err := df.getConn()
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: no connection: %w", err)
	}

	// Create channel for response.
	respCh := make(chan *diam.Message, 1)
	hopByHop := df.nextHopByHopID()
	df.addPending(hopByHop, respCh)
	defer df.removePending(hopByHop)

	// Build DER message.
	m := diam.NewRequest(268, AppIDAAP, conn.Dictionary())
	m.Header.HopByHopID = hopByHop

	addAVP := func(code interface{}, flags uint8, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, flags, vendor, data)
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
	if err := addAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost); err != nil {
		return nil, err
	}
	if err := addAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm); err != nil {
		return nil, err
	}
	if err := addAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.DestinationHost, avp.Mbit, 0, datatype.DiameterIdentity(df.destHost)); err != nil {
		return nil, err
	}
	if err := addAVP(avp.DestinationRealm, avp.Mbit, 0, datatype.DiameterIdentity(df.destRealm)); err != nil {
		return nil, err
	}
	// User-Name: GPSI from sessionID (format: "nssAAF;{nano};{authCtxID}")
	// Extract the GPSI/authCtxID portion for User-Name AVP.
	userName := sessionID
	if len(sessionID) > 0 {
		// sessionID format: "nssAAF;{unixnano};{authCtxID}"
		// Use the whole sessionID as User-Name; AAA-S can extract what it needs.
	}
	if err := addAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(userName)); err != nil {
		return nil, err
	}
	// EAP-Payload AVP (code 209, RFC 4072 §4.2) — M-bit required for NSSAA.
	if err := addAVP(209, avp.Mbit, 0, datatype.OctetString(eapPayload)); err != nil {
		return nil, err
	}
	// 3GPP-SNSSAI AVP (code 310).
	snssaiAVP, err := encodeSnssaiAVP(sst, sd)
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: failed to encode SNSSAI: %w", err)
	}
	m.AddAVP(snssaiAVP)

	// Set deadline on the connection.
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
		df.removePending(hopByHop)
		return nil, fmt.Errorf("diameter_forward: failed to send DER: %w", err)
	}

	df.logger.Debug("diameter_forward_der_sent",
		"session_id", sessionID,
		"hop_by_hop", hopByHop,
		"eap_len", len(eapPayload),
	)

	// Wait for response or context cancellation.
	select {
	case <-ctx.Done():
		df.removePending(hopByHop)
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("diameter_forward: connection lost")
		}
		data, err := resp.Serialize()
		if err != nil {
			return nil, fmt.Errorf("diameter_forward: failed to serialize DEA: %w", err)
		}
		return data, nil
	}
}

// handleDEA dispatches DEA (and STA) responses to pending channels by hop-by-hop ID.
func (df *diamForwarder) handleDEA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		hopByHop := m.Header.HopByHopID
		df.pendingMu.RLock()
		ch, ok := df.pending[hopByHop]
		df.pendingMu.RUnlock()
		if !ok {
			df.logger.Warn("diameter_forward_unexpected_responser",
				"hop_by_hop", hopByHop,
			)
			return
		}
		ch <- m
	}
}

// getConn returns the current connection, connecting if necessary.
func (df *diamForwarder) getConn() (diam.Conn, error) {
	df.mu.RLock()
	conn := df.conn
	df.mu.RUnlock()

	if conn != nil {
		return conn, nil
	}

	// Attempt to reconnect synchronously.
	df.mu.Lock()
	defer df.mu.Unlock()

	// Double-check after acquiring write lock.
	if df.conn != nil {
		return df.conn, nil
	}

	newConn, err := df.smClient.DialNetwork(df.network, df.addr)
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: reconnect failed: %w", err)
	}

	df.conn = newConn
	df.connected = true
	return newConn, nil
}

// PeerMetadata returns the peer's metadata from the CER/CEA handshake.
func (df *diamForwarder) PeerMetadata() (*smpeer.Metadata, error) {
	conn, err := df.getConn()
	if err != nil {
		return nil, err
	}
	meta, ok := smpeer.FromContext(conn.Context())
	if !ok {
		return nil, fmt.Errorf("diameter_forward: no peer metadata available")
	}
	return meta, nil
}

func (df *diamForwarder) nextHopByHopID() uint32 {
	return uint32(atomic.AddUint64(&df.hopByHopSeq, 1))
}

func (df *diamForwarder) addPending(hopByHop uint32, ch chan *diam.Message) {
	df.pendingMu.Lock()
	df.pending[hopByHop] = ch
	df.pendingMu.Unlock()
}

func (df *diamForwarder) removePending(hopByHop uint32) {
	df.pendingMu.Lock()
	delete(df.pending, hopByHop)
	df.pendingMu.Unlock()
}

// encodeSnssaiAVP encodes S-NSSAI as a grouped AVP (code 310, 3GPP vendor).
// Spec: TS 29.061; same logic as internal/diameter.EncodeSnssaiAVP.
func encodeSnssaiAVP(sst uint8, sd string) (*diam.AVP, error) {
	sstAVP := diam.NewAVP(259, avp.Mbit|avp.Vbit, VendorID3GPP, datatype.Unsigned32(sst))

	var group *diam.GroupedAVP
	if sd != "" && sd != "FFFFFF" {
		sdBytes, err := parseSD(sd)
		if err != nil {
			return nil, err
		}
		sdAVP := diam.NewAVP(260, avp.Mbit|avp.Vbit, VendorID3GPP, datatype.OctetString(sdBytes))
		group = &diam.GroupedAVP{AVP: []*diam.AVP{sstAVP, sdAVP}}
	} else {
		group = &diam.GroupedAVP{AVP: []*diam.AVP{sstAVP}}
	}

	return diam.NewAVP(310, avp.Mbit|avp.Vbit, VendorID3GPP, group), nil
}

// parseSD converts a 6-character hex string to 3 bytes.
// Returns error if sd is not exactly 6 hex characters or contains invalid chars.
func parseSD(sd string) ([]byte, error) {
	if len(sd) != 6 {
		return nil, fmt.Errorf("invalid SNSSAI SD length: %q (expected 6 hex chars)", sd)
	}
	var result [3]byte
	for i := 0; i < 6; i++ {
		var val byte
		c := sd[i]
		switch {
		case c >= '0' && c <= '9':
			val = c - '0'
		case c >= 'A' && c <= 'F':
			val = c - 'A' + 10
		case c >= 'a' && c <= 'f':
			val = c - 'a' + 10
		default:
			return nil, fmt.Errorf("invalid SNSSAI SD: %q contains non-hex char %c", sd, c)
		}
		if i%2 == 0 {
			result[i/2] = val << 4
		} else {
			result[i/2] |= val
		}
	}
	return result[:], nil
}
