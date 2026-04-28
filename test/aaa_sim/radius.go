// Package aaa_sim provides RADIUS EAP handling for the AAA-S simulator.
package aaa_sim

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"time"
)

// RADIUS codes. RFC 2865.
const (
	radiusAccessRequest   = 1
	radiusAccessAccept    = 2
	radiusAccessReject    = 3
	radiusAccessChallenge = 11
)

// RADIUS attribute types. RFC 2865, RFC 3579.
const (
	attrUserName         = 1
	attrUserPassword     = 2
	attrNASIPAddress     = 4
	attrNASPort          = 5
	attrServiceType      = 6
	attrReplyMessage     = 18
	attrState            = 24
	attrClass            = 25
	attrVendorSpecific   = 26
	attrEAPMessage       = 79
	attrMessageAuth      = 80
	attrCallingStationID = 31
	attrNASIdentifier    = 32
)

// ServiceType values.
const (
	serviceTypeAuthenticateOnly = 8
	nasPortTypeVirtual          = 5
)

// RadiusServer handles RADIUS EAP requests.
type RadiusServer struct {
	ln            net.PacketConn
	mode          Mode
	sharedSecret  []byte
	logger        *slog.Logger
	seenChallenge map[string]bool
}

// NewRadiusServer creates a RADIUS server.
func NewRadiusServer(ln net.PacketConn, mode Mode, secret []byte, logger *slog.Logger) *RadiusServer {
	return &RadiusServer{
		ln:            ln,
		mode:          mode,
		sharedSecret:  secret,
		logger:        logger,
		seenChallenge: make(map[string]bool),
	}
}

// Run starts the RADIUS server loop.
func (s *RadiusServer) Run(ctx context.Context) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.ln.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, clientAddr, err := s.ln.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.logger.Error("RADIUS read error", "error", err)
			continue
		}
		go s.handlePacket(clientAddr, buf[:n])
	}
}

func (s *RadiusServer) handlePacket(clientAddr net.Addr, raw []byte) {
	if len(raw) < 20 {
		s.logger.Warn("radius_packet_too_short", "len", len(raw))
		return
	}
	code := raw[0]
	if code != radiusAccessRequest {
		return
	}

	// Validate Message-Authenticator if present.
	if hasMessageAuth(raw) {
		if !verifyMessageAuth(raw, s.sharedSecret) {
			s.logger.Warn("radius_invalid_message_auth")
			return
		}
	}

	sessionID := extractSessionID(raw)
	switch s.mode {
	case ModeEAP_TLS_SUCCESS:
		resp := s.buildResponse(raw, radiusAccessAccept, sessionID)
		s.sendResponse(clientAddr, resp)
	case ModeEAP_TLS_FAILURE:
		resp := s.buildResponse(raw, radiusAccessReject, sessionID)
		s.sendResponse(clientAddr, resp)
	case ModeEAP_TLS_CHALLENGE:
		if s.seenChallenge[sessionID] {
			resp := s.buildResponse(raw, radiusAccessAccept, sessionID)
			delete(s.seenChallenge, sessionID)
			s.sendResponse(clientAddr, resp)
		} else {
			resp := s.buildChallengeResponse(raw, sessionID)
			s.seenChallenge[sessionID] = true
			s.sendResponse(clientAddr, resp)
		}
	}
}

func (s *RadiusServer) sendResponse(clientAddr net.Addr, resp []byte) {
	_, err := s.ln.WriteTo(resp, clientAddr)
	if err != nil {
		s.logger.Error("failed to send RADIUS response", "error", err)
	}
}

func (s *RadiusServer) buildResponse(req []byte, replyCode uint8, sessionID string) []byte {
	var eapPayload []byte
	switch replyCode {
	case radiusAccessAccept:
		eapPayload = []byte{3, 0, 0, 4} // EAP Success
	case radiusAccessReject:
		eapPayload = []byte{4, 0, 0, 4} // EAP Failure
	default:
		eapPayload = []byte{1, 13, 0, 6, 0, 0, 0, 0} // EAP Request
	}

	attrs := buildEAPAttr(eapPayload)
	if sessionID != "" {
		attrs = append(attrs, buildStateAttr(sessionID)...)
	}

	return s.buildRadiusPacket(req, replyCode, attrs)
}

func (s *RadiusServer) buildChallengeResponse(req []byte, sessionID string) []byte {
	eapPayload := []byte{1, 13, 0, 6, 0, 0, 0, 0} // EAP Request (TLS)
	if sessionID == "" {
		sessionID = fmt.Sprintf("challenge-%d", rand.Int63())
	}

	attrs := buildEAPAttr(eapPayload)
	attrs = append(attrs, buildStateAttr(sessionID)...)

	replyMsg := "EAP-TLS Challenge"
	replyAttr := []byte{attrReplyMessage, byte(2 + len(replyMsg))}
	replyAttr = append(replyAttr, replyMsg...)
	attrs = append(attrs, replyAttr...)

	return s.buildRadiusPacket(req, radiusAccessChallenge, attrs)
}

