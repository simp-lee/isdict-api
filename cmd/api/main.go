package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/simp-lee/isdict-api/internal/api/handler"
	"github.com/simp-lee/isdict-api/internal/api/middleware"
	"github.com/simp-lee/isdict-api/internal/applog"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-data/postgresutil"
	"github.com/simp-lee/isdict-data/repository"
	"github.com/simp-lee/isdict-data/service"
)

type managedLogger = applog.ManagedLogger

type gracefulShutdownServer interface {
	Shutdown(context.Context) error
}

type lifecycleServer interface {
	gracefulShutdownServer
	ListenAndServe() error
}

func main() {
	os.Exit(run())
}

func run() int {
	return runWithDependencies(config.Load, newBootstrapLogger, newAppLogger, openDatabase, postgresutil.EnsureRequiredExtensionsEnabled)
}

func runWithDependencies(
	loadConfig func() (*config.Config, error),
	newBootstrapLogger func() (managedLogger, error),
	newAppLogger func(*config.Config) (managedLogger, error),
	openDatabase func(*config.Config) (*gorm.DB, error),
	ensureRequiredExtensions func(*gorm.DB) error,
) int {
	bootstrapLogger, err := newBootstrapLogger()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize bootstrap logger: %v\n", err)
		return 1
	}
	defer cleanupLogger(bootstrapLogger, os.Stderr)
	defer restoreDefaultLogger(bootstrapLogger.With("phase", "bootstrap"))()

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return 1
	}

	appLogger, err := newAppLogger(cfg)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		return 1
	}
	defer cleanupLogger(appLogger, os.Stderr)

	log := appLogger.With(
		"service", "isdict-api",
		"gin_mode", cfg.GinMode,
		"log_output", normalizedLogOutput(cfg.LogOutput),
	)
	defer restoreDefaultLogger(log)()

	log.Info("logger initialized", "log_level", normalizedLogLevel(cfg.LogLevel))

	// Set Gin mode
	gin.SetMode(cfg.GinMode)

	// Connect to database
	db, err := openDatabase(cfg)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		return 1
	}

	// Get underlying SQL DB early so every startup failure path closes cleanly.
	sqlDB, err := db.DB()
	if err != nil {
		log.Error("failed to get database instance", "error", err)
		return 1
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Error("failed to close database instance", "error", err)
		}
	}()

	if err := ensureRequiredExtensions(db); err != nil {
		log.Error("required extension setup failed", "error", err, "extension", postgresutil.RequiredExtensionName)
		return 1
	}

	// Configure connection pool
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Info("database connection pool configured",
		"db_max_idle_conns", cfg.DBMaxIdleConns,
		"db_max_open_conns", cfg.DBMaxOpenConns,
		"db_conn_max_lifetime", time.Hour.String(),
	)

	// Initialize layers
	repo := repository.NewRepository(db)
	var wordService handler.WordServiceInterface = service.NewWordService(repo, service.ServiceConfig{
		BatchMaxSize:    cfg.APIBatchMaxSize,
		SearchMaxLimit:  cfg.APISearchMaxLimit,
		SuggestMaxLimit: cfg.APISuggestMaxLimit,
	})
	wordHandler := handler.NewWordHandler(wordService, cfg)

	// Setup router
	router := setupRouter(wordHandler, cfg, sqlDB)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	return serveServer(srv, srv.Addr, quit, log, middleware.CleanupMiddleware, gracefulShutdown)
}

func newBootstrapLogger() (managedLogger, error) {
	return applog.NewBootstrap()
}

func newAppLogger(cfg *config.Config) (managedLogger, error) {
	return applog.NewConfigured(cfg)
}

func cleanupLogger(log managedLogger, fallback io.Writer) {
	applog.Cleanup(log, fallback)
}

func restoreDefaultLogger(log *slog.Logger) func() {
	previousDefault := slog.Default()
	slog.SetDefault(log)

	return func() {
		slog.SetDefault(previousDefault)
	}
}

func openDatabase(cfg *config.Config) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{})
}

func normalizedLogLevel(level string) string {
	return applog.NormalizeLevel(level)
}

