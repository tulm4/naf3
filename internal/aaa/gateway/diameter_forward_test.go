// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
package gateway

import (
	"log/slog"
	"testing"
	"time"
)

// DefaultConfig returns a default diamForwarderConfig with spec-compliant values.
func DefaultConfig() *diamForwarderConfig {
	return &diamForwarderConfig{
		AuthRequestType:   2, // AUTHORIZE_AUTHENTICATE (RFC 4072)
		AuthApplicationID: AppIDAAP, // 5 (Diameter EAP)
	}
}

func TestDiamForwarder_OriginStateId_InitialValue(t *testing.T) {
	cfg := DefaultConfig()
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	// Initial value should be 0 before increment
	if id := df.getOriginStateID(); id != 0 {
		t.Errorf("expected initial OriginStateID=0, got %d", id)
	}
}

func TestDiamForwarder_OriginStateId_Increments(t *testing.T) {
	cfg := DefaultConfig()
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	id1 := df.incrementOriginStateID()
	if id1 != 1 {
		t.Errorf("expected first increment to return 1, got %d", id1)
	}

	id2 := df.incrementOriginStateID()
	if id2 != 2 {
		t.Errorf("expected second increment to return 2, got %d", id2)
	}

	current := df.getOriginStateID()
	if current != 2 {
		t.Errorf("expected current OriginStateID=2, got %d", current)
	}
}

func TestDiamForwarder_OriginStateId_ConcurrentAccess(t *testing.T) {
	cfg := DefaultConfig()
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	done := make(chan struct{})
	iterations := 100

	go func() {
		for i := 0; i < iterations; i++ {
			df.incrementOriginStateID()
		}
		close(done)
	}()

	for i := 0; i < iterations; i++ {
		df.incrementOriginStateID()
	}

	<-done

	expected := uint64(iterations * 2)
	if got := df.getOriginStateID(); got != expected {
		t.Errorf("expected OriginStateID=%d after concurrent increments, got %d", expected, got)
	}
}

func TestDiamForwarder_AuthRequestType_Default(t *testing.T) {
	cfg := DefaultConfig()
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	// Default should be 2 (AUTHORIZE_AUTHENTICATE)
	if df.cfg.AuthRequestType != 2 {
		t.Errorf("expected default AuthRequestType=2, got %d", df.cfg.AuthRequestType)
	}
}

func TestDiamForwarder_AuthRequestType_Configurable(t *testing.T) {
	cfg := &diamForwarderConfig{
		AuthRequestType:   3, // AUTHORIZE_ONLY
		AuthApplicationID: AppIDAAP,
	}
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	if df.cfg.AuthRequestType != 3 {
		t.Errorf("expected AuthRequestType=3, got %d", df.cfg.AuthRequestType)
	}
}

func TestDiamForwarder_AuthApplicationId_Default(t *testing.T) {
	cfg := DefaultConfig()
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	// Default should be 5 (Diameter EAP)
	if df.cfg.AuthApplicationID != 5 {
		t.Errorf("expected default AuthApplicationID=5, got %d", df.cfg.AuthApplicationID)
	}
}

func TestDiamForwarder_AuthApplicationId_Configurable(t *testing.T) {
	cfg := &diamForwarderConfig{
		AuthRequestType:   2,
		AuthApplicationID: 6, // IMS
	}
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	if df.cfg.AuthApplicationID != 6 {
		t.Errorf("expected AuthApplicationID=6, got %d", df.cfg.AuthApplicationID)
	}
}

func TestDiamForwarder_ZeroAuthRequestType_DefaultsToAuthorizeAuthenticate(t *testing.T) {
	cfg := &diamForwarderConfig{
		AuthRequestType:   0, // Zero should trigger default
		AuthApplicationID: AppIDAAP,
	}
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	// Zero should default to 2 (AUTHORIZE_AUTHENTICATE)
	if df.cfg.AuthRequestType != 2 {
		t.Errorf("expected default AuthRequestType=2 for zero input, got %d", df.cfg.AuthRequestType)
	}
}

func TestDiamForwarder_ZeroAuthApplicationId_DefaultsToDiameterEAP(t *testing.T) {
	cfg := &diamForwarderConfig{
		AuthRequestType:   2,
		AuthApplicationID: 0, // Zero should trigger default
	}
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		cfg,
		slog.Default(),
	)

	// Zero should default to AppIDAAP (5 - Diameter EAP)
	if df.cfg.AuthApplicationID != AppIDAAP {
		t.Errorf("expected default AuthApplicationID=%d for zero input, got %d", AppIDAAP, df.cfg.AuthApplicationID)
	}
}

func TestDiamForwarder_GetConnectionStats(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	stats := df.GetConnectionStats()

	if !stats.ConnectedAt.IsZero() {
		t.Error("ConnectedAt should be zero before Connect() is called")
	}
	if stats.MessagesSent != 0 {
		t.Errorf("expected 0 MessagesSent, got %d", stats.MessagesSent)
	}
	if stats.MessagesRecv != 0 {
		t.Errorf("expected 0 MessagesRecv, got %d", stats.MessagesRecv)
	}
}

func TestDiamForwarder_recordDWA(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	time.Sleep(10 * time.Millisecond)
	df.recordDWA()

	stats := df.GetConnectionStats()
	if stats.LastDWA.IsZero() {
		t.Error("LastDWA should be set after recordDWA()")
	}
}

func TestDiamForwarder_recordDWR(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	df.recordDWR()

	stats := df.GetConnectionStats()
	if stats.LastDWR.IsZero() {
		t.Error("LastDWR should be set after recordDWR()")
	}
}

func TestDiamForwarder_incrementMessagesSent(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	df.incrementMessagesSent()
	df.incrementMessagesSent()

	stats := df.GetConnectionStats()
	if stats.MessagesSent != 2 {
		t.Errorf("expected 2 messages sent, got %d", stats.MessagesSent)
	}
}

func TestDiamForwarder_incrementMessagesRecv(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	df.incrementMessagesRecv()
	df.incrementMessagesRecv()
	df.incrementMessagesRecv()

	stats := df.GetConnectionStats()
	if stats.MessagesRecv != 3 {
		t.Errorf("expected 3 messages received, got %d", stats.MessagesRecv)
	}
}

func TestDiamForwarder_ConnectionStats_ConcurrentAccess(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
	)

	done := make(chan struct{})
	iterations := 100

	go func() {
		for i := 0; i < iterations; i++ {
			df.incrementMessagesSent()
			df.recordDWR()
		}
		close(done)
	}()

	for i := 0; i < iterations; i++ {
		df.incrementMessagesRecv()
		df.recordDWA()
	}

	<-done

	stats := df.GetConnectionStats()
	if stats.MessagesSent != uint64(iterations) {
		t.Errorf("expected MessagesSent=%d, got %d", iterations, stats.MessagesSent)
	}
	if stats.MessagesRecv != uint64(iterations) {
		t.Errorf("expected MessagesRecv=%d, got %d", iterations, stats.MessagesRecv)
	}
	if stats.LastDWR.IsZero() {
		t.Error("LastDWR should be set after concurrent recordDWR() calls")
	}
	if stats.LastDWA.IsZero() {
		t.Error("LastDWA should be set after concurrent recordDWA() calls")
	}
}
