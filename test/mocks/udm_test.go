package mocks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUDMMock_SetAuthSubscription(t *testing.T) {
	m := NewUDMMock()
	defer m.Close()

	m.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://aaa-gateway:1812")

	req := httptest.NewRequest(http.MethodGet, m.URL()+"/nudm-uem/v1/subscribers/imsi-208046000000001/auth-contexts", nil)
	resp := httptest.NewRecorder()
	m.Server.Config.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	body := resp.Body.String()
	if !strings.Contains(body, "EAP_TLS") {
		t.Errorf("expected response to contain 'EAP_TLS', got: %s", body)
	}
}
