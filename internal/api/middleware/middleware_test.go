package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/simp-lee/isdict-api/internal/applog"
	"github.com/simp-lee/isdict-api/internal/config"
)

func TestSetupMiddleware_FileLoggingUsesDefaultLoggerForRequestAndRecovery(t *testing.T) {
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	logPath := filepath.Join(t.TempDir(), "requests.log")
	cfg := &config.Config{
		GinMode:         gin.TestMode,
		LogLevel:        "info",
		LogOutput:       "file",
		LogFilePath:     logPath,
		LogMaxSizeMB:    1,
		LogMaxBackups:   1,
		LogMaxAgeDays:   1,
		EnableRequestID: true,
	}

	managedLogger, err := applog.NewConfigured(cfg)
	if err != nil {
		t.Fatalf("applog.NewConfigured() error = %v", err)
	}

	cleanedUp := false
	cleanupLogger := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true
		applog.Cleanup(managedLogger, io.Discard)
	}
	t.Cleanup(cleanupLogger)

	previousDefault := slog.Default()
	slog.SetDefault(managedLogger.With("service", "middleware-test"))
	t.Cleanup(func() {
		slog.SetDefault(previousDefault)
	})

	router := gin.New()
	router.Use(SetupMiddleware(cfg))
	router.GET("/api/v1/request-log", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/api/v1/panic", func(c *gin.Context) {
		panic("middleware panic")
	})

	slog.Info("logger initialized")

	requestRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/request-log?foo=bar&token=secret", nil)
	router.ServeHTTP(requestRecorder, request)
	if requestRecorder.Code != http.StatusOK {
		t.Fatalf("request status = %d, want %d", requestRecorder.Code, http.StatusOK)
	}

	panicRecorder := httptest.NewRecorder()
	panicRequest := httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil)
	router.ServeHTTP(panicRecorder, panicRequest)
	if panicRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want %d", panicRecorder.Code, http.StatusInternalServerError)
	}

	cleanupLogger()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	logOutput := string(content)
	for _, want := range []string{
		"logger initialized",
		"HTTP Request",
		"/api/v1/request-log",
		"foo=bar&token=%5BREDACTED%5D",
		"Panic recovered",
		"/api/v1/panic",
		"middleware panic",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output %q does not contain %q", logOutput, want)
		}
	}

	if strings.Contains(logOutput, "foo=bar&token=secret") {
		t.Fatalf("log output %q leaked sensitive query value", logOutput)
	}
}

func TestSetupMiddleware_UsesStandardErrorEnvelopeForMiddlewareFailures(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		cfg         *config.Config
		setupRoute  func(*gin.Engine)
		warmup      func(*gin.Engine)
		wantStatus  int
		wantCode    string
		wantMessage string
		wantHeader  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "chain error",
			path: "/api/v1/error",
			cfg:  &config.Config{GinMode: gin.TestMode, EnableRequestID: true},
			setupRoute: func(router *gin.Engine) {
				router.GET("/api/v1/error", func(c *gin.Context) {
					_ = c.Error(io.ErrUnexpectedEOF)
				})
			},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_ERROR",
			wantMessage: "An internal error occurred",
		},
		{
			name: "panic recovery",
			path: "/api/v1/panic",
			cfg:  &config.Config{GinMode: gin.TestMode, EnableRequestID: true},
			setupRoute: func(router *gin.Engine) {
				router.GET("/api/v1/panic", func(c *gin.Context) {
					panic("boom")
				})
			},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_ERROR",
			wantMessage: "An internal error occurred",
		},
		{
			name: "rate limit",
			path: "/api/v1/limited",
			cfg: &config.Config{
				GinMode:         gin.TestMode,
				EnableRequestID: true,
				EnableRateLimit: true,
				RateLimitRPS:    1,
				RateLimitBurst:  1,
			},
			setupRoute: func(router *gin.Engine) {
				router.GET("/api/v1/limited", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"status": "ok"})
				})
			},
			warmup: func(router *gin.Engine) {
				first := performMiddlewareRequest(router, http.MethodGet, "/api/v1/limited")
				if first.Code != http.StatusOK {
					t.Fatalf("warmup status = %d, want %d", first.Code, http.StatusOK)
				}
			},
			wantStatus:  http.StatusTooManyRequests,
			wantCode:    "RATE_LIMIT_EXCEEDED",
			wantMessage: "Rate limit exceeded",
			wantHeader: func(t *testing.T, recorder *httptest.ResponseRecorder) {
				t.Helper()
				if got := recorder.Header().Get("Retry-After"); got == "" {
					t.Fatal("expected Retry-After header on rate limited response")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CleanupMiddleware()
			t.Cleanup(CleanupMiddleware)

			router := gin.New()
			router.Use(SetupMiddleware(tt.cfg))
			tt.setupRoute(router)

			if tt.warmup != nil {
				tt.warmup(router)
			}

			recorder := performMiddlewareRequest(router, http.MethodGet, tt.path)
			assertMiddlewareErrorEnvelope(t, recorder, tt.wantStatus, tt.wantCode, tt.wantMessage)
			if tt.wantHeader != nil {
				tt.wantHeader(t, recorder)
			}
		})
	}
}

