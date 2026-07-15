package nrf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
)

// nilSafeFactory returns a Factory wired with a no-op resilience registry,
// suitable for tests that don't care about circuit breaker behavior.
func nilSafeFactory() *nfclient.Factory {
	return nfclient.NewFactory(nil)
}

// testInstanceID is a fixed UUID used as NF instance ID across integration tests.
// Kept as a constant to satisfy goconst while still being readable.
const testInstanceID = "550e8400-e29b-41d4-a716-446655440000"

func TestNRFClientWithAllComponents(t *testing.T) {
	cfg := config.NRFConfig{
		BaseURL:    "https://nrf.operator.com",
		CacheTTL:   5 * time.Minute,
		InstanceID: testInstanceID,
		AccessToken: config.TokenConfig{
			Enabled:      true,
			AuthServer:   "https://nrf.operator.com/oauth2/token",
			ClientID:     "nssAAF-client",
			ClientSecret: "secret",
			Scope:        "nnrf-nfm",
		},
	}

	client := NewClientWithConfig(cfg, nil)

	if client == nil {
		t.Fatal("NewClientWithConfig returned nil")
	}

	if client.TokenCache() == nil {
		t.Error("TokenCache should be initialized when AccessToken.Enabled is true")
	}

	if client.HeartbeatManager() != nil {
		t.Error("HeartbeatManager should not be initialized before SetProfilePath")
	}
}

func TestNRFClientWithoutToken(t *testing.T) {
	cfg := config.NRFConfig{
		BaseURL:    "https://nrf.operator.com",
		CacheTTL:   5 * time.Minute,
		InstanceID: testInstanceID,
		AccessToken: config.TokenConfig{
			Enabled: false,
		},
	}

	client := NewClientWithConfig(cfg, nil)

	if client.TokenCache() != nil {
		t.Error("TokenCache should be nil when AccessToken.Enabled is false")
	}
}

func TestNRFClientSetProfilePath(t *testing.T) {
	// Write a temporary YAML file to validate SetProfilePath.
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "nf-profile.yaml")

	yamlContent := `instanceId: "` + testInstanceID + `"
instanceName: "nssAAF-gw-001"
fqdn: "nssAAF.operator.com"
ipv4Addresses:
  - "10.0.1.50"
plmnList:
  - mcc: "208"
    mnc: "001"
nfServices:
  nnssaaf-nssaa:
    serviceInstanceId: "nnssaaf-nssaa-001"
    apiPrefix: "/nnssaaf-nssaa/v1"
    allowedNfTypes: ["AMF"]
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cfg := config.NRFConfig{
		BaseURL:  "https://nrf.example",
		CacheTTL: 5 * time.Minute,
	}
	client := NewClientWithConfig(cfg, nil)

	if err := client.SetProfilePath(yamlPath, config.HeartbeatConfig{
		InitialInterval:          30 * time.Second,
		AcceptNegotiatedInterval: true,
		MaxConsecutiveFailures:   3,
	}); err != nil {
		t.Fatalf("SetProfilePath failed: %v", err)
	}

	if client.HeartbeatManager() == nil {
		t.Error("HeartbeatManager should be initialized after SetProfilePath")
	}
	if got := client.NFInstanceID(); got != testInstanceID {
		t.Errorf("NFInstanceID() = %q, want %s", got, testInstanceID)
	}
}

func TestNRFClientSetProfilePathMissingFile(t *testing.T) {
	cfg := config.NRFConfig{BaseURL: "https://nrf.example"}
	client := NewClientWithConfig(cfg, nil)
	err := client.SetProfilePath("/nonexistent/path.yaml", config.HeartbeatConfig{})
	if err == nil {
		t.Error("expected error for missing YAML file")
	}
}

func TestNRFClientStartHeartbeatWithoutManager(t *testing.T) {
	cfg := config.NRFConfig{BaseURL: "https://nrf.example"}
	client := NewClientWithConfig(cfg, nil)
	err := client.StartHeartbeat(context.Background())
	if err == nil {
		t.Error("StartHeartbeat should fail when heartbeat manager is nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error message should mention initialization, got: %v", err)
	}
}

func TestNRFClientHeartbeatIntegration(t *testing.T) {
	// End-to-end test for register -> heartbeat -> deregister using a mock NRF
	// that returns a new etag on heartbeat.
	var (
		registerCount   int
		heartbeatCount  int
		deregisterCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			registerCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"heartBeatTimer": 60,
				"etag":           `"v1"`,
			})
		case http.MethodPatch:
			heartbeatCount++
			// Verify If-Match header is sent
			if r.Header.Get("If-Match") == "" {
				t.Error("If-Match header should be set on heartbeat")
			}
			if r.Header.Get("Content-Type") != "application/json-patch+json" {
				t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			deregisterCount++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:  server.URL,
		CacheTTL: 5 * time.Minute,
	}
	client := NewClientWithConfig(cfg, nil)

	// nil factory panics in Register/Heartbeat/Deregister since they go through
	// c.factory.Do. Provide a real factory for this end-to-end test.
	client.factory = nilSafeFactory()

	interval, etag, err := client.Register(context.Background(), nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if interval != 60*time.Second {
		t.Errorf("heartbeat interval = %v, want 60s", interval)
	}
	if etag != `"v1"` {
		t.Errorf("etag = %q, want %q", etag, `"v1"`)
	}

	etag2, err := client.Heartbeat(context.Background(), client.NFInstanceID(), etag)
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	if etag2 != "" {
		t.Logf("new etag from heartbeat: %q", etag2)
	}

	if err := client.Deregister(context.Background(), client.NFInstanceID()); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}

	if registerCount != 1 {
		t.Errorf("registerCount = %d, want 1", registerCount)
	}
	if heartbeatCount != 1 {
		t.Errorf("heartbeatCount = %d, want 1", heartbeatCount)
	}
	if deregisterCount != 1 {
		t.Errorf("deregisterCount = %d, want 1", deregisterCount)
	}
}

func TestNRFClientProfileBuilder(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "nf-profile.yaml")

	yamlContent := `instanceId: "test-id-001"
instanceName: "nssAAF-test"
fqdn: "nssAAF.test.example"
ipv4Addresses:
  - "10.0.0.1"
plmnList:
  - mcc: "001"
    mnc: "01"
nfServices:
  nnssaaf-nssaa:
    serviceInstanceId: "svc-1"
    apiPrefix: "/nnssaaf-nssaa/v1"
    allowedNfTypes: ["AMF"]
  nnssaaf-aiw:
    serviceInstanceId: "svc-2"
    apiPrefix: "/nnssaaf-aiw/v1"
    allowedNfTypes: ["AUSF"]
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	pb := &ProfileBuilder{yamlPath: yamlPath}
	profile, err := pb.LoadFromYAML(90)
	if err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	if profile.NFInstanceID != "test-id-001" {
		t.Errorf("NFInstanceID = %q", profile.NFInstanceID)
	}
	if profile.HeartBeatTimer != 90 {
		t.Errorf("HeartBeatTimer = %d, want 90", profile.HeartBeatTimer)
	}
	if len(profile.NfServices) != 2 {
		t.Errorf("NfServices count = %d, want 2", len(profile.NfServices))
	}
}
