package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/simp-lee/isdict-api/internal/api/middleware"
	"github.com/simp-lee/isdict-api/internal/applog"
	"github.com/simp-lee/isdict-api/internal/config"
)

func TestNewAppLogger_UsesStdoutOutput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			t.Fatalf("reader.Close() error = %v", closeErr)
		}
	}()

	originalStdout := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	logger, err := newAppLogger(&config.Config{
		GinMode:   gin.TestMode,
		LogLevel:  "debug",
		LogOutput: "stdout",
	})
	if err != nil {
		t.Fatalf("newAppLogger() error = %v", err)
	}

	logger.With("test", "stdout").Info("stdout logger initialized")
	cleanupLogger(logger, io.Discard)
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if !strings.Contains(string(output), "stdout logger initialized") {
		t.Fatalf("stdout output %q does not contain log message", string(output))
	}
}

func TestNewAppLogger_UsesFileOutput(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "isdict-api.log")
	logger, err := newAppLogger(&config.Config{
		GinMode:       gin.ReleaseMode,
		LogLevel:      "info",
		LogOutput:     "file",
		LogFilePath:   logPath,
		LogMaxSizeMB:  1,
		LogMaxBackups: 1,
		LogMaxAgeDays: 1,
	})
	if err != nil {
		t.Fatalf("newAppLogger() error = %v", err)
	}

	logger.With("test", "file").Info("file logger initialized")
	cleanupLogger(logger, io.Discard)

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "file logger initialized") {
		t.Fatalf("file output %q does not contain log message", string(content))
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info default", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "normalized", input: " WARN ", want: slog.LevelWarn},
		{name: "unknown defaults to info", input: "verbose", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applog.ParseLevel(tt.input); got != tt.want {
				t.Fatalf("applog.ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRunWithDependencies_CleansUpLoggerOnStartupFailure(t *testing.T) {
	bootstrapLogger := newStubManagedLogger()
	appLogger := newStubManagedLogger()

	exitCode := runWithDependencies(
		func() (*config.Config, error) {
			return &config.Config{
				DBHost:         "localhost",
				DBPort:         "5432",
				DBUser:         "postgres",
				DBPassword:     "postgres",
				DBName:         "isdict",
				DBSSLMode:      "disable",
				Port:           "8080",
				DBMaxIdleConns: 1,
				DBMaxOpenConns: 1,
				GinMode:        gin.TestMode,
				LogLevel:       "debug",
				LogOutput:      "stdout",
			}, nil
		},
		func() (managedLogger, error) { return bootstrapLogger, nil },
		func(*config.Config) (managedLogger, error) { return appLogger, nil },
		func(*config.Config) (*gorm.DB, error) { return nil, errors.New("database unavailable") },
		func(*gorm.DB) error { return nil },
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want %d", exitCode, 1)
	}
	if bootstrapLogger.syncCalls != 1 || bootstrapLogger.closeCalls != 1 {
		t.Fatalf("bootstrap cleanup = sync %d close %d, want 1/1", bootstrapLogger.syncCalls, bootstrapLogger.closeCalls)
	}
	if appLogger.syncCalls != 1 || appLogger.closeCalls != 1 {
		t.Fatalf("app logger cleanup = sync %d close %d, want 1/1", appLogger.syncCalls, appLogger.closeCalls)
	}
}

func TestRunWithDependencies_RestoresDefaultLoggerAcrossRepeatedRuns(t *testing.T) {
	originalDefault := slog.New(slog.NewTextHandler(io.Discard, nil))
	previousDefault := slog.Default()
	slog.SetDefault(originalDefault)
	defer slog.SetDefault(previousDefault)

	for runIndex := 0; runIndex < 2; runIndex++ {
		bootstrapLogger := newStubManagedLogger()
		appLogger := newStubManagedLogger()

		exitCode := runWithDependencies(
			func() (*config.Config, error) {
				return &config.Config{
					DBHost:         "localhost",
					DBPort:         "5432",
					DBUser:         "postgres",
					DBPassword:     "postgres",
					DBName:         "isdict",
					DBSSLMode:      "disable",
					Port:           "8080",
					DBMaxIdleConns: 1,
					DBMaxOpenConns: 1,
					GinMode:        gin.TestMode,
					LogLevel:       "debug",
					LogOutput:      "stdout",
				}, nil
			},
			func() (managedLogger, error) { return bootstrapLogger, nil },
			func(*config.Config) (managedLogger, error) { return appLogger, nil },
			func(*config.Config) (*gorm.DB, error) { return nil, errors.New("database unavailable") },
			func(*gorm.DB) error { return nil },
		)

		if exitCode != 1 {
			t.Fatalf("runWithDependencies() run %d = %d, want %d", runIndex+1, exitCode, 1)
		}
		if slog.Default() != originalDefault {
			t.Fatalf("default logger after run %d = %p, want %p", runIndex+1, slog.Default(), originalDefault)
		}
	}
}

func TestRunWithDependencies_FailsWhenRequiredExtensionSetupFails(t *testing.T) {
	bootstrapLogger := newStubManagedLogger()
	appLogger := newStubManagedLogger()
	ensureCalls := 0
	gormDB, sqlDB := newMockGORMDB(t)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	exitCode := runWithDependencies(
		func() (*config.Config, error) {
			return &config.Config{
				DBHost:         "localhost",
				DBPort:         "5432",
				DBUser:         "postgres",
				DBPassword:     "postgres",
				DBName:         "isdict",
				DBSSLMode:      "disable",
				Port:           "8080",
				DBMaxIdleConns: 1,
				DBMaxOpenConns: 1,
				GinMode:        gin.TestMode,
				LogLevel:       "debug",
				LogOutput:      "stdout",
			}, nil
		},
		func() (managedLogger, error) { return bootstrapLogger, nil },
		func(*config.Config) (managedLogger, error) { return appLogger, nil },
		func(*config.Config) (*gorm.DB, error) { return gormDB, nil },
		func(*gorm.DB) error {
			ensureCalls++
			return errors.New("enable required extension pg_trgm: permission denied")
		},
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want %d", exitCode, 1)
	}
	if ensureCalls != 1 {
		t.Fatalf("ensureRequiredExtensions() calls = %d, want 1", ensureCalls)
	}
	if bootstrapLogger.syncCalls != 1 || bootstrapLogger.closeCalls != 1 {
		t.Fatalf("bootstrap cleanup = sync %d close %d, want 1/1", bootstrapLogger.syncCalls, bootstrapLogger.closeCalls)
	}
	if appLogger.syncCalls != 1 || appLogger.closeCalls != 1 {
		t.Fatalf("app logger cleanup = sync %d close %d, want 1/1", appLogger.syncCalls, appLogger.closeCalls)
	}
}

func TestRunWithDependencies_ClosesDatabaseWhenRequiredExtensionSetupFails(t *testing.T) {
	bootstrapLogger := newStubManagedLogger()
	appLogger := newStubManagedLogger()
	gormDB, mockDB := newMockGORMDB(t)

	exitCode := runWithDependencies(
		func() (*config.Config, error) {
			return &config.Config{
				DBHost:         "localhost",
				DBPort:         "5432",
				DBUser:         "postgres",
				DBPassword:     "postgres",
				DBName:         "isdict",
				DBSSLMode:      "disable",
				Port:           "8080",
				DBMaxIdleConns: 1,
				DBMaxOpenConns: 1,
				GinMode:        gin.TestMode,
				LogLevel:       "debug",
				LogOutput:      "stdout",
			}, nil
		},
		func() (managedLogger, error) { return bootstrapLogger, nil },
		func(*config.Config) (managedLogger, error) { return appLogger, nil },
		func(*config.Config) (*gorm.DB, error) { return gormDB, nil },
		func(*gorm.DB) error { return errors.New("enable required extension pg_trgm: permission denied") },
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want %d", exitCode, 1)
	}
	if err := mockDB.Ping(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("database should be closed after extension setup failure, ping error = %v", err)
	}
}

func newMockGORMDB(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open() error = %v", err)
	}

	return gormDB, sqlDB
}

func TestServeServer_CleansUpMiddlewareOnAllExitPaths(t *testing.T) {
	tests := []struct {
		name          string
		listenErr     error
		shutdownErr   error
		quitSignal    os.Signal
		wantExitCode  int
		wantCallOrder []string
	}{
		{
			name:          "cleanup runs after successful shutdown",
			listenErr:     http.ErrServerClosed,
			quitSignal:    syscall.SIGTERM,
			wantExitCode:  0,
			wantCallOrder: []string{"shutdown", "cleanup"},
		},
		{
			name:          "cleanup still runs when shutdown fails",
			listenErr:     http.ErrServerClosed,
			shutdownErr:   errors.New("shutdown failed"),
			quitSignal:    syscall.SIGTERM,
			wantExitCode:  1,
			wantCallOrder: []string{"shutdown", "cleanup"},
		},
		{
			name:          "cleanup runs on unexpected listen failure",
			listenErr:     errors.New("listen failed"),
			wantExitCode:  1,
			wantCallOrder: []string{"cleanup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]string, 0, 2)
			quit := make(chan os.Signal, 1)
			if tt.quitSignal != nil {
				quit <- tt.quitSignal
			}

			exitCode := serveServer(&stubLifecycleServer{
				listenErr: tt.listenErr,
				shutdownFunc: func(context.Context) error {
					calls = append(calls, "shutdown")
					return tt.shutdownErr
				},
			}, ":8080", quit, slog.New(slog.NewTextHandler(io.Discard, nil)), func() {
				calls = append(calls, "cleanup")
			}, gracefulShutdown)

			if exitCode != tt.wantExitCode {
				t.Fatalf("serveServer() = %d, want %d", exitCode, tt.wantExitCode)
			}
			if !reflect.DeepEqual(calls, tt.wantCallOrder) {
				t.Fatalf("serveServer() call order = %v, want %v", calls, tt.wantCallOrder)
			}
		})
	}
}

func TestGracefulShutdown_ReturnsServerError(t *testing.T) {
	err := gracefulShutdown(shutdownServerFunc(func(context.Context) error {
		return errors.New("shutdown failed")
	}))
	if err == nil || err.Error() != "shutdown failed" {
		t.Fatalf("gracefulShutdown() error = %v, want shutdown failed", err)
	}
}

func TestSetupRouter_LivenessAlwaysReturnsOK(t *testing.T) {
	tests := []struct {
		name    string
		pingErr error
	}{
		{name: "database available"},
		{name: "database unavailable", pingErr: errors.New("db unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupRouter(nil, &config.Config{}, newPingTestDB(t, tt.pingErr))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/health", nil)

			router.ServeHTTP(recorder, request)

			assertHealthResponse(t, recorder, http.StatusOK, map[string]any{
				"status":  "ok",
				"service": "isdict-api",
			})
		})
	}
}

func TestSetupRouter_ReadinessReturnsOKWhenDatabaseIsAvailable(t *testing.T) {
	router := setupRouter(nil, &config.Config{}, newPingTestDB(t, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	router.ServeHTTP(recorder, request)

	assertHealthResponse(t, recorder, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})
}

func TestSetupRouter_ReadinessReturnsServiceUnavailableWhenRequiredExtensionIsMissing(t *testing.T) {
	missing := false
	router := setupRouter(nil, &config.Config{}, newPingTestDBWithBehavior(t, pingTestBehavior{extensionPresent: &missing}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	router.ServeHTTP(recorder, request)

	assertHealthResponse(t, recorder, http.StatusServiceUnavailable, map[string]any{
		"status":  "not_ready",
		"service": "isdict-api",
	})
}

func TestSetupRouter_ReadinessReturnsServiceUnavailableWhenDatabaseIsUnavailable(t *testing.T) {
	router := setupRouter(nil, &config.Config{}, newPingTestDB(t, errors.New("db unavailable")))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	router.ServeHTTP(recorder, request)

	assertHealthResponse(t, recorder, http.StatusServiceUnavailable, map[string]any{
		"status":  "not_ready",
		"service": "isdict-api",
	})
}

func TestSetupRouter_ReadinessDoesNotAttemptToEnableRequiredExtension(t *testing.T) {
	execCalls := 0
	queryCalls := 0
	router := setupRouter(nil, &config.Config{}, newPingTestDBWithBehavior(t, pingTestBehavior{
		onExec: func(string, []driver.NamedValue) {
			execCalls++
		},
		onQuery: func(string, []driver.NamedValue) {
			queryCalls++
		},
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	router.ServeHTTP(recorder, request)

	assertHealthResponse(t, recorder, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})
	if queryCalls != 1 {
		t.Fatalf("queryCalls = %d, want 1", queryCalls)
	}
	if execCalls != 0 {
		t.Fatalf("execCalls = %d, want 0", execCalls)
	}
}

func TestSetupRouter_ReadinessSkipsRateLimiting(t *testing.T) {
	middleware.CleanupMiddleware()
	t.Cleanup(middleware.CleanupMiddleware)

	router := setupRouter(nil, newMiddlewareTestConfig(), newPingTestDB(t, nil))
	router.GET("/api/v1/rate-probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, map[string]any{"status": "ok"})
	})

	firstHealth := performRequest(router, http.MethodGet, "/api/v1/health")
	secondHealth := performRequest(router, http.MethodGet, "/api/v1/health")
	firstProbe := performRequest(router, http.MethodGet, "/api/v1/rate-probe")
	secondProbe := performRequest(router, http.MethodGet, "/api/v1/rate-probe")

	assertHealthResponse(t, firstHealth, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})
	assertHealthResponse(t, secondHealth, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})

	if firstProbe.Code != http.StatusOK {
		t.Fatalf("first probe status = %d, want %d", firstProbe.Code, http.StatusOK)
	}
	if secondProbe.Code != http.StatusTooManyRequests {
		t.Fatalf("second probe status = %d, want %d", secondProbe.Code, http.StatusTooManyRequests)
	}
}

func TestSetupRouter_ReadinessSkipsResponseCaching(t *testing.T) {
	middleware.CleanupMiddleware()
	t.Cleanup(middleware.CleanupMiddleware)

	cfg := newMiddlewareTestConfig()
	cfg.EnableRateLimit = false
	pingCalls := 0
	router := setupRouter(nil, cfg, newPingTestDBWithBehavior(t, pingTestBehavior{
		onPing: func() {
			pingCalls++
		},
	}))
	cacheProbeCounter := 0
	router.GET("/api/v1/cache-probe", func(c *gin.Context) {
		cacheProbeCounter++
		c.JSON(http.StatusOK, map[string]any{"count": cacheProbeCounter})
	})

	firstHealth := performRequest(router, http.MethodGet, "/api/v1/health")
	secondHealth := performRequest(router, http.MethodGet, "/api/v1/health")
	firstProbe := performRequest(router, http.MethodGet, "/api/v1/cache-probe")
	secondProbe := performRequest(router, http.MethodGet, "/api/v1/cache-probe")

	assertHealthResponse(t, firstHealth, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})
	assertHealthResponse(t, secondHealth, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "isdict-api",
	})

	if pingCalls != 2 {
		t.Fatalf("expected readiness requests to bypass cache and ping DB twice, got %d calls", pingCalls)
	}

	assertJSONField(t, firstProbe, http.StatusOK, "count", float64(1))
	assertJSONField(t, secondProbe, http.StatusOK, "count", float64(1))
}

func TestSetupRouter_ReadinessUsesConfiguredTimeout(t *testing.T) {
	middleware.CleanupMiddleware()
	t.Cleanup(middleware.CleanupMiddleware)

	delay := 3200 * time.Millisecond
	cfg := newMiddlewareTestConfig()
	cfg.TimeoutSeconds = 3
	router := setupRouter(nil, cfg, newPingTestDBWithBehavior(t, pingTestBehavior{delay: delay}))
	router.GET("/api/v1/slow-probe", func(c *gin.Context) {
		time.Sleep(delay)
		c.JSON(http.StatusOK, map[string]any{"status": "slow"})
	})

	startedAt := time.Now()
	health := performRequest(router, http.MethodGet, "/api/v1/health")
	healthDuration := time.Since(startedAt)
	probe := performRequest(router, http.MethodGet, "/api/v1/slow-probe")

	assertHealthResponse(t, health, http.StatusServiceUnavailable, map[string]any{
		"status":  "not_ready",
		"service": "isdict-api",
	})
	configuredTimeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if healthDuration < configuredTimeout-time.Second {
		t.Fatalf("expected readiness request to honor configured timeout close to %v, got %v", configuredTimeout, healthDuration)
	}
	if healthDuration > configuredTimeout+time.Second {
		t.Fatalf("expected readiness request to stay near configured timeout %v, got %v", configuredTimeout, healthDuration)
	}
	if got := health.Header().Get("X-Timeout"); got != "" {
		t.Fatalf("expected readiness response to use dedicated timeout instead of middleware timeout header, got %q", got)
	}

	assertErrorEnvelope(t, probe, http.StatusRequestTimeout, "REQUEST_TIMEOUT", "Request timed out")
	if got := probe.Header().Get("X-Timeout"); got != "true" {
		t.Fatalf("expected slow probe to be wrapped by timeout middleware, got X-Timeout=%q", got)
	}
}

func TestSetupRouter_StaticAssetsExposeOnlyRuntimeFiles(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	t.Chdir(repoRoot)

	router := setupRouter(nil, &config.Config{}, newPingTestDB(t, nil))

	jsAsset := performRequest(router, http.MethodGet, "/static/js/alpine.min.js")
	if jsAsset.Code != http.StatusOK {
		t.Fatalf("js asset status = %d, want %d", jsAsset.Code, http.StatusOK)
	}

	testHarness := performRequest(router, http.MethodGet, "/static/dictionary_app.test.mjs")
	if testHarness.Code != http.StatusNotFound {
		t.Fatalf("test harness status = %d, want %d", testHarness.Code, http.StatusNotFound)
	}
}

func assertHealthResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantBody map[string]any) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want %q", contentType, "application/json; charset=utf-8")
	}

	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got) != len(wantBody) {
		t.Fatalf("json field count = %d, want %d; body = %s", len(got), len(wantBody), recorder.Body.String())
	}

	for key, wantValue := range wantBody {
		if got[key] != wantValue {
			t.Fatalf("json field %q = %#v, want %#v", key, got[key], wantValue)
		}
	}

	for _, forbiddenKey := range []string{"success", "data", "error", "meta"} {
		if _, exists := got[forbiddenKey]; exists {
			t.Fatalf("unexpected envelope field %q in response body %s", forbiddenKey, recorder.Body.String())
		}
	}
}

