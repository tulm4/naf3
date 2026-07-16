package nrf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
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

	// Integrated components from Task 6.
	tokenCache       *TokenCache
	profileBuilder   *ProfileBuilder
	heartbeatManager *HeartbeatManager
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

// ProfileBuilder wraps YAML profile loading and building.
type ProfileBuilder struct {
	yamlPath string
}

// LoadFromYAML loads and builds NFProfile from YAML config.
// heartbeatTimer is the value (in seconds) used for NFProfile.heartBeatTimer.
// Pass 0 to leave the field unset and rely on NRF negotiation at registration.
func (pb *ProfileBuilder) LoadFromYAML(heartbeatTimer int) (*NFProfile, error) {
	yamlProfile, err := LoadProfileFromYAML(pb.yamlPath)
	if err != nil {
		return nil, err
	}
	return BuildNFProfile(yamlProfile, heartbeatTimer), nil
}

// NewClient creates a new NRF client.
func NewClient(cfg config.NRFConfig, factory *nfclient.Factory) *Client {
	return NewClientWithConfig(cfg, factory)
}

// NewClientWithConfig creates a new NRF client with full configuration.
func NewClientWithConfig(cfg config.NRFConfig, factory *nfclient.Factory) *Client {
	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute
	}

	instanceID := cfg.InstanceID
	if instanceID == "" {
		instanceID = fmt.Sprintf("nssAAF-instance-%d", time.Now().UnixNano())
	}

	client := &Client{
		baseURL:      cfg.BaseURL,
		nfInstanceID: instanceID,
		cache: &NRFDiscoveryCache{
			ttl: cacheTTL,
		},
		factory: factory,
	}

	// Initialize token cache if enabled.
	if cfg.AccessToken.Enabled {
		client.tokenCache = NewTokenCache(cfg.AccessToken)
	}

	return client
}

// TokenCache returns the OAuth2 token cache.
func (c *Client) TokenCache() *TokenCache {
	return c.tokenCache
}

// HeartbeatManager returns the heartbeat manager.
func (c *Client) HeartbeatManager() *HeartbeatManager {
	return c.heartbeatManager
}

// NFInstanceID returns the NF instance ID used in NRF paths.
func (c *Client) NFInstanceID() string {
	return c.nfInstanceID
}

// SetProfilePath sets the NFProfile YAML path and initializes components.
func (c *Client) SetProfilePath(yamlPath string, heartbeatCfg config.HeartbeatConfig) error {
	// Load profile.
	yamlProfile, err := LoadProfileFromYAML(yamlPath)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	// Update instance ID if not already set by config.
	if yamlProfile.InstanceID != "" {
		c.nfInstanceID = yamlProfile.InstanceID
	}

	// Create heartbeat manager wired to this client.
	hbCfg := config.HeartbeatConfig{
		InitialInterval:          heartbeatCfg.InitialInterval,
		AcceptNegotiatedInterval: heartbeatCfg.AcceptNegotiatedInterval,
		MaxConsecutiveFailures:   heartbeatCfg.MaxConsecutiveFailures,
	}
	c.heartbeatManager = NewHeartbeatManager(c, c.nfInstanceID, hbCfg)

	// Also remember the profile builder for ad-hoc loads.
	c.profileBuilder = &ProfileBuilder{yamlPath: yamlPath}

	return nil
}

// StartHeartbeat begins the heartbeat loop with initial registration.
// Returns an error only if the heartbeat manager was not initialized via
// SetProfilePath first.
func (c *Client) StartHeartbeat(ctx context.Context) error {
	if c.heartbeatManager == nil {
		return fmt.Errorf("heartbeat manager not initialized, call SetProfilePath first")
	}
	return c.heartbeatManager.Start(ctx)
}

// StopHeartbeat halts the heartbeat manager.
func (c *Client) StopHeartbeat() {
	if c.heartbeatManager != nil {
		c.heartbeatManager.Stop()
	}
}

// doRequest executes an HTTP request using the factory.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return c.factory.Do(ctx, c.baseURL, method, path, body)
}

