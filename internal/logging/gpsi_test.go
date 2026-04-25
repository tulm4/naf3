package logging

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashGPSI_Deterministic(t *testing.T) {
	gpsi := "51234567890"

	hash1 := HashGPSI(gpsi)
	hash2 := HashGPSI(gpsi)

	assert.Equal(t, hash1, hash2, "same GPSI should produce same hash")
}

func TestHashGPSI_DifferentInputs(t *testing.T) {
	hash1 := HashGPSI("51234567890")
	hash2 := HashGPSI("59876543210")

	assert.NotEqual(t, hash1, hash2, "different GPSIs should produce different hashes")
}

func TestHashGPSI_Length(t *testing.T) {
	hash := HashGPSI("51234567890")

	// base64.RawURLEncoding of 8 bytes produces 11 characters (no padding)
	// 8 bytes * 8 bits/byte = 64 bits
	// 64 bits / 6 bits per base64 char = 10.67 chars, rounded up = 11 chars
	assert.Equal(t, 11, len(hash), "hash length should be 11 for 8 bytes with base64url encoding")
}

func TestHashGPSI_EmptyString(t *testing.T) {
	hash := HashGPSI("")

	// Should produce a valid hash for empty string
	assert.NotEmpty(t, hash, "hash of empty string should not be empty")
	assert.Equal(t, 11, len(hash), "hash of empty string should still be 11 chars")
}

func TestHashGPSI_URLSafe(t *testing.T) {
	hash := HashGPSI("51234567890")

	// base64.RawURLEncoding uses - and _ instead of + and /
	// Should not contain + or /
	assert.False(t, strings.ContainsAny(hash, "+/="), "hash should not contain +, /, or = (not URL safe)")
}

func TestHashGPSI_ConsistencyWithDocs(t *testing.T) {
	// Test that the hash matches the documented format
	// SHA256(gpsi)[0:8], base64url encoded
	gpsi := "51234567890"
	hash := HashGPSI(gpsi)

	// Verify the hash is a valid base64url string
	assert.Regexp(t, `^[A-Za-z0-9_-]+$`, hash, "hash should be valid base64url")
}
