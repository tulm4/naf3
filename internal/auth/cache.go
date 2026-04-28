package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// JWKSKey represents a single JWK key from the JWKS endpoint.
type JWKSKey struct {
	Kty string `json:"kty"` // "RSA" or "EC"
	Kid string `json:"kid"` // key ID
	Use string `json:"use"` // "sig"
	Alg string `json:"alg"` // "RS256", "ES256", etc.
	N   string `json:"n"`   // RSA modulus (base64url)
	E   string `json:"e"`   // RSA exponent (base64url)
	Crv string `json:"crv"` // EC curve (P-256, P-384, P-521)
	X   string `json:"x"`   // EC X coord (base64url)
	Y   string `json:"y"`   // EC Y coord (base64url)
}

// JWKS represents the JSON Web Key Set fetched from NRF.
type JWKS struct {
	Keys []JWKSKey `json:"keys"`
}

// JWKSEntry holds a parsed public key and its expiry.
type JWKSEntry struct {
	Key       crypto.PublicKey // accepts *rsa.PublicKey or *ecdsa.PublicKey
	FetchedAt time.Time
	TTL       time.Duration // 15 minutes
}

// JWKSFetcher fetches and caches JWKS from NRF.
// Thread-safe with a read-write mutex. Supports both RSA and ECDSA keys (ES256/ES384/ES512).
// Spec: TS 33.501 §16.3 — RS256, RS384, RS512, ES256, ES384, ES512 accepted.
type JWKSFetcher struct {
	mu         sync.RWMutex
	jwksURL    string
	httpClient *http.Client
	entries    map[string]crypto.PublicKey // kid -> parsed key
	fetchedAt  time.Time
	ttl        time.Duration
}

// NewJWKSFetcher creates a JWKSFetcher that caches keys for ttl duration.
func NewJWKSFetcher(jwksURL string, ttl time.Duration) *JWKSFetcher {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &JWKSFetcher{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		entries:    make(map[string]crypto.PublicKey),
		ttl:        ttl,
	}
}

// GetKey returns the public key for the given kid.
// Fetches from NRF if cache is empty or expired.
// Accepts RSA keys (*rsa.PublicKey) and ECDSA keys (*ecdsa.PublicKey).
//
// Uses double-checked locking: fast path under read lock (cached key),
// slow path upgrades to write lock for refresh.
func (f *JWKSFetcher) GetKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	// Fast path: check cache under read lock — no write contention
	f.mu.RLock()
	if entry, ok := f.entries[kid]; ok && time.Since(f.fetchedAt) <= f.ttl {
		f.mu.RUnlock()
		return entry, nil
	}
	f.mu.RUnlock()

	// Slow path: refresh cache under write lock
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have refreshed)
	if entry, ok := f.entries[kid]; ok && time.Since(f.fetchedAt) <= f.ttl {
		return entry, nil
	}

	// Fetch and refresh cache under lock
	if err := f.refreshLocked(ctx); err != nil {
		return nil, err
	}

	entry, ok := f.entries[kid]
	if !ok {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	return entry, nil
}

// refreshLocked fetches JWKS from NRF and parses RSA and EC keys.
// Caller must hold f.mu.
func (f *JWKSFetcher) refreshLocked(ctx context.Context) error {
	// Double-check: avoid redundant fetch if another goroutine just refreshed
	if time.Since(f.fetchedAt) <= f.ttl && len(f.entries) > 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJWKSFetch, err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJWKSFetch, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: NRF JWKS returned %d", ErrJWKSFetch, resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("%w: invalid JSON: %w", ErrJWKSFetch, err)
	}

	newEntries := make(map[string]crypto.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty == "RSA" {
			key, err := parseRSAKey(k)
			if err != nil {
				continue // Skip malformed keys
			}
			newEntries[k.Kid] = key
		} else if k.Kty == "EC" {
			key, err := parseECKey(k)
			if err != nil {
				continue // Skip malformed keys
			}
			newEntries[k.Kid] = key
		}
		// Ignore other key types (oct, symmetric, etc.)
	}

	f.entries = newEntries
	f.fetchedAt = time.Now()
	return nil
}

// parseRSAKey parses an RSA JWK into an *rsa.PublicKey.
func parseRSAKey(k JWKSKey) (*rsa.PublicKey, error) {
	nBytes, err := base64URLDecode(k.N)
	if err != nil {
		return nil, fmt.Errorf("invalid N: %w", err)
	}
	eBytes, err := base64URLDecode(k.E)
	if err != nil {
		return nil, fmt.Errorf("invalid E: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	return &rsa.PublicKey{N: n, E: e}, nil
}

// parseECKey parses an EC JWK into an *ecdsa.PublicKey.
func parseECKey(k JWKSKey) (*ecdsa.PublicKey, error) {
	if k.Crv == "" || k.X == "" || k.Y == "" {
		return nil, fmt.Errorf("EC key missing curve, x, or y")
	}
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve: %s", k.Crv)
	}
	xBytes, err := base64URLDecode(k.X)
	if err != nil {
		return nil, fmt.Errorf("invalid X: %w", err)
	}
	yBytes, err := base64URLDecode(k.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid Y: %w", err)
	}
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func base64URLDecode(s string) ([]byte, error) {
	// Replace URL-safe chars with standard base64
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	// Add padding
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}
