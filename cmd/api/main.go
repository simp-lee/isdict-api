package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/simp-lee/isdict-api/internal/api/handler"
	"github.com/simp-lee/isdict-api/internal/api/middleware"
	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/api/service"
	"github.com/simp-lee/isdict-api/internal/config"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Set Gin mode
	gin.SetMode(cfg.GinMode)

	// Connect to database
	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Get underlying SQL DB for connection management
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get database instance: %v", err)
	}
	defer sqlDB.Close()

	// Configure connection pool
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Printf("Database connection pool configured: max_idle=%d, max_open=%d", cfg.DBMaxIdleConns, cfg.DBMaxOpenConns)

	// Initialize layers
	repo := repository.NewRepository(db)
	wordService := service.NewWordService(repo, cfg)
	wordHandler := handler.NewWordHandler(wordService, cfg)

	// Setup router
	router := setupRouter(wordHandler, cfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting isdict API server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Cleanup middleware resources
	middleware.CleanupMiddleware()

	// Give outstanding requests 5 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func setupRouter(wordHandler *handler.WordHandler, cfg *config.Config) *gin.Engine {
	router := gin.New()

	// Apply middleware chain
	router.Use(middleware.SetupMiddleware(cfg))

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "isdict-api",
		})
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
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

	// Note: Static files are served by Nginx in production
	// For local development, uncomment the following lines:
	// Access the web interface at http://localhost:PORT/
	router.Static("/static", "./web")
	router.StaticFile("/", "./web/index.html")
	router.StaticFile("/favicon.ico", "./web/favicon.ico")

	return router
}
