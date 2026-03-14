package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	defaultLogOutput     = "stdout"
	defaultLogMaxSizeMB  = 100
	defaultLogMaxBackups = 7
	defaultLogMaxAgeDays = 30
	explicitEnvFileVar   = "ISDICT_API_ENV_FILE"
)

// Config holds application configuration
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string
	Port       string
	// Database connection pool settings
	DBMaxIdleConns int
	DBMaxOpenConns int
	// Server settings
	GinMode string // "debug", "release", "test"
	// Log settings
	LogLevel      string // "debug", "info", "warn", "error"
	LogOutput     string // "stdout" or "file"
	LogFilePath   string // Required when LogOutput is "file"
	LogMaxSizeMB  int    // File rotation size threshold in MB
	LogMaxBackups int    // Number of rotated files to retain
	LogMaxAgeDays int    // Maximum age of rotated files in days
	// API limit settings
	APIBatchMaxSize    int // Maximum words per batch request
	APISearchMaxLimit  int // Maximum search results limit
	APISuggestMaxLimit int // Maximum suggestion results limit
	// Middleware settings
	EnableRateLimit     bool   // Enable rate limiting
	RateLimitRPS        int    // Requests per second
	RateLimitBurst      int    // Burst size
	RateLimitPerHour    int    // Maximum requests per hour (0 = disabled)
	RateLimitPerDay     int    // Maximum requests per day (0 = disabled)
	EnableCORS          bool   // Enable CORS middleware
	CORSAllowOrigins    string // CORS allowed origins (comma-separated, use "*" for all)
	EnableTimeout       bool   // Enable request timeout
	TimeoutSeconds      int    // Request timeout in seconds
	EnableRequestID     bool   // Enable request ID middleware
	EnableCache         bool   // Enable response cache
	CacheMaxSize        int    // Cache max size
	CacheExpirationMins int    // Cache expiration in minutes
}

