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
	FXProviderURL   string
	FXCacheTTL      time.Duration
	FXStaleWindow   time.Duration
	FXWarmupEvery   time.Duration
	FXWarmupLimit   int
	FXHTTPTimeout   time.Duration
	FXHistoryKeep   time.Duration
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
		FXProviderURL:   getEnv("FX_PROVIDER_URL", "https://api.frankfurter.dev"),
		FXCacheTTL:      getEnvDuration("FX_CACHE_TTL", 30*time.Minute),
		FXStaleWindow:   getEnvDuration("FX_STALE_WINDOW", 6*time.Hour),
		FXWarmupEvery:   getEnvDuration("FX_WARMUP_EVERY", 30*time.Minute),
		FXWarmupLimit:   int(getEnvInt("FX_WARMUP_LIMIT", 30)),
		FXHTTPTimeout:   getEnvDuration("FX_HTTP_TIMEOUT", 8*time.Second),
		FXHistoryKeep:   getEnvDuration("FX_HISTORY_KEEP", 8760*time.Hour),
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
