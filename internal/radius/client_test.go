// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Transport
// ---------------------------------------------------------------------------

// mockTransport implements the Client interface for testing.
type mockTransport struct {
	mu        sync.Mutex
	responses map[string][]byte // server address → response
	sendLog   []sendEntry
	calls     int
	closed    bool
}

type sendEntry struct {
	Packet []byte
	Server string
}

func newMockTransport() *mockTransport {
	return &mockTransport{responses: make(map[string][]byte)}
}

func (m *mockTransport) Send(_ context.Context, packet []byte, server string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	m.sendLog = append(m.sendLog, sendEntry{Packet: packet, Server: server})

	if resp, ok := m.responses[server]; ok {
		return resp, nil
	}
	return nil, context.DeadlineExceeded
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockTransport) Addr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1812}
}

func (m *mockTransport) SetResponse(server string, response []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[server] = response
}

func (m *mockTransport) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockTransport) LastSend() (packet []byte, server string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sendLog) == 0 {
		return nil, ""
	}
	last := m.sendLog[len(m.sendLog)-1]
	return last.Packet, last.Server
}

// ---------------------------------------------------------------------------
// Config Defaults
// ---------------------------------------------------------------------------

func TestClientRadiusClientDefaults(t *testing.T) {
	logger := slog.Default()

	// Create client directly with mock transport.
	c := &Client{
		config: Config{
			ServerAddress:  "127.0.0.1",
			ServerPort:     1812,
			SharedSecret:   "secret",
			Timeout:        10 * time.Second,
			MaxRetries:     3,
			ResponseWindow: 10 * time.Second,
		},
		transport: newMockTransport(),
		logger:    logger,
	}

	assert.NotNil(t, c)
	assert.Equal(t, 1812, c.config.ServerPort)
}

// ---------------------------------------------------------------------------
// Message Authenticator
// ---------------------------------------------------------------------------

func TestComputeMessageAuthenticator(t *testing.T) {
	// Build a minimal Access-Request packet.
	auth := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	// Encode without MA first.
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()

	// Add Message-Authenticator.
	ma := ComputeMessageAuthenticator(raw, "shared-secret")
	assert.Len(t, ma, 16)
}

func TestVerifyMessageAuthenticator(t *testing.T) {
	auth := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	secret := "test-secret"

	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "alice"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, secret)

	ok := VerifyMessageAuthenticator(raw, secret)
	assert.True(t, ok)

	// Wrong secret.
	ok = VerifyMessageAuthenticator(raw, "wrong-secret")
	assert.False(t, ok)
}

// Regression: VerifyMessageAuthenticator must not panic on a truncated MA attribute.
// Previously it sliced from offset (instead of offset+2) and skipped 2 bytes
// from the received value, masking the bug. The correct code slices from
// offset+2 and compares directly.
func TestVerifyMessageAuthenticatorTruncatedPacket(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, "secret")

	// Truncate the packet so the MA value is cut short.
	// The MA attribute header (type=80, len=18) is at offset≥20, but the 16-byte
	// value is partially outside the buffer. Must not panic.
	truncated := raw[:len(raw)-8]
	assert.NotPanics(t, func() {
		ok := VerifyMessageAuthenticator(truncated, "secret")
		// Should safely return false.
		assert.False(t, ok)
	})
}

// Regression: VerifyMessageAuthenticator must return false (not panic) when
// a malformed MA attribute has type=80 and len=18 but is positioned near
// the end of the packet with fewer than 18 bytes remaining.
func TestVerifyMessageAuthenticatorMalformedNearEnd(t *testing.T) {
	// Build a valid Access-Request with a User-Name attribute.
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "test"),
	})
	raw := packet.Encode()

	// Manually append a MA-like attribute at the very end, claiming length=18
	// but without enough bytes to back it up. This would previously cause
	// VerifyMessageAuthenticator to panic on slice bounds.
	malformed := append(raw, AttrMessageAuthenticator, 18)

	assert.NotPanics(t, func() {
		ok := VerifyMessageAuthenticator(malformed, "secret")
		assert.False(t, ok)
	})
}

// Regression: ParsePrivateKey must accept PKCS8-encoded keys (the default format
// produced by most tools). Previously it only handled PKCS1.
func TestParsePrivateKeyPKCS8(t *testing.T) {
	key, err := generateRSAPrivateKey(2048)
	require.NoError(t, err)

	// Encode as PKCS8.
	pkcs8Bytes, err := encodePKCS8(key)
	require.NoError(t, err)

	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	parsed, err := ParsePrivateKey(pemData)
	assert.NoError(t, err)
	assert.NotNil(t, parsed)
}

