package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type SecretBox struct {
	key []byte
}

func LoadOrCreateSecretBox(keyPath string) (*SecretBox, error) {
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}

	key, err := os.ReadFile(keyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, readErr := rand.Read(key); readErr != nil {
			return nil, fmt.Errorf("generate encryption key: %w", readErr)
		}
		if writeErr := os.WriteFile(keyPath, key, 0o600); writeErr != nil {
			return nil, fmt.Errorf("write key file: %w", writeErr)
		}
	} else {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid key file length: %d", len(key))
		}
		if chmodErr := os.Chmod(keyPath, 0o600); chmodErr != nil {
			return nil, fmt.Errorf("harden key file permissions: %w", chmodErr)
		}
	}

	return &SecretBox{key: key}, nil
}

func (s *SecretBox) Encrypt(plaintext string) (ciphertext []byte, nonce []byte, err error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create gcm: %w", err)
	}

	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("read nonce: %w", err)
	}

	ciphertext = aead.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

func (s *SecretBox) Decrypt(ciphertext []byte, nonce []byte) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}

	return string(plaintext), nil
}