// doRequestWithHeaders executes a request with custom headers.
// Used for PATCH/heartbeat which requires If-Match and Content-Type overrides.
func (c *Client) doRequestWithHeaders(ctx context.Context, method, path string, body []byte, headers map[string]string) (int, []byte, error) {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return 0, nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("nrf: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("nrf: read body: %w", err)
	}

	return resp.StatusCode, respBody, nil
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

			interval, etag, err := c.Register(ctx, nil)
			if err != nil {
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
				"heartbeat_interval", interval,
				"etag", etag,
			)
			c.registered.Store(true)
			return
		}
	}()
}

// Register sends Nnrf_NFRegistration to the NRF.
// Uses PUT /nnrf-disc/v1/nf-instances/{id} per TS 29.510 §5.2.2.2.
// If profile is nil, a minimal default profile is constructed from c.nfInstanceID.
// Returns (negotiatedHeartbeatInterval, etag, error).
func (c *Client) Register(ctx context.Context, profile *NFProfile) (time.Duration, string, error) {
	if profile == nil {
		profile = &NFProfile{
			NFInstanceID:   c.nfInstanceID,
			NFType:         NFTypeNSSAAF,
			NFStatus:       NFStatusRegistered,
			HeartBeatTimer: 300,
		}
	}

	body, err := json.Marshal(profile)
	if err != nil {
		return 0, "", fmt.Errorf("nrf: marshal profile: %w", err)
	}

	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", c.nfInstanceID)
	status, respBody, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return 0, "", fmt.Errorf("nrf: register: %w", err)
	}

	if status != http.StatusCreated && status != http.StatusOK {
		return 0, "", fmt.Errorf("nrf: unexpected status %d: %s", status, respBody)
	}

	interval := parseHeartbeatInterval(respBody)
	etag := parseETag(respBody)

	c.registered.Store(true)
	return interval, etag, nil
}

// Heartbeat sends PATCH to keep registration alive.
// Uses PATCH /nnrf-disc/v1/nf-instances/{id} per TS 29.510 §5.2.2.3.1B.
// Returns the new etag from the response.
func (c *Client) Heartbeat(ctx context.Context, instanceID, etag string) (string, error) {
	patch := `{"nfStatus":"REGISTERED"}`

	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", instanceID)
	status, respBody, err := c.doRequestWithHeaders(ctx, http.MethodPatch, path, []byte(patch), map[string]string{
		"Content-Type": "application/json-patch+json",
		"If-Match":     etag,
	})
	if err != nil {
		return "", fmt.Errorf("nrf: heartbeat: %w", err)
	}

	if status != http.StatusNoContent {
		return "", fmt.Errorf("nrf: heartbeat status %d: %s", status, respBody)
	}

	return parseETag(respBody), nil
}

// Deregister sends Nnrf_NFDeregistration to remove the NF profile.
func (c *Client) Deregister(ctx context.Context, instanceID string) error {
	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances/%s", instanceID)
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

// FindNF discovers an NF instance by type and returns the first matching
// profile. Returns (nil, nil) when NRF has no matching instances.
// REQ-03 / TS 29.510 §5.3 (Nnrf_NFDiscovery_Search).
func (c *Client) FindNF(ctx context.Context, nfType string) (*NFProfile, error) {
	// Normalize NF type so callers may pass "udm", "Udm", etc.
	normalizedType := NFType(strings.ToUpper(nfType))

	path := fmt.Sprintf("/nnrf-disc/v1/nf-instances?target-nf-type=%s", normalizedType)
	status, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("nrf: find nf: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("nrf: find nf status %d: %s", status, respBody)
	}

	var result struct {
		NFInstances []NFProfile `json:"nfInstances"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("nrf: decode find result: %w", err)
	}

	if len(result.NFInstances) == 0 {
		return nil, nil // Not found, return nil without error
	}

	// Return the first matching NF instance.
	return &result.NFInstances[0], nil
}

// IsRegistered returns true if NRF registration succeeded.
func (c *Client) IsRegistered() bool {
	return c.registered.Load()
}
