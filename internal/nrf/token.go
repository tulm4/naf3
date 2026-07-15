package nrf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

// TokenCache caches OAuth2 access tokens with automatic refresh.
// Spec: TS 29.510 §6.7.3 (OAuth 2.0 client credentials grant for NRF).
type TokenCache struct {
	cfg   config.TokenConfig
	mu    sync.RWMutex
	token *CachedToken
}

// CachedToken holds a token and its expiration time.
type CachedToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

// NewTokenCache creates a new token cache.
func NewTokenCache(cfg config.TokenConfig) *TokenCache {
	return &TokenCache{
		cfg: cfg,
	}
}

// GetToken returns a valid access token, refreshing if necessary.
// Refreshes 5 minutes before expiry to avoid token expiration during use.
// Spec: TS 29.510 §6.7.3 (client_credentials grant).
func (c *TokenCache) GetToken(ctx context.Context) (string, error) {
	// Fast path: check cache without lock
	c.mu.RLock()
	if c.token != nil && time.Until(c.token.ExpiresAt) > 5*time.Minute {
		token := c.token.AccessToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Slow path: refresh token
	return c.refresh(ctx)
}

// refresh acquires write lock and refreshes the token.
func (c *TokenCache) refresh(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring lock
	if c.token != nil && time.Until(c.token.ExpiresAt) > 5*time.Minute {
		return c.token.AccessToken, nil
	}

	// Build form request
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("scope", c.cfg.Scope)
	form.Set("requester_nf_type", "NSSAAF")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.AuthServer, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}

	c.token = &CachedToken{
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return c.token.AccessToken, nil
}
