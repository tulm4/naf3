package auth

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims holds the claims extracted from a validated JWT.
type TokenClaims struct {
	jwt.RegisteredClaims
	Scope    string `json:"scope"`
	ClientID string `json:"client_id"`
	NfType   string `json:"nf_type"`
	NfID     string `json:"nf_id"`
	CN       string `json:"cn"`
}

// TokenValidator validates JWT tokens using NRF's public keys.
type TokenValidator struct {
	jwksFetcher    *JWKSFetcher
	issuer         string
	audiences      []string
	allowedNfTypes []string
	allowedScopes  []string
}

// TokenValidatorConfig holds configuration for TokenValidator.
type TokenValidatorConfig struct {
	JWKSURL        string
	Issuer         string
	Audiences      []string
	AllowedNfTypes []string
	AllowedScopes  []string
	JWKSTTL        time.Duration
}

// NewTokenValidator creates a new TokenValidator.
func NewTokenValidator(cfg TokenValidatorConfig) *TokenValidator {
	if cfg.JWKSURL == "" {
		panic("JWKSURL is required")
	}
	if cfg.Issuer == "" {
		panic("Issuer is required")
	}
	if len(cfg.Audiences) == 0 {
		cfg.Audiences = []string{"nnssaaf-nssaa", "nnssaaf-aiw"}
	}
	if len(cfg.AllowedNfTypes) == 0 {
		cfg.AllowedNfTypes = []string{"AMF", "AUSF"}
	}
	if len(cfg.AllowedScopes) == 0 {
		cfg.AllowedScopes = []string{"nnssaaf-nssaa", "nnssaaf-aiw"}
	}
	if cfg.JWKSTTL == 0 {
		cfg.JWKSTTL = 15 * time.Minute
	}
	fetcher := NewJWKSFetcher(cfg.JWKSURL, cfg.JWKSTTL)
	return &TokenValidator{
		jwksFetcher:    fetcher,
		issuer:         cfg.Issuer,
		audiences:      cfg.Audiences,
		allowedNfTypes: cfg.AllowedNfTypes,
		allowedScopes:  cfg.AllowedScopes,
	}
}

// Validate parses and validates a JWT token.
// requiredScope must be one of the allowed scopes.
//
//nolint:gocyclo // complexity inherent in JWT validation with multiple failure modes
func (v *TokenValidator) Validate(ctx context.Context, tokenString string, requiredScope string) (*TokenClaims, error) {
	// Parse without verification first to get the kid
	unverified, _, err := new(jwt.Parser).ParseUnverified(tokenString, &TokenClaims{})
	if err != nil {
		return nil, fmt.Errorf("%w: parse failed: %w", ErrInvalidToken, err)
	}
	_ = unverified.Claims.(*TokenClaims)

	kid := unverified.Header["kid"]
	var kidStr string
	if kid != nil {
		kidStr, _ = kid.(string)
	}

	// Get the public key for this kid
	var pubKey crypto.PublicKey
	if kidStr != "" {
		pubKey, err = v.jwksFetcher.GetKey(ctx, kidStr)
		if err != nil {
			return nil, fmt.Errorf("%w: JWKS fetch: %w", ErrInvalidToken, err)
		}
	} else {
		// No kid — try all keys (not ideal, but for robustness)
		return nil, fmt.Errorf("%w: no kid in token header", ErrInvalidToken)
	}

	// Full validation with key
	parsed, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		// Verify signing algorithm
		switch t.Method.Alg() {
		case "RS256", "RS384", "RS512":
			isRSA := t.Method.(*jwt.SigningMethodRSA)
			if isRSA == nil {
				return nil, ErrInvalidSigningMethod
			}
		case "ES256", "ES384", "ES512":
			isECDSA := t.Method.(*jwt.SigningMethodECDSA)
			if isECDSA == nil {
				return nil, ErrInvalidSigningMethod
			}
		default:
			return nil, ErrInvalidSigningMethod
		}
		return pubKey, nil
	})

	if err != nil {
		if strings.Contains(err.Error(), "token is expired") {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	parsedClaims, ok := parsed.Claims.(*TokenClaims)
	if !ok {
		return nil, ErrInvalidClaims
	}

	// Validate issuer
	if parsedClaims.Issuer != v.issuer {
		return nil, ErrInvalidIssuer
	}

	// Validate audience
	validAudience := false
	for _, aud := range parsedClaims.Audience {
		for _, allowed := range v.audiences {
			if aud == allowed {
				validAudience = true
				break
			}
		}
	}
	if !validAudience {
		return nil, ErrInvalidAudience
	}

	// Validate scope
	scopes := strings.Fields(parsedClaims.Scope)
	scopeValid := false
	for _, s := range scopes {
		if s == requiredScope {
			scopeValid = true
			break
		}
	}
	if !scopeValid {
		return nil, ErrInsufficientScope
	}

	// Validate NF type
	nfTypeValid := false
	for _, allowed := range v.allowedNfTypes {
		if parsedClaims.NfType == allowed {
			nfTypeValid = true
			break
		}
	}
	if !nfTypeValid {
		return nil, ErrInvalidNfType
	}

	return parsedClaims, nil
}

// TokenCache provides short-term in-memory caching of validated tokens.
// Not used in Phase 5 HTTP Gateway middleware (stateless per request).
// Available for future use (e.g., rate limiting by token).
type TokenCache struct {
	mu      sync.RWMutex
	entries map[string]*TokenClaims
}

// NewTokenCache creates a new TokenCache.
func NewTokenCache() *TokenCache {
	return &TokenCache{
		entries: make(map[string]*TokenClaims),
	}
}

// Get returns a cached token claims if present.
func (c *TokenCache) Get(tokenHash string) (*TokenClaims, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	claims, ok := c.entries[tokenHash]
	return claims, ok
}

// Set stores token claims in the cache.
func (c *TokenCache) Set(tokenHash string, claims *TokenClaims) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[tokenHash] = claims
}

// Remove removes a token from the cache.
func (c *TokenCache) Remove(tokenHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, tokenHash)
}

// globalValidator is the package-level TokenValidator singleton.
var globalValidator *TokenValidator
var validatorMu sync.RWMutex

// Init initializes the auth package with configuration.
// Returns an error if initialization fails (e.g., invalid config).
func Init(cfg TokenValidatorConfig) error {
	validatorMu.Lock()
	defer validatorMu.Unlock()
	if globalValidator != nil {
		return errors.New("auth.Init called twice")
	}
	globalValidator = NewTokenValidator(cfg)
	return nil
}

// Validator returns the global TokenValidator instance.
func Validator() *TokenValidator {
	validatorMu.RLock()
	defer validatorMu.RUnlock()
	if globalValidator == nil {
		panic("auth package not initialized, call auth.Init first")
	}
	return globalValidator
}
