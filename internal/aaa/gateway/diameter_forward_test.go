// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
package gateway

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
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

// fakeNotifierConn is a minimal diam.Conn implementation that also satisfies
// diam.CloseNotifier so it can drive watchDisconnect in unit tests without a
// live network connection.
type fakeNotifierConn struct {
	notifyCh chan struct{}
	closed   int32
	ctxMu    sync.RWMutex
	ctx      context.Context
}

func newFakeNotifierConn() *fakeNotifierConn {
	f := &fakeNotifierConn{notifyCh: make(chan struct{})}
	f.ctx = context.Background()
	return f
}

func (f *fakeNotifierConn) CloseNotify() <-chan struct{} { return f.notifyCh }

func (f *fakeNotifierConn) Write(b []byte) (int, error) {
	if atomic.LoadInt32(&f.closed) != 0 {
		return 0, io.EOF
	}
	return len(b), nil
}

func (f *fakeNotifierConn) WriteStream(b []byte, stream uint) (int, error) {
	return f.Write(b)
}

func (f *fakeNotifierConn) Close()                    { atomic.StoreInt32(&f.closed, 1); close(f.notifyCh) }
func (f *fakeNotifierConn) LocalAddr() net.Addr       { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3868} }
func (f *fakeNotifierConn) RemoteAddr() net.Addr      { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3869} }
func (f *fakeNotifierConn) TLS() *tls.ConnectionState { return nil }
func (f *fakeNotifierConn) Dictionary() *dict.Parser   { return dict.Default }
func (f *fakeNotifierConn) Context() context.Context {
	f.ctxMu.RLock()
	defer f.ctxMu.RUnlock()
	return f.ctx
}
func (f *fakeNotifierConn) SetContext(ctx context.Context) {
	f.ctxMu.Lock()
	defer f.ctxMu.Unlock()
	f.ctx = ctx
}
func (f *fakeNotifierConn) Connection() net.Conn { return nil }

// TestDiamForwarder_WatchDisconnect_NilsConnOnCloseNotify verifies that when
// the underlying socket signals CloseNotify, watchDisconnect clears df.conn
// and df.connected so monitorConnection will reconnect.
func TestDiamForwarder_WatchDisconnect_NilsConnOnCloseNotify(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
	)

	fake := newFakeNotifierConn()
	df.mu.Lock()
	df.conn = fake
	df.connected = true
	df.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		df.watchDisconnect(ctx)
		close(done)
	}()

	// Simulate peer-initiated close.
	fake.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchDisconnect did not return after CloseNotify")
	}

	df.mu.RLock()
	defer df.mu.RUnlock()
	if df.conn != nil {
		t.Errorf("expected df.conn to be nil after peer close, got %T", df.conn)
	}
	if df.connected {
		t.Error("expected df.connected=false after peer close")
	}
}

// TestDiamForwarder_WatchDisconnect_NoOpWhenConnNil is a safety check:
// when df.conn is nil watchDisconnect returns without panicking. This
// happens when monitorConnection has already finished reconnect setup
// or Close() was invoked on the forwarder.
func TestDiamForwarder_WatchDisconnect_NoOpWhenConnNil(t *testing.T) {
	df := newDiamForwarder(
		"localhost:3868",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
	)
	// df.conn is nil by default; the function must return immediately.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	df.watchDisconnect(ctx)
}

// TestDiamForwarder_GetConn_AfterDisconnect_SyncReconnectAttempt verifies
// that when df.conn is nil and the server is unreachable, getConn returns
// an error instead of blocking. This guards the synchronous-reconnect path
// used by Forward().
func TestDiamForwarder_GetConn_AfterDisconnect_SyncReconnectAttempt(t *testing.T) {
	df := newDiamForwarder(
		"127.0.0.1:1", // nothing listens here — DialNetwork must fail
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
		nil, // forwardToBiz — tests don't exercise server-initiated path
		nil, // registry
	)

	if _, err := df.getConn(); err == nil {
		t.Fatal("expected error from getConn when server unreachable, got nil")
	}
}

// TestDiamForwarder_ASR_FiresOnForwarderMachine verifies the architectural
// migration: an inbound ASR on the gateway's outbound TCP socket fires
// handleASR (now registered on diamForwarder.machine, not on a separate
// DiameterHandler state machine bound to an inbound listener).
func TestDiamForwarder_ASR_FiresOnForwarderMachine(t *testing.T) {
	registry := NewServerInitiatedRegistry(30 * time.Second)
	var forwarded []byte
	var forwardedMu sync.Mutex
	forwardToBiz := func(ctx context.Context, sessionID, transportType, messageType string, raw []byte) {
		forwardedMu.Lock()
		forwarded = raw
		forwardedMu.Unlock()
		resp := &ServerInitiatedResponse{ResultCode: 2001}
		registry.Complete(sessionID, "ASR", resp)
	}

	df := newDiamForwarder(
		"127.0.0.1:1",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.Default(),
		forwardToBiz,
		registry,
	)

	fake := newFakeNotifierConn()
	// The state machine's handshakeOK wrapper only invokes non-CER/CEA handlers
	// when a peer is associated with the connection context. Synthesize that
	// peer so the ASR handler actually fires.
	fake.SetContext(smpeer.NewContext(fake.Context(), &smpeer.Metadata{
		OriginHost:   datatype.DiameterIdentity("aaa-server.example.com"),
		OriginRealm:  datatype.DiameterIdentity("example.com"),
		Applications: []uint32{df.cfg.AuthApplicationID},
	}))

	m := diam.NewRequest(diam.AbortSession, df.cfg.AuthApplicationID, dict.Default)
	_, _ = m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String("sess-1"))
	m.Header.HopByHopID = 1
	m.Header.EndToEndID = 2

	df.machine.ServeDIAM(fake, m)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		forwardedMu.Lock()
		done := len(forwarded) > 0
		forwardedMu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	forwardedMu.Lock()
	forwardedLen := len(forwarded)
	forwardedMu.Unlock()
	if forwardedLen == 0 {
		t.Fatal("forwardToBiz was never called by ASR handler")
	}
}
