// Package main provides a mock AAA-S (Authentication, Authorization, Accounting Server)
// for local development and testing. It simulates a 3GPP NSS-AAA Server by listening on
// RADIUS (UDP/1812) and Diameter (TCP/3868) and returning hardcoded EAP responses.
//
// This mock is stateless — it does not persist sessions. It is suitable for integration
// testing of the NSSAAF Biz Pod ↔ AAA Gateway ↔ AAA-S flow.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"time"
)

// RADIUS constants (RFC 2865, 2866).
const (
	radiusAccessRequest     uint8 = 1
	radiusAccessAccept      uint8 = 2
	radiusAccessReject      uint8 = 3
	radiusAccessChallenge   uint8 = 11
	radiusCoARequest        uint8 = 43
	radiusDisconnectRequest uint8 = 40

	attrUserName    uint8 = 1
	attrEAPMessage  uint8 = 79
	attrState       uint8 = 24
	attrMessageAuth uint8 = 80
)

// RADIUS shared secret used for message authenticator (mock only).
var radiusSecret = []byte("testing123")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start RADIUS UDP server on :1812
	go runRadiusServer(ctx, ":1812", logger)

	// Start Diameter TCP server on :3868
	go runDiameterServer(ctx, ":3868", logger)

	slog.Info("mock-aaa-s started", "radius", ":1812", "diameter", ":3868")

	<-ctx.Done()
}

// ─── RADIUS ──────────────────────────────────────────────────────────────────

// runRadiusServer listens for RADIUS Access-Request packets and responds with
// hardcoded EAP-Challenge or EAP-Accept/Reject responses.
func runRadiusServer(ctx context.Context, addr string, logger *slog.Logger) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		logger.Error("failed to listen RADIUS", "addr", addr, "error", err)
		return
	}
	defer pc.Close()

	buf := make([]byte, 4096)
	logger.Info("RADIUS UDP listener started", "addr", addr)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			pc.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, clientAddr, err := pc.ReadFrom(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				logger.Error("RADIUS read error", "error", err)
				continue
			}
			go handleRadiusPacket(pc, clientAddr, buf[:n], logger)
		}
	}
}

// handleRadiusPacket processes a single RADIUS packet.
func handleRadiusPacket(pc net.PacketConn, clientAddr net.Addr, raw []byte, logger *slog.Logger) {
	if len(raw) < 20 {
		logger.Warn("radius_packet_too_short", "len", len(raw))
		return
	}

	code := raw[0]
	replyCode := radiusAccessAccept

	switch code {
	case radiusAccessRequest:
		logger.Debug("radius_access_request_received", "from", clientAddr)
		// Respond with Access-Challenge, then Access-Accept on next request.
		replyCode = radiusAccessChallenge
	case radiusAccessChallenge:
		replyCode = radiusAccessAccept
	case radiusAccessReject:
		return
	default:
		logger.Warn("radius_unknown_code", "code", code)
		return
	}

	sessionID := fmt.Sprintf("mock-session-%d", rand.Int63())
	resp := buildRadiusResponse(raw, replyCode, sessionID)

	_, err := pc.WriteTo(resp, clientAddr)
	if err != nil {
		logger.Error("failed to send RADIUS response", "error", err)
	}
}

// buildRadiusResponse constructs a RADIUS response packet from an Access-Request.
// It copies the Request Authenticator from the original and builds an appropriate
// Access-Accept, Access-Reject, or Access-Challenge response.
func buildRadiusResponse(req []byte, replyCode uint8, sessionID string) []byte {
	if len(req) < 20 {
		return nil
	}

	// RADIUS header: code(1) + id(1) + length(2) + authenticator(16)
	// Response authenticator = MD5(code+id+length+request+attributes+secret)
	// For simplicity, we use the same authenticator (mock only).
	respAuth := make([]byte, 16)
	copy(respAuth, req[4:20])

	// Build attributes
	var attrs []byte

	// State attribute (type 24)
	stateAttr := []byte{attrState, byte(2 + len(sessionID))}
	stateAttr = append(stateAttr, sessionID...)
	attrs = append(attrs, stateAttr...)

	// EAP-Message attribute (type 79) — hardcoded challenge payload
	eapPayload := buildMockEAPPayload(replyCode)
	eapAttr := []byte{attrEAPMessage, byte(2 + len(eapPayload))}
	eapAttr = append(eapAttr, eapPayload...)
	attrs = append(attrs, eapAttr...)

	// Build header
	totalLen := 20 + len(attrs)
	resp := make([]byte, totalLen)
	resp[0] = replyCode
	resp[1] = req[1] // same ID
	binary.BigEndian.PutUint16(resp[2:4], uint16(totalLen))
	copy(resp[4:20], respAuth)
	copy(resp[20:], attrs)

	return resp
}

