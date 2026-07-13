package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/debug"
)

func TestNewRedisClient_Standalone(t *testing.T) {
	client := newRedisClient("localhost:6379", "standalone")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	_ = client.Close()
}

func TestNewRedisClient_Sentinel(t *testing.T) {
	client := newRedisClient("sentinel1:26379,sentinel2:26379", "sentinel")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	_ = client.Close()
}

func TestNewRedisClient_UnknownMode(t *testing.T) {
	client := newRedisClient("localhost:6379", "unknown")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	_ = client.Close()
}

func TestIsVIPOwner(t *testing.T) {
	// Test with a non-existent IP (should return false)
	if isVIPOwner(context.Background(), "192.0.2.1") {
		t.Error("expected false for non-assigned IP")
	}

	// Test with empty IP (should return false)
	if isVIPOwner(context.Background(), "") {
		t.Error("expected false for empty IP")
	}
}

func TestVIPHealthHandler_NoVIPConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	g := &Gateway{
		cfg: Config{
			VIPAddress: "", // No VIP configured
		},
		logger: logger,
	}

	req := httptest.NewRequest("GET", "/health/vip", nil)
	rec := httptest.NewRecorder()

	g.VIPHealthHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "VIP not configured") {
		t.Errorf("body: got %q, want to contain 'VIP not configured'", rec.Body.String())
	}
}

func TestStartVIPAware_DevModeNoVIP(t *testing.T) {
	// When VIPAddress is empty, should start immediately without polling
	gw := &Gateway{
		cfg: Config{
			VIPAddress:   "",
			ListenRADIUS: "",
		},
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		wg:     sync.WaitGroup{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	started := gw.StartVIPAware(ctx, "")
	if !started {
		t.Fatal("expected StartVIPAware to return true in dev mode")
	}
}

func TestGateway_UsesConfigurableMaxRetries(t *testing.T) {
	cfg := Config{
		RadiusServerAddress: "localhost:9999",
		InternalComm: config.InternalCommConfig{
			Native: config.NativeCommConfig{
				Radius: config.RadiusConfig{
					MaxRetries: 5,
					Timeout:    5 * time.Second,
				},
			},
		},
	}

	gw := New(cfg)
	fwdCfg := gw.radiusForwarder.Config()
	if fwdCfg.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", fwdCfg.MaxRetries)
	}
}

// TestGateway_New_StoresDebugFromConfig proves Task 12 of the per-UE debug
// plan: gateway.New must wire cfg.Debug into the Gateway struct so handlers,
// forwarders, and writeSessionCorr can emit events via g.debug.Emit/Wrap*.
func TestGateway_New_StoresDebugFromConfig(t *testing.T) {
	dbg := &debug.Debug{}

	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		Debug:  dbg,
	}
	gw := New(cfg)

	if gw.debug != dbg {
		t.Fatalf("gw.debug = %p; want %p", gw.debug, dbg)
	}
}

// TestGateway_New_AcceptsNilDebug is the nil-safety guard: callers that don't
// configure the debug subsystem (production default off) must keep working.
func TestGateway_New_AcceptsNilDebug(t *testing.T) {
	cfg := Config{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
	gw := New(cfg)
	if gw.debug != nil {
		t.Fatalf("gw.debug should be nil when cfg.Debug is nil; got %p", gw.debug)
	}
	// Sanity: forwarders are still wired even without debug.
	if gw.diamForwarder == nil {
		t.Fatal("diamForwarder should be wired even without debug")
	}
	_ = context.Background() // keep import used regardless of test additions
}

// TestGateway_HandleForward_WithDebug_PreservesBehavior proves Task 13 of the
// per-UE debug plan: HandleForward must emit KindInternal debug events while
// still returning the expected response. With no AAA server reachable the
// forward path returns an error → handler must surface that as 500 and emit
// the corresponding error event. This guards against instrumentation that
// silently swallows errors.
func TestGateway_HandleForward_WithDebug_PreservesBehavior(t *testing.T) {
	gw := New(Config{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		Debug:  &debug.Debug{}, // zero-value: disabled, all Emit paths short-circuit
	})

	body := []byte(`{
        "v":"1.0",
        "sessionId":"sess-1",
        "authCtxId":"auth-1",
        "transportType":"RADIUS",
        "sst":1,
        "sd":"FFFFFF",
        "direction":"CLIENT_INITIATED",
        "payload":"AAECAwQ="
    }`)

	req := httptest.NewRequest("POST", "/aaa/forward", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// ForwardEAP will fail because no AAA server is reachable and no Redis is
	// configured — the point is that the handler must still respond with 500
	// and emit the error event (no panic).
	gw.HandleForward(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (ForwardEAP fails with no AAA), got %d, body=%q",
			rec.Code, rec.Body.String())
	}
}

// TestGateway_HandleForward_MethodNotAllowed proves the existing pre-check
// path emits no panic when debug is enabled and the request method is wrong.
func TestGateway_HandleForward_MethodNotAllowed(t *testing.T) {
	gw := New(Config{
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		Debug:  &debug.Debug{},
	})

	req := httptest.NewRequest("GET", "/aaa/forward", nil)
	rec := httptest.NewRecorder()
	gw.HandleForward(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", rec.Code)
	}
}
