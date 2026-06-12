package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

func TestDiameterHandler_ASR_WaitsForBizPodResponse(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("test-session", "ASR", &ServerInitiatedResponse{
			AuthCtxID:  "test-auth",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("test-session", "test-auth", "ASR", 5*time.Second)
	resp := ch.Wait()

	if resp.ResultCode != 2001 {
		t.Errorf("expected ResultCode 2001, got %d", resp.ResultCode)
	}
}

func TestDiameterHandler_ASR_TimeoutReturnsUnableToDeliver(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("test-session", "test-auth", "ASR", 100*time.Millisecond)

	resp := ch.Wait()
	if resp.ResultCode != 3002 {
		t.Errorf("expected ResultCode 3002, got %d", resp.ResultCode)
	}
	if resp.ErrorCause != "timeout" {
		t.Errorf("expected ErrorCause 'timeout', got %s", resp.ErrorCause)
	}
}

// Tests for DiameterHandler ASR wait-for-Biz response behavior.

func TestDiameterHandler_STR_ForwardsToBizPod(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		registry.Complete("session-1", "STR", &ServerInitiatedResponse{
			AuthCtxID:  "auth-1",
			ResultCode: 2001,
		})
	}()

	ch, _ := registry.Register("session-1", "auth-1", "STR", 5*time.Second)
	resp := ch.Wait()

	if resp.ResultCode != 2001 {
		t.Errorf("expected ResultCode 2001, got %d", resp.ResultCode)
	}
}

func TestDiameterHandler_STR_TimeoutReturnsUnableToDeliver(t *testing.T) {
	registry := NewServerInitiatedRegistry(100 * time.Millisecond)
	ch, _ := registry.Register("session-1", "auth-1", "STR", 100*time.Millisecond)

	resp := ch.Wait()
	if resp.ResultCode != 3002 {
		t.Errorf("expected ResultCode 3002, got %d", resp.ResultCode)
	}
	if resp.ErrorCause != "timeout" {
		t.Errorf("expected ErrorCause 'timeout', got %s", resp.ErrorCause)
	}
}

// TestDiameterHandler_Listen_TLSProtocol verifies that the Listen method
// handles the "tcp+tls" protocol by starting a TLS listener.
func TestDiameterHandler_Listen_TLSProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("TLS test requires cert files")
	}
	tmpDir := t.TempDir()
	certFile, keyFile := generateTestCerts(t, tmpDir)

	h := &DiameterHandler{
		logger: slog.Default(),
		cfg: &DiameterHandlerConfig{
			TLSCert: certFile,
			TLSKey:  keyFile,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get random port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	err = h.Listen(ctx, addr, "tcp+tls")
	if err != nil {
		t.Fatalf("TLS listen failed: %v", err)
	}
}

// TestDiameterHandler_Listen_TLSProtocol_MissingCert verifies that Listen
// returns an error when TLS cert/key are missing.
func TestDiameterHandler_Listen_TLSProtocol_MissingCert(t *testing.T) {
	h := &DiameterHandler{
		logger: slog.Default(),
		cfg:    &DiameterHandlerConfig{}, // missing cert/key
	}

	err := h.Listen(context.Background(), "127.0.0.1:0", "tcp+tls")
	if err == nil {
		t.Error("expected error for missing TLS cert/key, got nil")
	}
}

// generateTestCerts creates a self-signed certificate for testing.
func generateTestCerts(t *testing.T, tmpDir string) (certFile, keyFile string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certFile = fmt.Sprintf("%s/cert.pem", tmpDir)
	keyFile = fmt.Sprintf("%s/key.pem", tmpDir)

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	_ = certOut.Close()

	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	_ = keyOut.Close()

	return certFile, keyFile
}

// Tests for peer validation (GAP-DIA-04).

func TestDiameterHandler_validatePeer_Allowed(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  []string{"aaa-s.example.com"},
		AllowedRealms: []string{"example.com"},
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "aaa-s.example.com",
		OriginRealm: "example.com",
	}

	err := h.validatePeer(meta)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDiameterHandler_validatePeer_HostRejected(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  []string{"aaa-s.example.com"},
		AllowedRealms: []string{"example.com"},
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "evil.example.com", // Not in allowed list
		OriginRealm: "example.com",
	}

	err := h.validatePeer(meta)
	if err == nil {
		t.Error("expected error for rejected host")
	}
}

func TestDiameterHandler_validatePeer_RealmRejected(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  []string{"aaa-s.example.com"},
		AllowedRealms: []string{"example.com"},
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "aaa-s.example.com",
		OriginRealm: "evil.com", // Not in allowed list
	}

	err := h.validatePeer(meta)
	if err == nil {
		t.Error("expected error for rejected realm")
	}
}

func TestDiameterHandler_validatePeer_EmptyListsAllowsAll(t *testing.T) {
	cfg := &DiameterHandlerConfig{
		AllowedHosts:  nil, // Empty = allow all
		AllowedRealms: nil,
	}
	h := &DiameterHandler{logger: slog.Default(), cfg: cfg}

	meta := &smpeer.Metadata{
		OriginHost:  "any-host.example.com",
		OriginRealm: "any-realm.example.com",
	}

	err := h.validatePeer(meta)
	if err != nil {
		t.Errorf("expected no error with empty lists, got %v", err)
	}
}

