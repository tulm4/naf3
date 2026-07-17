package gen

import (
	"os"
	"path/filepath"
	"testing"

	"layeh.com/radius"
)

// TestGolden_AccessRequestNSSAI verifies that the recorded wire bytes
// (access-request-nssai.packet) parse back into the expected RADIUS
// attributes, locking the encoder's output format and the dictionary
// helpers' on-wire representation.
func TestGolden_AccessRequestNSSAI(t *testing.T) {
	goldenPath := filepath.Join("..", "testdata", "golden", "access-request-nssai.packet")

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", goldenPath, err)
	}

	secret := []byte("testing123")
	pkt, err := radius.Parse(golden, secret)
	if err != nil {
		t.Fatalf("radius.Parse: %v", err)
	}

	if pkt.Code != radius.CodeAccessRequest {
		t.Errorf("Code = %v, want %v", pkt.Code, radius.CodeAccessRequest)
	}
	if pkt.Identifier != 1 {
		t.Errorf("Identifier = %d, want 1", pkt.Identifier)
	}

	userName, err := UserName_Lookup(pkt)
	if err != nil {
		t.Fatalf("User-Name: %v", err)
	}
	if string(userName) != "user@example.com" {
		t.Errorf("User-Name = %q, want %q", string(userName), "user@example.com")
	}

	nasID, err := NASIdentifier_Lookup(pkt)
	if err != nil {
		t.Fatalf("NAS-Identifier: %v", err)
	}
	if string(nasID) != "naf3.local" {
		t.Errorf("NAS-Identifier = %q, want %q", string(nasID), "naf3.local")
	}

	callingID, err := CallingStationID_Lookup(pkt)
	if err != nil {
		t.Fatalf("Calling-Station-Id: %v", err)
	}
	if string(callingID) != "00:11:22:33:44:55" {
		t.Errorf("Calling-Station-Id = %q, want %q", string(callingID), "00:11:22:33:44:55")
	}

	nssais, err := GetNSSAIAttributes(pkt)
	if err != nil {
		t.Fatalf("GetNSSAIAttributes: %v", err)
	}
	if len(nssais) != 1 {
		t.Fatalf("expected 1 NSSAI, got %d", len(nssais))
	}

	want := NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}}
	if nssais[0] != want {
		t.Errorf("NSSAI = %+v, want %+v", nssais[0], want)
	}

	ma, err := MessageAuthenticator_Lookup(pkt)
	if err != nil {
		t.Fatalf("Message-Authenticator: %v", err)
	}
	if len(ma) != 16 {
		t.Errorf("Message-Authenticator length = %d, want 16", len(ma))
	}
}

// TestGolden_RoundTrip verifies the recorded wire bytes can be re-encoded
// to the exact same byte stream — a regression guard for any future change
// in encoder ordering, attribute padding, or HMAC patching behavior.
func TestGolden_RoundTrip(t *testing.T) {
	goldenPath := filepath.Join("..", "testdata", "golden", "access-request-nssai.packet")
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", goldenPath, err)
	}

	secret := []byte("testing123")

	pkt1, err := radius.Parse(golden, secret)
	if err != nil {
		t.Fatalf("radius.Parse: %v", err)
	}

	buf, err := pkt1.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if len(buf) != len(golden) {
		t.Fatalf("round-trip length: got %d, want %d", len(buf), len(golden))
	}
	for i := range buf {
		if buf[i] != golden[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, buf[i], golden[i])
			break
		}
	}
}