func (s *RadiusServer) buildRadiusPacket(req []byte, replyCode uint8, attrs []byte) []byte {
	// Compute total length first (without MA).
	maAttr := buildMessageAuthAttr()
	totalLen := 20 + len(attrs) + len(maAttr)
	packet := make([]byte, totalLen)
	packet[0] = replyCode
	packet[1] = req[1]
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))

	// Response authenticator = MD5(code+id+length+requestAuth+attributes+secret)
	respAuth := md5Authenticator(packet[:20], req[4:20], attrs, s.sharedSecret)
	copy(packet[4:20], respAuth)

	// Copy attributes.
	offset := 20
	copy(packet[offset:], attrs)
	offset += len(attrs)

	// Copy Message-Authenticator placeholder.
	copy(packet[offset:], maAttr)
	offset += len(maAttr)

	// Now compute and fill Message-Authenticator value.
	// MA = HMAC-MD5(packet-header + attributes + MA-type + len, secret)
	// Exclude the 16-byte MA value itself (set to zeros for computation).
	maValueOffset := offset - 16
	for i := 0; i < 16; i++ {
		packet[maValueOffset+i] = 0
	}
	maData := make([]byte, 20+len(attrs)+len(maAttr))
	copy(maData, packet[:20])            // header
	copy(maData[20:], attrs)             // attributes
	copy(maData[20+len(attrs):], maAttr) // MA attr (with zero value)

	ma := computeHMACMD5(maData, s.sharedSecret)
	copy(packet[maValueOffset:maValueOffset+16], ma)

	return packet
}

func buildEAPAttr(payload []byte) []byte {
	var attrs []byte
	// Max EAP-Message per RADIUS is 253 bytes (1 type + 1 len + ≤251 data).
	for i := 0; i < len(payload); i += 251 {
		end := i + 251
		if end > len(payload) {
			end = len(payload)
		}
		chunk := payload[i:end]
		attr := []byte{attrEAPMessage, byte(2 + len(chunk))}
		attr = append(attr, chunk...)
		attrs = append(attrs, attr...)
	}
	return attrs
}

func buildStateAttr(sessionID string) []byte {
	attr := []byte{attrState, byte(2 + len(sessionID))}
	return append(attr, sessionID...)
}

func buildMessageAuthAttr() []byte {
	attr := []byte{attrMessageAuth, 18}
	return append(attr, make([]byte, 16)...) // 16-byte zero placeholder
}

// md5Authenticator computes Response Authenticator.
// RFC 2865: ResponseAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
func md5Authenticator(header []byte, reqAuth []byte, attrs []byte, secret []byte) []byte {
	h := md5.New()
	h.Write(header[:4]) // code + id + length
	h.Write(reqAuth)    // Request Authenticator from Access-Request
	h.Write(attrs)      // attributes from original request
	h.Write(secret)     // shared secret
	return h.Sum(nil)
}

// computeHMACMD5 computes HMAC-MD5 over data.
func computeHMACMD5(data, secret []byte) []byte {
	h := hmac.New(md5.New, secret)
	h.Write(data)
	return h.Sum(nil)
}

// hasMessageAuth reports whether the packet contains a Message-Authenticator attribute.
func hasMessageAuth(data []byte) bool {
	return findAttr(data, attrMessageAuth) >= 0
}

func findAttr(data []byte, attrType uint8) int {
	pos := 20
	for pos+2 <= len(data) {
		length := int(data[pos+1])
		if length < 2 || pos+length > len(data) {
			break
		}
		if data[pos] == attrType {
			return pos
		}
		pos += length
	}
	return -1
}

// verifyMessageAuth validates the Message-Authenticator in an Access-Request.
// RFC 3579 §3.2: MA = HMAC-MD5(packet, secret) where packet has MA zeroed.
func verifyMessageAuth(data []byte, secret []byte) bool {
	if len(data) < 20+2+16 {
		return false
	}
	offset := findAttr(data, attrMessageAuth)
	if offset < 0 || offset+18 > len(data) {
		return false
	}

	// Extract expected value.
	expected := make([]byte, 16)
	copy(expected, data[offset+2:offset+18])

	// Compute with zeroed MA field.
	work := make([]byte, len(data))
	copy(work, data)
	for i := 0; i < 16; i++ {
		work[offset+2+i] = 0
	}

	// MA covers header + attributes including MA attr.
	maData := make([]byte, offset+18)
	copy(maData, work[:offset+18])
	computed := computeHMACMD5(maData, secret)

	return hmac.Equal(expected, computed)
}

func extractSessionID(raw []byte) string {
	offset := findAttr(raw, attrState)
	if offset < 0 {
		return fmt.Sprintf("session-%d", rand.Int63())
	}
	length := int(raw[offset+1])
	if length < 3 {
		return ""
	}
	return string(raw[offset+2 : offset+length])
}
