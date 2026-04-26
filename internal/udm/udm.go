// Package udm provides UDM (Unified Data Management) client for
// subscription data retrieval via N59 interface.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9.
package udm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nrf"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the UDM Nudm_UECM client.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
type Client struct {
	baseURL    string
	nrfClient  *nrf.Client
	httpClient *http.Client
}

// NewClient creates a new UDM client.
func NewClient(cfg config.UDMConfig, nrfClient *nrf.Client) *Client {
	return &Client{
		baseURL:   cfg.BaseURL,
		nrfClient: nrfClient,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// AuthSubscription represents the auth context from UDM.
// Spec: TS 29.526 §7.3 / docs/design/05_nf_profile.md §3.2.
type AuthSubscription struct {
	AuthType  string `json:"authType"`  // "EAP_TLS", "EAP_AKA_PRIME"
	AAAServer string `json:"aaaServer"` // e.g. "radius://aaa.operator.com:1812"
}

// GetAuthContext calls Nudm_UECM_Get to retrieve auth subscription for a SUPI.
// REQ-04: Called before AAA routing to determine EAP method and AAA server.
// Spec: TS 29.526 §7.3.2, TS 23.502 §4.2.9.2 step 2.
// Returns interface{} to satisfy nssaa.WithUDMClient interface{GetAuthContext(...)(interface{}, error)}.
func (c *Client) GetAuthContext(ctx context.Context, supi string) (interface{}, error) {
	baseURL := c.baseURL
	if baseURL == "" && c.nrfClient != nil {
		plmn := extractPLMNFromSupi(supi)
		udmEndpoint, err := c.nrfClient.DiscoverUDM(ctx, plmn)
		if err != nil {
			return nil, fmt.Errorf("udm: discover via nrf: %w", err)
		}
		baseURL = udmEndpoint
	}

	url := fmt.Sprintf("%s/nudm-uem/v1/subscribers/%s/auth-contexts", baseURL, supi)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("udm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("udm: get auth context: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("udm: subscriber %s not found", supi)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("udm: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		AuthContexts []AuthSubscription `json:"authContexts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("udm: decode response: %w", err)
	}
	if len(result.AuthContexts) == 0 {
		return nil, fmt.Errorf("udm: no auth contexts found for %s", supi)
	}
	return &result.AuthContexts[0], nil
}

// UpdateAuthContext calls Nudm_UECM_UpdateAuthContext to update auth status.
// REQ-05: Called after EAP completion to update auth context in UDM.
// Spec: TS 29.526 §7.3.3.
func (c *Client) UpdateAuthContext(ctx context.Context, supi, authCtxId, status string) error {
	baseURL := c.baseURL
	if baseURL == "" && c.nrfClient != nil {
		plmn := extractPLMNFromSupi(supi)
		udmEndpoint, err := c.nrfClient.DiscoverUDM(ctx, plmn)
		if err != nil {
			return fmt.Errorf("udm: discover via nrf: %w", err)
		}
		baseURL = udmEndpoint
	}

	url := fmt.Sprintf("%s/nudm-uem/v1/subscribers/%s/auth-contexts/%s", baseURL, supi, authCtxId)
	payload := map[string]string{"authResult": status}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("udm: marshal update payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("udm: create update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("udm: update auth context: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("udm: update status %d", resp.StatusCode)
	}
	return nil
}

// extractPLMNFromSupi extracts PLMN from SUPI format: imu-{mcc}{mnc}{rest}.
// e.g. imu-208001000000000 → "208001"
func extractPLMNFromSupi(supi string) string {
	if len(supi) >= 10 {
		return supi[4:10] // "imu-" = 4 chars, next 6 = MCC+MNC
	}
	return "208001" // default PLMN
}