func TestSetupMiddleware_OptionsRequestsSkipAllRateLimitBuckets(t *testing.T) {
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	cfg := &config.Config{
		GinMode:          gin.TestMode,
		EnableCORS:       true,
		CORSAllowOrigins: "https://web.isdict.test",
		EnableRateLimit:  true,
		RateLimitRPS:     1,
		RateLimitBurst:   1,
		RateLimitPerHour: 1,
		RateLimitPerDay:  1,
	}

	router := gin.New()
	router.Use(SetupMiddleware(cfg))
	router.OPTIONS("/api/v1/limited", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/v1/limited", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	for attempt := 0; attempt < 3; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/limited", nil)
		request.Header.Set("Origin", "https://web.isdict.test")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		router.ServeHTTP(recorder, request)

		if recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("OPTIONS request %d unexpectedly rate limited; body = %s", attempt+1, recorder.Body.String())
		}
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://web.isdict.test" {
			t.Fatalf("OPTIONS request %d Access-Control-Allow-Origin = %q, want %q", attempt+1, got, "https://web.isdict.test")
		}
	}

	firstGet := performMiddlewareRequest(router, http.MethodGet, "/api/v1/limited")
	secondGet := performMiddlewareRequest(router, http.MethodGet, "/api/v1/limited")

	if firstGet.Code != http.StatusOK {
		t.Fatalf("first GET status = %d, want %d; body = %s", firstGet.Code, http.StatusOK, firstGet.Body.String())
	}
	assertMiddlewareErrorEnvelope(t, secondGet, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Rate limit exceeded")
}

