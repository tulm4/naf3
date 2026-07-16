// Package discovery provides NF discovery client for Biz Pod to discover
// NFs (UDM, AMF) via HTTP Gateway's internal discovery API.
//
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 2
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/nrf"
)

// NFDiscoveryClient is the interface for NF discovery.
// Allows swapping between HTTP Gateway discovery and direct NRF calls.
type NFDiscoveryClient interface {
	// FindNF discovers an NF instance by type and returns its profile.
	FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error)
}

// httpDiscoveryClient implements NFDiscoveryClient using HTTP Gateway's internal API.
type httpDiscoveryClient struct {
	baseURL string
	client  *http.Client
}

// NewClient creates a new HTTP-based NF discovery client.
func NewClient(baseURL string) NFDiscoveryClient {
	return &httpDiscoveryClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FindNF discovers an NF instance by type via HTTP Gateway's internal API.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §New Internal API
func (c *httpDiscoveryClient) FindNF(ctx context.Context, nfType string) (*nrf.NFProfile, error) {
	url := fmt.Sprintf("%s/internal/nf-discovery/%s", c.baseURL, nfType)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("discovery: read body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("discovery: %s not found", nfType)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery: unexpected status %d: %s", resp.StatusCode, body)
	}

	var profile nrf.NFProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("discovery: decode profile: %w", err)
	}

	return &profile, nil
}