package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestReadKeepalivedState(t *testing.T) {
	// Test with non-existent file
	_, err := readKeepalivedState("/nonexistent/path")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReadKeepalivedState_ValidFile(t *testing.T) {
	// Create a temp file
	tmp, err := os.CreateTemp("", "keepalived-state")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	// Write some content
	tmp.WriteString("BACKUP\n")
	tmp.Close()

	state, err := readKeepalivedState(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	if state != "BACKUP" {
		t.Errorf("state: got %q, want %q", state, "BACKUP")
	}
}

func TestVIPHealthHandler_MissingStateFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	g := &Gateway{
		cfg: Config{
			KeepalivedStatePath: "/nonexistent/keepalived/state",
		},
		logger: logger,
	}

	req := httptest.NewRequest("GET", "/health/vip", nil)
	rec := httptest.NewRecorder()

	// Just verify it doesn't panic
	g.VIPHealthHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestStartVIPAware_DevModeNoStateFile(t *testing.T) {
	// When statePath is empty, should start immediately without polling
	gw := &Gateway{
		cfg: Config{
			KeepalivedStatePath: "",
			ListenRADIUS:       "",
			ListenDIAMETER:     "",
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

func TestStartVIPAware_DevModeDevNull(t *testing.T) {
	gw := &Gateway{
		cfg: Config{
			KeepalivedStatePath: "/dev/null",
			ListenRADIUS:       "",
			ListenDIAMETER:     "",
		},
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		wg:     sync.WaitGroup{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	started := gw.StartVIPAware(ctx, "/dev/null")
	if !started {
		t.Fatal("expected StartVIPAware to return true with /dev/null state path")
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
