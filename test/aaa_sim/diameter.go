// Package aaa_sim provides a standalone AAA-S simulator for E2E testing.
package aaa_sim

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// Diameter command codes.
const (
	diameterCER = 257 // Capabilities-Exchange-Request/Answer
	diameterDEA = 268 // Device-in-Gateway-Answer (answer to DER)
	diameterDER = 268 // Device-in-Gateway-Request
)

// Diameter AVP codes.
const (
	avpResultCode        = 268
	avpAuthApplicationID = 418
	avpSessionID         = 263
	avpAuthRequestType   = 450
	avpEAPPayload        = 1265
	avpVendorSpecific    = 260
	avpOriginHost        = 264
	avpOriginRealm       = 296
	avpDestinationHost   = 293
	avpDestinationRealm  = 283
)

// Vendor IDs (3GPP).
const (
	vendor3GPP = 10415
)

// Auth-Application-Id for NASREQ.
const (
	authAppNASREQ = 256
)

// Diameter result codes.
const (
	diameterSuccess      = 2001
	diameterAuthRejected = 4003
	diameterChallenge    = 4002
)

// DiameterServer handles Diameter EAP requests.
type DiameterServer struct {
	ln     net.Listener
	mode   Mode
	logger *slog.Logger
}

// NewDiameterServer creates a Diameter server.
func NewDiameterServer(ln net.Listener, mode Mode, logger *slog.Logger) *DiameterServer {
	return &DiameterServer{
		ln:     ln,
		mode:   mode,
		logger: logger,
	}
}

// Run starts the Diameter server loop.
func (s *DiameterServer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.ln.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := s.ln.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.logger.Error("Diameter accept error", "error", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *DiameterServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 8192)

	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return
			}
			s.logger.Error("Diameter read error", "error", err)
			return
		}
		if n == 0 {
			return
		}

		msgs := parseDiameterMessages(buf[:n])
		for _, msg := range msgs {
			cmd := getDiameterCommand(msg)
			s.logger.Debug("diameter_message_received", "cmd", cmd)

			switch cmd {
			case "CER":
				resp := s.buildCEA(msg)
				conn.Write(resp)
			case "DER":
				resp := s.buildDEA(msg)
				conn.Write(resp)
			default:
				s.logger.Warn("unsupported Diameter command", "cmd", cmd)
			}
		}
	}
}

func parseDiameterMessages(data []byte) [][]byte {
	var msgs [][]byte
	pos := 0
	for pos+20 <= len(data) {
		length := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		if length < 20 || pos+length > len(data) {
			break
		}
		msgs = append(msgs, data[pos:pos+length])
		pos += length
	}
	return msgs
}

func getDiameterCommand(msg []byte) string {
	if len(msg) < 20 {
		return "UNKNOWN"
	}
	cc := binary.BigEndian.Uint32(msg[4:8])
	flags := msg[8]
	isRequest := (flags & 0x80) == 0

	switch cc {
	case diameterCER:
		if isRequest {
			return "CER"
		}
		return "CEA"
	case diameterDER:
		if isRequest {
			return "DER"
		}
		return "DEA"
	default:
		return fmt.Sprintf("CMD-%d", cc)
	}
}

func (s *DiameterServer) buildCEA(capexReq []byte) []byte {
	avps := make([]byte, 0)

	// Result-Code AVP (no vendor, code 268, 4-byte value).
	avps = append(avps, buildAVP(avpResultCode, 0, i32ToBytes(diameterSuccess))...)

	// Auth-Application-Id AVP (code 418, value 256 for NASREQ).
	avps = append(avps, buildAVP(avpAuthApplicationID, 0, i32ToBytes(authAppNASREQ))...)

	// Origin-Host and Origin-Realm from CER.
	avps = append(avps, buildAVP(avpOriginHost, 0, []byte("aaa-sim"))...)
	avps = append(avps, buildAVP(avpOriginRealm, 0, []byte("test.local"))...)

	// Product-Name.
	avps = append(avps, buildAVP(0, 0, []byte("ProductName"))...)

	return buildDiameterResponse(capexReq, diameterCER, avps)
}

