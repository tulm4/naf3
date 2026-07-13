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
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/diameter"
)

const (
	// VendorID3GPP is the 3GPP vendor ID (10415).
	// Spec: TS 29161; same value as internal/diameter.VendorID3GPP.
	VendorID3GPP uint32 = 10415
	// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
	AppIDAAP = 5
)

// ConnectionStats tracks connection health metrics for observability.
// Spec: GAP-DIA-06, GAP-DIA-07
type ConnectionStats struct {
	ConnectedAt   time.Time `json:"connected_at"`
	LastDWA       time.Time `json:"last_dwa"`
	LastDWR       time.Time `json:"last_dwr"`
	MessagesSent  uint64    `json:"messages_sent"`
	MessagesRecv  uint64    `json:"messages_recv"`
	HandshakeAt   time.Time `json:"handshake_at"`
	Errors        uint64    `json:"errors"`
}

// diamForwarderConfig holds configuration for the Diameter forwarder.
// Spec: RFC 6733, RFC 4072, TS 29.561 Ch.17
type diamForwarderConfig struct {
	// Transport is the dial network passed to sm.Client.DialNetwork.
	// Valid values: "tcp" (default) or "sctp".
	// Spec: RFC 6733 §3.
	Transport string
	// AuthRequestType is the AVP 406 value for DER messages.
	// Default: 2 (AUTHORIZE_AUTHENTICATE)
	// Spec: RFC 4072 §3.1
	AuthRequestType uint32
	// AuthApplicationID is the AVP 258 value for CER and DER.
	// Default: 5 (Diameter EAP)
	// Spec: RFC 4072
	AuthApplicationID uint32
}

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
	debug       *debug.Debug // optional; nil-safe — see internal/debug hooks

	// Server-initiated inbound (handlers live on df.machine, fire on the
	// forwarder's outbound socket — TCP is bidirectional, RFC 6733 §5.6).
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	registry     *ServerInitiatedRegistry

	// Protocol configuration (GAP-AAA-04, GAP-DIA-02, GAP-DIA-03)
	cfg *diamForwarderConfig

	// Origin-State-Id tracking (GAP-DIA-02): increments on each connection establishment.
	// Spec: RFC 6733 §8.8
	originStateID uint64
	stateMu       sync.Mutex

	// Pending requests: hop-by-hop ID → result channel.
	pending   map[uint32]chan *diam.Message
	pendingMu sync.RWMutex

	// Atomic counter for generating unique hop-by-hop IDs.
	hopByHopSeq uint64

	// Connection statistics (GAP-DIA-06, GAP-DIA-07)
	connStats ConnectionStats
}

// newDiamForwarder creates a new Diameter forwarder.
// addr is the AAA-S address (e.g. "nss-aaa-server:3868").
// network is "tcp" or "sctp".
// originHost/originRealm are the AAA Gateway's identity (Origin-Host in CER).
// destHost/destRealm are the AAA-S identity (Destination-Host in DER).
// cfg contains protocol configuration parameters.
// forwardToBiz is the callback used by server-initiated inbound handlers (ASR/RAR/STR)
// to deliver inbound Diameter server messages to the Biz Pod via the registry/HTTP path.
// registry tracks pending server-initiated requests until Biz Pod acknowledges.
// dbg is the per-UE debug subsystem; nil-safe.
// Spec: RFC 6733 §5.3 (CER/CEA), RFC 4072 (DER/DEA), RFC 6733 §5.6 (TCP bidirectional)
func newDiamForwarder(
	addr, network, originHost, originRealm, destHost, destRealm string,
	cfg *diamForwarderConfig,
	logger *slog.Logger,
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte),
	registry *ServerInitiatedRegistry,
	dbg *debug.Debug,
) *diamForwarder {
	// Apply defaults for optional config fields (GAP-AAA-04, GAP-DIA-02, GAP-DIA-03)
	if cfg.Transport == "" {
		cfg.Transport = "tcp"
	}
	if cfg.AuthRequestType == 0 {
		cfg.AuthRequestType = 2 // AUTHORIZE_AUTHENTICATE
	}
	if cfg.AuthApplicationID == 0 {
		cfg.AuthApplicationID = AppIDAAP // 5 (Diameter EAP)
	}

	df := &diamForwarder{
		addr:         addr,
		network:      network,
		originHost:   originHost,
		originRealm:  originRealm,
		destHost:     destHost,
		destRealm:    destRealm,
		logger:       logger,
		cfg:          cfg,
		debug:        dbg,
		pending:      make(map[uint32]chan *diam.Message),
		forwardToBiz: forwardToBiz,
		registry:     registry,
	}

	df.settings = &sm.Settings{
		OriginHost:  datatype.DiameterIdentity(originHost),
		OriginRealm: datatype.DiameterIdentity(originRealm),
		VendorID:    datatype.Unsigned32(VendorID3GPP),
		ProductName: "NSSAAF-GW",
		Dict:        diameter.Dict(),
	}

	df.machine = sm.New(df.settings)

	df.smClient = &sm.Client{
		Dict:               diameter.Dict(),
		Handler:            df.machine,
		MaxRetransmits:     3,
		RetransmitInterval: 5 * time.Second,
		EnableWatchdog:     true, // DWR/DWA per RFC 6733 §5.5
		WatchdogInterval:   30 * time.Second,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(cfg.AuthApplicationID)),
		},
	}

	// Register handler for DEA responses (pending-channel dispatch by hop-by-hop ID).
	df.machine.Handle("DEA", df.handleDEA())

	// Register DWR/DWA handlers for connection stats (GAP-DIA-06, GAP-DIA-07)
	df.machine.Handle("DWR", df.handleDWR())
	df.machine.Handle("DWA", df.handleDWA())

	// Register server-initiated inbound handlers (ASR/ASA/RAR/RAA/STR/STA) on this
	// same state machine so they fire on the forwarder's outbound TCP socket.
	// RFC 6733 §5.6: TCP connections are bidirectional; server-initiated messages
	// arrive on the same socket the gateway dialed.
	df.machine.Handle("ASR", df.handleASR())
	df.machine.Handle("ASA", df.handleASA())
	df.machine.Handle("RAR", df.handleRAR())
	df.machine.Handle("RAA", df.handleRAA())
	df.machine.Handle("STR", df.handleSTR())
	df.machine.Handle("STA", df.handleSTA())

	return df
}