// AC-B7-3: OPTIONS 预检响应必须返回包含请求方法的 Access-Control-Allow-Methods，且普通 GET 的 CORS 校验与预检校验保持分离。
func TestSetupMiddleware_CORSPreflightIncludesRequestedMethod(t *testing.T) {
	// REG-019
	// REG-024
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	cfg := &config.Config{
		GinMode:          gin.TestMode,
		EnableCORS:       true,
		CORSAllowOrigins: "https://web.isdict.test",
	}

	router := gin.New()
	router.Use(SetupMiddleware(cfg))
	router.GET("/api/v1/words", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.OPTIONS("/api/v1/words", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/words", nil)
	getRequest.Header.Set("Origin", "https://web.isdict.test")
	router.ServeHTTP(getRecorder, getRequest)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if got := getRecorder.Header().Get("Access-Control-Allow-Origin"); got != "https://web.isdict.test" {
		t.Fatalf("GET Access-Control-Allow-Origin = %q, want %q", got, "https://web.isdict.test")
	}

	optionsRecorder := httptest.NewRecorder()
	optionsRequest := httptest.NewRequest(http.MethodOptions, "/api/v1/words", nil)
	optionsRequest.Header.Set("Origin", "https://web.isdict.test")
	optionsRequest.Header.Set("Access-Control-Request-Method", http.MethodGet)
	router.ServeHTTP(optionsRecorder, optionsRequest)

	if optionsRecorder.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d; body = %s", optionsRecorder.Code, http.StatusNoContent, optionsRecorder.Body.String())
	}
	if got := optionsRecorder.Header().Get("Access-Control-Allow-Origin"); got != "https://web.isdict.test" {
		t.Fatalf("OPTIONS Access-Control-Allow-Origin = %q, want %q", got, "https://web.isdict.test")
	}

	allowMethods := optionsRecorder.Header().Get("Access-Control-Allow-Methods")
	if allowMethods == "" {
		t.Fatal("OPTIONS Access-Control-Allow-Methods = empty, want requested method to be listed")
	}

	requestedMethodListed := false
	for _, method := range strings.Split(allowMethods, ",") {
		if strings.TrimSpace(method) == http.MethodGet {
			requestedMethodListed = true
			break
		}
	}
	if !requestedMethodListed {
		t.Fatalf("OPTIONS Access-Control-Allow-Methods = %q, want to contain %q", allowMethods, http.MethodGet)
	}
}

func TestCleanupMiddleware_ClosesAndResetsCacheState(t *testing.T) {
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	cfg := &config.Config{
		GinMode:             gin.TestMode,
		EnableCache:         true,
		CacheMaxSize:        64,
		CacheExpirationMins: 1,
	}

	firstCounter := 0
	firstRouter := gin.New()
	firstRouter.Use(SetupMiddleware(cfg))
	firstRouter.GET("/api/v1/cache-probe", func(c *gin.Context) {
		firstCounter++
		c.JSON(http.StatusOK, gin.H{"count": firstCounter})
	})

	first := performMiddlewareRequest(firstRouter, http.MethodGet, "/api/v1/cache-probe")
	second := performMiddlewareRequest(firstRouter, http.MethodGet, "/api/v1/cache-probe")
	assertJSONFieldValue(t, first, http.StatusOK, "count", float64(1))
	assertJSONFieldValue(t, second, http.StatusOK, "count", float64(1))
	if firstCounter != 1 {
		t.Fatalf("first router counter = %d, want %d", firstCounter, 1)
	}

	CleanupMiddleware()

	secondCounter := 0
	secondRouter := gin.New()
	secondRouter.Use(SetupMiddleware(cfg))
	secondRouter.GET("/api/v1/cache-probe", func(c *gin.Context) {
		secondCounter++
		c.JSON(http.StatusOK, gin.H{"count": secondCounter})
	})

	rebuiltFirst := performMiddlewareRequest(secondRouter, http.MethodGet, "/api/v1/cache-probe")
	rebuiltSecond := performMiddlewareRequest(secondRouter, http.MethodGet, "/api/v1/cache-probe")
	assertJSONFieldValue(t, rebuiltFirst, http.StatusOK, "count", float64(1))
	assertJSONFieldValue(t, rebuiltSecond, http.StatusOK, "count", float64(1))
	if secondCounter != 1 {
		t.Fatalf("second router counter = %d, want %d", secondCounter, 1)
	}
}

// AC-M005: CleanupMiddleware 必须清理缓存与限流器状态，允许后续测试/路由重新初始化。
func TestCleanupMiddleware_ResetsRateLimiterState(t *testing.T) {
	// REG-018
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	cfg := &config.Config{
		GinMode:         gin.TestMode,
		EnableRateLimit: true,
		RateLimitRPS:    1,
		RateLimitBurst:  1,
	}

	firstRouter := gin.New()
	firstRouter.Use(SetupMiddleware(cfg))
	firstRouter.GET("/api/v1/limited", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	first := performMiddlewareRequest(firstRouter, http.MethodGet, "/api/v1/limited")
	limited := performMiddlewareRequest(firstRouter, http.MethodGet, "/api/v1/limited")
	if first.Code != http.StatusOK {
		t.Fatalf("first router initial status = %d, want %d; body = %s", first.Code, http.StatusOK, first.Body.String())
	}
	assertMiddlewareErrorEnvelope(t, limited, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Rate limit exceeded")

	CleanupMiddleware()

	secondRouter := gin.New()
	secondRouter.Use(SetupMiddleware(cfg))
	secondRouter.GET("/api/v1/limited", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	rebuiltFirst := performMiddlewareRequest(secondRouter, http.MethodGet, "/api/v1/limited")
	rebuiltLimited := performMiddlewareRequest(secondRouter, http.MethodGet, "/api/v1/limited")
	if rebuiltFirst.Code != http.StatusOK {
		t.Fatalf("rebuilt router initial status = %d, want %d; body = %s", rebuiltFirst.Code, http.StatusOK, rebuiltFirst.Body.String())
	}
	assertMiddlewareErrorEnvelope(t, rebuiltLimited, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Rate limit exceeded")
}

func TestSetupMiddleware_TimeoutUsesStandardErrorEnvelope(t *testing.T) {
	CleanupMiddleware()
	t.Cleanup(CleanupMiddleware)

	cfg := &config.Config{
		GinMode:          gin.TestMode,
		EnableCORS:       true,
		CORSAllowOrigins: "https://web.isdict.test",
		EnableRequestID:  true,
		EnableTimeout:    true,
		TimeoutSeconds:   1,
	}

	router := gin.New()
	router.Use(SetupMiddleware(cfg))
	router.GET("/api/v1/slow", func(c *gin.Context) {
		time.Sleep(1100 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"status": "slow"})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil)
	request.Header.Set("Origin", "https://web.isdict.test")
	router.ServeHTTP(recorder, request)

	assertMiddlewareErrorEnvelope(t, recorder, http.StatusRequestTimeout, "REQUEST_TIMEOUT", "Request timed out")
	if got := recorder.Header().Get("X-Timeout"); got != "true" {
		t.Fatalf("X-Timeout = %q, want %q", got, "true")
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://web.isdict.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://web.isdict.test")
	}
}

func performMiddlewareRequest(router http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertMiddlewareErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode, wantMessage string) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, wantStatus, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if success, ok := body["success"].(bool); !ok || success {
		t.Fatalf("success = %#v, want false", body["success"])
	}

	errorObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %#v, want object", body["error"])
	}
	if errorObj["code"] != wantCode {
		t.Fatalf("error.code = %#v, want %q", errorObj["code"], wantCode)
	}
	if errorObj["message"] != wantMessage {
		t.Fatalf("error.message = %#v, want %q", errorObj["message"], wantMessage)
	}
}

func assertJSONFieldValue(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, key string, wantValue any) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body[key] != wantValue {
		t.Fatalf("json field %q = %#v, want %#v", key, body[key], wantValue)
	}
}
