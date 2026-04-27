// Package ausf provides AUSF (Authentication Server Function) client for
// N60 interface communication and MSK forwarding.
// REQ-08: internal/ausf/ created with ForwardMSK.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9.
package ausf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the AUSF N60 client for MSK forwarding.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new AUSF client.
func NewClient(cfg config.AUSFConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// ForwardMSK forwards the Master Session Key (MSK) to AUSF after EAP-TLS completion.
// REQ-08: AUSF N60 client with ForwardMSK operation.
// Spec: TS 29.526 §7.3.4 — AUSF receives MSK for key derivation.
func (c *Client) ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error {
	if c.baseURL == "" {
		return fmt.Errorf("ausf: baseURL not configured")
	}

	payload := map[string]interface{}{
		"authCtxId": authCtxID,
		"msk":       msk,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ausf: marshal msk: %w", err)
	}

	url := fmt.Sprintf("%s/nnssaaaf-aiw/v1/msk", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ausf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ausf: forward msk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ausf: unexpected status %d", resp.StatusCode)
	}
	return nil
}
