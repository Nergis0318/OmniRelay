package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/handlers"
	"omnirelay/internal/middleware"
	"omnirelay/internal/proxy"
	"omnirelay/internal/service"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const maxBodySize = 32 << 20 // 32MB

func main() {
	cfg := config.Load()

	db, err := database.Init(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("failed to init database: %v", err)
	}
	defer db.Close()

	authService := service.NewAuthService(db)
	authService.SetJWTSecret(cfg.JWTSecret)
	providerService := service.NewProviderService(db)
	modelService := service.NewModelService(db)
	apiKeyService := service.NewAPIKeyService(db)
	usageService := service.NewUsageService(db)

	proxyEngine := proxy.NewEngine(providerService, modelService, usageService, nil)

	r := gin.Default()

	// CORS: allow origins from config, fall back to defaults
	allowedOrigins := []string{"http://localhost:5173", "http://localhost:3000"}
	if cfg.CORSOrigins != "" {
		allowedOrigins = splitAndTrim(cfg.CORSOrigins, ",")
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Health check endpoints
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if err := db.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	admin := r.Group("/admin")
	{
		admin.POST("/auth/register", handlers.Register(authService))
		admin.POST("/auth/login", handlers.Login(authService))

		adminAuth := admin.Group("")
		adminAuth.Use(middleware.JWTAuth(cfg.JWTSecret))
		{
			adminAuth.GET("/providers", handlers.ListProviders(providerService))
			adminAuth.POST("/providers", handlers.CreateProvider(providerService))
			adminAuth.PUT("/providers/:id", handlers.UpdateProvider(providerService))
			adminAuth.DELETE("/providers/:id", handlers.DeleteProvider(providerService))
			adminAuth.POST("/providers/:id/sync", handlers.SyncProviderModels(providerService, modelService))

			adminAuth.GET("/models", handlers.ListModels(modelService))
			adminAuth.POST("/models", handlers.CreateModel(modelService))
			adminAuth.PUT("/models/:id", handlers.UpdateModel(modelService))
			adminAuth.DELETE("/models/:id", handlers.DeleteModel(modelService))

			adminAuth.GET("/api-keys", handlers.ListAPIKeys(apiKeyService))
			adminAuth.POST("/api-keys", handlers.CreateAPIKey(apiKeyService))
			adminAuth.DELETE("/api-keys/:id", handlers.DeleteAPIKey(apiKeyService))

			adminAuth.GET("/usage", handlers.ListUsage(usageService))
			adminAuth.GET("/stats", handlers.GetStats(usageService, apiKeyService, modelService))

			adminAuth.GET("/users", handlers.ListUsers(authService))
		}
	}

	v1 := r.Group("/v1")
	v1.Use(middleware.APIKeyAuth(apiKeyService), bodySizeLimit())
	{
		v1.POST("/chat/completions", proxyEngine.HandleChatCompletions)
		v1.GET("/models", proxyEngine.HandleListModels)
		v1.GET("/models/*model", proxyEngine.HandleGetModel)
		v1.POST("/messages", proxyEngine.HandleMessages)
	}

	// Path-based routing: /:provider_key/v1/*endpoint
	pbr := r.Group("/")
	pbr.Use(middleware.APIKeyAuth(apiKeyService), bodySizeLimit())
	pbr.Any("/:provider_key/v1/*endpoint", proxyEngine.HandlePathRouted)
	pbr.Any("/:provider_key/v1beta/*endpoint", proxyEngine.HandlePathRouted)
	pbr.Any("/:provider_key/api/*endpoint", proxyEngine.HandlePathRouted)

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: r,
	}

	go func() {
		log.Printf("OmniRelay starting on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %v, shutting down gracefully...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server exited")
}

// bodySizeLimit returns a middleware that enforces a maximum request body size.
func bodySizeLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
		c.Next()
	}
}

// splitAndTrim splits a string by a separator and trims whitespace from each part.
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := trimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func split(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if string(s[i]) == sep && i >= start {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
