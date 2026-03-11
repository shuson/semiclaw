package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOllamaBaseURL = "http://127.0.0.1:11434"
	defaultModel         = "llama3.2"
	defaultOllamaTimeout = 180 * time.Second
	defaultDataDirName   = ".semiclaw"
)

type Config struct {
	DataDir           string
	DBPath            string
	EncryptionKeyPath string
	SessionTokenPath  string
	OllamaBaseURL     string
	DefaultModel      string
	OllamaTimeout     time.Duration
	OwnerID           string
	MigrationsDir     string
}

func Load() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home directory: %w", err)
	}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(homeDir, defaultDataDirName)
	}

	paths := BuildPaths(dataDir)
	migrationsDir := resolvePath(getEnv("MIGRATIONS_DIR", ""), []string{
		"app/migrations",
		"migrations",
		"../app/migrations",
		"../migrations",
	})

	cfg := Config{
		DataDir:           paths.DataDir,
		DBPath:            paths.DBPath,
		EncryptionKeyPath: paths.EncryptionKeyPath,
		SessionTokenPath:  paths.SessionTokenPath,
		OllamaBaseURL:     getEnv("OLLAMA_BASE_URL", defaultOllamaBaseURL),
		DefaultModel:      getEnv("OLLAMA_MODEL", defaultModel),
		OllamaTimeout:     getEnvDurationSeconds("OLLAMA_TIMEOUT_SECONDS", defaultOllamaTimeout),
		OwnerID:           "cli:owner",
		MigrationsDir:     migrationsDir,
	}

	if err := ensureDir(paths.DataSubdir, 0o700); err != nil {
		return Config{}, fmt.Errorf("initialize data directory: %w", err)
	}

	return cfg, nil
}

type Paths struct {
	DataDir           string
	DataSubdir        string
	DBPath            string
	EncryptionKeyPath string
	SessionTokenPath  string
}

func BuildPaths(dataDir string) Paths {
	dataDir = strings.TrimSpace(dataDir)
	dataSubdir := filepath.Join(dataDir, "data")
	return Paths{
		DataDir:           dataDir,
		DataSubdir:        dataSubdir,
		DBPath:            filepath.Join(dataSubdir, "agent.db"),
		EncryptionKeyPath: filepath.Join(dataSubdir, "secret.key"),
		SessionTokenPath:  filepath.Join(dataSubdir, "session.token"),
	}
}

func DefaultDataDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(homeDir, defaultDataDirName), nil
}

func ensureDir(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func getEnv(key string, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

func getEnvDurationSeconds(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(val)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func resolvePath(explicit string, candidates []string) string {
	if explicit != "" {
		return explicit
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}