// Connect establishes and maintains a persistent connection to AAA-S.
// It performs CER/CEA handshake automatically via sm.Client.
// After connecting, a monitor goroutine watches for disconnection and reconnects,
// while watchDisconnect clears the conn on peer-initiated loss.
func (df *diamForwarder) Connect(ctx context.Context) error {
	conn, err := df.smClient.DialNetwork(df.network, df.addr)
	if err != nil {
		return fmt.Errorf("diameter_forward: failed to connect to %s: %w", df.addr, err)
	}

	df.mu.Lock()
	df.conn = conn
	df.connected = true
	df.connStats.ConnectedAt = time.Now()
	df.connStats.HandshakeAt = time.Now()
	df.mu.Unlock()

	// Increment Origin-State-Id on each new connection (GAP-DIA-02).
	// Spec: RFC 6733 §8.8
	osi := df.incrementOriginStateID()

	df.logger.Info("diameter_forward_connected",
		"server", df.addr,
		"network", df.network,
		"origin_host", df.originHost,
		"origin_state_id", osi,
	)

	// Watch peer disconnect via diam.CloseNotifier (RFC 6733 §5.6 + sm internals).
	go df.watchDisconnect(ctx)
	// Drive reconnect loop independently.
	go df.monitorConnection(ctx)

	return nil
}

// watchDisconnect blocks until the underlying socket signals CloseNotify.
// On notification it clears df.conn so monitorConnection will reconnect.
// Spec: RFC 6733 §5.6 (DPR), go-diameter diam.CloseNotifier.
func (df *diamForwarder) watchDisconnect(ctx context.Context) {
	df.mu.RLock()
	conn := df.conn
	df.mu.RUnlock()
	if conn == nil {
		return
	}
	notifier, ok := conn.(diam.CloseNotifier)
	if !ok {
		// CloseNotify not supported on this conn; nothing to watch.
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-notifier.CloseNotify():
		df.mu.Lock()
		if df.conn == conn {
			df.conn = nil
			df.connected = false
			df.logger.Warn("diameter_forward_peer_lost", "server", df.addr)
			df.mu.Unlock()
			df.clearPending()
		} else {
			df.mu.Unlock()
		}
	}
}