// Regression: ParsePrivateKey must fall back to PKCS1 when PKCS8 fails.
func TestParsePrivateKeyPKCS1Fallback(t *testing.T) {
	key, err := generateRSAPrivateKey(2048)
	require.NoError(t, err)

	pkcs1Bytes := x509.MarshalPKCS1PrivateKey(key)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: pkcs1Bytes})

	parsed, err := ParsePrivateKey(pemData)
	assert.NoError(t, err)
	assert.NotNil(t, parsed)
}

func TestAddMessageAuthenticator(t *testing.T) {
	auth := [16]byte{0x10, 0x0f, 0x0e, 0x0d, 0x0c, 0x0b, 0x0a, 0x09,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}

	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "bob"),
	})
	raw := packet.Encode()

	// Add MA twice — should replace, not duplicate.
	raw = AddMessageAuthenticator(raw, "secret")
	raw = AddMessageAuthenticator(raw, "secret")

	// Verify there is exactly one MA attribute.
	count := 0
	offset := 20
	for offset+1 < len(raw) {
		if raw[offset] == AttrMessageAuthenticator {
			count++
		}
		offset += int(raw[offset+1])
	}
	assert.Equal(t, 1, count)
}

func TestZeroMessageAuthenticator(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, "secret")

	// Zero out MA field.
	zeroed := zeroMessageAuthenticator(raw)
	assert.NotNil(t, zeroed)
}

func TestClientRemoveMessageAuthenticator(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, "secret")

	removed := removeMessageAuthenticator(raw)
	assert.Greater(t, len(raw), len(removed))

	// Verify MA is gone.
	offset := 20
	found := false
	for offset+1 < len(removed) {
		if removed[offset] == AttrMessageAuthenticator {
			found = true
			break
		}
		offset += int(removed[offset+1])
	}
	assert.False(t, found)
}

func TestFindMessageAuthenticator(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, "secret")

	idx := findMessageAuthenticator(raw)
	assert.Greater(t, idx, 0)

	// Without MA.
	packet2 := BuildAccessRequest(2, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "nobody"),
	})
	raw2 := packet2.Encode()
	idx2 := findMessageAuthenticator(raw2)
	assert.Equal(t, -1, idx2)
}

func TestComputeResponseAuthenticator(t *testing.T) {
	requestAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	secret := "response-secret"

	respAuth := ComputeResponseAuthenticator(
		CodeAccessAccept, 1, 200, requestAuth, []byte{}, secret,
	)
	assert.Len(t, respAuth, 16)

	// Deterministic: same inputs → same output.
	respAuth2 := ComputeResponseAuthenticator(
		CodeAccessAccept, 1, 200, requestAuth, []byte{}, secret,
	)
	assert.Equal(t, respAuth, respAuth2)
}

// ---------------------------------------------------------------------------
// Access Challenge Validation
// ---------------------------------------------------------------------------

func TestValidateAccessChallenge(t *testing.T) {
	secret := "challenge-secret"
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build an Access-Challenge.
	challenge := BuildAccessChallenge(1, auth, []Attribute{
		MakeStringAttribute(AttrState, "opaque-state-123"),
	})
	raw := challenge.Encode()
	raw = AddMessageAuthenticator(raw, secret)

	state, err := ValidateAccessChallenge(raw, auth, secret)
	require.NoError(t, err)
	assert.Equal(t, "opaque-state-123", string(state))
}

func TestValidateAccessChallengeWrongCode(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
	})
	raw := packet.Encode()

	_, err := ValidateAccessChallenge(raw, auth, "secret")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetAttributeBytes
// ---------------------------------------------------------------------------

func TestClientGetAttributeBytes(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "alice"),
		MakeIntegerAttribute(AttrServiceType, 8),
		Make3GPPSNSSAIAttribute(1, "ABCDEF"),
	})
	raw := packet.Encode()

	// Get EAP-Message (not present).
	eapData := GetAttributeBytes(raw, AttrEAPMessage)
	assert.Nil(t, eapData)

	// Get User-Name.
	username := GetAttributeBytes(raw, AttrUserName)
	assert.Equal(t, []byte("alice"), username)

	// Too short.
	assert.Nil(t, GetAttributeBytes([]byte{1, 2}, AttrUserName))
}

// ---------------------------------------------------------------------------
// Fragmentation
// ---------------------------------------------------------------------------

