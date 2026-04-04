package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"secureshare/internal/config"
	"secureshare/internal/handlers"
	"secureshare/internal/middleware"
	"secureshare/internal/services"
	"secureshare/internal/storage"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize PostgreSQL
	db, err := storage.NewPostgres(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()
	log.Println("Connected to PostgreSQL")

	// Initialize Redis
	rdb, err := storage.NewRedis(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()
	log.Println("Connected to Redis")

	// Initialize filesystem storage
	fs, err := storage.NewFilesystem(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize filesystem storage: %v", err)
	}
	log.Println("Filesystem storage initialized")

	// Initialize Discord webhook service
	discord := services.NewDiscord(cfg)

	// Initialize cleanup service
	cleanup := services.NewCleanup(cfg, db, rdb, fs)
	go cleanup.Start()
	defer cleanup.Stop()
	log.Println("Cleanup service started")

	// Initialize upload service
	uploadService := services.NewUpload(cfg, db, rdb, fs)
	go uploadService.StartPendingCleanup()
	defer uploadService.Stop()
	log.Println("Upload service started")

	// Set Gin mode
	if cfg.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	// Load HTML templates
	templates := template.Must(template.ParseGlob("web/templates/*.html"))
	router.SetHTMLTemplate(templates)

	// Initialize middleware
	ipMiddleware := middleware.NewIPMiddleware(cfg)
	rateLimiter := middleware.NewRateLimiter(rdb)

	// Apply global middleware
	router.Use(ipMiddleware.Handler())

	// Initialize handlers
	pageHandler := handlers.NewPageHandler(cfg)
	uploadHandler := handlers.NewUploadHandler(cfg, db, rdb, fs, uploadService)
	downloadHandler := handlers.NewDownloadHandler(cfg, db, fs)
	reportHandler := handlers.NewReportHandler(cfg, db, discord)

	// Serve static files
	router.Static("/static", "./web/static")

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// Page routes
	router.GET("/", pageHandler.Index)
	router.GET("/tos", pageHandler.ToS)
	router.GET("/privacy", pageHandler.Privacy)
	router.GET("/shared", pageHandler.SharedLookup)
	router.GET("/shared/:id", pageHandler.SharedFile)

	// API routes
	api := router.Group("/api")
	{
		// Upload routes (with rate limiting)
		upload := api.Group("/upload")
		upload.Use(rateLimiter.Handler())
		{
			upload.POST("/init", uploadHandler.Init)
			upload.POST("/chunk", uploadHandler.Chunk)
			upload.POST("/complete", uploadHandler.Complete)
			upload.DELETE("/cancel", uploadHandler.Cancel)
		}

		// File routes
		file := api.Group("/file")
		{
			file.GET("/:id", downloadHandler.GetMetadata)
			file.GET("/:id/download", downloadHandler.Download)
			file.GET("/code/:code", downloadHandler.GetByCode)
			file.POST("/:id/report", reportHandler.Report)
		}
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Give outstanding requests 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}