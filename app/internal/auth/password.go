package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	saltLen             = 16
)

func HashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password cannot be empty")
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read random salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, b64Salt, b64Hash)
	return encoded, nil
}

func VerifyPassword(password string, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, errors.New("invalid password hash format")
	}
	if parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("unsupported password hash format")
	}

	memory, timeCost, threads, err := parseArgonParams(parts[3])
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode argon2 salt: %w", err)
	}

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode argon2 hash: %w", err)
	}

	calculated := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(decodedHash)))
	if subtle.ConstantTimeCompare(decodedHash, calculated) == 1 {
		return true, nil
	}
	return false, nil
}

func parseArgonParams(raw string) (memory uint32, timeCost uint32, threads uint8, err error) {
	segments := strings.Split(raw, ",")
	if len(segments) != 3 {
		return 0, 0, 0, errors.New("invalid argon2 parameters")
	}

	for _, segment := range segments {
		kv := strings.SplitN(segment, "=", 2)
		if len(kv) != 2 {
			return 0, 0, 0, errors.New("invalid argon2 parameter segment")
		}

		value, convErr := strconv.ParseUint(kv[1], 10, 32)
		if convErr != nil {
			return 0, 0, 0, fmt.Errorf("parse argon2 parameter %s: %w", kv[0], convErr)
		}

		switch kv[0] {
		case "m":
			memory = uint32(value)
		case "t":
			timeCost = uint32(value)
		case "p":
			threads = uint8(value)
		default:
			return 0, 0, 0, fmt.Errorf("unknown argon2 parameter %q", kv[0])
		}
	}

	if memory == 0 || timeCost == 0 || threads == 0 {
		return 0, 0, 0, errors.New("argon2 parameters must be greater than zero")
	}

	return memory, timeCost, threads, nil
}
