package mocks

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !strings.Contains(body, "udm-mock") {
		t.Errorf("expected response to contain 'udm-mock', got: %s", body)
	}
}
