package main

import (
	"log"
	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/handlers"
	"omnirelay/internal/middleware"
	"omnirelay/internal/proxy"
	"omnirelay/internal/service"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

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

	proxyEngine := proxy.NewEngine(providerService, modelService, usageService)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

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
			adminAuth.GET("/stats", handlers.GetStats(usageService, apiKeyService))

			adminAuth.GET("/users", handlers.ListUsers(authService))
		}
	}

	v1 := r.Group("/v1")
	v1.Use(middleware.APIKeyAuth(apiKeyService))
	{
		v1.POST("/chat/completions", proxyEngine.HandleChatCompletions)
		v1.GET("/models", proxyEngine.HandleListModels)
		v1.GET("/models/*model", proxyEngine.HandleGetModel)
		v1.POST("/messages", proxyEngine.HandleMessages)
	}

	// Path-based routing: /:provider_key/v1/*endpoint
	// Example: POST /openai/v1/chat/completions with model="gpt-4o"
	pbr := r.Group("/")
	pbr.Use(middleware.APIKeyAuth(apiKeyService))
	pbr.Any("/:provider_key/v1/*endpoint", proxyEngine.HandlePathRouted)
	pbr.Any("/:provider_key/v1beta/*endpoint", proxyEngine.HandlePathRouted)
	pbr.Any("/:provider_key/api/*endpoint", proxyEngine.HandlePathRouted)

	log.Printf("OmniRelay starting on %s", cfg.ListenAddr)
	if err := r.Run(cfg.ListenAddr); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
