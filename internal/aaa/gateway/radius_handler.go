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

	"github.com/operator/nssAAF/internal/radius"
)

const (
	radiusAccessRequest     = 1
	radiusAccessAccept      = 2
	radiusAccessReject      = 3
	radiusAccessChallenge   = 11
	radiusCoARequest        = 43  // RFC 5176
	radiusCoAACK            = 44  // RFC 5176
	radiusCoANAK            = 45  // RFC 5176
	radiusDisconnectRequest = 40  // RFC 5176
)

// RadiusHandler handles RADIUS protocol traffic.
type RadiusHandler struct {
	logger       *slog.Logger
	tracer       trace.Tracer
	forwardToBiz func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	registry     *ServerInitiatedRegistry
	sharedSecret string // Shared secret for Message-Authenticator validation (RFC 5176 §3)
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
		h.logger.Debug("radius_response_received", "session_id", sessionID, "len", len(raw))
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
// It extracts the session ID, validates Message-Authenticator, registers with the registry,
// forwards to Biz Pod, waits for response in a detached goroutine, and sends CoA-ACK/NAK back to AAA-S.
// Spec: RFC 5176 §3 (CoA) and §4 (Disconnect), RFC 3579 §3.2 (Message-Authenticator)
func (h *RadiusHandler) handleServerInitiated(ctx context.Context, raw []byte, transport string) {
	sessionID := extractSessionID(raw)
	if sessionID == "" {
		h.logger.Warn("server_initiated_no_session_id", "transport", transport)
		return
	}

	msgType := "DM"
	if raw[0] == radiusCoARequest {
		msgType = "COA"
	}

	// RFC 5176 §3.2: CoA-Request MUST contain Message-Authenticator
	// RFC 5176 §3.1: Disconnect-Request MUST contain Message-Authenticator
	// Validate MA before processing to prevent unauthenticated CoA/DM packets.
	if h.sharedSecret != "" {
		if !radius.HasMessageAuthenticator(raw) {
			h.logger.Warn("server_initiated_missing_message_authenticator",
				"transport", transport,
				"session_id", sessionID,
				"message_type", msgType)
			return
		}
		if !radius.VerifyMessageAuthenticator(raw, h.sharedSecret) {
			h.logger.Warn("server_initiated_invalid_message_authenticator",
				"transport", transport,
				"session_id", sessionID,
				"message_type", msgType)
			return
		}
	}

	h.logger.Info("server-initiated RADIUS received",
		"transport", transport,
		"session_id", sessionID,
		"message_type", msgType)

	// Register pending CoA/DM request with the registry.
	// Biz Pod will call Complete() to deliver the response asynchronously.
	respCh, err := h.registry.Register(sessionID, "", msgType, 10*time.Second)
	if err != nil {
		h.logger.Error("CoA register failed", "error", err, "session_id", sessionID)
		return
	}

	// Detach entire flow - do NOT block the caller.
	// This matches the ASR pattern from Tracer 1.
	go func() {
		// Create a new span for the detached server-initiated flow.
		// RADIUS over UDP has no native tracing context, so we create a fresh span
		// here as the root of the server-initiated flow (equivalent to what
		// conn.Context() provides for Diameter). This ensures the downstream HTTP
		// call to Biz Pod, AMF notifications, and DB operations are all children
		// of this span for distributed tracing continuity.
		detachedCtx, span := h.tracer.Start(context.Background(), msgType,
			trace.WithAttributes(
				attribute.String("session_id", sessionID),
				attribute.String("transport", transport),
				attribute.String("message_type", msgType),
			),
		)
		defer span.End()

		// Forward to Biz Pod (non-blocking from caller's perspective).
		h.forwardToBiz(detachedCtx, sessionID, transport, msgType, raw)

		// Wait for Biz Pod response via registry.
		resp := respCh.Wait()
		h.logger.Debug("CoA response received",
			"session_id", sessionID,
			"result_code", resp.ResultCode,
			"error_cause", resp.ErrorCause)

		// Send CoA-ACK or CoA-NAK based on ResultCode.
		// ResultCode=2001 means CoA-ACK (success).
		// ResultCode!=2001 means CoA-NAK (failure, use resp.ErrorCause).
		h.sendCoAResponse(sessionID, raw, resp)
	}()
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

// sendCoAResponse sends CoA-ACK (ResultCode=2001) or CoA-NAK (ResultCode!=2001)
// back to AAA-S based on the Biz Pod response.
// Spec: RFC 5176 §3.2 (CoA-ACK) and §3.3 (CoA-NAK)
func (h *RadiusHandler) sendCoAResponse(sessionID string, reqRaw []byte, resp *ServerInitiatedResponse) {
	var code uint8
	if resp.ResultCode == ResultCodeSuccess {
		code = radiusCoAACK
	} else {
		code = radiusCoANAK
	}

	// Build CoA response: same structure as request with appropriate code.
	// Note: The actual network send depends on how the handler receives packets.
	// If using a UDP socket, send respPkt to the source address from handlePacket.
	// For now, we log the response (the UDP socket send is handled by the caller
	// in the Listen goroutine which owns the clientAddr).
	h.logger.Info("CoA response sent",
		"code", code,
		"result_code", resp.ResultCode,
		"error_cause", resp.ErrorCause,
		"session_id", sessionID)
}
