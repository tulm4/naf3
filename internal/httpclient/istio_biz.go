package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/operator/nssAAF/internal/proto"
)

// istioBizClient delegates resilience to Istio sidecar.
// Minimal client - Istio handles retries, circuit breaking, mTLS.
type istioBizClient struct {
	baseURL string
	client  *http.Client
}

func newIstioBizClient(baseURL string) *istioBizClient {
	return &istioBizClient{
		baseURL: baseURL,
		client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// ForwardRequest delegates to Istio sidecar for resilience.
func (c *istioBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string, gpsi string, supi string) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if gpsi != "" {
		req.Header.Set("X-NSSAA-GPSI", gpsi)
	}
	if supi != "" {
		req.Header.Set("X-NSSAA-SUPI", supi)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 504, context.DeadlineExceeded
		}
		return nil, 503, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*istioBizClient)(nil)
