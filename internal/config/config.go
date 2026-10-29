package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config contains runtime configuration loaded from environment variables.
type Config struct {
	Port                   string
	DatabaseURL            string
	DatabaseMaxConns       int32
	DatabaseMinConns       int32
	DatabaseMaxConnLife    time.Duration
	DatabaseMaxConnIdle    time.Duration
	LogLevel               string
	Environment            string
	WorkerEnabled          bool
	WorkerPollInterval     time.Duration
	WorkerBatchSize        int
	IdempotencyExpiryHours int
	PlatformFeeBps         int64 // Basis points: e.g. 290 = 2.9%
	PlatformFeeFixedMinor  int64 // Fixed minor units: e.g. 30 = $0.30
}

// Load reads and parses configuration from environment variables with safe defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                   getEnv("PORT", "8080"),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/payflow?sslmode=disable"),
		DatabaseMaxConns:       getEnvInt32("DATABASE_MAX_CONNS", 25),
		DatabaseMinConns:       getEnvInt32("DATABASE_MIN_CONNS", 5),
		DatabaseMaxConnLife:    getEnvDuration("DATABASE_MAX_CONN_LIFETIME", 30*time.Minute),
		DatabaseMaxConnIdle:    getEnvDuration("DATABASE_MAX_CONN_IDLE", 5*time.Minute),
		LogLevel:               getEnv("LOG_LEVEL", "INFO"),
		Environment:            getEnv("ENVIRONMENT", "development"),
		WorkerEnabled:          getEnvBool("WORKER_ENABLED", true),
		WorkerPollInterval:     getEnvDuration("WORKER_POLL_INTERVAL", 1*time.Second),
		WorkerBatchSize:        getEnvInt("WORKER_BATCH_SIZE", 20),
		IdempotencyExpiryHours: getEnvInt("IDEMPOTENCY_EXPIRY_HOURS", 24),
		PlatformFeeBps:         getEnvInt64("PLATFORM_FEE_BPS", 290),      // 2.9%
		PlatformFeeFixedMinor:  getEnvInt64("PLATFORM_FEE_FIXED_MINOR", 30), // $0.30 (30 cents)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks for mandatory fields and sensible bounds.
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.Port == "" {
		return errors.New("PORT is required")
	}
	if c.DatabaseMaxConns <= 0 {
		return errors.New("DATABASE_MAX_CONNS must be positive")
	}
	if c.DatabaseMinConns < 0 || c.DatabaseMinConns > c.DatabaseMaxConns {
		return errors.New("DATABASE_MIN_CONNS must be >= 0 and <= DATABASE_MAX_CONNS")
	}
	if c.WorkerBatchSize <= 0 || c.WorkerBatchSize > 500 {
		return errors.New("WORKER_BATCH_SIZE must be between 1 and 500")
	}
	if c.IdempotencyExpiryHours <= 0 {
		return errors.New("IDEMPOTENCY_EXPIRY_HOURS must be positive")
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvInt32(key string, defaultVal int32) int32 {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.ParseInt(val, 10, 32); err == nil {
			return int32(i)
		}
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
