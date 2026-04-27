package auth

import "errors"

var (
	// ErrMissingToken is returned when Authorization header is absent.
	ErrMissingToken = errors.New("missing Authorization header")

	// ErrInvalidToken is returned when JWT parsing fails.
	ErrInvalidToken = errors.New("invalid token")

	// ErrInvalidClaims is returned when JWT claims are not of the expected type.
	ErrInvalidClaims = errors.New("invalid token claims")

	// ErrTokenExpired is returned when the token's exp claim is in the past.
	ErrTokenExpired = errors.New("token expired")

	// ErrInvalidIssuer is returned when the token's iss claim does not match NRF.
	ErrInvalidIssuer = errors.New("invalid token issuer")

	// ErrInvalidAudience is returned when the token's aud claim does not include NSSAAF.
	ErrInvalidAudience = errors.New("invalid token audience")

	// ErrInsufficientScope is returned when the token lacks the required scope.
	ErrInsufficientScope = errors.New("insufficient scope")

	// ErrInvalidNfType is returned when the token's nf_type claim is not allowed.
	ErrInvalidNfType = errors.New("invalid NF type")

	// ErrInvalidSigningMethod is returned when the JWT uses an unsupported signing method.
	ErrInvalidSigningMethod = errors.New("invalid signing method")

	// ErrJWKSFetch is returned when fetching JWKS from NRF fails.
	ErrJWKSFetch = errors.New("failed to fetch JWKS")
)
