package nrf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
)

// Client is the NRF service discovery client.
// REQ-01: NRF registration on startup (degraded mode, D-04).
// REQ-02: Heartbeat every 5 minutes.
// REQ-03: Discovery with 5-min TTL cache.
type Client struct {
	baseURL      string
	nfInstanceID string
	cache        *NRFDiscoveryCache
	registered   atomic.Bool
	factory      *nfclient.Factory
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

// Get retrieves a cached value by key.
// If allowStale is true, returns cached data even if expired (graceful degradation).
func (c *NRFDiscoveryCache) Get(key string, allowStale bool) (interface{}, bool) {
	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().Before(entry.expiresAt) {
		return entry.data, true
	}

	if allowStale {
		return entry.data, true
	}

	return nil, false
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

// NewClient creates a new NRF client.
func NewClient(cfg config.NRFConfig, factory *nfclient.Factory) *Client {
	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute
	}
	return &Client{
		baseURL:      cfg.BaseURL,
		nfInstanceID: fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano()),
		cache: &NRFDiscoveryCache{
			ttl: cacheTTL,
		},
		factory: factory,
	}
}

// doRequest executes an HTTP request using the factory.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return c.factory.Do(ctx, c.baseURL, method, path, body)
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
		NFType:         "NSSAAF",
		NFStatus:       "REGISTERED",
		HeartBeatTimer: 300,
		Load:           0,
	}
	body, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("nrf: marshal profile: %w", err)
	}
	status, respBody, err := c.doRequest(ctx, http.MethodPost, "/nnrf-disc/v1/nf-instances", body)
	if err != nil {
		return fmt.Errorf("nrf: register: %w", err)
	}
	if status != http.StatusCreated {
		return fmt.Errorf("nrf: unexpected status %d: %s", status, respBody)
	}
	c.registered.Store(true)
	return nil
}

// Heartbeat sends Nnrf_NFHeartBeat every 5 minutes.
// REQ-02: PUT /nnrf-disc/v1/nf-instances/{id} with nfStatus="REGISTERED", heartBeatTimer=300, load=0-100.
func (c *Client) Heartbeat(ctx context.Context) error {
	payload := map[string]interface{}{
		"nfInstanceId":   c.nfInstanceID,
		"nfStatus":       "REGISTERED",
		"heartBeatTimer": 300,
		"load":           0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nrf: marshal heartbeat: %w", err)
	}
	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", c.nfInstanceID)
	status, respBody, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("nrf: heartbeat: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("nrf: heartbeat status %d: %s", status, respBody)
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
func (c *Client) DiscoverUDM(ctx context.Context, plmnID string) (string, error) {
	key := fmt.Sprintf("udm:uem:%s", plmnID)
	if endpoint, ok := c.cache.Get(key, true); ok {
		return endpoint.(string), nil
	}

	path := "/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem"
	status, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: discover udm: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("nrf: discover udm status %d: %s", status, respBody)
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
	if err := json.Unmarshal(respBody, &result); err != nil {
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
	return "", fmt.Errorf("nrf: no UDM found for plmnId %s", plmnID)
}

// DiscoverAMF discovers an AMF by instance ID.
// REQ-03 / docs/design/05_nf_profile.md §3.1.
func (c *Client) DiscoverAMF(ctx context.Context, amfID string) (string, error) {
	key := fmt.Sprintf("amf:%s", amfID)
	if endpoint, ok := c.cache.Get(key, true); ok {
		return endpoint.(string), nil
	}

	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", amfID)
	status, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("nrf: discover amf: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("nrf: discover amf status %d: %s", status, respBody)
	}
	var amf struct {
		NFInstanceID string `json:"nfInstanceId"`
	}
	if err := json.Unmarshal(respBody, &amf); err != nil {
		return "", fmt.Errorf("nrf: decode amf: %w", err)
	}
	c.cache.Set(key, amf.NFInstanceID)
	return amf.NFInstanceID, nil
}

// Deregister sends Nnrf_NFDeregistration to remove the NF profile.
func (c *Client) Deregister(ctx context.Context) error {
	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", c.nfInstanceID)
	status, respBody, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("nrf: deregister: %w", err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("nrf: deregister status %d: %s", status, respBody)
	}
	c.registered.Store(false)
	return nil
}

// IsRegistered returns true if NRF registration succeeded.
func (c *Client) IsRegistered() bool {
	return c.registered.Load()
}
