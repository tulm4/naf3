// Package gateway provides the AAA Gateway component.
package gateway

import (
	"context"
	"log/slog"
	"net"
	"time"
)

const (
	radiusAccessRequest      = 1
	radiusAccessAccept      = 2
	radiusAccessReject      = 3
	radiusAccessChallenge   = 11
	radiusCoARequest        = 43 // RFC 5176
	radiusDisconnectRequest = 40 // RFC 5176
)

// RadiusHandler handles RADIUS protocol traffic.
type RadiusHandler struct {
	logger          *slog.Logger
	publishResponse func(sessionID string, raw []byte)
}

// Listen starts the RADIUS UDP listener.
func (h *RadiusHandler) Listen(ctx context.Context, addr string) {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		h.logger.Error("failed to listen on RADIUS UDP", "addr", addr, "error", err)
		return
	}
	defer conn.Close()

	h.logger.Info("RADIUS UDP listener started", "addr", conn.LocalAddr())

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				h.logger.Error("RADIUS read error", "error", err)
				continue
			}
			h.handlePacket(conn, clientAddr, buf[:n])
		}
	}
}

// handlePacket processes an incoming RADIUS packet from AAA-S.
func (h *RadiusHandler) handlePacket(conn *net.UDPConn, addr *net.UDPAddr, raw []byte) {
	if len(raw) < 4 {
		h.logger.Warn("radius_packet_too_short", "len", len(raw))
		return
	}

	msgType := raw[0]

	// Client-initiated: AAA-S responding to our Access-Request
	if msgType == radiusAccessAccept || msgType == radiusAccessReject || msgType == radiusAccessChallenge {
		sessionID := extractSessionID(raw)
		h.publishResponse(sessionID, raw)
		return
	}

	// Server-initiated: CoA or DM from AAA-S
	if msgType == radiusCoARequest || msgType == radiusDisconnectRequest {
		h.handleServerInitiated(raw, "RADIUS")
		return
	}
}

// Forward sends an EAP payload to AAA-S and returns the response.
// This is a stub — the actual implementation forwards to AAA-S.
func (h *RadiusHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement actual RADIUS forwarding to AAA-S server
	// For now, return a placeholder RAR-Nak response
	return []byte{2, 0, 0, 12}, nil
}

// handleServerInitiated handles server-initiated RADIUS packets (CoA, DM).
func (h *RadiusHandler) handleServerInitiated(raw []byte, transport string) {
	// TODO: Implement server-initiated flow
	// - Parse session from RADIUS State attribute
	// - Look up session correlation in Redis
	// - Forward to Biz Pod via HTTP POST /aaa/server-initiated
	h.logger.Info("server-initiated RADIUS received", "transport", transport)
}

// extractSessionID extracts the session ID from RADIUS packet.
func extractSessionID(raw []byte) string {
	// Extract from RADIUS State attribute (attribute type 24)
	// For now, return empty string as a placeholder
	return ""
}
