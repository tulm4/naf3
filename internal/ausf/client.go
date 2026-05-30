// Package ausf provides AUSF (Authentication Server Function) client for
// N60 interface communication and MSK forwarding.
// REQ-08: internal/ausf/ created with ForwardMSK.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9.
package ausf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
)

// Client is the AUSF N60 client for MSK forwarding.
type Client struct {
	baseURL string
	factory *nfclient.Factory
}

// NewClient creates a new AUSF client.
func NewClient(cfg config.AUSFConfig, factory *nfclient.Factory) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		factory: factory,
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return c.factory.Do(ctx, c.baseURL, method, path, body)
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
	status, _, err := c.doRequest(ctx, http.MethodPost, "/nnssaaaf-aiw/v1/msk", body)
	if err != nil {
		return fmt.Errorf("ausf: forward msk: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("ausf: unexpected status %d", status)
	}
	return nil
}
