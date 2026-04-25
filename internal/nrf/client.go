package nrf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client is the NRF service discovery client.
// REQ-01: NRF registration on startup (degraded mode, D-04).
// REQ-02: Heartbeat every 5 minutes.
// REQ-03: Discovery with 5-min TTL cache.
type Client struct {
	baseURL      string
	httpClient   *http.Client
	nfInstanceID string
	cache        *NRFDiscoveryCache
	registered   atomic.Bool
}

// NRFDiscoveryCache holds cached NF discovery results with 5-min TTL.
// Cache keys per docs/design/05_nf_profile.md §3.3:
//   - "udm:uem:{plmnId}" → UDM Nudm_UECM endpoint
//   - "amf:{amfId}" → AMF profile
type NRFDiscoveryCache struct {
	mu    sync.RWMutex
	cache map[string]*cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

func (c *NRFDiscoveryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

func (c *NRFDiscoveryCache) Set(key string, data interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]*cacheEntry)
	}
	c.cache[key] = &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// NFProfile is the NSSAAF NF profile for NRF registration.
// Spec: TS 29.510 §6 — fields from docs/design/05_nf_profile.md §2.2.
type NFProfile struct {
	NFInstanceID   string `json:"nfInstanceId"`
	NFType        string `json:"nfType"` // "NSSAAF"
	NFStatus      string `json:"nfStatus"`
	HeartBeatTimer int   `json:"heartBeatTimer"`
	Load          int    `json:"load"`
}

// NewClient creates a new NRF client.
func NewClient(cfg config.NRFConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: cfg.DiscoverTimeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		nfInstanceID: fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano()),
		cache: &NRFDiscoveryCache{
			ttl: 5 * time.Minute,
		},
	}
}

// RegisterAsync registers the NSSAAF profile with NRF in a background goroutine.
// REQ-01 / D-04: Returns immediately (degraded mode), retries in background.
func (c *Client) RegisterAsync(ctx context.Context) {
	go func() {
		backoff := time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := c.Register(ctx); err != nil {
				slog.Warn("nrf registration failed, retrying",
					"error", err,
					"backoff", backoff,
				)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}

			slog.Info("nrf registration successful",
				"nf_instance_id", c.nfInstanceID,
			)
			c.registered.Store(true)
			return
		}
	}()
}

// Register sends Nnrf_NFRegistration to the NRF.
// REQ-01: POST /nnrf-disc/v1/nf-instances with NFProfile.
func (c *Client) Register(ctx context.Context) error {
	profile := NFProfile{
		NFInstanceID:   c.nfInstanceID,
		NFType:        "NSSAAF",
		NFStatus:      "REGISTERED",
		HeartBeatTimer: 300,
		Load:          0,
	}
	body, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("nrf: marshal profile: %w", err)
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("nrf: unexpected status %d", resp.StatusCode)
	}
	c.registered.Store(true)
	return nil
}

// Heartbeat sends Nnrf_NFHeartBeat every 5 minutes.
// REQ-02: PUT /nnrf-disc/v1/nf-instances/{id} with nfStatus="REGISTERED", heartBeatTimer=300, load=0-100.
func (c *Client) Heartbeat(ctx context.Context) error {
	payload := map[string]interface{}{
		"nfInstanceId":   c.nfInstanceID,
		"nfStatus":      "REGISTERED",
		"heartBeatTimer": 300,
		"load":          0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nrf: marshal heartbeat: %w", err)
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, c.nfInstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf: create heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: heartbeat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nrf: heartbeat status %d", resp.StatusCode)
	}
	return nil
}

// StartHeartbeat runs the heartbeat goroutine every 5 minutes.
func (c *Client) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Heartbeat(ctx); err != nil {
				slog.Warn("nrf heartbeat failed", "error", err)
			}
		}
	}
}

// DiscoverUDM discovers a UDM that exposes the nudm-uem service.
// REQ-03 / docs/design/05_nf_profile.md §3.2.
func (c *Client) DiscoverUDM(ctx context.Context, plmnId string) (string, error) {
	key := fmt.Sprintf("udm:uem:%s", plmnId)
	if endpoint, ok := c.cache.Get(key); ok {
		return endpoint.(string), nil
	}
	// NRF discovery query
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: create discovery request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nrf: discover udm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nrf: discover udm status %d", resp.StatusCode)
	}
	var result struct {
		NFInstances []struct {
			NFServices map[string]struct {
				IPEndPoints []struct {
					IPv4Address string `json:"ipv4Address"`
					Port        int    `json:"port"`
				} `json:"ipEndPoints"`
			} `json:"nfServices"`
		} `json:"nfInstances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("nrf: decode discovery: %w", err)
	}
	// Extract first UDM's nudm-uem endpoint
	for _, inst := range result.NFInstances {
		if svc, ok := inst.NFServices["nudm-uem"]; ok {
			for _, ep := range svc.IPEndPoints {
				endpoint := fmt.Sprintf("http://%s:%d", ep.IPv4Address, ep.Port)
				c.cache.Set(key, endpoint)
				return endpoint, nil
			}
		}
	}
	return "", fmt.Errorf("nrf: no UDM found for plmnId %s", plmnId)
}

// DiscoverAMF discovers an AMF by instance ID.
// REQ-03 / docs/design/05_nf_profile.md §3.1.
func (c *Client) DiscoverAMF(ctx context.Context, amfId string) (string, error) {
	key := fmt.Sprintf("amf:%s", amfId)
	if endpoint, ok := c.cache.Get(key); ok {
		return endpoint.(string), nil
	}
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, amfId)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: create amf request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nrf: discover amf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nrf: discover amf status %d", resp.StatusCode)
	}
	var amf struct {
		NFInstanceID string `json:"nfInstanceId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&amf); err != nil {
		return "", fmt.Errorf("nrf: decode amf: %w", err)
	}
	c.cache.Set(key, amf.NFInstanceID)
	return amf.NFInstanceID, nil
}

// Deregister sends Nnrf_NFDeregistration to remove the NF profile.
func (c *Client) Deregister(ctx context.Context) error {
	url := fmt.Sprintf("%s/nnrf-disc/v1/nf-instances/%s", c.baseURL, c.nfInstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("nrf: create deregister request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nrf: deregister: %w", err)
	}
	defer resp.Body.Close()
	c.registered.Store(false)
	return nil
}

// IsRegistered returns true if NRF registration succeeded.
func (c *Client) IsRegistered() bool {
	return c.registered.Load()
}
