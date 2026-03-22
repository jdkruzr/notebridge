package config

import (
	"fmt"
	"os"
)

// Config holds all NoteBridge configuration loaded from environment variables.
type Config struct {
	// Database
	DBPath string

	// Storage
	StoragePath string
	BackupPath  string
	CachePath   string

	// Network
	WebListenAddr  string
	SyncListenAddr string

	// Logging
	LogLevel  string
	LogFormat string

	// Authentication
	UserEmail        string
	UserPasswordHash string
}

// Load reads configuration from environment variables with defaults.
// Returns an error if required fields are missing.
func Load() (*Config, error) {
	cfg := &Config{
		DBPath:           envOrDefault("NB_DB_PATH", "/data/notebridge.db"),
		StoragePath:      envOrDefault("NB_STORAGE_PATH", "/data/storage"),
		BackupPath:       envOrDefault("NB_BACKUP_PATH", "/data/backups"),
		CachePath:        envOrDefault("NB_CACHE_PATH", "/data/cache"),
		WebListenAddr:    envOrDefault("NB_WEB_LISTEN_ADDR", ":8443"),
		SyncListenAddr:   envOrDefault("NB_SYNC_LISTEN_ADDR", ":19071"),
		LogLevel:         envOrDefault("NB_LOG_LEVEL", "info"),
		LogFormat:        envOrDefault("NB_LOG_FORMAT", "json"),
		UserEmail:        os.Getenv("NB_USER_EMAIL"),
		UserPasswordHash: os.Getenv("NB_USER_PASSWORD_HASH"),
	}

	// Validate required fields
	if cfg.UserEmail == "" {
		return nil, fmt.Errorf("missing required field: NB_USER_EMAIL")
	}
	if cfg.UserPasswordHash == "" {
		return nil, fmt.Errorf("missing required field: NB_USER_PASSWORD_HASH")
	}

	return cfg, nil
}

// envOrDefault returns the environment variable value, or the default if not set.
func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
