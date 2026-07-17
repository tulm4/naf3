package layeh

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/radius/layeh/gen"
	"layeh.com/radius"
)

// TestE2E_AccessChallengeState exercises a multi-round EAP conversation where
// the server returns Access-Challenge with a State attribute on the first
// round and Access-Accept after receiving that State back.
//
// EAP-AKA' (RFC 5448) and other EAP methods require several Access-Request /
// Access-Challenge rounds, with the server opaque State attribute carrying
// session context from round to round. This test verifies the layeh client's
// ability to thread the State attribute through successive AccessRequest
// calls so that downstream NSS-AAA flows can drive EAP-AKA' conversations.
//
// Spec: RFC 3579 §4 (Access-Challenge), RFC 5448 (EAP-AKA'), TS 29.561 §16.
func TestE2E_AccessChallengeState(t *testing.T) {
	server, addrStr := startChallengeServer(t)
	defer server.Close()

	client, err := NewClient(Config{
		ServerAddr: addrStr,
		Secret:     []byte("testing123"),
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Round 1: no State — server should respond with Access-Challenge.
	resp1, err := client.AccessRequest(ctx, &AccessRequest{
		UserName: "user@example.com",
		NSSAI:    gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
	})
	if err != nil {
		t.Fatalf("AccessRequest (round 1): %v", err)
	}
	if resp1.Code != radius.CodeAccessChallenge {
		t.Fatalf("round 1: expected Access-Challenge, got %v", resp1.Code)
	}
	if len(resp1.State) == 0 {
		t.Fatal("round 1: expected non-empty State attribute from server")
	}

	// Round 2: send the State back. Server should accept.
	resp2, err := client.AccessRequest(ctx, &AccessRequest{
		UserName: "user@example.com",
		NSSAI:    gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
		State:    resp1.State,
	})
	if err != nil {
		t.Fatalf("AccessRequest (round 2): %v", err)
	}
	if resp2.Code != radius.CodeAccessAccept {
		t.Fatalf("round 2: expected Access-Accept, got %v", resp2.Code)
	}

	// Server must have observed two distinct requests with the same identifier
	// space and the round-2 State matching round-1.
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.receivedState) != 2 {
		t.Fatalf("server received %d requests, want 2", len(server.receivedState))
	}
	// Round 1 must not have included any State (the server hasn't issued one yet).
	if len(server.receivedState[0]) != 0 {
		t.Errorf("round 1 request: client unexpectedly included State %x", server.receivedState[0])
	}
	// Round 2 must echo back the State the server issued in round 1.
	if !bytes.Equal(server.receivedState[1], server.issuedState) {
		t.Errorf("round 2 State mismatch: got %x, want %x",
			server.receivedState[1], server.issuedState)
	}
}

// TestE2E_CoARequest validates CoA-Request / CoA-ACK handling per RFC 5176 §3.
//
// The legacy implementation in internal/aaa/gateway/radius_handler.go had two
// bugs in sendCoAResponse: it computed Message-Authenticator over the wrong
// bytes (request bytes instead of response bytes) and recomputed the Response
// Authenticator instead of copying the Request Authenticator. This test
// exercises the equivalent round-trip using layeh directly, so any future
// regression in our MA-on-wire helper or in layeh's encoding would surface
// here.
//
// Spec: RFC 5176 §3.2 (CoA-Request / CoA-ACK), RFC 3579 §3.2 (Message-Authenticator).
func TestE2E_CoARequest(t *testing.T) {
	server, addrStr := startCoAServer(t)
	defer server.Close()

	secret := []byte("testing123")

	// Build a CoA-Request with a User-Name and a (placeholder) Message-Authenticator.
	pkt := radius.New(radius.CodeCoARequest, secret)
	if err := gen.UserName_AddString(pkt, "user@example.com"); err != nil {
		t.Fatalf("UserName_AddString: %v", err)
	}
	if err := gen.ThreeGPPSNSSAI_Add(pkt, (&gen.NSSAI{SST: 1, SD: [3]byte{0x01, 0x02, 0x03}}).Pack()); err != nil {
		t.Fatalf("ThreeGPPSNSSAI_Add: %v", err)
	}
	if err := gen.MessageAuthenticator_Add(pkt, make([]byte, 16)); err != nil {
		t.Fatalf("MessageAuthenticator_Add: %v", err)
	}
	// Patch the MA so the server's MA-validation path accepts the request.
	if err := patchMessageAuthenticator(pkt); err != nil {
		t.Fatalf("patchMessageAuthenticator: %v", err)
	}

	// Use layeh's Exchange to send and receive. We cannot reach c.radiusClient
	// from outside the package without exporting it, but this test lives in the
	// layeh package — so the field is accessible.
	c := mustClient(t, addrStr, secret)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.radiusClient.Exchange(ctx, pkt, addrStr)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if resp.Code != radius.CodeCoAACK {
		t.Errorf("expected CoA-ACK (44), got %v (=%d)", resp.Code, int(resp.Code))
	}

	// Validate Message-Authenticator on the response. The CoA-Request's
	// Request Authenticator that was actually sent on the wire is the one
	// from the server's captured request bytes — pkt.Authenticator here is
	// the original random value set by radius.New() before Encode()
	// recomputed it.
	server.mu.Lock()
	reqWire := server.lastReqWire
	server.mu.Unlock()
	if len(reqWire) < 20 {
		t.Fatal("server did not capture a CoA-Request wire image")
	}
	reqAuth := reqWire[4:20]
	verifyMessageAuthenticator(t, resp, reqAuth, secret)
}

// TestE2E_MessageAuthenticatorValidation verifies that layeh's Exchange()
// successfully receives an Access-Accept carrying a properly computed
// Message-Authenticator, and that the MA value parsed by layeh matches the
// MA bytes shipped on the wire.
//
// layeh's Exchange validates the Response Authenticator (MD5 over the packet
// + request authenticator + secret) but does not enforce Message-Authenticator
// verification — that is the responsibility of higher layers (e.g. NSS-AAA
// handlers). This test focuses on what we CAN verify cheaply: that the layeh
// parse/encode pipeline preserves the MA attribute value across the wire.
//
// The CoA test (TestE2E_CoARequest) uses verifyMessageAuthenticator to
// re-derive the expected MA via an independent re-encode, providing
// complementary coverage of the MA computation algorithm.
//
// Spec: RFC 3579 §3.2.
func TestE2E_MessageAuthenticatorValidation(t *testing.T) {
	server, addrStr := startMockServerWithMA(t)
	defer server.Close()

	secret := []byte("testing123")
	c := mustClient(t, addrStr, secret)
	defer c.Close()

	// Build an Access-Request. For Access-Request, Encode does NOT recompute
	// the Request Authenticator, so pkt.Authenticator[:] is what the server
	// saw on the wire and what it copied into the response via pkt.Response.
	pkt := radius.New(radius.CodeAccessRequest, secret)
	if err := gen.UserName_AddString(pkt, "user@example.com"); err != nil {
		t.Fatalf("UserName_AddString: %v", err)
	}
	if err := gen.ThreeGPPSNSSAI_Add(pkt, (&gen.NSSAI{SST: 1}).Pack()); err != nil {
		t.Fatalf("ThreeGPPSNSSAI_Add: %v", err)
	}
	if err := gen.MessageAuthenticator_Add(pkt, make([]byte, 16)); err != nil {
		t.Fatalf("MessageAuthenticator_Add: %v", err)
	}
	if err := patchMessageAuthenticator(pkt); err != nil {
		t.Fatalf("patchMessageAuthenticator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.radiusClient.Exchange(ctx, pkt, addrStr)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if resp.Code != radius.CodeAccessAccept {
		t.Fatalf("expected Access-Accept, got %v", resp.Code)
	}

	// The parsed MA must equal the MA in the wire bytes. If this mismatches,
	// layeh is corrupting the attribute during Parse/Encode.
	parsedMA, ok := resp.Attributes.Lookup(gen.MessageAuthenticator_Type)
	if !ok || len(parsedMA) != 16 {
		t.Fatal("response has no Message-Authenticator attribute")
	}

	server.mu.Lock()
	sentWire := server.lastSentWire
	server.mu.Unlock()
	if len(sentWire) == 0 {
		t.Fatal("server did not capture the wire bytes it sent")
	}

	maOffset := -1
	for i := 20; i+1 < len(sentWire); {
		if sentWire[i] == byte(gen.MessageAuthenticator_Type) && sentWire[i+1] == 18 {
			maOffset = i
			break
		}
		i += int(sentWire[i+1])
	}
	if maOffset < 0 {
		t.Fatal("server-sent wire bytes have no Message-Authenticator")
	}
	maOnWire := sentWire[maOffset+2 : maOffset+18]

	if !hmac.Equal(parsedMA, maOnWire) {
		t.Errorf("parsed MA differs from wire MA:\n parsed %x\n wire    %x",
			parsedMA, maOnWire)
	}
}

// TestE2E_MultipleNSSAI verifies that the client correctly handles a response
// containing multiple 3GPP-S-NSSAI attributes and that they round-trip through
// gen.GetNSSAIAttributes.
//
// Spec: TS 29.561 §16.3.2 (3GPP-S-NSSAI).
func TestE2E_MultipleNSSAI(t *testing.T) {
	server, addrStr := startMockServerWithMultipleNSSAI(t)
	defer server.Close()

	client, err := NewClient(Config{
		ServerAddr: addrStr,
		Secret:     []byte("testing123"),
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	resp, err := client.AccessRequest(context.Background(), &AccessRequest{
		UserName: "user@example.com",
		NSSAI:    gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
	})
	if err != nil {
		t.Fatalf("AccessRequest: %v", err)
	}
	if resp.Code != radius.CodeAccessAccept {
		t.Fatalf("expected Access-Accept, got %v", resp.Code)
	}
	if len(resp.NSSAI) != 2 {
		t.Fatalf("expected 2 NSSAI in response, got %d", len(resp.NSSAI))
	}

	want := []gen.NSSAI{
		{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
		{SST: 7, SD: [3]byte{0x00, 0x00, 0x01}},
	}
	for i, got := range resp.NSSAI {
		if got != want[i] {
			t.Errorf("NSSAI[%d]: got %+v, want %+v", i, got, want[i])
		}
	}
}

// ---- Mock servers ----

// challengeServer tracks State round-trips so we can assert the client
// threaded the State attribute correctly through the second request.
type challengeServer struct {
	conn          *net.UDPConn
	mu            sync.Mutex
	receivedState [][]byte
	issuedState   []byte
}

func (s *challengeServer) Close() { s.conn.Close() }

// startChallengeServer returns a UDP listener that replies Access-Challenge
// (with a State attribute) on the first Access-Request, then Access-Accept
// on any subsequent Access-Request that echoes the issued State back.
func startChallengeServer(t *testing.T) (*challengeServer, string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	s := &challengeServer{
		conn:          conn,
		issuedState:   []byte("challenge-state-1"),
		receivedState: nil,
	}

	go func() {
		buf := make([]byte, 4096)
		secret := []byte("testing123")
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, readErr := conn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			pkt, parseErr := radius.Parse(buf[:n], secret)
			if parseErr != nil {
				continue
			}

			s.mu.Lock()
			state := gen.State_Get(pkt)
			stateCopy := append([]byte(nil), state...)
			s.receivedState = append(s.receivedState, stateCopy)

			// Determine response based on whether the client sent the State
			// we issued. First request gets a Challenge; subsequent with
			// matching State get Accept.
			var resp *radius.Packet
			if bytes.Equal(state, s.issuedState) {
				resp = pkt.Response(radius.CodeAccessAccept)
				_ = gen.ThreeGPPSNSSAI_Add(resp, (&gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}).Pack())
			} else {
				resp = pkt.Response(radius.CodeAccessChallenge)
				_ = gen.State_Add(resp, s.issuedState)
			}
			s.mu.Unlock()

			wire, encErr := resp.Encode()
			if encErr != nil {
				continue
			}
			_, _ = conn.WriteToUDP(wire, clientAddr)
		}
	}()

	return s, conn.LocalAddr().String()
}

// coaServer responds to CoA-Request with CoA-ACK, including a properly
// computed Message-Authenticator per RFC 5176 §3.2 / RFC 3579 §3.2. It also
// records the wire bytes of the most recent CoA-Request so tests can
// validate the MA independently.
type coaServer struct {
	conn         *net.UDPConn
	mu           sync.Mutex
	lastReqWire  []byte
}

func (s *coaServer) Close() { s.conn.Close() }

func startCoAServer(t *testing.T) (*coaServer, string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	s := &coaServer{conn: conn}

	go func() {
		buf := make([]byte, 4096)
		secret := []byte("testing123")
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, readErr := conn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			pkt, parseErr := radius.Parse(buf[:n], secret)
			if parseErr != nil {
				continue
			}
			if pkt.Code != radius.CodeCoARequest {
				continue
			}

			// Build CoA-ACK, copy request's authenticator (RFC 5176 §3.2).
			resp := pkt.Response(radius.CodeCoAACK)
			// Mirror the NSSAI back as an echo.
			nssais, _ := gen.ThreeGPPSNSSAI_Gets(pkt)
			if len(nssais) > 0 {
				_ = gen.ThreeGPPSNSSAI_Add(resp, nssais[0])
			}

			// Compute MA + Response Authenticator consistently via the two-pass
			// algorithm in buildResponseWithMA.
			wire, encErr := buildResponseWithMA(resp, secret)
			if encErr != nil {
				continue
			}

			s.mu.Lock()
			s.lastReqWire = append([]byte(nil), buf[:n]...)
			s.mu.Unlock()

			_, _ = conn.WriteToUDP(wire, clientAddr)
		}
	}()

	return s, conn.LocalAddr().String()
}

// maServer sends Access-Accept with a properly computed Message-Authenticator,
// and records the wire bytes it actually sent so the test can independently
// re-validate the MA.
type maServer struct {
	conn         *net.UDPConn
	mu           sync.Mutex
	lastSentWire []byte
}

func (s *maServer) Close() { s.conn.Close() }

func startMockServerWithMA(t *testing.T) (*maServer, string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	s := &maServer{conn: conn}

	go func() {
		buf := make([]byte, 4096)
		secret := []byte("testing123")
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, readErr := conn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			pkt, parseErr := radius.Parse(buf[:n], secret)
			if parseErr != nil {
				continue
			}

			resp := pkt.Response(radius.CodeAccessAccept)
			wire, encErr := buildResponseWithMA(resp, secret)
			if encErr != nil {
				continue
			}

			s.mu.Lock()
			s.lastSentWire = append([]byte(nil), wire...)
			s.mu.Unlock()

			_, _ = conn.WriteToUDP(wire, clientAddr)
		}
	}()

	return s, conn.LocalAddr().String()
}

// multiNSSAIServer echoes two 3GPP-S-NSSAI attributes back in the Accept.
type multiNSSAIServer struct {
	conn *net.UDPConn
}

func (s *multiNSSAIServer) Close() { s.conn.Close() }

func startMockServerWithMultipleNSSAI(t *testing.T) (*multiNSSAIServer, string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	s := &multiNSSAIServer{conn: conn}

	go func() {
		buf := make([]byte, 4096)
		secret := []byte("testing123")
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, readErr := conn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			pkt, parseErr := radius.Parse(buf[:n], secret)
			if parseErr != nil {
				continue
			}

			resp := pkt.Response(radius.CodeAccessAccept)
			// Echo the original NSSAI back, plus a second authorized NSSAI.
			original := gen.ThreeGPPSNSSAI_Get(pkt)
			if len(original) > 0 {
				_ = gen.ThreeGPPSNSSAI_Add(resp, original)
			}
			_ = gen.ThreeGPPSNSSAI_Add(resp, (&gen.NSSAI{SST: 7, SD: [3]byte{0x00, 0x00, 0x01}}).Pack())

			wire, encErr := resp.Encode()
			if encErr != nil {
				continue
			}
			_, _ = conn.WriteToUDP(wire, clientAddr)
		}
	}()

	return s, conn.LocalAddr().String()
}

