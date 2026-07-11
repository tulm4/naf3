// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, RFC 6733, RFC 4072, TS 29.561 Ch.16/17
package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/diameter"
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

// DiameterHandlerConfig holds TLS configuration and peer allowlist for the Diameter server.
type DiameterHandlerConfig struct {
	AllowedHosts  []string // Allowed Origin-Host values (empty = allow all)
	AllowedRealms []string // Allowed Origin-Realm values (empty = allow all)
	TLSCert       string   // Path to TLS certificate file
	TLSKey        string   // Path to TLS private key file
	TLSCACert     string   // Path to CA certificate for client auth (optional)
}

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
	cfg           *DiameterHandlerConfig
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
	cfg *DiameterHandlerConfig,
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
		cfg:           cfg,
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

// Listen starts the Diameter server on the configured protocol (TCP, SCTP, or TCP+TLS).
// Each incoming connection is wrapped with diam.NewConn() and handed to the
// sm.StateMachine for CER/CEA handling. After handshake, the StateMachine
// dispatches ASR/ASA/RAR/RAA/STR/STA to registered handlers.
// Spec: PHASE §2.3, §6.3; RFC 6733 §5.3 (CER/CEA — both peers MUST exchange)
func (h *DiameterHandler) Listen(ctx context.Context, addr, protocol string) error {
	switch protocol {
	case "tcp":
		return h.listenTCP(ctx, addr)
	case "tcp+tls":
		return h.listenTLS(ctx, addr)
	case "sctp":
		return h.listenSCTP(ctx, addr)
	default:
		return fmt.Errorf("unsupported diameter protocol: %s (expected tcp, tcp+tls, or sctp)", protocol)
	}
}

// listenTCP starts a plain TCP listener.
func (h *DiameterHandler) listenTCP(_ context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("diameter tcp listen: %w", err)
	}
	//nolint:contextcheck
	go h.serveTCP(listener)
	return nil
}

// listenTLS starts a TLS listener for Diameter over TCP+TLS.
// Spec: GAP-DIA-01
func (h *DiameterHandler) listenTLS(_ context.Context, addr string) error {
	if h.cfg == nil || h.cfg.TLSCert == "" || h.cfg.TLSKey == "" {
		return fmt.Errorf("TLS cert and key required for tcp+tls protocol")
	}

	cert, err := tls.LoadX509KeyPair(h.cfg.TLSCert, h.cfg.TLSKey)
	if err != nil {
		return fmt.Errorf("load TLS cert/key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	if h.cfg.TLSCACert != "" {
		caCertPEM, err := os.ReadFile(h.cfg.TLSCACert)
		if err != nil {
			return fmt.Errorf("read CA cert: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCertPEM) {
			return fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = caPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caPool
	}

	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS listen: %w", err)
	}

	h.logger.Info("Diameter TLS listener started", "addr", listener.Addr())
	//nolint:contextcheck
	go h.serveTCP(listener)
	return nil
}

// listenSCTP starts a SCTP listener, falling back to TCP if SCTP is unavailable.
func (h *DiameterHandler) listenSCTP(_ context.Context, addr string) error {
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
		// Handshake succeeded. Validate peer identity before accepting.
		if meta, ok := smpeer.FromContext(peerConn.Context()); ok {
			if err := h.validatePeer(meta); err != nil {
				h.logger.Error("peer_validation_failed", "error", err)
				peerConn.Close()
				return
			}
			h.logger.Info("Diameter peer handshake completed",
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

// validatePeer DELETED: inbound peer validation no longer needed.
// aaa-gateway never accepts inbound TCP connections — its outbound socket
// connects to the configured AAA-S, which is trusted by configuration.
// Spec: RFC 6733 §5.6, TS 29.561 Ch.17.

