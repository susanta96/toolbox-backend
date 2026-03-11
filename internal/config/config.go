package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Port            string
	DatabaseURL     string
	UploadDir       string
	GeneratedDir    string
	MaxUploadSizeMB int64
	MaxMergeFiles   int
	CleanupInterval time.Duration
	FileRetention   time.Duration
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		UploadDir:       getEnv("UPLOAD_DIR", "./uploads"),
		GeneratedDir:    getEnv("GENERATED_DIR", "./generated"),
		MaxUploadSizeMB: getEnvInt("MAX_UPLOAD_SIZE_MB", 50),
		MaxMergeFiles:   int(getEnvInt("MAX_MERGE_FILES", 10)),
		CleanupInterval: getEnvDuration("CLEANUP_INTERVAL", 10*time.Minute),
		FileRetention:   getEnvDuration("FILE_RETENTION", 1*time.Hour),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