// ---- Helpers ----

// buildResponseWithMA returns the wire bytes for a response packet whose
// Message-Authenticator is computed correctly AND whose Response
// Authenticator remains consistent with the MA value. This is what RFC 5176
// §3.2 requires for CoA-ACK, and what layeh's IsAuthenticResponse expects
// when MA is present.
//
// Algorithm:
//  1. Replace any existing MA on the packet with a zero placeholder.
//  2. Encode pkt (Response Authenticator computed with MA=zero).
//  3. Compute HMAC-MD5 over those wire bytes (with MA zeroed), then patch
//     the value into pkt.Attributes so subsequent Encode() includes it.
//  4. Encode pkt again. The Response Authenticator is now recomputed over
//     attrs-with-actual-MA, so the wire bytes are internally consistent:
//     Response Authenticator matches the attrs, and MA matches the rest of
//     the packet.
//
// Spec: RFC 3579 §3.2, RFC 5176 §3.2.
func buildResponseWithMA(pkt *radius.Packet, secret []byte) ([]byte, error) {
	// Replace any pre-existing MA so we don't end up with two MA attributes
	// when serializing.
	pkt.Attributes.Del(gen.MessageAuthenticator_Type)
	if err := gen.MessageAuthenticator_Add(pkt, make([]byte, 16)); err != nil {
		return nil, err
	}

	// Pass 1: Encode to get the Response Authenticator computed with MA=zero.
	wire1, err := pkt.Encode()
	if err != nil {
		return nil, err
	}

	// Find the MA AVP in wire1 and compute the correct HMAC.
	maOffset := -1
	for i := 20; i+1 < len(wire1); {
		if wire1[i] == byte(gen.MessageAuthenticator_Type) && wire1[i+1] == 18 {
			maOffset = i
			break
		}
		i += int(wire1[i+1])
	}
	if maOffset < 0 {
		return nil, errors.New("buildResponseWithMA: MA not found after Add")
	}

	zeroed := make([]byte, len(wire1))
	copy(zeroed, wire1)
	for i := 0; i < 16; i++ {
		zeroed[maOffset+2+i] = 0
	}
	mac := hmac.New(md5.New, secret)
	mac.Write(zeroed)
	sum := mac.Sum(nil)

	// Patch the MA in the in-memory packet so Pass 2 includes it when
	// recomputing the Response Authenticator.
	attr, ok := pkt.Attributes.Lookup(gen.MessageAuthenticator_Type)
	if !ok {
		return nil, errors.New("buildResponseWithMA: MA attribute missing from packet")
	}
	copy(attr, sum)

	// Pass 2: Encode again. Response Authenticator now computed over
	// attrs-with-actual-MA.
	return pkt.Encode()
}