// buildMockEAPPayload returns a minimal EAP payload for the given RADIUS reply code.
func buildMockEAPPayload(radiusCode uint8) []byte {
	switch radiusCode {
	case radiusAccessChallenge:
		// EAP Request: EAP-TLS Challenge (code=1, type=13 for EAP-TLS)
		return []byte{1, 13, 0, 6, 0, 0, 0, 0}
	case radiusAccessAccept:
		// EAP Success (code=3, id=0, len=4)
		return []byte{3, 0, 0, 4}
	default:
		// EAP Failure (code=4, id=0, len=4)
		return []byte{4, 0, 0, 4}
	}
}

// ─── Diameter ────────────────────────────────────────────────────────────────

// runDiameterServer listens for Diameter TCP connections and handles CER/CEA
// (capabilities exchange) and DER/DEA (device-in-Gateway-request/answer) exchanges.
// It implements a minimal subset of RFC 6733 sufficient for NSSAAF testing.
func runDiameterServer(ctx context.Context, addr string, logger *slog.Logger) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen Diameter", "addr", addr, "error", err)
		return
	}
	defer ln.Close()

	logger.Info("Diameter TCP listener started", "addr", addr)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			ln.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
			conn, err := ln.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				logger.Error("Diameter accept error", "error", err)
				continue
			}
			go handleDiameterConnection(conn, logger)
		}
	}
}

// handleDiameterConnection processes a single Diameter TCP connection.
// It expects CER → CEA, then DER → DEA exchanges.
func handleDiameterConnection(conn net.Conn, logger *slog.Logger) {
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return
			}
			logger.Error("Diameter read error", "error", err)
			return
		}
		if n == 0 {
			return
		}

		msgs, err := parseDiameterAVPs(buf[:n])
		if err != nil {
			logger.Warn("failed to parse Diameter message", "error", err)
			continue
		}

		for _, msg := range msgs {
			cmd := getDiameterCommand(msg)
			logger.Debug("diameter_message_received", "cmd", cmd)

			switch cmd {
			case "CER":
				resp := buildDiameterCEA(msg)
				conn.Write(resp)
			case "DER":
				resp := buildDiameterDEA(msg)
				conn.Write(resp)
			default:
				logger.Warn("unsupported Diameter command", "cmd", cmd)
			}
		}
	}
}

// parseDiameterAVPs splits a byte slice into individual Diameter messages.
// Diameter messages are TLV-encoded; each message starts with a 20-byte header.
func parseDiameterAVPs(data []byte) ([][]byte, error) {
	var msgs [][]byte
	pos := 0
	for pos+20 <= len(data) {
		// Read message length from bytes 5-8 (version=1 byte, then 3-byte length)
		length := int(data[pos+5])<<16 | int(data[pos+6])<<8 | int(data[pos+7])
		if length < 20 || pos+length > len(data) {
			break
		}
		msgs = append(msgs, data[pos:pos+length])
		pos += length
	}
	return msgs, nil
}

// getDiameterCommand extracts the command name from a Diameter message header.
func getDiameterCommand(msg []byte) string {
	if len(msg) < 20 {
		return "UNKNOWN"
	}
	// Bytes 12-15: Command-Code (4 bytes, network order)
	// CER = 257, CEA = 257, DER = 268, DEA = 268
	cc := binary.BigEndian.Uint32(msg[12:16])
	flags := msg[16]
	isRequest := (flags & 0x80) == 0

	switch cc {
	case 257:
		if isRequest {
			return "CER"
		}
		return "CEA"
	case 268:
		if isRequest {
			return "DER"
		}
		return "DEA"
	default:
		return fmt.Sprintf("CMD-%d", cc)
	}
}

