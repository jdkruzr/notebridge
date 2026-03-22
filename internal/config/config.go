package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all NoteBridge configuration loaded from environment variables.
type Config struct {
	// Database
	DBPath string

	// Storage
	StoragePath   string
	BackupPath    string
	CachePath     string
	BlobStorePath string
	ChunkStorePath string

	// Network
	WebListenAddr  string
	SyncListenAddr string

	// Logging
	LogLevel  string
	LogFormat string

	// Authentication
	UserEmail        string
	UserPasswordHash string

	// OCR Pipeline
	OCREnabled     bool
	OCRAPIFormat   string
	OCRAPIURL      string
	OCRAPIKey      string
	OCRModel       string
	OCRConcurrency int
	OCRMaxFileMB   int
}

// Load reads configuration from environment variables with defaults.
// Returns an error if required fields are missing.
func Load() (*Config, error) {
	ocrEnabledStr := envOrDefault("NB_OCR_ENABLED", "false")
	ocrEnabled := ocrEnabledStr == "true" || ocrEnabledStr == "1"

	ocrConcurrencyStr := envOrDefault("NB_OCR_CONCURRENCY", "1")
	ocrConcurrency := 1
	if n, err := strconv.Atoi(ocrConcurrencyStr); err == nil && n > 0 {
		ocrConcurrency = n
	}

	ocrMaxFileMBStr := envOrDefault("NB_OCR_MAX_FILE_MB", "50")
	ocrMaxFileMB := 50
	if n, err := strconv.Atoi(ocrMaxFileMBStr); err == nil && n > 0 {
		ocrMaxFileMB = n
	}

	cfg := &Config{
		DBPath:           envOrDefault("NB_DB_PATH", "/data/notebridge.db"),
		StoragePath:      envOrDefault("NB_STORAGE_PATH", "/data/storage"),
		BackupPath:       envOrDefault("NB_BACKUP_PATH", "/data/backups"),
		CachePath:        envOrDefault("NB_CACHE_PATH", "/data/cache"),
		BlobStorePath:    envOrDefault("NB_BLOB_STORE_PATH", "/data/blobs"),
		ChunkStorePath:   envOrDefault("NB_CHUNK_STORE_PATH", "/data/chunks"),
		WebListenAddr:    envOrDefault("NB_WEB_LISTEN_ADDR", ":8443"),
		SyncListenAddr:   envOrDefault("NB_SYNC_LISTEN_ADDR", ":19071"),
		LogLevel:         envOrDefault("NB_LOG_LEVEL", "info"),
		LogFormat:        envOrDefault("NB_LOG_FORMAT", "json"),
		UserEmail:        os.Getenv("NB_USER_EMAIL"),
		UserPasswordHash: os.Getenv("NB_USER_PASSWORD_HASH"),
		OCREnabled:       ocrEnabled,
		OCRAPIFormat:     envOrDefault("NB_OCR_FORMAT", "anthropic"),
		OCRAPIURL:        os.Getenv("NB_OCR_API_URL"),
		OCRAPIKey:        os.Getenv("NB_OCR_API_KEY"),
		OCRModel:         os.Getenv("NB_OCR_MODEL"),
		OCRConcurrency:   ocrConcurrency,
		OCRMaxFileMB:     ocrMaxFileMB,
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