// Load loads configuration from environment variables.
func Load() (*Config, error) {
	if err := loadEnvFile(); err != nil {
		return nil, err
	}

	ginMode := getEnv("GIN_MODE", "debug")

	cfg := &Config{
		DBHost:             getEnvWithAliases("localhost", "DB_HOST", "PGHOST"),
		DBPort:             getEnvWithAliases("5432", "DB_PORT", "PGPORT"),
		DBUser:             getEnvWithAliases("postgres", "DB_USER", "PGUSER"),
		DBPassword:         getEnvWithAliases("postgres", "DB_PASSWORD", "PGPASSWORD"),
		DBName:             getEnvWithAliases("isdict", "DB_NAME", "PGDATABASE"),
		DBSSLMode:          getEnvWithAliases("prefer", "DB_SSLMODE", "PGSSLMODE"),
		Port:               getEnv("PORT", "8080"),
		DBMaxIdleConns:     getEnvAsInt("DB_MAX_IDLE_CONNS", 10),
		DBMaxOpenConns:     getEnvAsInt("DB_MAX_OPEN_CONNS", 100),
		GinMode:            ginMode,
		LogLevel:           getEnv("LOG_LEVEL", defaultLogLevel(ginMode)),
		LogOutput:          getEnv("LOG_OUTPUT", defaultLogOutput),
		LogFilePath:        getEnv("LOG_FILE_PATH", ""),
		LogMaxSizeMB:       getEnvAsInt("LOG_MAX_SIZE_MB", defaultLogMaxSizeMB),
		LogMaxBackups:      getEnvAsInt("LOG_MAX_BACKUPS", defaultLogMaxBackups),
		LogMaxAgeDays:      getEnvAsInt("LOG_MAX_AGE_DAYS", defaultLogMaxAgeDays),
		APIBatchMaxSize:    getEnvAsInt("API_BATCH_MAX_SIZE", 100),
		APISearchMaxLimit:  getEnvAsInt("API_SEARCH_MAX_LIMIT", 100),
		APISuggestMaxLimit: getEnvAsInt("API_SUGGEST_MAX_LIMIT", 50),
		// Middleware settings
		EnableRateLimit:     getEnvAsBool("ENABLE_RATE_LIMIT", true),
		RateLimitRPS:        getEnvAsInt("RATE_LIMIT_RPS", 100),
		RateLimitBurst:      getEnvAsInt("RATE_LIMIT_BURST", 200),
		RateLimitPerHour:    getEnvAsInt("RATE_LIMIT_PER_HOUR", 10000),
		RateLimitPerDay:     getEnvAsInt("RATE_LIMIT_PER_DAY", 0), // Disabled by default
		EnableCORS:          getEnvAsBool("ENABLE_CORS", true),
		CORSAllowOrigins:    getEnv("CORS_ALLOW_ORIGINS", "*"),
		EnableTimeout:       getEnvAsBool("ENABLE_TIMEOUT", true),
		TimeoutSeconds:      getEnvAsInt("TIMEOUT_SECONDS", 30),
		EnableRequestID:     getEnvAsBool("ENABLE_REQUEST_ID", true),
		EnableCache:         getEnvAsBool("ENABLE_CACHE", true),
		CacheMaxSize:        getEnvAsInt("CACHE_MAX_SIZE", 1000),
		CacheExpirationMins: getEnvAsInt("CACHE_EXPIRATION_MINS", 5),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func loadEnvFile() error {
	if envPath := strings.TrimSpace(os.Getenv(explicitEnvFileVar)); envPath != "" {
		if err := godotenv.Load(envPath); err != nil {
			return fmt.Errorf("load %s from %s: %w", explicitEnvFileVar, envPath, err)
		}
		slog.Info("loaded environment variables", "path", envPath, "source", explicitEnvFileVar)
		return nil
	}

	for _, envPath := range []string{
		".env",
		"api/.env",
		filepath.Join("..", ".env"),
	} {
		err := godotenv.Load(envPath)
		if err == nil {
			slog.Info("loaded environment variables", "path", envPath, "source", "fallback")
			break
		}
		if isMissingEnvFileError(err) {
			continue
		}
		return fmt.Errorf("load fallback env file %s: %w", envPath, err)
	}

	return nil
}

func isMissingEnvFileError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || os.IsNotExist(err)
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	for _, validate := range []func() error{
		c.validateRequiredFields,
		c.validateConnectionPool,
		c.normalizeModes,
		c.validateLogging,
		c.validateRateLimit,
		c.validateTimeout,
		c.validateCache,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateRequiredFields() error {
	if c.DBHost == "" {
		return fmt.Errorf("DB_HOST cannot be empty")
	}
	if c.DBName == "" {
		return fmt.Errorf("DB_NAME cannot be empty")
	}
	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	if c.APIBatchMaxSize < 1 {
		return fmt.Errorf("API_BATCH_MAX_SIZE must be at least 1")
	}
	if c.APISearchMaxLimit < 1 {
		return fmt.Errorf("API_SEARCH_MAX_LIMIT must be at least 1")
	}
	if c.APISuggestMaxLimit < 1 {
		return fmt.Errorf("API_SUGGEST_MAX_LIMIT must be at least 1")
	}
	return nil
}

func (c *Config) validateConnectionPool() error {
	if c.DBMaxIdleConns < 0 {
		return fmt.Errorf("DB_MAX_IDLE_CONNS must be non-negative")
	}
	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1")
	}
	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	return nil
}

func (c *Config) normalizeModes() error {
	ginMode := normalizeConfigValue(c.GinMode)
	if ginMode == "" {
		ginMode = "debug"
	}
	if !isAllowedValue(ginMode, "debug", "release", "test") {
		return fmt.Errorf("GIN_MODE must be one of: debug, release, test")
	}
	c.GinMode = ginMode

	logLevel := normalizeConfigValue(c.LogLevel)
	if logLevel == "" {
		logLevel = defaultLogLevel(c.GinMode)
	}
	if !isAllowedValue(logLevel, "debug", "info", "warn", "error") {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}
	c.LogLevel = logLevel
	return nil
}

func (c *Config) validateLogging() error {
	logOutput := normalizeConfigValue(c.LogOutput)
	if logOutput == "" {
		logOutput = defaultLogOutput
	}
	if !isAllowedValue(logOutput, "stdout", "file") {
		return fmt.Errorf("LOG_OUTPUT must be one of: stdout, file")
	}
	c.LogOutput = logOutput
	c.LogFilePath = strings.TrimSpace(c.LogFilePath)
	if logOutput == "file" && c.LogFilePath == "" {
		return fmt.Errorf("LOG_FILE_PATH cannot be empty when LOG_OUTPUT=file")
	}
	if c.LogMaxSizeMB < 0 {
		return fmt.Errorf("LOG_MAX_SIZE_MB must be non-negative")
	}
	if c.LogMaxBackups < 0 {
		return fmt.Errorf("LOG_MAX_BACKUPS must be non-negative")
	}
	if c.LogMaxAgeDays < 0 {
		return fmt.Errorf("LOG_MAX_AGE_DAYS must be non-negative")
	}
	return nil
}

func (c *Config) validateRateLimit() error {
	if !c.EnableRateLimit {
		return nil
	}
	if c.RateLimitRPS < 0 {
		return fmt.Errorf("RATE_LIMIT_RPS must be non-negative when ENABLE_RATE_LIMIT=true")
	}
	if c.RateLimitBurst < 0 {
		return fmt.Errorf("RATE_LIMIT_BURST must be non-negative when ENABLE_RATE_LIMIT=true")
	}
	if c.RateLimitRPS > 0 && c.RateLimitBurst < 1 {
		return fmt.Errorf("RATE_LIMIT_BURST must be at least 1 when RATE_LIMIT_RPS > 0")
	}
	if c.RateLimitPerHour < 0 {
		return fmt.Errorf("RATE_LIMIT_PER_HOUR must be non-negative when ENABLE_RATE_LIMIT=true")
	}
	if c.RateLimitPerDay < 0 {
		return fmt.Errorf("RATE_LIMIT_PER_DAY must be non-negative when ENABLE_RATE_LIMIT=true")
	}
	return nil
}

func (c *Config) validateTimeout() error {
	if c.EnableTimeout && c.TimeoutSeconds < 1 {
		return fmt.Errorf("TIMEOUT_SECONDS must be at least 1 when ENABLE_TIMEOUT=true")
	}
	return nil
}

func (c *Config) validateCache() error {
	if !c.EnableCache {
		return nil
	}
	if c.CacheMaxSize < 0 {
		return fmt.Errorf("CACHE_MAX_SIZE must be non-negative when ENABLE_CACHE=true")
	}
	if c.CacheExpirationMins < 0 {
		return fmt.Errorf("CACHE_EXPIRATION_MINS must be non-negative when ENABLE_CACHE=true")
	}
	return nil
}

// GetDSN returns the database connection string
func (c *Config) GetDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvWithAliases(defaultValue string, keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		slog.Warn("invalid integer config value, using default", "key", key, "default", defaultValue, "value", valueStr)
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		slog.Warn("invalid boolean config value, using default", "key", key, "default", defaultValue, "value", valueStr)
		return defaultValue
	}
	return value
}

func defaultLogLevel(ginMode string) string {
	switch normalizeConfigValue(ginMode) {
	case "release":
		return "info"
	default:
		return "debug"
	}
}

func normalizeConfigValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAllowedValue(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}
