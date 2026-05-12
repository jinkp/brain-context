// Package crypto provides AES-256-GCM encryption for sensitive values
// like embed API keys stored server-side.
//
// The server encryption key is read from the BRAIN_ENCRYPTION_KEY
// environment variable (32 hex-encoded bytes = 64 hex chars).
// Generate one with: openssl rand -hex 32
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const envKey = "BRAIN_ENCRYPTION_KEY"

// Encrypt encrypts plaintext using AES-256-GCM with the server key.
// Returns a hex-encoded string: nonce + ciphertext + tag.
func Encrypt(plaintext string) (string, error) {
	key, err := serverKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a hex-encoded AES-256-GCM ciphertext using the server key.
func Decrypt(ciphertextHex string) (string, error) {
	key, err := serverKey()
	if err != nil {
		return "", err
	}

	data, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsConfigured returns true if BRAIN_ENCRYPTION_KEY is set and valid.
func IsConfigured() bool {
	_, err := serverKey()
	return err == nil
}

func serverKey() ([]byte, error) {
	raw := os.Getenv(envKey)
	if raw == "" {
		return nil, fmt.Errorf(
			"BRAIN_ENCRYPTION_KEY is not set — generate one with: openssl rand -hex 32",
		)
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("BRAIN_ENCRYPTION_KEY must be 64 hex characters: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("BRAIN_ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d", len(key))
	}
	return key, nil
}
