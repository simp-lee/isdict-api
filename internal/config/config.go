package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
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

// Load loads configuration from environment variables
func Load() *Config {
	// Try to load .env file from configs/ directory or fallback locations
	envPaths := []string{
		"configs/api.env",           // configs directory (recommended)
		".env",                      // Current directory (fallback)
		"api/.env",                  // api subdirectory (legacy)
		filepath.Join("..", ".env"), // Parent directory (fallback)
	}

	for _, envPath := range envPaths {
		if err := godotenv.Load(envPath); err == nil {
			log.Printf("Loaded environment variables from %s", envPath)
			break
		}
	}

	cfg := &Config{
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             getEnv("DB_PORT", "5432"),
		DBUser:             getEnv("DB_USER", "postgres"),
		DBPassword:         getEnv("DB_PASSWORD", "postgres"),
		DBName:             getEnv("DB_NAME", "isdict"),
		DBSSLMode:          getEnv("DB_SSLMODE", "prefer"),
		Port:               getEnv("PORT", "8080"),
		DBMaxIdleConns:     getEnvAsInt("DB_MAX_IDLE_CONNS", 10),
		DBMaxOpenConns:     getEnvAsInt("DB_MAX_OPEN_CONNS", 100),
		GinMode:            getEnv("GIN_MODE", "debug"),
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
		log.Fatalf("Invalid configuration: %v", err)
	}

	return cfg
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.DBHost == "" {
		return fmt.Errorf("DB_HOST cannot be empty")
	}
	if c.DBName == "" {
		return fmt.Errorf("DB_NAME cannot be empty")
	}
	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	if c.DBMaxIdleConns < 0 {
		return fmt.Errorf("DB_MAX_IDLE_CONNS must be non-negative")
	}
	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1")
	}
	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
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

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("Warning: Invalid value for %s, using default %d", key, defaultValue)
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
		log.Printf("Warning: Invalid boolean value for %s, using default %t", key, defaultValue)
		return defaultValue
	}
	return value
}