func normalizedLogOutput(output string) string {
	return applog.NormalizeOutput(output)
}

func gracefulShutdown(srv gracefulShutdownServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}

func checkReadiness(ctx context.Context, sqlDB *sql.DB) error {
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}

	return postgresutil.CheckRequiredExtensionPresent(ctx, sqlDB)
}

func registerReadinessRoute(group *gin.RouterGroup, cfg *config.Config, sqlDB *sql.DB, readinessCheck func(context.Context, *sql.DB) error) {
	group.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessPingTimeout(cfg))
		defer cancel()

		if err := readinessCheck(ctx, sqlDB); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "not_ready",
				"service": "isdict-api",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "isdict-api",
		})
	})
}

func readinessPingTimeout(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.TimeoutSeconds < 1 {
		return time.Second
	}

	return time.Duration(cfg.TimeoutSeconds) * time.Second
}

func serveServer(
	srv lifecycleServer,
	address string,
	quit <-chan os.Signal,
	log *slog.Logger,
	cleanupMiddleware func(),
	shutdown func(gracefulShutdownServer) error,
) int {
	defer cleanupMiddleware()
	serverErrCh := make(chan error, 1)

	go func() {
		log.Info("starting isdict API server", "address", address)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErrCh:
		log.Error("server exited unexpectedly", "error", err)
		return 1
	}

	log.Info("shutting down server")

	if err := shutdown(srv); err != nil {
		log.Error("server forced to shutdown", "error", err)
		return 1
	}

	log.Info("server exited gracefully")
	return 0
}

func setupRouter(wordHandler *handler.WordHandler, cfg *config.Config, sqlDB *sql.DB) *gin.Engine {
	router := gin.New()

	// Apply middleware chain
	router.Use(middleware.SetupMiddleware(cfg))

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "isdict-api",
		})
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		registerReadinessRoute(v1, cfg, sqlDB, checkReadiness)

		// P0 Core endpoints
		v1.GET("/words/:headword", wordHandler.GetWord)
		v1.GET("/words/:headword/pronunciations", wordHandler.GetPronunciations)
		v1.GET("/words/:headword/senses", wordHandler.GetSenses)
		v1.GET("/words/by-variant/:variant", wordHandler.GetWordByVariant)
		v1.POST("/words/batch", wordHandler.GetWordsBatch)
		v1.GET("/search", wordHandler.SearchWords)
		v1.GET("/suggest", wordHandler.SuggestWords)
		v1.GET("/phrases", wordHandler.SearchPhrases)
	}

	// Serve the bundled web UI directly from the API process.
	// Deployments can still offload these routes to a reverse proxy if desired.
	if assetRoot := resolveWebAssetRoot(); assetRoot != "" {
		router.Static("/static/js", filepath.Join(assetRoot, "js"))
		router.StaticFile("/", filepath.Join(assetRoot, "index.html"))
	}

	return router
}

func resolveWebAssetRoot() string {
	for _, candidate := range webAssetRootCandidates() {
		if isWebAssetRoot(candidate) {
			return candidate
		}
	}

	return ""
}

func webAssetRootCandidates() []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	addCandidate := func(path string) {
		if path == "" {
			return
		}

		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}

		seen[cleaned] = struct{}{}
		candidates = append(candidates, cleaned)
	}

	if cwd, err := os.Getwd(); err == nil {
		addCandidate(filepath.Join(cwd, "web"))
	}

	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		addCandidate(filepath.Join(executableDir, "web"))
		addCandidate(filepath.Join(executableDir, "..", "web"))
	}

	if _, sourceFile, _, ok := runtime.Caller(0); ok {
		addCandidate(filepath.Join(filepath.Dir(sourceFile), "..", "..", "web"))
	}

	return candidates
}

func isWebAssetRoot(path string) bool {
	if path == "" {
		return false
	}

	indexPath := filepath.Join(path, "index.html")
	jsPath := filepath.Join(path, "js", "alpine.min.js")

	if _, err := os.Stat(indexPath); err != nil {
		return false
	}

	if _, err := os.Stat(jsPath); err != nil {
		return false
	}

	return true
}