// monitorConnection reconnects when df.connected is false (set by either
// watchDisconnect on peer loss or Close). Backoff is exponential, capped at 30s.
func (df *diamForwarder) monitorConnection(ctx context.Context) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		df.mu.RLock()
		connected := df.connected
		df.mu.RUnlock()

		if !connected {
			df.mu.Lock()
			newConn, err := df.smClient.DialNetwork(df.network, df.addr)
			if err != nil {
				df.mu.Unlock()
				df.logger.Error("diameter_forward_reconnect_failed",
					"error", err, "backoff", backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			df.conn = newConn
			df.connected = true
			df.connStats.ConnectedAt = time.Now()
			df.mu.Unlock()

			df.logger.Info("diameter_forward_reconnected", "server", df.addr)
			backoff = 1 * time.Second
			df.clearPending()

			// Watch the new connection for peer-initiated close.
			go df.watchDisconnect(ctx)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
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

// buildDERMessage constructs a Diameter-EAP-Request message with all required AVPs.
// Spec: RFC 4072, RFC 6733 §8.8, TS 29.561 §17
func (df *diamForwarder) buildDERMessage(conn diam.Conn, hopByHop uint32, sessionID string, eapPayload []byte, sst uint8, sd string) (*diam.Message, error) {
	m := diam.NewRequest(268, df.cfg.AuthApplicationID, conn.Dictionary())
	m.Header.HopByHopID = hopByHop

	addAVP := func(code interface{}, flags uint8, _ uint32, data datatype.Type) error {
		_, avpErr := m.NewAVP(code, flags, 0, data)
		return avpErr
	}

	if avpErr := addAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(df.cfg.AuthApplicationID)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(df.cfg.AuthRequestType)); avpErr != nil { // GAP-AAA-04: configurable
		return nil, avpErr
	}
	if avpErr := addAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Unsigned32(1)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(df.getOriginStateID())); avpErr != nil { // GAP-DIA-02: tracked state ID
		return nil, avpErr
	}
	if avpErr := addAVP(avp.DestinationHost, avp.Mbit, 0, datatype.DiameterIdentity(df.destHost)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.DestinationRealm, avp.Mbit, 0, datatype.DiameterIdentity(df.destRealm)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(sessionID)); avpErr != nil {
		return nil, avpErr
	}
	if avpErr := addAVP(209, avp.Mbit, 0, datatype.OctetString(eapPayload)); avpErr != nil {
		return nil, avpErr
	}

	snssaiAVP, err := encodeSnssaiAVP(sst, sd)
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: failed to encode SNSSAI: %w", err)
	}
	m.AddAVP(snssaiAVP)

	return m, nil
}

// Forward encodes raw EAP payload into a DER message, sends it to AAA-S,
// and waits for the DEA response.
// Spec: RFC 4072 (Diameter EAP), RFC 6733 §8.8, TS 29.561 §17
func (df *diamForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string, sst uint8, sd string) ([]byte, error) {
	conn, err := df.getConn()
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: no connection: %w", err)
	}

	respCh := make(chan *diam.Message, 1)
	hopByHop := df.nextHopByHopID()
	df.addPending(hopByHop, respCh)
	defer df.removePending(hopByHop)

	m, err := df.buildDERMessage(conn, hopByHop, sessionID, eapPayload, sst, sd)
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

	// Protocol-kind debug event before the wire write so an operator pulling a
	// single UE's stream sees the DER with command code + peer + session before
	// the corresponding response. The actual underlying send call is wrapped
	// with WrapProtocol in Task 14 — this Emit is the higher-level "we are
	// about to send DER" signal with request metadata.
	df.debug.Emit(ctx, debug.Event{
		Op:     "diameter.eap.send",
		Kind:   debug.KindProtocol,
		AuthID: sessionID,
		Detail: map[string]any{
			"command_code": 268, // DER per RFC 4072 §3.1
			"peer":         df.addr,
			"network":      df.network,
			"hop_by_hop":   hopByHop,
			"eap_len":      len(eapPayload),
		},
		Status: "ok",
	})

	_, err = m.WriteTo(conn)
	if err != nil {
		df.removePending(hopByHop)
		return nil, fmt.Errorf("diameter_forward: failed to send DER: %w", err)
	}

	df.incrementMessagesSent()

	// Log at Info (not Debug) so E2E tests at the default log level can verify
	// that a DER was actually emitted. The size/number fields are diagnostic,
	// not security-sensitive (EAP payload length and a session-id).
	// Spec: RFC 4072 §3.1 (DER), RFC 6733 §8.8 (header).
	df.logger.Info("diameter_forward_der_sent",
		"session_id", sessionID,
		"hop_by_hop", hopByHop,
		"eap_len", len(eapPayload),
	)

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
		df.incrementMessagesRecv()
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

// getOriginStateID returns the current Origin-State-Id value.
// Spec: RFC 6733 §8.8 (GAP-DIA-02)
func (df *diamForwarder) getOriginStateID() uint64 {
	df.stateMu.Lock()
	defer df.stateMu.Unlock()
	return df.originStateID
}

