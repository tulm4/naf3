// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, RFC 6733, RFC 4072, TS 29.561 Ch.16/17
package gateway

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

// DiameterHandler handles Diameter protocol traffic on the SERVER-INITIATED path
// (AAA-S → NSSAAF). The client-initiated path (NSSAAF → AAA-S) is handled by
// diamForwarder.
//
// It uses go-diameter/v4/sm.StateMachine for RFC 6733 §5.3 compliance:
// - CER/CEA handshake on every incoming connection (both sides MUST exchange)
// - DWR/DWA watchdog (RFC 6733 §5.5)
// - ASR (Abort-Session-Request) routing to Biz Pod after handshake
//
// The sm.StateMachine wraps each raw net.Conn via diam.NewConn(), then manages
// CER/CEA internally. Registered handlers only fire after the handshake succeeds.
type DiameterHandler struct {
	logger          *slog.Logger
	publishResponse func(sessionID string, raw []byte)
	forwardToBiz    func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	version         string
	bizURL          string
	httpClient      *http.Client
	diamForwarder   *diamForwarder // client-initiated forwarder

	// sm is the state machine for server-side CER/CEA handling.
	// Created in NewDiameterHandler with the AAA Gateway's identity.
	sm *sm.StateMachine
}

// NewDiameterHandler creates a DiameterHandler with a go-diameter/v4 state machine.
func NewDiameterHandler(
	logger *slog.Logger,
	publishResponse func(sessionID string, raw []byte),
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte),
	version, bizURL string,
	httpClient *http.Client,
	diamForwarder *diamForwarder,
	originHost, originRealm string,
) *DiameterHandler {
	settings := &sm.Settings{
		OriginHost:  datatype.DiameterIdentity(originHost),
		OriginRealm: datatype.DiameterIdentity(originRealm),
		VendorID:    datatype.Unsigned32(VendorID3GPP),
		ProductName: "NSSAAF-GW",
	}

	machine := sm.New(settings)

	h := &DiameterHandler{
		logger:          logger,
		publishResponse: publishResponse,
		forwardToBiz:    forwardToBiz,
		version:         version,
		bizURL:          bizURL,
		httpClient:      httpClient,
		diamForwarder:   diamForwarder,
		sm:              machine,
	}

	// Register ASR handler. It only fires AFTER the peer passes CER/CEA
	// (sm.StateMachine wraps it with handshakeOK guard).
	h.sm.Handle("ASR", h.handleASR())
	h.sm.Handle("ASA", h.handleASA())
	h.sm.Handle("RAR", h.handleRAR())
	h.sm.Handle("RAA", h.handleRAA())
	h.sm.Handle("STR", h.handleSTR())
	h.sm.Handle("STA", h.handleSTA())

	return h
}

// Listen starts the Diameter server on the configured protocol (TCP or SCTP).
// Each incoming connection is wrapped with diam.NewConn() and handed to the
// sm.StateMachine for CER/CEA handling. After handshake, the StateMachine
// dispatches ASR/ASA/RAR/RAA/STR/STA to registered handlers.
// Spec: PHASE §2.3, §6.3; RFC 6733 §5.3 (CER/CEA — both peers MUST exchange)
func (h *DiameterHandler) Listen(ctx context.Context, addr, protocol string) error {
	switch protocol {
	case "tcp":
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("diameter tcp listen: %w", err)
		}
		go h.serveTCP(listener)
	case "sctp":
		listener, err := net.Listen("sctp", addr)
		if err != nil {
			h.logger.Warn("SCTP not available on this host", "error", err)
			// Fall back to TCP
			listener, err = net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("diameter tcp fallback listen: %w", err)
			}
			h.logger.Info("Diameter falling back to TCP", "addr", addr)
			go h.serveTCP(listener)
		} else {
			go h.serveSCTP(listener)
		}
	default:
		return fmt.Errorf("unsupported diameter protocol: %s (expected tcp or sctp)", protocol)
	}
	return nil
}

// serveTCP accepts incoming TCP connections and handles each with sm.StateMachine.
func (h *DiameterHandler) serveTCP(listener net.Listener) {
	defer listener.Close()
	h.logger.Info("Diameter TCP listener started", "addr", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			h.logger.Error("Diameter TCP accept error", "error", err)
			continue
		}
		go h.HandleConnection(conn)
	}
}

// serveSCTP accepts incoming SCTP connections and handles each with sm.StateMachine.
func (h *DiameterHandler) serveSCTP(listener net.Listener) {
	defer listener.Close()
	h.logger.Info("Diameter SCTP listener started", "addr", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			h.logger.Error("Diameter SCTP accept error", "error", err)
			continue
		}
		go h.HandleConnection(conn)
	}
}

