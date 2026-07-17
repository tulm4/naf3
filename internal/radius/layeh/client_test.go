package layeh

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"net"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/radius/layeh/gen"
	"layeh.com/radius"
)

// TestClient_AccessRequest verifies a successful Access-Request/Access-Accept round trip.
func TestClient_AccessRequest(t *testing.T) {
	// Start mock server
	server, addrStr := startMockServer(t, false)
	defer server.Close()

	// Create client
	client, err := NewClient(Config{
		ServerAddr: addrStr,
		Secret:     []byte("testing123"),
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// Send Access-Request
	resp, err := client.AccessRequest(context.Background(), &AccessRequest{
		UserName:         "user@example.com",
		NASIdentifier:    "naf3.local",
		CallingStationID: "00:11:22:33:44:55",
		NSSAI:            gen.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
	})
	if err != nil {
		t.Fatalf("AccessRequest: %v", err)
	}

	// Verify response
	if resp.Code != radius.CodeAccessAccept {
		t.Errorf("expected CodeAccessAccept, got %v", resp.Code)
	}
}

// TestClient_AccessReject verifies Access-Reject response handling.
func TestClient_AccessReject(t *testing.T) {
	server, addrStr := startMockServer(t, true)
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
		UserName: "reject@example.com",
		NSSAI:    gen.NSSAI{SST: 1},
	})
	if err != nil {
		t.Fatalf("AccessRequest: %v", err)
	}

	if resp.Code != radius.CodeAccessReject {
		t.Errorf("expected CodeAccessReject, got %v", resp.Code)
	}
}

// TestClient_Timeout verifies timeout behavior when no server is listening.
func TestClient_Timeout(t *testing.T) {
	client, err := NewClient(Config{
		ServerAddr: "127.0.0.1:19999",
		Secret:     []byte("testing123"),
		Timeout:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	_, err = client.AccessRequest(context.Background(), &AccessRequest{
		UserName: "user@example.com",
	})
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

// TestPatchMessageAuthenticator verifies that the Message-Authenticator attribute
// is correctly computed per RFC 3579 §3.2 (HMAC-MD5 over the packet with MA zeroed).
func TestPatchMessageAuthenticator(t *testing.T) {
	secret := []byte("testing123")
	pkt := radius.New(radius.CodeAccessRequest, secret)
	if err := gen.UserName_AddString(pkt, "user@example.com"); err != nil {
		t.Fatalf("UserName_AddString: %v", err)
	}
	if err := gen.MessageAuthenticator_Add(pkt, make([]byte, 16)); err != nil {
		t.Fatalf("MessageAuthenticator_Add: %v", err)
	}

	if err := patchMessageAuthenticator(pkt); err != nil {
		t.Fatalf("patchMessageAuthenticator: %v", err)
	}

	wire, err := pkt.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Verify MA attribute is present in wire bytes and has been patched.
	maOffset := -1
	for i := 20; i+1 < len(wire); {
		if wire[i] == byte(gen.MessageAuthenticator_Type) && wire[i+1] == 18 {
			maOffset = i
			break
		}
		i += int(wire[i+1])
	}
	if maOffset < 0 {
		t.Fatal("Message-Authenticator attribute not found in encoded packet")
	}
	maValue := wire[maOffset+2 : maOffset+18]
	nonZero := false
	for _, b := range maValue {
		if b != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Error("Message-Authenticator value is all zeros; HMAC not computed")
	}

	// Cross-check: independently compute the expected HMAC-MD5 and compare.
	// RFC 3579 §3.2: HMAC-MD5(Code + ID + Length + RequestAuth + Attributes
	//                  + Secret) with MA value replaced by 16 zero octets.
	zeroed := make([]byte, len(wire))
	copy(zeroed, wire)
	for i := 0; i < 16; i++ {
		zeroed[maOffset+2+i] = 0
	}
	mac := hmac.New(md5.New, secret)
	mac.Write(zeroed)
	expected := mac.Sum(nil)
	if !hmac.Equal(maValue, expected) {
		t.Errorf("Message-Authenticator mismatch:\n got  %x\n want %x", maValue, expected)
	}
}

// Mock server helpers

type mockServerState struct {
	conn   *net.UDPConn
	reject bool
}

func (s *mockServerState) Close() {
	s.conn.Close()
}

func startMockServer(t *testing.T, reject bool) (*mockServerState, string) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}

	state := &mockServerState{conn: conn, reject: reject}

	go func() {
		buf := make([]byte, 4096)
		for {
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}

			secret := []byte("testing123")
			pkt, err := radius.Parse(buf[:n], secret)
			if err != nil {
				continue
			}

			code := radius.CodeAccessAccept
			if state.reject {
				code = radius.CodeAccessReject
			}

			// Build response with the same identifier and request authenticator
			// as the incoming Access-Request so the layeh client can validate
			// the Response Authenticator.
			resp := pkt.Response(code)
			gen.MessageAuthenticator_Add(resp, make([]byte, 16))

			wire, err := resp.Encode()
			if err != nil {
				continue
			}
			conn.WriteToUDP(wire, clientAddr)
		}
	}()

	return state, conn.LocalAddr().String()
}
