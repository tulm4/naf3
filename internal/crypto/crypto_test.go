package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("hello, world!")

	ed, err := Encrypt(plaintext, key, nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(ed.Nonce) != 12 {
		t.Errorf("nonce: got %d, want 12", len(ed.Nonce))
	}
	if len(ed.Tag) != 16 {
		t.Errorf("tag: got %d, want 16", len(ed.Tag))
	}

	decrypted, err := Decrypt(ed, key, nil)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWrongKeySize(t *testing.T) {
	shortKey := make([]byte, 31)
	plaintext := []byte("test")

	_, err := Encrypt(plaintext, shortKey, nil)
	if err == nil {
		t.Error("Encrypt with 31-byte key: expected error, got nil")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("secret data")

	ed, err := Encrypt(plaintext, key, nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Tamper with ciphertext copy (not the slice returned by Encrypt)
	ctCopy := make([]byte, len(ed.Ciphertext))
	copy(ctCopy, ed.Ciphertext)
	ctCopy[0] ^= 0xFF
	tampered := EncryptedData{
		Ciphertext: ctCopy,
		Nonce:      ed.Nonce,
		Tag:        ed.Tag,
	}

	_, err = Decrypt(tampered, key, nil)
	if err == nil {
		t.Error("Decrypt tampered ciphertext: expected error, got nil")
	}
}

func TestDecryptWithTagRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	plaintext := []byte("tagged data")

	ed, err := Encrypt(plaintext, key, nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	decrypted, err := DecryptWithTag(ed.Ciphertext, ed.Nonce, ed.Tag, key, nil)
	if err != nil {
		t.Fatalf("DecryptWithTag: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestRandomBytesLength(t *testing.T) {
	b, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	if len(b) != 32 {
		t.Errorf("got %d bytes, want 32", len(b))
	}
}

func TestRandomBytesUniqueness(t *testing.T) {
	b1, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	b2, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	if bytes.Equal(b1, b2) {
		t.Error("two calls to RandomBytes returned the same value")
	}
}

func TestRandomHexString(t *testing.T) {
	s, err := RandomHexString(64)
	if err != nil {
		t.Fatalf("RandomHexString: %v", err)
	}
	if len(s) != 64 {
		t.Errorf("got %d chars, want 64", len(s))
	}
	_, err = hex.DecodeString(s)
	if err != nil {
		t.Errorf("output is not valid hex: %v", err)
	}
}

func TestRandomHexStringOddLength(t *testing.T) {
	_, err := RandomHexString(63)
	if err == nil {
		t.Error("RandomHexString(63): expected error for odd length, got nil")
	}
}

func TestGCMNonce(t *testing.T) {
	n, err := GCMNonce()
	if err != nil {
		t.Fatalf("GCMNonce: %v", err)
	}
	if len(n) != 12 {
		t.Errorf("got %d bytes, want 12", len(n))
	}
}

func TestHashGPSI(t *testing.T) {
	gpsi := "5123456789"
	h1 := HashGPSI(gpsi)
	h2 := HashGPSI(gpsi)

	if len(h1) != 32 {
		t.Errorf("HashGPSI: got %d chars, want 32", len(h1))
	}
	if h1 != h2 {
		t.Error("HashGPSI: not deterministic")
	}
}

func TestHashSUPI(t *testing.T) {
	supi := "imu-123456789012345"
	h1 := HashSUPI(supi)
	h2 := HashSUPI(supi)

	if len(h1) != 32 {
		t.Errorf("HashSUPI: got %d chars, want 32", len(h1))
	}
	if h1 != h2 {
		t.Error("HashSUPI: not deterministic")
	}
}

func TestHashMessage(t *testing.T) {
	msg := []byte("eap-payload")
	h1 := HashMessage(msg)
	h2 := HashMessage(msg)

	if len(h1) != 64 {
		t.Errorf("HashMessage: got %d chars, want 64", len(h1))
	}
	if h1 != h2 {
		t.Error("HashMessage: not deterministic")
	}
}

func TestHMACSHA256(t *testing.T) {
	key := []byte("secret-key")
	data := []byte("authenticated message")

	mac := HMACSHA256(key, data)
	if len(mac) != 32 {
		t.Errorf("got %d bytes, want 32", len(mac))
	}
}

func TestVerifyHMAC(t *testing.T) {
	key := []byte("my-secret-key")
	data := []byte("test data")

	mac := HMACSHA256(key, data)
	if !VerifyHMAC(key, data, mac) {
		t.Error("VerifyHMAC: expected true for correct MAC")
	}

	wrongMac := make([]byte, len(mac))
	if VerifyHMAC(key, data, wrongMac) {
		t.Error("VerifyHMAC: expected false for incorrect MAC")
	}
}

func TestDeriveKeyLengths(t *testing.T) {
	ikm := bytes.Repeat([]byte{0x01}, 32)
	salt := []byte("salt")
	info := []byte("info")

	for _, length := range []int{16, 32, 64} {
		key, err := DeriveKey(ikm, salt, info, length)
		if err != nil {
			t.Errorf("DeriveKey(ikm, salt, info, %d): %v", length, err)
			continue
		}
		if len(key) != length {
			t.Errorf("got %d bytes, want %d", len(key), length)
		}
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	ikm := bytes.Repeat([]byte{0x42}, 32)
	k1, err := DeriveKey(ikm, nil, []byte("test"), 32)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	k2, err := DeriveKey(ikm, nil, []byte("test"), 32)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("DeriveKey: not deterministic")
	}
}

func TestDeriveKeyEmptyIKM(t *testing.T) {
	_, err := DeriveKey(nil, nil, []byte("info"), 32)
	if err == nil {
		t.Error("DeriveKey with empty IKM: expected error, got nil")
	}
}

func TestDeriveKeyZeroLength(t *testing.T) {
	ikm := bytes.Repeat([]byte{0x01}, 32)
	_, err := DeriveKey(ikm, nil, []byte("info"), 0)
	if err == nil {
		t.Error("DeriveKey with length=0: expected error, got nil")
	}
}

func TestSessionKEK(t *testing.T) {
	masterKey := bytes.Repeat([]byte{0xAB}, 32)
	key, err := SessionKEK(masterKey, "auth-123")
	if err != nil {
		t.Fatalf("SessionKEK: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("got %d bytes, want 32", len(key))
	}
}

func TestTLSExporter(t *testing.T) {
	masterSecret := []byte("0123456789abcdef")
	label := "EXPORTER-MSK"
	data, err := TLSExporter(masterSecret, label, nil, 64)
	if err != nil {
		t.Fatalf("TLSExporter: %v", err)
	}
	if len(data) != 64 {
		t.Errorf("got %d bytes, want 64", len(data))
	}
}

func TestGenerateDEK(t *testing.T) {
	dek1, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}
	if len(dek1) != 32 {
		t.Errorf("got %d bytes, want 32", len(dek1))
	}

	dek2, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}
	if bytes.Equal(dek1, dek2) {
		t.Error("two calls to GenerateDEK returned the same value")
	}
}

func TestGenerateKEK(t *testing.T) {
	kek1, err := GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK: %v", err)
	}
	if len(kek1) != 32 {
		t.Errorf("got %d bytes, want 32", len(kek1))
	}
}

func TestEnvelopeEncryptDecryptRoundTrip(t *testing.T) {
	kek := bytes.Repeat([]byte{0xCA}, 32)
	plaintext := []byte("envelope test payload")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}
	if len(env.EncryptedDEK) != 60 {
		t.Errorf("EncryptedDEK: got %d bytes, want 60", len(env.EncryptedDEK))
	}

	decrypted, err := EnvelopeDecrypt(env, kek)
	if err != nil {
		t.Fatalf("EnvelopeDecrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestEnvelopeEncryptDifferentCiphertext(t *testing.T) {
	kek := bytes.Repeat([]byte{0xFE}, 32)
	plaintext := []byte("same plaintext")

	env1, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}
	env2, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}
	if bytes.Equal(env1.Ciphertext, env2.Ciphertext) {
		t.Error("EnvelopeEncrypt: same plaintext produced identical ciphertext (DEK not random)")
	}
}

func TestEnvelopeDecryptWrongKEK(t *testing.T) {
	kek := bytes.Repeat([]byte{0xAA}, 32)
	wrongKEK := bytes.Repeat([]byte{0xBB}, 32)
	plaintext := []byte("secret")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	_, err = EnvelopeDecrypt(env, wrongKEK)
	if err == nil {
		t.Error("EnvelopeDecrypt with wrong KEK: expected error, got nil")
	}
}

func TestEnvelopeDecryptMultiCurrent(t *testing.T) {
	currentKEK := bytes.Repeat([]byte{0xCC}, 32)
	previousKEK := bytes.Repeat([]byte{0xDD}, 32)
	plaintext := []byte("multi key test")

	env, err := EnvelopeEncrypt(plaintext, currentKEK, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	decrypted, err := EnvelopeDecryptMulti(env, currentKEK, previousKEK)
	if err != nil {
		t.Fatalf("EnvelopeDecryptMulti: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestEnvelopeDecryptMultiPrevious(t *testing.T) {
	currentKEK := bytes.Repeat([]byte{0xEE}, 32)
	previousKEK := bytes.Repeat([]byte{0xFF}, 32)
	plaintext := []byte("previous kek test")

	env, err := EnvelopeEncrypt(plaintext, previousKEK, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	decrypted, err := EnvelopeDecryptMulti(env, currentKEK, previousKEK)
	if err != nil {
		t.Fatalf("EnvelopeDecryptMulti with previousKEK: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestSoftKeyManagerWrapUnwrap(t *testing.T) {
	mgr, err := NewSoftKeyManager("0102030405060708091011121314151617181920212223242526272829303132")
	if err != nil {
		t.Fatalf("NewSoftKeyManager: %v", err)
	}

	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}

	wrapped, ver, err := mgr.Wrap(nil, dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if ver != 1 {
		t.Errorf("Wrap: got version %d, want 1", ver)
	}

	unwrapped, err := mgr.Unwrap(nil, wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(unwrapped, dek) {
		t.Errorf("round-trip: got %x, want %x", unwrapped, dek)
	}
}

func TestSoftKeyManagerUnwrapWrongKey(t *testing.T) {
	mgr1, err := NewSoftKeyManager("0102030405060708091011121314151617181920212223242526272829303132")
	if err != nil {
		t.Fatalf("NewSoftKeyManager(mgr1): %v", err)
	}
	// mgr2 has a different master key (all bytes shifted by 1)
	mgr2, err := NewSoftKeyManager("0203040506070809101112131415161718192021222324252627282930313233")
	if err != nil {
		t.Fatalf("NewSoftKeyManager(mgr2): %v", err)
	}
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}

	wrapped, _, err := mgr1.Wrap(nil, dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	_, err = mgr2.Unwrap(nil, wrapped)
	if err == nil {
		t.Error("Unwrap with wrong manager's key: expected error, got nil")
	}
}

func TestInitTwice(t *testing.T) {
	defer func() {
		globalKM = nil
	}()
	Init(&Config{KeyManager: "soft", MasterKeyHex: "0102030405060708091011121314151617181920212223242526272829303132"})
	err := Init(&Config{KeyManager: "soft", MasterKeyHex: "0102030405060708091011121314151617181920212223242526272829303132"})
	if err == nil {
		t.Error("Init twice: expected error, got nil")
	}
}

func TestKMNotInitialized(t *testing.T) {
	globalKM = nil
	defer func() { globalKM = nil }()
	defer func() {
		if r := recover(); r == nil {
			t.Error("KM() should panic when not initialized")
		}
	}()
	KM()
}