// buildDiameterCEA builds a Capabilities-Exchange-Answer message.
func buildDiameterCEA(capexReq []byte) []byte {
	// CEA header: version(1) + message length(3) + command code(4) + flags(1) +
	// application ID(4) + hop-by-hop(4) + end-to-end(4)
	// For mock, return minimal valid CEA.
	cea := make([]byte, 20)
	cea[0] = 1 // version = 1
	cea[5] = 0
	cea[6] = 0
	cea[7] = 20                                 // length = 20 (no AVPs)
	binary.BigEndian.PutUint32(cea[12:16], 257) // command code = CER
	cea[16] = 0x40                              // response flag set
	binary.BigEndian.PutUint32(cea[20:24], 0)   // result code = DIAMETER_SUCCESS
	return cea
}

// buildDiameterDEA builds a Device-in-Gateway-Answer message responding to a DER.
func buildDiameterDEA(der []byte) []byte {
	// For a minimal DEA, return a success response with hardcoded EAP-Payload AVP.
	// Real DEA would include: Result-Code, Session-ID, Auth-Application-Id, EAP-Payload.
	eapPayload := []byte{3, 0, 0, 4} // EAP Success

	// Build AVPs
	var avps []byte

	// Result-Code AVP (Vendor-Id=10415, code=268)
	resultCode := buildDiameterAVP(10415, 268, []byte{0, 0, 0, 0}) // DIAMETER_SUCCESS
	avps = append(avps, resultCode...)

	// Auth-Application-Id AVP (code=418, value=16777264 for NASREQ)
	appID := buildDiameterAVP(0, 418, []byte{0, 0, 1, 0}) // 256
	avps = append(avps, appID...)

	// EAP-Payload AVP (Vendor-Id=10415, code=1265)
	eapAvp := buildDiameterAVPVendor(10415, 1265, eapPayload)
	avps = append(avps, eapAvp...)

	// Build DEA header (reuse hop-by-hop and end-to-end from DER if present)
	dea := make([]byte, 20)
	dea[0] = 1 // version
	length := 20 + len(avps)
	dea[5] = byte(length >> 16)
	dea[6] = byte(length >> 8)
	dea[7] = byte(length)
	binary.BigEndian.PutUint32(dea[12:16], 268) // command code = DER
	dea[16] = 0x40                              // response flag
	binary.BigEndian.PutUint32(dea[20:24], 0)   // result code = 0

	// Copy hop-by-hop from DER
	if len(der) >= 24 {
		copy(dea[20:24], der[20:24])
	}
	copy(dea[24:], avps)
	return dea
}

// buildDiameterAVP builds a basic AVP (no vendor ID, 8-byte header: code(4)+flags(1)+len(3)+value).
func buildDiameterAVP(vendorID uint32, code uint32, value []byte) []byte {
	avpLen := 8 + len(value)
	// Pad to 4-byte boundary
	if avpLen%4 != 0 {
		avpLen += 4 - (avpLen % 4)
	}

	avp := make([]byte, avpLen)
	binary.BigEndian.PutUint32(avp[0:4], code)
	avp[4] = 0 // flags
	if vendorID != 0 {
		avp[4] = 0x80 // V flag
	}
	binary.BigEndian.PutUint32(avp[5:8], uint32(8+len(value))) // AVP length (no padding in len field)
	copy(avp[8:], value)
	return avp
}

// buildDiameterAVPVendor builds a vendor-specific AVP (V flag set, 12-byte header).
func buildDiameterAVPVendor(vendorID, code uint32, value []byte) []byte {
	avpLen := 12 + len(value)
	if avpLen%4 != 0 {
		avpLen += 4 - (avpLen % 4)
	}

	avp := make([]byte, avpLen)
	binary.BigEndian.PutUint32(avp[0:4], code)
	avp[4] = 0x80 // V flag
	binary.BigEndian.PutUint32(avp[5:8], uint32(12+len(value)))
	binary.BigEndian.PutUint32(avp[8:12], vendorID)
	copy(avp[12:], value)
	return avp
}
