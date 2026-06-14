package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
)

type Config struct {
	NRFBaseURL string
	UDMBaseURL string
	AMFBaseURL string
	RetryCfg   resilience.RetryConfig
	Timeout    time.Duration
}

type ProxyHandler struct {
	nrfClient *nfClient
	udmClient *nfClient
	amfClient *nfClient
}

type nfClient struct {
	baseURL   string
	httpClient *http.Client
	retryCfg   resilience.RetryConfig
}

func NewNFClient(baseURL string, retryCfg resilience.RetryConfig, timeout time.Duration) *nfClient {
	return &nfClient{
		baseURL:   baseURL,
		httpClient: &http.Client{Timeout: timeout},
		retryCfg:   retryCfg,
	}
}

func (c *nfClient) Do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var lastErr error
	var lastStatus int
	var lastBody []byte

	url := strings.TrimSuffix(c.baseURL, "/") + path

	err := resilience.Do(ctx, c.retryCfg, func() error {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			return err
		}
		defer resp.Body.Close()

		lastStatus = resp.StatusCode
		lastBody, _ = io.ReadAll(resp.Body)

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			return lastErr
		}
		return nil
	})

	if err != nil {
		return lastStatus, lastBody, lastErr
	}
	return lastStatus, lastBody, nil
}

func NewProxyHandler(cfg Config) *ProxyHandler {
	return &ProxyHandler{
		nrfClient: NewNFClient(cfg.NRFBaseURL, cfg.RetryCfg, cfg.Timeout),
		udmClient: NewNFClient(cfg.UDMBaseURL, cfg.RetryCfg, cfg.Timeout),
		amfClient: NewNFClient(cfg.AMFBaseURL, cfg.RetryCfg, cfg.Timeout),
	}
}

func (h *ProxyHandler) RegisterProxyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/internal/nrf/", h.handleNRFProxy)
	mux.HandleFunc("/internal/udm/", h.handleUDMProxy)
	mux.HandleFunc("/internal/amf/", h.handleAMFProxy)
}

func (h *ProxyHandler) handleNRFProxy(w http.ResponseWriter, r *http.Request) {
	h.proxyRequest(w, r, h.nrfClient)
}

func (h *ProxyHandler) handleUDMProxy(w http.ResponseWriter, r *http.Request) {
	h.proxyRequest(w, r, h.udmClient)
}

func (h *ProxyHandler) handleAMFProxy(w http.ResponseWriter, r *http.Request) {
	h.proxyRequest(w, r, h.amfClient)
}

func (h *ProxyHandler) proxyRequest(w http.ResponseWriter, r *http.Request, client *nfClient) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}

	path := extractProxyPath(r.URL.Path)
	status, respBody, err := client.Do(ctx, r.Method, path, body)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}

	w.WriteHeader(status)
	if len(respBody) > 0 {
		_, _ = w.Write(respBody)
	}
}

func extractProxyPath(path string) string {
	parts := strings.SplitN(path, "/", 4)
	if len(parts) >= 4 {
		return "/" + parts[3]
	}
	return path
}