// incrementOriginStateID increments and returns the new Origin-State-Id value.
// Called on each connection establishment.
// Spec: RFC 6733 §8.8 (GAP-DIA-02)
func (df *diamForwarder) incrementOriginStateID() uint64 {
	df.stateMu.Lock()
	defer df.stateMu.Unlock()
	df.originStateID++
	return df.originStateID
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

// GetConnectionStats returns a snapshot of the current connection statistics.
// Spec: GAP-DIA-06, GAP-DIA-07
func (df *diamForwarder) GetConnectionStats() ConnectionStats {
	df.mu.RLock()
	defer df.mu.RUnlock()
	return df.connStats
}

// recordDWR records that a Device-Watchdog-Request was sent.
// Spec: GAP-DIA-06
func (df *diamForwarder) recordDWR() {
	df.mu.Lock()
	defer df.mu.Unlock()
	now := time.Now()
	df.connStats.LastDWR = now
}

// recordDWA records that a Device-Watchdog-Answer was received.
// Spec: GAP-DIA-07
func (df *diamForwarder) recordDWA() {
	df.mu.Lock()
	defer df.mu.Unlock()
	now := time.Now()
	df.connStats.LastDWA = now
}

// incrementMessagesSent increments the sent message counter.
// Spec: GAP-DIA-06
func (df *diamForwarder) incrementMessagesSent() {
	df.mu.Lock()
	defer df.mu.Unlock()
	df.connStats.MessagesSent++
}

// incrementMessagesRecv increments the received message counter.
// Spec: GAP-DIA-07
func (df *diamForwarder) incrementMessagesRecv() {
	df.mu.Lock()
	defer df.mu.Unlock()
	df.connStats.MessagesRecv++
}

// handleDWR handles incoming Device-Watchdog-Request messages.
// Spec: GAP-DIA-06
func (df *diamForwarder) handleDWR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		df.recordDWR()
	}
}

// handleDWA handles incoming Device-Watchdog-Answer messages.
// Spec: GAP-DIA-07
func (df *diamForwarder) handleDWA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		df.recordDWA()
	}
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

// handleASR handles Abort-Session-Request from AAA-S (server-initiated).
// Fires on the forwarder's outbound TCP socket after CER/CEA handshake succeeds.
// Registers the pending ASR with the registry, forwards to Biz Pod, waits for Biz
// Pod response, and sends ASA with the result code.
//
// Spec: RFC 6733 §5.3, ASR/ASA as per TS 29.561 Ch.17
func (df *diamForwarder) handleASR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		authCtxID := df.extractAuthCtxID(m)

		df.logger.Info("Diameter ASR received",
			"session_id", sessionID,
			"hop_by_hop", m.Header.HopByHopID,
			"end_to_end", m.Header.EndToEndID,
		)

		raw, err := m.Serialize()
		if err != nil {
			df.logger.Error("failed to serialize ASR", "error", err)
			df.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := df.registry.Register(sessionID, authCtxID, "ASR", 10*time.Second)
		if err != nil {
			df.logger.Error("failed to register ASR", "error", err)
			df.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		// Detach entire flow into background goroutine to avoid blocking the
		// handler goroutine. Under high load, blocking would exhaust the pool.
		go func() {
			df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "ASR", raw)
			resp := respCh.Wait()
			df.logger.Info("ASR: received response from registry",
				"session_id", sessionID,
				"result_code", resp.ResultCode,
			)
			df.sendASAWithResult(conn, m, resp)
		}()
	}
}

// sendASAWithResult sends Abort-Session-Answer with the specified result code.
// This is used when waiting for Biz Pod response before sending ASA.
func (df *diamForwarder) sendASAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(resp.ResultCode)
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	if resp.Payload != nil {
		// Use AVP code 1269 (Experimental-Result-Code) for extended result codes
		_, _ = ans.NewAVP(avp.ExperimentalResult, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		df.logger.Error("failed to send ASA", "error", err)
	}
}

// handleASA handles Abort-Session-Answer from AAA-S (response to our STR).
func (df *diamForwarder) handleASA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		df.logger.Debug("diameter_asa_received", "session_id", sessionID)
	}
}

// handleRAR handles Re-Auth-Request from AAA-S (server-initiated).
// Follows the same register → forward → wait → sendRAA pattern as handleASR
// to ensure Biz Pod response is processed before sending RAA.
//
// Spec: RFC 6733 §5.3, RAR/RAA as per TS 29.561 Ch.17
func (df *diamForwarder) handleRAR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		authCtxID := df.extractAuthCtxID(m)

		df.logger.Info("Diameter RAR received", "session_id", sessionID)

		raw, err := m.Serialize()
		if err != nil {
			df.logger.Error("failed to serialize RAR", "error", err)
			df.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := df.registry.Register(sessionID, authCtxID, "RAR", 10*time.Second)
		if err != nil {
			df.logger.Error("failed to register RAR", "error", err)
			df.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		go func() {
			df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "RAR", raw)
			resp := respCh.Wait()
			df.logger.Info("RAR: received response from registry",
				"session_id", sessionID,
				"result_code", resp.ResultCode,
			)
			df.sendRAAWithResult(conn, m, resp)
		}()
	}
}

