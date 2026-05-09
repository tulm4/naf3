package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/operator/nssAAF/internal/proto"
)

// istioAAAClient delegates resilience to Istio sidecar.
// Minimal client - Istio handles retries, circuit breaking, mTLS.
type istioAAAClient struct {
	aaaGatewayURL string
	client       *http.Client
}

func newIstioAAAClient(aaaGatewayURL string) *istioAAAClient {
	return &istioAAAClient{
		aaaGatewayURL: aaaGatewayURL,
		client:        http.DefaultClient,
	}
}

// ForwardEAP delegates to Istio sidecar for resilience.
func (c *istioAAAClient) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.aaaGatewayURL + "/aaa/forward"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("aaa gateway unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aaa gateway returned %d", resp.StatusCode)
	}

	var fwdResp proto.AaaForwardResponse
	if err := json.NewDecoder(resp.Body).Decode(&fwdResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &fwdResp, nil
}

var _ proto.BizAAAClient = (*istioAAAClient)(nil)
