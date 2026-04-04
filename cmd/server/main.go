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

	"shareit/internal/config"
	"shareit/internal/handlers"
	"shareit/internal/middleware"
	"shareit/internal/services"
	"shareit/internal/storage"

	"github.com/gin-gonic/gin"
)

func main() {
	 
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	 
	db, err := storage.NewPostgres(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()
	log.Println("Connected to PostgreSQL")

	 
	rdb, err := storage.NewRedis(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()
	log.Println("Connected to Redis")

	 
	fs, err := storage.NewFilesystem(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize filesystem storage: %v", err)
	}
	log.Println("Filesystem storage initialized")

	 
	discord := services.NewDiscord(cfg)

	 
	cleanup := services.NewCleanup(cfg, db, rdb, fs)
	go cleanup.Start()
	defer cleanup.Stop()
	log.Println("Cleanup service started")

	 
	uploadService := services.NewUpload(cfg, db, rdb, fs)
	go uploadService.StartPendingCleanup()
	defer uploadService.Stop()
	log.Println("Upload service started")

	 
	if cfg.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}

	 
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	 
	templates := template.Must(template.ParseGlob("web/templates/*.html"))
	router.SetHTMLTemplate(templates)

	 
	ipMiddleware := middleware.NewIPMiddleware(cfg)
	rateLimiter := middleware.NewRateLimiter(rdb)

	 
	router.Use(ipMiddleware.Handler())

	 
	pageHandler := handlers.NewPageHandler(cfg)
	uploadHandler := handlers.NewUploadHandler(cfg, db, rdb, fs, uploadService)
	downloadHandler := handlers.NewDownloadHandler(cfg, db, fs)
	reportHandler := handlers.NewReportHandler(cfg, db, discord)

	 
	router.Static("/static", "./web/static")

	 
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	 
	router.GET("/", pageHandler.Index)
	router.GET("/tos", pageHandler.ToS)
	router.GET("/privacy", pageHandler.Privacy)
	router.GET("/shared", pageHandler.SharedLookup)
	router.GET("/shared/:id", pageHandler.SharedFile)

	 
	api := router.Group("/api")
	{
		 
		upload := api.Group("/upload")
        {
            upload.POST("/init", rateLimiter.Handler(), uploadHandler.Init)
            upload.POST("/chunk", uploadHandler.Chunk)
            upload.POST("/complete", uploadHandler.Complete)
			upload.GET("/status/:session_id", uploadHandler.AssemblyStatus)
			upload.POST("/finalize", uploadHandler.Finalize)
            upload.DELETE("/cancel", uploadHandler.Cancel)
        }

		 
		file := api.Group("/file")
		{
			file.GET("/:id", downloadHandler.GetMetadata)
			file.GET("/:id/download", downloadHandler.Download)
			file.GET("/code/:code", downloadHandler.GetByCode)
			file.POST("/:id/report", reportHandler.Report)
		}
	}

	 
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	 
	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	 
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	 
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}