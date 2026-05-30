// Package udm provides UDM (Unified Data Management) client for
// subscription data retrieval via N59 interface.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9.
package udm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/nrf"
)

// Client is the UDM Nudm_UECM client.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
type Client struct {
	baseURL   string
	nrfClient *nrf.Client
	factory   *nfclient.Factory
}

// NewClient creates a new UDM client.
func NewClient(cfg config.UDMConfig, factory *nfclient.Factory, nrfClient *nrf.Client) *Client {
	return &Client{
		baseURL:   cfg.BaseURL,
		nrfClient: nrfClient,
		factory:   factory,
	}
}

// AuthSubscription represents the auth context from UDM.
// Spec: TS 29.526 §7.3 / docs/design/05_nf_profile.md §3.2.
type AuthSubscription struct {
	AuthType  string `json:"authType"`  // "EAP_TLS", "EAP_AKA_PRIME"
	AAAServer string `json:"aaaServer"` // e.g. "radius://aaa.operator.com:1812"
}

func (c *Client) doRequest(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
	return c.factory.Do(ctx, baseURL, method, path, body)
}

func (c *Client) discoverBaseURL(ctx context.Context, supi string) (string, error) {
	if c.baseURL != "" {
		return c.baseURL, nil
	}
	if c.nrfClient == nil {
		return "", errors.New("udm: no baseURL and no NRF client configured")
	}
	plmn := extractPLMNFromSupi(supi)
	return c.nrfClient.DiscoverUDM(ctx, plmn)
}

// GetAuthContext calls Nudm_UECM_Get to retrieve auth subscription for a SUPI.
// REQ-04: Called before AAA routing to determine EAP method and AAA server.
// Spec: TS 29.526 §7.3.2, TS 23.502 §4.2.9.2 step 2.
// Returns interface{} to satisfy nssaa.WithUDMClient interface{GetAuthContext(...)(interface{}, error).
func (c *Client) GetAuthContext(ctx context.Context, supi string) (interface{}, error) {
	baseURL, err := c.discoverBaseURL(ctx, supi)
	if err != nil {
		return nil, fmt.Errorf("udm: discover: %w", err)
	}
	path := fmt.Sprintf("/nudm-uem/v1/subscribers/%s/auth-contexts", supi)

	status, body, err := c.doRequest(ctx, baseURL, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("udm: get auth context: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("udm: subscriber %s not found", supi)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("udm: unexpected status %d", status)
	}

	var result struct {
		AuthContexts []AuthSubscription `json:"authContexts"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
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
func (c *Client) UpdateAuthContext(ctx context.Context, supi, authCtxID, status string) error {
	baseURL, err := c.discoverBaseURL(ctx, supi)
	if err != nil {
		return fmt.Errorf("udm: discover: %w", err)
	}
	path := fmt.Sprintf("/nudm-uem/v1/subscribers/%s/auth-contexts/%s", supi, authCtxID)

	payload := map[string]string{"authResult": status}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("udm: marshal update payload: %w", err)
	}

	statusCode, _, err := c.doRequest(ctx, baseURL, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("udm: update auth context: %w", err)
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("udm: update status %d", statusCode)
	}
	return nil
}

// extractPLMNFromSupi extracts PLMN from SUPI format: imsi-{mcc}{mnc}{rest}.
// e.g. imsi-208001000000000 → "208001"
func extractPLMNFromSupi(supi string) string {
	if len(supi) < 11 {
		return "208001" // default PLMN — SUPI too short for PLMN extraction
	}
	return supi[5:11] // "imsi-" = 5 chars, next 6 = MCC+MNC
}