func assertJSONField(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, key string, wantValue any) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}

	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got[key] != wantValue {
		t.Fatalf("json field %q = %#v, want %#v", key, got[key], wantValue)
	}
}

func assertErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode, wantMessage string) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want %q", contentType, "application/json; charset=utf-8")
	}

	var got map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if success, ok := got["success"].(bool); !ok || success {
		t.Fatalf("success = %#v, want false", got["success"])
	}

	errorObj, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %#v, want object", got["error"])
	}
	if errorObj["code"] != wantCode {
		t.Fatalf("error.code = %#v, want %q", errorObj["code"], wantCode)
	}
	if errorObj["message"] != wantMessage {
		t.Fatalf("error.message = %#v, want %q", errorObj["message"], wantMessage)
	}
}

func newPingTestDB(t *testing.T, pingErr error) *sql.DB {
	t.Helper()
	return newPingTestDBWithBehavior(t, pingTestBehavior{err: pingErr})
}

func newPingTestDBWithBehavior(t *testing.T, behavior pingTestBehavior) *sql.DB {
	t.Helper()

	driverName := registerPingTestDriver(behavior)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func registerPingTestDriver(behavior pingTestBehavior) string {
	pingTestDriverRegistry.mu.Lock()
	defer pingTestDriverRegistry.mu.Unlock()

	pingTestDriverRegistry.counter++
	name := pingTestDriverName(pingTestDriverRegistry.counter)
	sql.Register(name, pingTestDriver{behavior: behavior})
	return name
}

func newMiddlewareTestConfig() *config.Config {
	return &config.Config{
		EnableTimeout:       true,
		TimeoutSeconds:      1,
		EnableRateLimit:     true,
		RateLimitRPS:        1,
		RateLimitBurst:      1,
		EnableCache:         true,
		CacheMaxSize:        128,
		CacheExpirationMins: 1,
		EnableRequestID:     true,
	}
}

func performRequest(router http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func pingTestDriverName(index int) string {
	return "ping-test-driver-" + string(rune('a'+index-1))
}

var pingTestDriverRegistry struct {
	mu      sync.Mutex
	counter int
}

type pingTestDriver struct {
	behavior pingTestBehavior
}

type shutdownServerFunc func(context.Context) error

func (fn shutdownServerFunc) Shutdown(ctx context.Context) error {
	return fn(ctx)
}

type stubLifecycleServer struct {
	listenErr     error
	shutdownFunc  func(context.Context) error
	listenStarted chan struct{}
}

func (s *stubLifecycleServer) ListenAndServe() error {
	if s.listenStarted != nil {
		close(s.listenStarted)
	}
	return s.listenErr
}

func (s *stubLifecycleServer) Shutdown(ctx context.Context) error {
	if s.shutdownFunc != nil {
		return s.shutdownFunc(ctx)
	}
	return nil
}

type stubManagedLogger struct {
	base       *slog.Logger
	syncCalls  int
	closeCalls int
}

func newStubManagedLogger() *stubManagedLogger {
	return &stubManagedLogger{
		base: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (l *stubManagedLogger) With(args ...any) *slog.Logger {
	return l.base.With(args...)
}

func (l *stubManagedLogger) Sync() error {
	l.syncCalls++
	return nil
}

func (l *stubManagedLogger) Close() error {
	l.closeCalls++
	return nil
}

func (d pingTestDriver) Open(string) (driver.Conn, error) {
	return pingTestConn(d), nil
}

type pingTestConn struct {
	behavior pingTestBehavior
}

type pingTestBehavior struct {
	err              error
	delay            time.Duration
	queryErr         error
	queryDelay       time.Duration
	onPing           func()
	onQuery          func(string, []driver.NamedValue)
	onExec           func(string, []driver.NamedValue)
	extensionPresent *bool
}

func (c pingTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c pingTestConn) Close() error {
	return nil
}

func (c pingTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c pingTestConn) Ping(ctx context.Context) error {
	if c.behavior.onPing != nil {
		c.behavior.onPing()
	}

	if c.behavior.delay > 0 {
		select {
		case <-time.After(c.behavior.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return c.behavior.err
}

func (c pingTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.behavior.onQuery != nil {
		c.behavior.onQuery(query, args)
	}

	if c.behavior.queryDelay > 0 {
		select {
		case <-time.After(c.behavior.queryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if c.behavior.queryErr != nil {
		return nil, c.behavior.queryErr
	}

	present := true
	if c.behavior.extensionPresent != nil {
		present = *c.behavior.extensionPresent
	}

	return &pingTestRows{value: present}, nil
}

func (c pingTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.behavior.onExec != nil {
		c.behavior.onExec("", nil)
	}
	return driver.RowsAffected(0), nil
}

type pingTestRows struct {
	yielded bool
	value   bool
}

func (r *pingTestRows) Columns() []string {
	return []string{"exists"}
}

func (r *pingTestRows) Close() error {
	return nil
}

func (r *pingTestRows) Next(dest []driver.Value) error {
	if r.yielded {
		return io.EOF
	}

	r.yielded = true
	dest[0] = r.value
	return nil
}
