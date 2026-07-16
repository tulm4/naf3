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
	"github.com/operator/nssAAF/internal/discovery"
	"github.com/operator/nssAAF/internal/nfclient"
)

// Client is the UDM Nudm_UECM client.
// REQ-04: Nudm_UECM_Get wired to N58 handler — gates AAA routing.
// REQ-05: Nudm_UECM_UpdateAuthContext called after EAP completion.
type Client struct {
	baseURL string
	disc    discovery.NFDiscoveryClient
	factory *nfclient.Factory
}

// NewClient creates a new UDM client.
// discoveryClient is used for on-demand NF discovery when baseURL is not configured.
func NewClient(cfg config.UDMConfig, factory *nfclient.Factory, discoveryClient discovery.NFDiscoveryClient) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		disc:    discoveryClient,
		factory: factory,
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
	if c.disc == nil {
		return "", errors.New("udm: no baseURL and no discovery client configured")
	}

	// Discover UDM via HTTP Gateway's internal discovery API.
	// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 3
	profile, err := c.disc.FindNF(ctx, "UDM")
	if err != nil {
		return "", fmt.Errorf("udm: discover UDM: %w", err)
	}

	// Extract base URL from NF profile.
	// Prefer first IPv4 address + first IPEndPoint.
	if len(profile.NfServices) == 0 {
		return "", errors.New("udm: no services in UDM profile")
	}

	svc := profile.NfServices[0]
	if len(svc.IPEndPoints) == 0 {
		return "", errors.New("udm: no IP endpoints in UDM service")
	}

	ep := svc.IPEndPoints[0]
	ip := ep.IPv4Address
	if ip == "" {
		return "", errors.New("udm: no IPv4 address in UDM endpoint")
	}

	port := ep.Port
	if port == 0 {
		port = 443 // Default HTTPS port
	}

	scheme := svc.Scheme
	if scheme == "" {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, ip, port), nil
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
