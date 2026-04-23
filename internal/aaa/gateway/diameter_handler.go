// Package gateway provides the AAA Gateway component.
package gateway

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// DiameterHandler handles Diameter protocol traffic.
type DiameterHandler struct {
	logger          *slog.Logger
	publishResponse func(sessionID string, raw []byte)
	forwardToBiz   func(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte)
	version         string
	bizURL         string
	httpClient     *http.Client
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
// It reads messages, determines the type, and routes to the appropriate handler.
// Spec: RFC 6733 App H (SCTP), RFC 4072 (Diameter EAP)
// Command Code 268 = DER/DEA (distinguished by R-bit)
// Command Code 274 = Abort-Session-Request (ASR) / Abort-Session-Answer (ASA)
// Route based on Command Code.
// Note: go-diameter/v4/sm is used for CER/CEA handshake (RFC 6733 §5.3: both peers
// MUST exchange). After handshake, manual header parsing is sufficient for ASR/DEA.
func (h *DiameterHandler) HandleConnection(conn net.Conn) {
	defer conn.Close()
	h.logger.Info("Diameter connection received", "remote", conn.RemoteAddr())

	for {
		// Diameter messages have a 20-byte header:
		// version(1) + flags(1) + command-code(3) + application-id(4) + hop-by-hop(4) + end-to-end(4) + length(3)
		header := make([]byte, 20)
		if _, err := io.ReadFull(conn, header); err != nil {
			if err != io.EOF {
				h.logger.Error("Diameter read error", "error", err)
			}
			return
		}

		// Parse header fields
		version := header[0]
		if version != 1 {
			h.logger.Warn("unsupported Diameter version", "version", version)
			return
		}

		commandCode := binary.BigEndian.Uint32(header[1:4]) >> 8 // top 3 bytes
		_length := binary.BigEndian.Uint32([]byte{0, header[16], header[17], header[18]})
		length := int(_length)

		if length < 20 {
			h.logger.Warn("Diameter message too short", "length", length)
			return
		}

		// Read remaining bytes
		remaining := length - 20
		if remaining > 0 {
			msg := make([]byte, remaining)
			if _, err := io.ReadFull(conn, msg); err != nil {
				h.logger.Error("Diameter read body error", "error", err)
				return
			}
			header = append(header, msg...)
		}

		// Route based on Command Code
		// 268 = DER/DEA (RFC 4072 — distinguished by R-bit in header flags)
		// 274 = ASR (Abort-Session-Request — server-initiated)
		// 280 = DWR/DWA (Device-Watchdog — RFC 6733 §5.5, App Id = 0)
		switch commandCode {
		case 268:
			// Client-initiated: response to our DER
			sessionID := extractDiameterSessionID(header)
			h.publishResponse(sessionID, header)
		case 274:
			// Server-initiated: ASR from AAA-S
			h.handleServerInitiated(header)
		case 280:
			// DWR (R-bit set) or DWA (R-bit cleared) — RFC 6733 §5.5
			h.handleWatchdog(conn, header)
		default:
			h.logger.Debug("Diameter unhandled command code",
				"command_code", commandCode,
				"length", length)
		}
	}
}

// handleWatchdog handles DWR/DWA messages per RFC 6733 §5.5.
// DWR is sent when no traffic exchanged; the peer MUST respond with DWA.
func (h *DiameterHandler) handleWatchdog(conn net.Conn, raw []byte) {
	// Check R-bit in header flags to distinguish DWR from DWA.
	// DWR: R-bit = 1 (is a request). DWA: R-bit = 0 (is an answer).
	rBitSet := (raw[2] & 0x80) != 0

	if rBitSet {
		// Received DWR — MUST respond with DWA per RFC 6733 §5.6 state machine.
		// "R-Rcv-DWR → Process-DWR, R-Snd-DWA"
		h.logger.Debug("Diameter DWR received, sending DWA")
		if err := h.sendDWA(conn, raw); err != nil {
			h.logger.Error("failed to send DWA", "error", err)
		}
	} else {
		// Received DWA — client-initiated path handles this via pending map
		// (go-diameter sm.Client processes it automatically).
		h.logger.Debug("Diameter DWA received (server-initiated path)")
	}
}

// sendDWA constructs and sends a Device-Watchdog-Answer in response to DWR.
// Spec: RFC 6733 §5.5.2
// Message format: Diameter Header (280, R-bit cleared) + Result-Code + Origin-Host + Origin-Realm
func (h *DiameterHandler) sendDWA(conn net.Conn, dwr []byte) error {
	// Extract Origin-Host and Origin-Realm from DWR to echo back in DWA.
	var originHost, originRealm []byte
	pos := 20
	for pos+8 <= len(dwr) {
		avpCode := binary.BigEndian.Uint32([]byte{0, dwr[pos], dwr[pos+1], dwr[pos+2]})
		avpLen := int(binary.BigEndian.Uint32([]byte{0, dwr[pos+4], dwr[pos+5], dwr[pos+6]}))
		if avpLen < 8 || pos+avpLen > len(dwr) {
			break
		}
		// AVP flags byte is at dwr[pos+3]
		vendorFlag := (dwr[pos+3] & 0x80) != 0
		vendorID := uint32(0)
		dataStart := 8
		if vendorFlag {
			vendorID = binary.BigEndian.Uint32([]byte{0, dwr[pos+8], dwr[pos+9], dwr[pos+10], dwr[pos+11]})
			dataStart = 12
		}
		_ = vendorID

		dataLen := avpLen - dataStart
		value := dwr[pos+dataStart : pos+avpLen]
		if avpCode == 264 && !vendorFlag { // Origin-Host (code 264, no vendor)
			originHost = make([]byte, dataLen)
			copy(originHost, value)
		} else if avpCode == 296 && !vendorFlag { // Origin-Realm (code 296, no vendor)
			originRealm = make([]byte, dataLen)
			copy(originRealm, value)
		}
		pos += avpLen
	}

	// Build DWA: 20-byte header + Result-Code(1) + Origin-Host(264) + Origin-Realm(296) + Origin-State-Id(278)
	// Result-Code = DIAMETER_OK (1)
	resultCode := []byte{0, 0, 0, 1}
	originStateID := []byte{0, 0, 0, 1} // Origin-State-Id = 1

	// Calculate total length
	totalLen := 20 + 8+4 + 8+len(originHost) + 8+len(originRealm) + 8+4
	// Pad to 8-byte boundary
	pad := (8 - (totalLen % 8)) % 8
	totalLen += pad

	// Build message
	buf := make([]byte, 20)
	buf[0] = 1                                          // version
	buf[2] = 0x80 | 0                                  // flags: R-bit cleared, P=0, E=0, T=0
	buf[3] = 0                                          // reserved
	binary.BigEndian.PutUint32(buf[4:8], 280)           // command code 280
	binary.BigEndian.PutUint32(buf[8:12], 0)            // App Id = 0 (base protocol)
	// hop-by-hop: echo from DWR header[12:16]
	copy(buf[12:16], dwr[12:16])
	// end-to-end: generate new
	endToEnd := uint32(time.Now().Unix()) & 0xFFFF
	binary.BigEndian.PutUint32(buf[16:20], endToEnd)
	// length in bytes 16-18 (3 bytes, little-endian of 20-bit value)
	buf[16] = byte(totalLen >> 16)
	buf[17] = byte(totalLen >> 8)
	buf[18] = byte(totalLen)

	avpOffset := 20

	// Result-Code AVP (code 296, M-bit set, vendor 0)
	avpLen := 8 + 4
	buf = append(buf, 0, 0, 1, 0x40) // code 296, M-bit, vendor 0
	buf = append(buf, 0, 0, 0, byte(avpLen))
	buf = append(buf, resultCode...)
	avpOffset += avpLen

	// Origin-Host AVP (code 264, M-bit set, vendor 0)
	avpLen = 8 + len(originHost)
	buf = append(buf, 0, 0, 1, 0x08) // code 264, M-bit, vendor 0
	buf = append(buf, 0, 0, 0, byte(avpLen))
	buf = append(buf, originHost...)
	avpOffset += avpLen

	// Origin-Realm AVP (code 296, M-bit set, vendor 0)
	avpLen = 8 + len(originRealm)
	buf = append(buf, 0, 0, 1, 0x28) // code 296, M-bit, vendor 0
	buf = append(buf, 0, 0, 0, byte(avpLen))
	buf = append(buf, originRealm...)
	avpOffset += avpLen

	// Origin-State-Id AVP (code 278, M-bit set, vendor 0)
	avpLen = 8 + 4
	buf = append(buf, 0, 0, 1, 0x16) // code 278, M-bit, vendor 0
	buf = append(buf, 0, 0, 0, byte(avpLen))
	buf = append(buf, originStateID...)
	avpOffset += avpLen

	// Pad
	buf = append(buf, make([]byte, pad)...)

	_, err := conn.Write(buf)
	return err
}

// handleServerInitiated handles ASR (Abort-Session-Request) from AAA-S.
func (h *DiameterHandler) handleServerInitiated(raw []byte) {
	sessionID := extractDiameterSessionID(raw)
	if sessionID == "" {
		h.logger.Warn("ASR: no session ID found")
		return
	}

	h.logger.Info("Diameter ASR received",
		"session_id", sessionID)

	h.forwardToBiz(context.Background(), sessionID, "DIAMETER", "ASR", raw)
}

// Forward sends a Diameter-EAP-Request to AAA-S and returns the DEA response.
// Spec: PHASE §2.3.5; RFC 6733 §2.1 (CER/CEA), RFC 4072 (Diameter EAP DER/DEA)
// NOTE: This is a STUB. The actual implementation requires diameter_forward.go
// which maintains a persistent TCP/SCTP connection with CER/CEA handshake and DWR/DWA
// watchdog. See PLAN §2.3.5 for the full design. Until implemented, DIAMETER
// transport silently fails — every DER gets an empty response.
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement diameter_forward.go per PLAN §2.3.5
	// - Connect to AAA-S, perform CER/CEA (go-diameter/v4 sm.Client)
	// - Build DER from EAP payload (Session-Id, Auth-Application-Id=5, EAP-Payload AVP=209)
	// - Register hop-by-hop ID → pending channel, send, wait for DEA
	// - DWR watchdog and reconnect on failure
	return []byte{}, fmt.Errorf("diameter_forward: not implemented (see PLAN §2.3.5)")
}

// extractDiameterSessionID extracts the Session-Id AVP from a Diameter message.
// The Session-Id AVP is in the message body after the 20-byte header.
func extractDiameterSessionID(raw []byte) string {
	if len(raw) < 24 {
		return ""
	}
	// Session-Id AVP: AVP code 263, flags, length (4 bytes), followed by UTF8String value
	pos := 20
	for pos+8 <= len(raw) {
		avpCode := binary.BigEndian.Uint32([]byte{0, raw[pos], raw[pos+1], raw[pos+2]})
		avpLen := int(binary.BigEndian.Uint32([]byte{0, raw[pos+4], raw[pos+5], raw[pos+6]}))
		if avpLen < 8 || pos+avpLen > len(raw) {
			break
		}
		if avpCode == 263 { // Session-Id AVP
			// Value starts at pos+8 (after code[4] + flags[1] + length[3])
			return string(raw[pos+8 : pos+avpLen])
		}
		pos += avpLen
	}
	return ""
}
