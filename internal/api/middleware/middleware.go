package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
	"github.com/simp-lee/ginx"

	"github.com/simp-lee/isdict-api/internal/config"
)

var cache shardedcache.CacheInterface

// SetupMiddleware configures and returns the middleware chain
func SetupMiddleware(cfg *config.Config) gin.HandlerFunc {
	// Initialize cache if enabled
	if cfg.EnableCache {
		cache = shardedcache.NewCache(shardedcache.Options{
			MaxSize:           cfg.CacheMaxSize,
			DefaultExpiration: time.Duration(cfg.CacheExpirationMins) * time.Minute,
			ShardCount:        16,              // Concurrent access optimization
			CleanupInterval:   1 * time.Minute, // Automatic cleanup
		})
	}

	// Define path conditions
	isAPIPath := ginx.PathHasPrefix("/api/")
	isHealthPath := ginx.PathIs("/health")
	isStaticPath := ginx.Or(
		ginx.PathHasPrefix("/static/"),
		ginx.PathIs("/", "/favicon.ico"),
	)

	// Build middleware chain
	chain := ginx.NewChain().
		// Error handler - returns JSON error responses
		OnError(func(c *gin.Context, err error) {
			requestID, _ := ginx.GetRequestID(c)
			c.JSON(500, gin.H{
				"error":      "Internal server error",
				"request_id": requestID,
			})
		})

	// Request ID - always enabled for better debugging
	if cfg.EnableRequestID {
		chain = chain.Use(ginx.RequestID())
	}

	// Recovery - panic protection with logging
	chain = chain.Use(ginx.Recovery())

	// Logger - structured request logging
	chain = chain.Use(ginx.Logger())

	// Timeout - different timeouts for different paths
	if cfg.EnableTimeout {
		timeoutDuration := time.Duration(cfg.TimeoutSeconds) * time.Second
		// Skip timeout for health check and static files
		chain = chain.Unless(
			ginx.Or(isHealthPath, isStaticPath),
			ginx.Timeout(ginx.WithTimeout(timeoutDuration)),
		)
	}

	// CORS - cross-origin resource sharing
	if cfg.EnableCORS {
		origins := parseOrigins(cfg.CORSAllowOrigins)
		chain = chain.Use(ginx.CORS(
			ginx.WithAllowOrigins(origins...),
			ginx.WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS"),
			ginx.WithAllowHeaders("Content-Type", "Authorization", "Cache-Control", "X-Requested-With"),
			ginx.WithAllowCredentials(false), // Set to true if you need cookies/auth
		))
	}

	// Rate limiting - protect against abuse
	if cfg.EnableRateLimit {
		// Skip rate limiting for health checks and static files
		skipCondition := ginx.Or(isHealthPath, isStaticPath)

		// RPS rate limiting (token bucket)
		if cfg.RateLimitRPS > 0 {
			chain = chain.Unless(
				skipCondition,
				ginx.RateLimit(cfg.RateLimitRPS, cfg.RateLimitBurst),
			)
		}

		// Hourly rate limiting
		if cfg.RateLimitPerHour > 0 {
			chain = chain.Unless(
				skipCondition,
				ginx.RateLimitPerHour(cfg.RateLimitPerHour),
			)
		}

		// Daily rate limiting
		if cfg.RateLimitPerDay > 0 {
			chain = chain.Unless(
				skipCondition,
				ginx.RateLimitPerDay(cfg.RateLimitPerDay),
			)
		}
	}

	// Cache - response caching for GET requests on API paths
	if cfg.EnableCache && cache != nil {
		chain = chain.When(
			ginx.And(ginx.MethodIs("GET"), isAPIPath),
			ginx.CacheWithGroup(cache, "api"),
		)
	}

	return chain.Build()
}

// parseOrigins parses comma-separated origins
func parseOrigins(origins string) []string {
	if origins == "" {
		return []string{"*"}
	}
	parts := strings.Split(origins, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"*"}
	}
	return result
}

// CleanupMiddleware cleans up middleware resources
func CleanupMiddleware() {
	// Cleanup rate limiters
	ginx.CleanupRateLimiters()
}
