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
			VIPAddress:     "",
			ListenRADIUS:   "",
			ListenDIAMETER: "",
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
