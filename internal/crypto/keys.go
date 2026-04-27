package crypto

// GenerateDEK generates a new 32-byte AES-256 Data Encryption Key.
// Uses crypto/rand for cryptographically secure random.
func GenerateDEK() ([]byte, error) {
	return RandomBytes(32)
}

// GenerateKEK generates a new 32-byte Key Encryption Key.
// Uses crypto/rand for cryptographically secure random.
func GenerateKEK() ([]byte, error) {
	return RandomBytes(32)
}
