// Package gateway provides the AAA Gateway component.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
)

// DiameterHandler handles Diameter protocol traffic.
type DiameterHandler struct {
	logger          *slog.Logger
	publishResponse func(sessionID string, raw []byte)
}

// Listen starts the Diameter server on the configured protocol (TCP or SCTP).
// Spec: PHASE §2.3, §6.3; RFC 6733 App H (SCTP considerations)
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

// serveTCP handles incoming Diameter TCP connections.
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

// serveSCTP handles incoming Diameter SCTP connections.
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

// HandleConnection processes an incoming Diameter connection from AAA-S.
func (h *DiameterHandler) HandleConnection(conn net.Conn) {
	defer conn.Close()
	// TODO: Use go-diameter/v4 to decode message header and determine message type.
	// Command Code 280 = Experimental-Result (used for DER/DEA)
	// Command Code 274 = Abort-Session-Request (ASR) / Abort-Session-Answer (ASA)
	h.logger.Info("Diameter connection received", "remote", conn.RemoteAddr())
}

// Forward sends a Diameter message to AAA-S and returns the response.
// This is a stub — the actual implementation forwards to AAA-S.
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement actual Diameter forwarding to AAA-S server
	// For now, return a placeholder ASA response
	return []byte{}, nil
}