func (s *DiameterServer) buildDEA(der []byte) []byte {
	// Determine result based on mode.
	var resultCode uint32
	var eapPayload []byte

	switch s.mode {
	case ModeEAP_TLS_SUCCESS:
		resultCode = diameterSuccess
		eapPayload = []byte{3, 0, 0, 4} // EAP Success
	case ModeEAP_TLS_FAILURE:
		resultCode = diameterSuccess    // We report success in DEA; EAP result in payload
		eapPayload = []byte{4, 0, 0, 4} // EAP Failure
	case ModeEAP_TLS_CHALLENGE:
		resultCode = diameterChallenge
		eapPayload = []byte{1, 13, 0, 6, 0, 0, 0, 0} // EAP Request
	}

	avps := make([]byte, 0)

	// Result-Code.
	avps = append(avps, buildAVP(avpResultCode, 0, i32ToBytes(resultCode))...)

	// Auth-Application-Id.
	avps = append(avps, buildAVP(avpAuthApplicationID, 0, i32ToBytes(authAppNASREQ))...)

	// Session-ID.
	sessionID := fmt.Sprintf("diameter-session-%d", time.Now().UnixNano())
	avps = append(avps, buildAVP(avpSessionID, 0, []byte(sessionID))...)

	// Auth-Request-Type.
	avps = append(avps, buildAVP(avpAuthRequestType, 0, i32ToBytes(1))...) // 1 = Authorize_Auth

	// EAP-Payload AVP (Vendor-Specific, 3GPP).
	if eapPayload != nil {
		avps = append(avps, buildVendorAVP(avpEAPPayload, vendor3GPP, eapPayload)...)
	}

	return buildDiameterResponse(der, diameterDER, avps)
}

func buildDiameterResponse(req []byte, cmdCode uint32, avps []byte) []byte {
	// Build header (20 bytes): version(1) + length(3) + command(4) + flags(1) + app-id(4) + hbh(4) + ete(4)
	length := 20 + len(avps)
	packet := make([]byte, length)

	packet[0] = 1 // version
	packet[1] = byte(length >> 16)
	packet[2] = byte(length >> 8)
	packet[3] = byte(length)
	binary.BigEndian.PutUint32(packet[4:8], cmdCode)
	packet[8] = 0x40                                         // Response flag set
	binary.BigEndian.PutUint32(packet[12:16], authAppNASREQ) // Auth-Application-Id
	// Copy hop-by-hop from request.
	if len(req) >= 24 {
		copy(packet[16:20], req[16:20])
	}
	// Copy AVPs.
	copy(packet[20:], avps)

	return packet
}

// buildAVP builds a basic AVP (no vendor flag).
// Format: code(4) + flags(1) + length(3) + value(variable, padded to 4-byte boundary)
func buildAVP(code uint32, flags uint8, value []byte) []byte {
	valueLen := len(value)
	avpLen := 8 + valueLen
	// Pad to 4-byte boundary.
	if rem := avpLen % 4; rem != 0 {
		avpLen += 4 - rem
	}

	avp := make([]byte, avpLen)
	binary.BigEndian.PutUint32(avp[0:4], code)
	avp[4] = flags
	avp[5] = byte(valueLen >> 16)
	avp[6] = byte(valueLen >> 8)
	avp[7] = byte(valueLen)
	copy(avp[8:], value)
	return avp
}

// buildVendorAVP builds a vendor-specific AVP (V flag = 0x80).
func buildVendorAVP(code uint32, vendorID uint32, value []byte) []byte {
	valueLen := len(value)
	avpLen := 12 + valueLen
	if rem := avpLen % 4; rem != 0 {
		avpLen += 4 - rem
	}

	avp := make([]byte, avpLen)
	binary.BigEndian.PutUint32(avp[0:4], code)
	avp[4] = 0x80 // V flag
	avp[5] = byte(valueLen >> 16)
	avp[6] = byte(valueLen >> 8)
	avp[7] = byte(valueLen)
	binary.BigEndian.PutUint32(avp[8:12], vendorID)
	copy(avp[12:], value)
	return avp
}

func i32ToBytes(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}