func TestClientFragmentEAPMessage(t *testing.T) {
	eapData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// No fragmentation needed.
	frags := FragmentEAPMessage(eapData, 253)
	require.Len(t, frags, 1)
	assert.Equal(t, eapData, frags[0])

	// Exact fit.
	frags = FragmentEAPMessage(eapData, 10)
	require.Len(t, frags, 1)
	assert.Equal(t, eapData, frags[0])
}

func TestClientFragmentEAPMessageMultiple(t *testing.T) {
	eapData := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ") // 26 bytes

	// Split into 10-byte chunks → 3 fragments.
	frags := FragmentEAPMessage(eapData, 10)
	require.Len(t, frags, 3)
	assert.Equal(t, []byte("ABCDEFGHIJ"), frags[0])
	assert.Equal(t, []byte("KLMNOPQRST"), frags[1])
	assert.Equal(t, []byte("UVWXYZ"), frags[2])
}

func TestClientFragmentEAPMessageSingleByte(t *testing.T) {
	eapData := []byte("x")
	frags := FragmentEAPMessage(eapData, 10)
	require.Len(t, frags, 1)
	assert.Equal(t, eapData, frags[0])
}

func TestClientAssembleEAPMessage(t *testing.T) {
	attrs := []Attribute{
		MakeAttribute(AttrEAPMessage, []byte("part1")),
		MakeAttribute(AttrUserName, []byte("should-be-skipped")),
		MakeAttribute(AttrEAPMessage, []byte("part2")),
		MakeAttribute(AttrEAPMessage, []byte("part3")),
	}

	result := AssembleEAPMessage(attrs)
	assert.Equal(t, []byte("part1part2part3"), result)
}

func TestClientAssembleEAPMessageEmpty(t *testing.T) {
	attrs := []Attribute{
		MakeAttribute(AttrUserName, []byte("user")),
	}
	result := AssembleEAPMessage(attrs)
	assert.Empty(t, result)
}

// ---------------------------------------------------------------------------
// Client / Transport Tests
// ---------------------------------------------------------------------------

func TestClientRadiusClientConstruction(t *testing.T) {
	logger := slog.Default()

	// Create client directly with mock transport.
	c := &Client{
		config: Config{
			ServerAddress:  "127.0.0.1",
			ServerPort:     1812,
			SharedSecret:   "secret",
			Timeout:        10 * time.Second,
			MaxRetries:     3,
			ResponseWindow: 10 * time.Second,
		},
		transport: newMockTransport(),
		logger:    logger,
	}

	assert.NotNil(t, c)
	assert.Equal(t, 1812, c.config.ServerPort)
}

func TestClientDecodeSnssaiVSA(t *testing.T) {
	sst, sd, err := DecodeSnssaiVSA([]byte{3})
	require.NoError(t, err)
	assert.Equal(t, uint8(3), sst)
	assert.Empty(t, sd)

	sst, sd, err = DecodeSnssaiVSA([]byte{1, 0xAB, 0xCD, 0xEF})
	require.NoError(t, err)
	assert.Equal(t, uint8(1), sst)
	assert.Equal(t, "ABCDEF", sd)
}

func TestClientSnssaiVSATooShort(t *testing.T) {
	_, _, err := DecodeSnssaiVSA([]byte{})
	assert.Error(t, err)
}

func TestClientMake3GPPSNSSAIAttribute(t *testing.T) {
	attr := Make3GPPSNSSAIAttribute(1, "123456")
	assert.Equal(t, uint8(AttrVendorSpecific), attr.Type)
	assert.Len(t, attr.Value, 8) // 4 (VSA header) + 3 (VID) + 1 (type) = 8
}

func TestClientSnssaiVSAEncodeDecode(t *testing.T) {
	attr := EncodeVSA(VendorID3GPP, VendorTypeSNSSAI, []byte{1, 2, 3})
	assert.Equal(t, uint8(AttrVendorSpecific), attr.Type)

	vsa, err := DecodeVSA(&attr)
	require.NoError(t, err)
	assert.Equal(t, VendorID3GPP, vsa.VendorID)
	assert.Equal(t, VendorTypeSNSSAI, vsa.VendorType)
	assert.Equal(t, []byte{1, 2, 3}, vsa.Data)
}

func TestVSAEncodeDecodeInvalid(t *testing.T) {
	// Wrong type.
	_, err := DecodeVSA(&Attribute{Type: AttrUserName, Value: []byte{1, 2, 3, 4, 5}})
	assert.Error(t, err)

	// Too short.
	_, err = DecodeVSA(&Attribute{Type: AttrVendorSpecific, Value: []byte{1, 2}})
	assert.Error(t, err)

	// Nil.
	_, err = DecodeVSA(nil)
	assert.Error(t, err)
}

