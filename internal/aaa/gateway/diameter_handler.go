// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, RFC 6733, RFC 4072, TS 29.561 Ch.16/17
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/diameter"
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
	logger        *slog.Logger
	forwardToBiz  func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	version       string
	bizURL        string
	httpClient    *http.Client
	diamForwarder *diamForwarder
	registry      *ServerInitiatedRegistry // tracks pending server-initiated requests
	sm            *sm.StateMachine
}

// NewDiameterHandler creates a DiameterHandler with a go-diameter/v4 state machine.
func NewDiameterHandler(
	logger *slog.Logger,
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte),
	version, bizURL string,
	httpClient *http.Client,
	diamForwarder *diamForwarder,
	registry *ServerInitiatedRegistry,
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
		logger:        logger,
		forwardToBiz:  forwardToBiz,
		version:       version,
		bizURL:        bizURL,
		httpClient:    httpClient,
		diamForwarder: diamForwarder,
		registry:      registry,
		sm:            machine,
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
		//nolint:contextcheck
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
			//nolint:contextcheck
			go h.serveTCP(listener)
		} else {
			//nolint:contextcheck
			go h.serveSCTP(listener)
		}
	default:
		return fmt.Errorf("unsupported diameter protocol: %s (expected tcp or sctp)", protocol)
	}
	return nil
}

// serveTCP accepts incoming TCP connections and handles each with sm.StateMachine.
func (h *DiameterHandler) serveTCP(listener net.Listener) {
	defer func() { _ = listener.Close() }()
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
	defer func() { _ = listener.Close() }()
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
	defer func() { _ = conn.Close() }()

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
// It registers the pending ASR with the registry, forwards to Biz Pod,
// waits for Biz Pod response, and sends ASA with the result code.
//
// Spec: RFC 6733 §5.3, ASR/ASA as per TS 29.561 Ch.17
func (h *DiameterHandler) handleASR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		authCtxID := h.extractAuthCtxID(m)

		h.logger.Info("Diameter ASR received",
			"session_id", sessionID,
			"hop_by_hop", m.Header.HopByHopID,
			"end_to_end", m.Header.EndToEndID,
		)

		raw, err := m.Serialize()
		if err != nil {
			h.logger.Error("failed to serialize ASR", "error", err)
			h.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := h.registry.Register(sessionID, authCtxID, "ASR", 10*time.Second)
		if err != nil {
			h.logger.Error("failed to register ASR", "error", err)
			h.sendASAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		// Detach entire flow into background goroutine to avoid blocking the
		// handler goroutine. Under high load, blocking would exhaust the pool.
		go func() {
			h.forwardToBiz(context.Background(), sessionID, "DIAMETER", "ASR", raw)
			resp := respCh.Wait()
			h.logger.Info("ASR: received response from registry",
				"session_id", sessionID,
				"result_code", resp.ResultCode,
			)
			h.sendASAWithResult(conn, m, resp)
		}()
	}
}

// sendASAWithResult sends Abort-Session-Answer with the specified result code.
// This is used when waiting for Biz Pod response before sending ASA.
func (h *DiameterHandler) sendASAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(resp.ResultCode)
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	if resp.Payload != nil {
		// Use AVP code 1269 (Experimental-Result-Code) for extended result codes
		_, _ = ans.NewAVP(avp.ExperimentalResult, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		h.logger.Error("failed to send ASA", "error", err)
	}
}

// handleASA handles Abort-Session-Answer from AAA-S (response to our STR).
func (h *DiameterHandler) handleASA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("diameter_asa_received", "session_id", sessionID)
	}
}

// handleRAR handles Re-Auth-Request from AAA-S (server-initiated).
// Follows the same register → forward → wait → sendRAA pattern as handleASR
// to ensure Biz Pod response is processed before sending RAA.
//
// Spec: RFC 6733 §5.3, RAR/RAA as per TS 29.561 Ch.17
func (h *DiameterHandler) handleRAR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		if sessionID == "" {
			sessionID = "unknown"
		}

		authCtxID := h.extractAuthCtxID(m)

		h.logger.Info("Diameter RAR received", "session_id", sessionID)

		raw, err := m.Serialize()
		if err != nil {
			h.logger.Error("failed to serialize RAR", "error", err)
			h.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "serialize_failed"})
			return
		}

		respCh, err := h.registry.Register(sessionID, authCtxID, "RAR", 10*time.Second)
		if err != nil {
			h.logger.Error("failed to register RAR", "error", err)
			h.sendRAAWithResult(conn, m, &ServerInitiatedResponse{ResultCode: 3002, ErrorCause: "register_failed"})
			return
		}

		go func() {
			h.forwardToBiz(context.Background(), sessionID, "DIAMETER", "RAR", raw)
			resp := respCh.Wait()
			h.logger.Info("RAR: received response from registry",
				"session_id", sessionID,
				"result_code", resp.ResultCode,
			)
			h.sendRAAWithResult(conn, m, resp)
		}()
	}
}

// sendRAAWithResult sends Re-Auth-Answer with the specified result code.
// Used when waiting for Biz Pod response before sending RAA.
func (h *DiameterHandler) sendRAAWithResult(conn diam.Conn, req *diam.Message, resp *ServerInitiatedResponse) {
	ans := req.Answer(resp.ResultCode)
	ans.Header.HopByHopID = req.Header.HopByHopID
	ans.Header.EndToEndID = req.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
	if resp.Payload != nil {
		_, _ = ans.NewAVP(diameter.AVPCodeEAPPayload, avp.Mbit, 0, datatype.OctetString(resp.Payload))
	}
	if _, err := ans.WriteTo(conn); err != nil {
		h.logger.Error("failed to send RAA", "error", err)
	}
}

// handleRAA handles Re-Auth-Answer from AAA-S.
func (h *DiameterHandler) handleRAA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("diameter_raa_received", "session_id", sessionID)
	}
}

// handleSTR handles Session-Termination-Request from AAA-S.
func (h *DiameterHandler) handleSTR() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Info("Diameter STR received", "session_id", sessionID)

		// Send STA back.
		h.sendSTA(conn, m)
	}
}

// handleSTA handles Session-Termination-Answer from AAA-S.
func (h *DiameterHandler) handleSTA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		sessionID := extractSessionIDFromMsg(m)
		h.logger.Debug("diameter_sta_received", "session_id", sessionID)
	}
}

// sendASA sends Abort-Session-Answer in response to ASR.
// Result-Code = DIAMETER_SUCCESS (2001).
func (h *DiameterHandler) sendASA(conn diam.Conn, m *diam.Message) {
	ans := m.Answer(diam.Success)
	ans.Header.HopByHopID = m.Header.HopByHopID
	ans.Header.EndToEndID = m.Header.EndToEndID
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
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
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
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
	_, _ = ans.NewAVP(avp.OriginHost, avp.Mbit, 0, h.sm.Settings().OriginHost)
	_, _ = ans.NewAVP(avp.OriginRealm, avp.Mbit, 0, h.sm.Settings().OriginRealm)
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

// extractAuthCtxID extracts the Auth-Application-Id AVP from a decoded diam.Message.
// This AVP identifies the authentication context/session.
func (h *DiameterHandler) extractAuthCtxID(m *diam.Message) string {
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
