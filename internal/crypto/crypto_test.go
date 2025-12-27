package crypto

import (
	"os"
	"testing"
)

func TestEncryptorRoundTrip(t *testing.T) {
	// Set up a test key
	os.Setenv("SCHOONER_ENCRYPTION_KEY", "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=") // 32 bytes base64
	defer os.Unsetenv("SCHOONER_ENCRYPTION_KEY")

	encryptor, err := NewEncryptor()
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple string", "hello world"},
		{"special characters", "p@ssw0rd!@#$%^&*()"},
		{"unicode", "héllo wörld 你好"},
		{"long string", "this is a very long string that should still encrypt and decrypt correctly even though it's much longer than the block size"},
		{"token-like", "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := encryptor.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Empty string returns empty
			if tt.plaintext == "" {
				if encrypted != "" {
					t.Errorf("Encrypt() empty string should return empty, got %v", encrypted)
				}
				return
			}

			// Encrypted should be different from plaintext
			if encrypted == tt.plaintext {
				t.Errorf("Encrypt() should produce different output")
			}

			// Decrypt should return original
			decrypted, err := encryptor.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptorDifferentNonces(t *testing.T) {
	os.Setenv("SCHOONER_ENCRYPTION_KEY", "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=")
	defer os.Unsetenv("SCHOONER_ENCRYPTION_KEY")

	encryptor, err := NewEncryptor()
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	plaintext := "same plaintext"
	encrypted1, _ := encryptor.Encrypt(plaintext)
	encrypted2, _ := encryptor.Encrypt(plaintext)

	// Same plaintext should produce different ciphertexts due to random nonce
	if encrypted1 == encrypted2 {
		t.Error("Encrypt() should produce different ciphertext for same plaintext (random nonce)")
	}

	// Both should decrypt correctly
	decrypted1, _ := encryptor.Decrypt(encrypted1)
	decrypted2, _ := encryptor.Decrypt(encrypted2)

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Both ciphertexts should decrypt to original plaintext")
	}
}

func TestDecryptInvalidData(t *testing.T) {
	os.Setenv("SCHOONER_ENCRYPTION_KEY", "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=")
	defer os.Unsetenv("SCHOONER_ENCRYPTION_KEY")

	encryptor, err := NewEncryptor()
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
	}{
		{"invalid base64", "not-valid-base64!@#"},
		{"too short", "dG9vIHNob3J0"}, // "too short" base64
		{"tampered data", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY3ODkw"}, // random data
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encryptor.Decrypt(tt.input)
			if err == nil {
				t.Error("Decrypt() should return error for invalid data")
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"github_token", true},
		{"cloudflare_tunnel_token", true},
		{"clone_directory", false},
		{"random_setting", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsSensitiveKey(tt.key); got != tt.expected {
				t.Errorf("IsSensitiveKey(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}