// sendRAAWithResult sends Re-Auth-Answer with the specified result code.
// Used when waiting for Biz Pod response before sending RAA.
func (df *diamForwarder) sendRAAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(resp.ResultCode)
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	if resp.Payload != nil {
		_, _ = ans.NewAVP(diameter.AVPCodeEAPPayload, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		df.logger.Error("failed to send RAA", "error", err)
	}
}

// handleRAA handles Re-Auth-Answer from AAA-S.
func (df *diamForwarder) handleRAA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		df.logger.Debug("diameter_raa_received", "session_id", sessionID)
	}
}

// handleSTR handles Session-Termination-Request from AAA-S (server-initiated).
// Fires on the forwarder's outbound TCP socket after CER/CEA handshake succeeds.
// Registers the pending STR with the registry, forwards to Biz Pod, waits for
// Biz Pod response, and sends STA with the result code.
//
// Spec: RFC 6733 §5.3, STR/STA as per TS 29.561 Ch.17
func (df *diamForwarder) handleSTR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		authCtxID := df.extractAuthCtxID(m)

		df.logger.Info("Diameter STR received", "session_id", sessionID)

		raw, err := m.Serialize()
		if err != nil {
			df.logger.Error("failed to serialize STR", "error", err)
			df.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := df.registry.Register(sessionID, authCtxID, "STR", 10*time.Second)
		if err != nil {
			df.logger.Error("failed to register STR", "error", err)
			df.sendSTAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		// Detach entire flow into background goroutine to avoid blocking the
		// handler goroutine. Under high load, blocking would exhaust the pool.
		go func() {
			df.forwardToBiz(context.Background(), sessionID, "DIAMETER", "STR", raw)
			resp := respCh.Wait()
			df.logger.Info("STR: received response from registry",
				"session_id", sessionID,
				"result_code", resp.ResultCode,
			)
			df.sendSTAWithResult(conn, m, resp)
		}()
	}
}

// handleSTA handles Session-Termination-Answer from AAA-S.
func (df *diamForwarder) handleSTA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		df.logger.Debug("diameter_sta_received", "session_id", sessionID)
	}
}

// sendSTAWithResult sends Session-Termination-Answer with the given result code.
// Used when waiting for Biz Pod response before sending STA.
// Spec: TS 29.561 §17.3 (STA)
func (df *diamForwarder) sendSTAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(resp.ResultCode)
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	if resp.Payload != nil {
		_, _ = ans.NewAVP(diameter.AVPCodeEAPPayload, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		df.logger.Error("failed to send STA", "error", err)
	}
}

// sendASA sends Abort-Session-Answer in response to ASR.
// Result-Code = DIAMETER_SUCCESS (2001).
func (df *diamForwarder) sendASA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		df.logger.Error("failed to send ASA", "error", err)
	}
}

// sendRAA sends Re-Auth-Answer in response to RAR.
func (df *diamForwarder) sendRAA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		df.logger.Error("failed to send RAA", "error", err)
	}
}

// sendSTA sends Session-Termination-Answer in response to STR.
func (df *diamForwarder) sendSTA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		df.logger.Error("failed to send STA", "error", err)
	}
}

// extractAuthCtxID extracts the Auth-Application-Id AVP from a decoded diam.Message.
// This AVP identifies the authentication context/session.
func (df *diamForwarder) extractAuthCtxID(m *diam.Message) string {
	for _, avp := range m.AVP {
		if avp.Code == 258 { // Auth-Application-Id AVP code
			if ui32, ok := avp.Data.(datatype.Unsigned32); ok {
				return fmt.Sprintf("%d", ui32)
			}
			if os, ok := avp.Data.(datatype.UTF8String); ok {
				return string(os)
			}
		}
	}
	return ""
}

// extractSessionIDFromMsg extracts the Session-Id AVP from a decoded diam.Message.
func extractSessionIDFromMsg(m *diam.Message) string {
	for _, avp := range m.AVP {
		if avp.Code == 263 { // Session-Id AVP code
			if os, ok := avp.Data.(datatype.UTF8String); ok {
				return string(os)
			}
			if os, ok := avp.Data.(datatype.OctetString); ok {
				return string(os)
			}
		}
	}
	return ""
}
