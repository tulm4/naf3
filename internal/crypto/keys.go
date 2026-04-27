package crypto

func GenerateDEK() ([]byte, error) {
	return RandomBytes(32)
}

func GenerateKEK() ([]byte, error) {
	return RandomBytes(32)
}