// verifyMessageAuthenticator computes the expected HMAC-MD5 over the encoded
// bytes of resp (with the MA value zeroed) and asserts equality with the MA
// in the response. reqAuth is the request authenticator used to compute the
// response's Response Authenticator; we re-encode the response with that
// authenticator to reproduce the same wire bytes that the server emitted.
//
// Spec: RFC 3579 §3.2.
func verifyMessageAuthenticator(t *testing.T, resp *radius.Packet, reqAuth []byte, secret []byte) {
	t.Helper()

	if len(reqAuth) != 16 {
		t.Fatalf("verifyMessageAuthenticator: reqAuth length %d, want 16", len(reqAuth))
	}

	// Reconstruct the response with the original Request Authenticator so
	// layeh's Encode produces the same Response Authenticator the server used.
	rebuilt := radius.New(resp.Code, secret)
	rebuilt.Identifier = resp.Identifier
	copy(rebuilt.Authenticator[:], reqAuth)
	for _, avp := range resp.Attributes {
		// Skip the MA AVP itself; we add a placeholder below.
		if avp.Type == gen.MessageAuthenticator_Type {
			continue
		}
		rebuilt.Attributes.Add(avp.Type, avp.Attribute)
	}
	_ = gen.MessageAuthenticator_Add(rebuilt, make([]byte, 16))

	wire, err := rebuilt.Encode()
	if err != nil {
		t.Fatalf("rebuilt.Encode: %v", err)
	}

	// The wire bytes now contain the response authenticator + a zeroed MA.
	// Compute the expected HMAC over them (with MA still zeroed) and
	// compare against the MA bytes parsed out of resp.
	maAttr, ok := resp.Attributes.Lookup(gen.MessageAuthenticator_Type)
	if !ok || len(maAttr) != 16 {
		t.Fatal("response has no Message-Authenticator attribute")
	}

	zeroed := make([]byte, len(wire))
	copy(zeroed, wire)
	maOffset := -1
	for i := 20; i+1 < len(wire); {
		if wire[i] == byte(gen.MessageAuthenticator_Type) && wire[i+1] == 18 {
			maOffset = i
			break
		}
		i += int(wire[i+1])
	}
	if maOffset < 0 {
		t.Fatal("MA not found in rebuilt wire bytes")
	}
	for i := 0; i < 16; i++ {
		zeroed[maOffset+2+i] = 0
	}
	mac := hmac.New(md5.New, secret)
	mac.Write(zeroed)
	expected := mac.Sum(nil)

	if !hmac.Equal(maAttr, expected) {
		t.Errorf("Message-Authenticator mismatch:\n got  %x\n want %x", maAttr, expected)
	}
}

// mustClient constructs a layeh Client for tests that need access to the
// private radiusClient field.
func mustClient(t *testing.T, addr string, secret []byte) *Client {
	t.Helper()
	c, err := NewClient(Config{
		ServerAddr: addr,
		Secret:     secret,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}