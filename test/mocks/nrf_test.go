package mocks

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNRFMock_SetServiceEndpoint(t *testing.T) {
	m := NewNRFMock()
	defer m.Close()

	m.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)

	req := httptest.NewRequest(http.MethodGet, m.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem", nil)
	resp := httptest.NewRecorder()
	m.Server.Config.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	if !contains(body, "udm-mock") {
		t.Errorf("expected response to contain 'udm-mock', got: %s", body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
