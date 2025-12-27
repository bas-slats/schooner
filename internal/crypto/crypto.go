package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	// ErrInvalidData is returned when decryption fails due to invalid data
	ErrInvalidData = errors.New("invalid encrypted data")
)

// Encryptor handles encryption and decryption of sensitive data
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates a new Encryptor with a key from environment or generates one
func NewEncryptor() (*Encryptor, error) {
	key, err := getOrCreateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext and returns base64-encoded ciphertext
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext and returns plaintext
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrInvalidData
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidData
	}

	return string(plaintext), nil
}

// getOrCreateKey gets the encryption key from environment or data file
func getOrCreateKey() ([]byte, error) {
	// First try environment variable
	if keyStr := os.Getenv("SCHOONER_ENCRYPTION_KEY"); keyStr != "" {
		key, err := base64.StdEncoding.DecodeString(keyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid SCHOONER_ENCRYPTION_KEY: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("SCHOONER_ENCRYPTION_KEY must be 32 bytes (base64 encoded)")
		}
		return key, nil
	}

	// Try to read from key file
	keyPath := getKeyPath()
	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("invalid key file: %w", err)
		}
		if len(key) == 32 {
			return key, nil
		}
	}

	// Generate new key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Save to file
	if err := os.MkdirAll("./data", 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0600); err != nil {
		return nil, fmt.Errorf("failed to save key: %w", err)
	}

	return key, nil
}

// getKeyPath returns the path to the key file
func getKeyPath() string {
	if path := os.Getenv("SCHOONER_KEY_PATH"); path != "" {
		return path
	}
	return "./data/.encryption_key"
}

// IsSensitiveKey returns true if the setting key contains sensitive data
func IsSensitiveKey(key string) bool {
	sensitiveKeys := map[string]bool{
		"github_token":            true,
		"cloudflare_tunnel_token": true,
	}
	return sensitiveKeys[key]
}
