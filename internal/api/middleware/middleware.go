package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
	"github.com/simp-lee/ginx"
	"github.com/simp-lee/isdict-commons/model"

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
	isHealthPath := ginx.PathIs("/health", "/api/v1/health")
	isStaticPath := ginx.Or(
		ginx.PathHasPrefix("/static/"),
		ginx.PathIs("/"),
	)

	// Build middleware chain
	chain := ginx.NewChain().
		WithErrorFormat(apiErrorFormatter).
		// Error handler - returns JSON error responses
		OnError(func(c *gin.Context, err error) {
			requestID, _ := ginx.GetRequestID(c)
			slog.Default().Error("middleware chain error",
				"path", c.Request.URL.Path,
				"request_id", requestID,
				"error", err,
			)
			if c.Writer.Written() {
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, apiErrorFormatter(http.StatusInternalServerError, "internal server error"))
		})

	// Request ID - always enabled for better debugging
	if cfg.EnableRequestID {
		chain = chain.Use(ginx.RequestID())
	}

	// Recovery and request logging share the process logger configured via slog.SetDefault.
	chain = chain.Use(recoveryMiddleware())
	chain = chain.Use(requestLoggerMiddleware())

	// CORS must run before timeout so browser clients still receive CORS headers on 408 responses.
	if cfg.EnableCORS {
		origins := parseOrigins(cfg.CORSAllowOrigins)
		chain = chain.Use(ginx.CORS(
			ginx.WithAllowOrigins(origins...),
			ginx.WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS"),
			ginx.WithAllowHeaders("Content-Type", "Authorization", "Cache-Control", "X-Requested-With"),
			ginx.WithAllowCredentials(false), // Set to true if you need cookies/auth
		))
	}

	// Timeout - different timeouts for different paths
	if cfg.EnableTimeout {
		timeoutDuration := time.Duration(cfg.TimeoutSeconds) * time.Second
		// Skip timeout for health check and static files
		chain = chain.Unless(
			ginx.Or(isHealthPath, isStaticPath),
			ginx.Timeout(ginx.WithTimeout(timeoutDuration)),
		)
	}

	// Rate limiting - protect against abuse
	if cfg.EnableRateLimit {
		// Skip rate limiting for health checks and static files
		skipCondition := ginx.Or(isHealthPath, isStaticPath, ginx.MethodIs(http.MethodOptions))

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
			ginx.And(ginx.MethodIs("GET"), isAPIPath, ginx.Not(isHealthPath)),
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

func requestLoggerMiddleware() ginx.Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			start := time.Now()
			next(c)

			status := c.Writer.Status()
			fields := []any{
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"query", sanitizeQueryForLog(c.Request.URL.RawQuery),
				"status", status,
				"latency", time.Since(start),
				"ip", c.ClientIP(),
				"user_agent", c.Request.UserAgent(),
				"size", c.Writer.Size(),
				"protocol", c.Request.Proto,
				"referer", c.Request.Referer(),
			}

			if requestID, ok := ginx.GetRequestID(c); ok && requestID != "" {
				fields = append(fields, "request_id", requestID)
			}

			log := slog.Default()
			switch {
			case status >= http.StatusInternalServerError:
				log.Error("HTTP Request", fields...)
			case status >= http.StatusBadRequest:
				log.Warn("HTTP Request", fields...)
			default:
				log.Info("HTTP Request", fields...)
			}

			if len(c.Errors) > 0 {
				errFields := []any{"path", c.Request.URL.Path, "errors", c.Errors.String()}
				if requestID, ok := ginx.GetRequestID(c); ok && requestID != "" {
					errFields = append(errFields, "request_id", requestID)
				}
				log.Error("Request errors", errFields...)
			}
		}
	}
}

func recoveryMiddleware() ginx.Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			defer func() {
				if err := recover(); err != nil {
					log := slog.Default()

					if isBrokenPipe(err) {
						fields := []any{
							"error", fmt.Sprintf("%v", err),
							"path", c.Request.URL.Path,
							"method", c.Request.Method,
							"ip", c.ClientIP(),
						}
						if requestID, ok := ginx.GetRequestID(c); ok && requestID != "" {
							fields = append(fields, "request_id", requestID)
						}
						log.Warn("Connection broken", fields...)
						if recoveredErr, ok := err.(error); ok {
							_ = c.Error(recoveredErr)
						}
						c.Abort()
						return
					}

					fields := []any{
						"error", fmt.Sprintf("%v", err),
						"path", c.Request.URL.Path,
						"method", c.Request.Method,
						"ip", c.ClientIP(),
						"user_agent", c.Request.UserAgent(),
						"stack", currentStack(),
					}
					if requestID, ok := ginx.GetRequestID(c); ok && requestID != "" {
						fields = append(fields, "request_id", requestID)
					}
					log.Error("Panic recovered", fields...)
					ginx.AbortWithError(c, http.StatusInternalServerError, "internal server error")
				}
			}()

			next(c)
		}
	}
}

func sanitizeQueryForLog(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[UNPARSEABLE_QUERY]"
	}

	for key := range values {
		if isSensitiveQueryKey(key) {
			values.Set(key, "[REDACTED]")
		}
	}

	return values.Encode()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "token", "access_token", "id_token", "jwt", "authorization", "auth", "password", "secret":
		return true
	default:
		return false
	}
}

func apiErrorFormatter(status int, message string) any {
	code, normalizedMessage := apiErrorDetails(status, message)
	return model.NewErrorResponse(code, normalizedMessage, nil)
}

func apiErrorDetails(status int, message string) (string, string) {
	switch status {
	case http.StatusRequestTimeout:
		return "REQUEST_TIMEOUT", "Request timed out"
	case http.StatusTooManyRequests:
		return "RATE_LIMIT_EXCEEDED", "Rate limit exceeded"
	default:
		return "INTERNAL_ERROR", "An internal error occurred"
	}
}

func currentStack() string {
	buf := make([]byte, 4096)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, len(buf)*2)
	}
}

func isBrokenPipe(err any) bool {
	recoveredErr, ok := err.(error)
	if !ok {
		return false
	}

	var opErr *net.OpError
	if !errors.As(recoveredErr, &opErr) {
		return false
	}

	var syscallErr *os.SyscallError
	if !errors.As(opErr, &syscallErr) {
		return false
	}

	message := strings.ToLower(syscallErr.Error())
	return strings.Contains(message, "broken pipe") || strings.Contains(message, "connection reset by peer")
}

// CleanupMiddleware cleans up middleware resources
func CleanupMiddleware() {
	if cache != nil {
		cache.Close()
		cache = nil
	}

	// Cleanup rate limiters
	ginx.CleanupRateLimiters()
}
