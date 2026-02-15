package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestAESGCMRoundtrip(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	aead, err := NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM failed: %v", err)
	}
	defer aead.Close()

	t.Logf("Hardware accelerated: %v", aead.IsHardwareAccelerated())

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("Hello, hardware crypto on JetKVM!")
	aad := []byte("additional authenticated data")

	ciphertext := aead.Seal(nil, nonce, plaintext, aad)

	decrypted, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Roundtrip mismatch\ngot:  %s\nwant: %s", decrypted, plaintext)
	}
}

func TestAESGCMWrongAAD(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	aead, err := NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM failed: %v", err)
	}
	defer aead.Close()

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret message")
	aad := []byte("correct AAD")
	wrongAAD := []byte("wrong AAD")

	ciphertext := aead.Seal(nil, nonce, plaintext, aad)

	// Try to decrypt with wrong AAD - should fail
	_, err = aead.Open(nil, nonce, ciphertext, wrongAAD)
	if err == nil {
		t.Error("Expected authentication failure with wrong AAD")
	}
}

func BenchmarkAESGCMSeal(b *testing.B) {
	key := make([]byte, 16)
	rand.Read(key)

	aead, err := NewAESGCM(key)
	if err != nil {
		b.Fatal(err)
	}
	defer aead.Close()

	b.Logf("Hardware accelerated: %v", aead.IsHardwareAccelerated())

	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)

	// Test with different payload sizes
	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		b.Run(byteSizeStr(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = aead.Seal(nil, nonce, plaintext, nil)
			}
		})
	}
}

func BenchmarkAESGCMOpen(b *testing.B) {
	key := make([]byte, 16)
	rand.Read(key)

	aead, err := NewAESGCM(key)
	if err != nil {
		b.Fatal(err)
	}
	defer aead.Close()

	b.Logf("Hardware accelerated: %v", aead.IsHardwareAccelerated())

	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)

	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)
		ciphertext := aead.Seal(nil, nonce, plaintext, nil)

		b.Run(byteSizeStr(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_, _ = aead.Open(nil, nonce, ciphertext, nil)
			}
		})
	}
}

func byteSizeStr(size int) string {
	if size >= 1024*1024 {
		return string(rune(size/(1024*1024))) + "MB"
	}
	if size >= 1024 {
		return string(rune(size/1024)) + "KB"
	}
	return string(rune(size)) + "B"
}