func TestClientVSAIs3GPPSNSSAI(t *testing.T) {
	attr := EncodeVSA(VendorID3GPP, VendorTypeSNSSAI, []byte{1, 2, 3})
	vsa, _ := DecodeVSA(&attr)
	assert.True(t, vsa.Is3GPPSNSSAI())

	// Wrong vendor type.
	attr2 := EncodeVSA(VendorID3GPP, 99, []byte{1, 2, 3})
	vsa2, _ := DecodeVSA(&attr2)
	assert.False(t, vsa2.Is3GPPSNSSAI())
}

func TestVSAParse3GPPSNSSAI(t *testing.T) {
	attr := Make3GPPSNSSAIAttribute(7, "DEF012")
	vsa, _ := DecodeVSA(&attr)

	sst, sd, err := vsa.Parse3GPPSNSSAI()
	require.NoError(t, err)
	assert.Equal(t, uint8(7), sst)
	assert.Equal(t, "DEF012", sd)

	// Non-SNSSAI VSA.
	attr2 := EncodeVSA(VendorID3GPP, 99, []byte{1, 2, 3})
	vsa2, _ := DecodeVSA(&attr2)
	_, _, err = vsa2.Parse3GPPSNSSAI()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// UDP Client
// ---------------------------------------------------------------------------

func TestNewUDPClient(t *testing.T) {
	transport, err := NewUDPClient(UDPConfig{
		LocalBindAddr: ":0",
		DialTimeout:   5 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, transport)

	err = transport.Close()
	assert.NoError(t, err)
}

func TestClientGenerateRandomAuthenticator(t *testing.T) {
	auth1, err := GenerateRandomAuthenticator()
	require.NoError(t, err)
	assert.Len(t, auth1, 16)

	auth2, err := GenerateRandomAuthenticator()
	require.NoError(t, err)

	// Should be different each time (statistically).
	assert.NotEqual(t, auth1, auth2)
}

func TestClientGenerateRandomAuthenticatorError(t *testing.T) {
	// It should not fail in normal circumstances.
	_, err := GenerateRandomAuthenticator()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DTLS Stubs
// ---------------------------------------------------------------------------

func TestDialDTLSReturnsError(t *testing.T) {
	_, err := DialDTLS(DTLSConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DTLS")
}

func TestParseCertificate(t *testing.T) {
	// Valid PEM block — generate a real self-signed cert so the base64
	// line breaks are guaranteed correct.
	generatePEM := func() []byte {
		key, err := generateRSAPrivateKey(2048)
		if err != nil {
			t.Fatalf("failed to generate RSA key: %v", err)
		}
		certDER, err := generateSelfSignedCert(key)
		if err != nil {
			t.Fatalf("failed to generate cert: %v", err)
		}
		block := &pem.Block{Type: "CERTIFICATE", Bytes: certDER}
		return pem.EncodeToMemory(block)
	}

	pemData := generatePEM()
	cert, err := ParseCertificate(pemData)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestParseCertificateInvalid(t *testing.T) {
	_, err := ParseCertificate([]byte("not pem"))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestPacketCodes(t *testing.T) {
	assert.Equal(t, uint8(1), CodeAccessRequest)
	assert.Equal(t, uint8(2), CodeAccessAccept)
	assert.Equal(t, uint8(3), CodeAccessReject)
	assert.Equal(t, uint8(11), CodeAccessChallenge)
	assert.Equal(t, uint8(40), CodeDisconnectRequest)
	assert.Equal(t, uint8(41), CodeDisconnectACK)
	assert.Equal(t, uint8(42), CodeDisconnectNAK)
}

func TestDefaultPorts(t *testing.T) {
	assert.Equal(t, 1812, DefaultServerPort)
	assert.Equal(t, 10*time.Second, DefaultResponseWindow)
	assert.Equal(t, 3, DefaultMaxRetries)
}

func TestVendorID(t *testing.T) {
	assert.Equal(t, uint32(10415), VendorID3GPP)
	assert.Equal(t, uint8(200), VendorTypeSNSSAI)
}

// ---------------------------------------------------------------------------
// Certificate generation helpers
// ---------------------------------------------------------------------------

func generateRSAPrivateKey(bits int) (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, bits)
}

func generateSelfSignedCert(key *rsa.PrivateKey) ([]byte, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}
	return x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
}

// encodePKCS8 encodes an RSA private key in PKCS8 format.
func encodePKCS8(key *rsa.PrivateKey) ([]byte, error) {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pkcs8, nil
}
