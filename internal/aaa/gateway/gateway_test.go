package gateway

import (
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
