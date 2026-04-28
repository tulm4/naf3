package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenValidator_Validate(t *testing.T) {
	// Generate test RSA key pair
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// Create JWKS server
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := JWKS{
			Keys: []JWKSKey{
				{
					Kty: "RSA",
					Kid: "test-key-1",
					Use: "sig",
					Alg: "RS256",
					N:   base64URLEncode(privKey.N.Bytes()),
					E:   base64URLEncode(big.NewInt(int64(privKey.E)).Bytes()),
				},
			},
		}
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer jwksServer.Close()

	validator := NewTokenValidator(TokenValidatorConfig{
		JWKSURL:        jwksServer.URL,
		Issuer:         "https://nrf.operator.com",
		Audiences:      []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
		AllowedNfTypes: []string{"AMF", "AUSF"},
		AllowedScopes:  []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
		JWKSTTL:        15 * time.Minute,
	})

	validToken := createTestToken(t, privKey, "test-key-1", jwt.MapClaims{
		"iss":     "https://nrf.operator.com",
		"sub":     "amf-001",
		"aud":     []string{"nnssaaf-nssaa"},
		"scope":   "nnssaaf-nssaa",
		"nf_type": "AMF",
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	})

	// Test: valid token
	claims, err := validator.Validate(context.Background(), validToken, "nnssaaf-nssaa")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if claims == nil {
		t.Fatal("expected claims, got nil")
	}
	if claims.Scope != "nnssaaf-nssaa" {
		t.Errorf("expected scope 'nnssaaf-nssaa', got %q", claims.Scope)
	}
	if claims.NfType != "AMF" {
		t.Errorf("expected nf_type 'AMF', got %q", claims.NfType)
	}

	// Test: missing token
	_, err = validator.Validate(context.Background(), "", "nnssaaf-nssaa")
	if err == nil {
		t.Error("expected error for empty token")
	}

	// Test: expired token
	expiredToken := createTestToken(t, privKey, "test-key-1", jwt.MapClaims{
		"iss":     "https://nrf.operator.com",
		"aud":     []string{"nnssaaf-nssaa"},
		"scope":   "nnssaaf-nssaa",
		"nf_type": "AMF",
		"exp":     time.Now().Add(-time.Hour).Unix(),
		"iat":     time.Now().Add(-2 * time.Hour).Unix(),
	})
	_, err = validator.Validate(context.Background(), expiredToken, "nnssaaf-nssaa")
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}

	// Test: wrong issuer
	wrongIssuerToken := createTestToken(t, privKey, "test-key-1", jwt.MapClaims{
		"iss":   "https://wrong.operator.com",
		"aud":   []string{"nnssaaf-nssaa"},
		"scope": "nnssaaf-nssaa",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	_, err = validator.Validate(context.Background(), wrongIssuerToken, "nnssaaf-nssaa")
	if !errors.Is(err, ErrInvalidIssuer) {
		t.Errorf("expected ErrInvalidIssuer, got %v", err)
	}

	// Test: wrong audience
	wrongAudToken := createTestToken(t, privKey, "test-key-1", jwt.MapClaims{
		"iss":   "https://nrf.operator.com",
		"aud":   []string{"wrong-service"},
		"scope": "nnssaaf-nssaa",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	_, err = validator.Validate(context.Background(), wrongAudToken, "nnssaaf-nssaa")
	if !errors.Is(err, ErrInvalidAudience) {
		t.Errorf("expected ErrInvalidAudience, got %v", err)
	}

	// Test: insufficient scope
	noScopeToken := createTestToken(t, privKey, "test-key-1", jwt.MapClaims{
		"iss":   "https://nrf.operator.com",
		"aud":   []string{"nnssaaf-nssaa"},
		"scope": "other-scope",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	_, err = validator.Validate(context.Background(), noScopeToken, "nnssaaf-nssaa")
	if !errors.Is(err, ErrInsufficientScope) {
		t.Errorf("expected ErrInsufficientScope, got %v", err)
	}
}

//nolint:unparam // test helper where kid parameter is always "test-key-1"
func createTestToken(t *testing.T, privKey *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	token.Header["alg"] = "RS256"
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func base64URLEncode(data []byte) string {
	enc := base64.RawURLEncoding
	out := make([]byte, enc.EncodedLen(len(data)))
	enc.Encode(out, data)
	return string(out)
}
