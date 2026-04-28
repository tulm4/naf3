// Package gateway provides the AAA Gateway component.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	radiusAccessRequest     = 1
	radiusAccessAccept      = 2
	radiusAccessReject      = 3
	radiusAccessChallenge   = 11
	radiusCoARequest        = 43 // RFC 5176
	radiusDisconnectRequest = 40 // RFC 5176
)

// RadiusHandler handles RADIUS protocol traffic.
type RadiusHandler struct {
	logger          *slog.Logger
	tracer          trace.Tracer
	publishResponse func(sessionID string, raw []byte)
	forwardToBiz    func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
}

// Listen starts the RADIUS UDP listener.
func (h *RadiusHandler) Listen(ctx context.Context, addr string) {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		h.logger.Error("failed to listen on RADIUS UDP", "addr", addr, "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	h.logger.Info("RADIUS UDP listener started", "addr", conn.LocalAddr())

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					continue
				}
				h.logger.Error("RADIUS read error", "error", err)
				continue
			}
			h.handlePacket(ctx, conn, clientAddr, buf[:n])
		}
	}
}

// handlePacket processes an incoming RADIUS packet from AAA-S.
func (h *RadiusHandler) handlePacket(ctx context.Context, _ *net.UDPConn, addr *net.UDPAddr, raw []byte) {
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
		h.handleServerInitiated(ctx, raw, "RADIUS")
		return
	}
}

// Forward is no longer used for the client-initiated path.
// The Gateway.ForwardEAP() now calls radiusForwarder.Forward() directly.
// This method is kept for backwards compatibility with any direct callers.
func (h *RadiusHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	return nil, fmt.Errorf("radius_forward: use radiusForwarder.Forward() directly (deprecated)")
}

// handleServerInitiated handles server-initiated RADIUS packets (CoA, DM).
// It extracts the session ID, looks up the Biz Pod, and forwards the request.
func (h *RadiusHandler) handleServerInitiated(ctx context.Context, raw []byte, transport string) {
	sessionID := extractSessionID(raw)
	if sessionID == "" {
		h.logger.Warn("server_initiated_no_session_id", "transport", transport)
		return
	}

	msgType := "RAR"
	if raw[0] == radiusCoARequest {
		msgType = "COA"
	}

	h.logger.Info("server-initiated RADIUS received",
		"transport", transport,
		"session_id", sessionID,
		"message_type", msgType)

	// Create a new span representing this server-initiated RADIUS initiation.
	// RADIUS over UDP has no native tracing context, so we create a fresh span
	// here as the root of the server-initiated flow (equivalent to what
	// conn.Context() provides for Diameter). This ensures the downstream HTTP
	// call to Biz Pod, AMF notifications, and DB operations are all children
	// of this span for distributed tracing continuity.
	ctx, span := h.tracer.Start(ctx, msgType,
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
			attribute.String("transport", transport),
			attribute.String("message_type", msgType),
		))
	defer span.End()

	h.forwardToBiz(ctx, sessionID, "RADIUS", msgType, raw)
}

// extractSessionID extracts the session ID from RADIUS packet.
// It looks for the State attribute (type 24) which carries the session correlation key.
func extractSessionID(raw []byte) string {
	if len(raw) < 20 {
		return ""
	}
	// RADIUS packet structure: 20-byte header + attributes
	// State attribute: type=24, length=variable
	pos := 20
	for pos+2 <= len(raw) {
		attrType := raw[pos]
		attrLen := int(raw[pos+1])
		if attrLen < 2 || pos+attrLen > len(raw) {
			break
		}
		if attrType == 24 { // State attribute
			return string(raw[pos+2 : pos+attrLen])
		}
		pos += attrLen
	}
	return ""
}
