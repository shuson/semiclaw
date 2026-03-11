package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

func GenerateSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TokenMatchesHash(token string, storedHash string) bool {
	computed := HashToken(token)
	if len(computed) != len(storedHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