// HandleConnection wraps a raw net.Conn with diam.NewConn() and hands it to the
// sm.StateMachine. The StateMachine handles CER/CEA and DWR/DWA internally.
// After the handshake succeeds, registered handlers (ASR, ASA, RAR, RAA, STR, STA)
// receive messages. ASR is forwarded to the Biz Pod via forwardToBiz.
// Spec: RFC 6733 §5.3 (CER/CEA — both peers MUST exchange), §5.5 (DWR/DWA)
func (h *DiameterHandler) HandleConnection(conn net.Conn) {
	defer conn.Close()

	// Wrap raw net.Conn with diam.Conn interface.
	// diam.NewConn starts a goroutine that reads messages and dispatches to the handler.
	// The sm.StateMachine handles CER/CEA and DWR/DWA internally.
	// After handshake, ASR/ASA/RAR/RAA/STR/STA are dispatched to registered handlers.
	if _, err := diam.NewConn(conn, conn.RemoteAddr().String(), h.sm, dict.Default); err != nil {
		h.logger.Error("diameter: failed to wrap connection", "error", err, "remote", conn.RemoteAddr())
		return
	}

	h.logger.Info("Diameter connection opened",
		"remote", conn.RemoteAddr(),
		"local", conn.LocalAddr(),
	)

	// Wait for handshake completion or connection close.
	// HandshakeNotify() sends the diam.Conn after CER/CEA succeeds.
	select {
	case peerConn := <-h.sm.HandshakeNotify():
		// Handshake succeeded. Log peer metadata.
		if meta, ok := smpeer.FromContext(peerConn.Context()); ok {
			h.logger.Info("Diameter handshake completed",
				"peer_host", meta.OriginHost,
				"peer_realm", meta.OriginRealm,
				"peer_apps", meta.Applications,
			)
		}
		// Connection remains open; sm.StateMachine continues reading and dispatching.
		// Application messages (ASR, etc.) will be handled by registered handlers.
		// Block until the connection is closed.
		<-make(chan struct{})
	case <-time.After(60 * time.Second):
		h.logger.Warn("Diameter handshake timeout", "remote", conn.RemoteAddr())
	}
}

// handleASR handles Abort-Session-Request from AAA-S (server-initiated).
// This handler only fires after CER/CEA handshake succeeds.
func (h *DiameterHandler) handleASR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		h.logger.Info("Diameter ASR received",
			"session_id", sessionID,
			"hop_by_hop", m.Header.HopByHopID,
			"end_to_end", m.Header.EndToEndID,
		)

		// Send ASA back to AAA-S.
		h.sendASA(conn, m)

		// Serialize the raw ASR message and forward to Biz Pod.
		raw, err := m.Serialize()
		if err != nil {
			h.logger.Error("failed to serialize ASR", "error", err)
			return
		}
		// Extract context from the connection for distributed tracing continuity.
		// This ensures ASR/RAR server-initiated messages are traced as children
		// of the AAA-S initiation span (TS 23.502 §4.2.9.3).
		h.forwardToBiz(conn.Context(), sessionID, "DIAMETER", "ASR", raw)
	}
}

// handleASA handles Abort-Session-Answer from AAA-S (response to our STR).
func (h *DiameterHandler) handleASA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("Diameter ASA received", "session_id", sessionID)
		raw, _ := m.Serialize()
		h.publishResponse(sessionID, raw)
	}
}

// handleRAR handles Re-Auth-Request from AAA-S (server-initiated reauth).
func (h *DiameterHandler) handleRAR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		h.logger.Info("Diameter RAR received", "session_id", sessionID)

		// Send RAA back.
		h.sendRAA(conn, m)

		raw, _ := m.Serialize()
		// Extract context from the connection for distributed tracing continuity.
		// This ensures RAR server-initiated re-auth is traced as a child of the
		// AAA-S initiation span (TS 23.502 §4.2.9.3).
		h.forwardToBiz(conn.Context(), sessionID, "DIAMETER", "RAR", raw)
	}
}

// handleRAA handles Re-Auth-Answer from AAA-S.
func (h *DiameterHandler) handleRAA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("Diameter RAA received", "session_id", sessionID)
		raw, _ := m.Serialize()
		h.publishResponse(sessionID, raw)
	}
}

// handleSTR handles Session-Termination-Request from AAA-S.
func (h *DiameterHandler) handleSTR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Info("Diameter STR received", "session_id", sessionID)

		// Send STA back.
		h.sendSTA(conn, m)

		raw, _ := m.Serialize()
		h.publishResponse(sessionID, raw)
	}
}

// handleSTA handles Session-Termination-Answer from AAA-S.
func (h *DiameterHandler) handleSTA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("Diameter STA received", "session_id", sessionID)
		raw, _ := m.Serialize()
		h.publishResponse(sessionID, raw)
	}
}

// sendASA sends Abort-Session-Answer in response to ASR.
// Result-Code = DIAMETER_SUCCESS (2001).
func (h *DiameterHandler) sendASA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		h.logger.Error("failed to send ASA", "error", err)
	}
}

// sendRAA sends Re-Auth-Answer in response to RAR.
func (h *DiameterHandler) sendRAA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		h.logger.Error("failed to send RAA", "error", err)
	}
}

// sendSTA sends Session-Termination-Answer in response to STR.
func (h *DiameterHandler) sendSTA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	_, err := ans.WriteTo(conn)
	if err != nil {
		h.logger.Error("failed to send STA", "error", err)
	}
}

// Forward delegates to the diamForwarder for the client-initiated path.
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	if h.diamForwarder == nil {
		return nil, fmt.Errorf("diameter_forward: forwarder not configured")
	}
	return nil, fmt.Errorf("diameter_forward: use diamForwarder.Forward() directly with Sst/Sd")
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

// extractDiameterSessionID extracts the Session-Id AVP from raw bytes.
// This is used for the legacy manual-parsing path.
func extractDiameterSessionID(raw []byte) string {
	if len(raw) < 24 {
		return ""
	}
	pos := 20
	for pos+8 <= len(raw) {
		avpCode := binary.BigEndian.Uint32([]byte{0, raw[pos], raw[pos+1], raw[pos+2]})
		avpLen := int(binary.BigEndian.Uint32([]byte{0, raw[pos+4], raw[pos+5], raw[pos+6]}))
		if avpLen < 8 || pos+avpLen > len(raw) {
			break
		}
		if avpCode == 263 { // Session-Id
			return string(raw[pos+8 : pos+avpLen])
		}
		pos += avpLen
	}
	return ""
}
