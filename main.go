package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"meangcodes/backend/internal/auth"
	"meangcodes/backend/internal/database"
	"meangcodes/backend/internal/handlers"
	"meangcodes/backend/internal/models"
	"meangcodes/backend/internal/routes"
	"meangcodes/backend/internal/seed"
	"meangcodes/backend/internal/storage"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("SUPABASE_DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("DATABASE_URL or SUPABASE_DATABASE_URL is required")
	}

	gormDB, err := database.NewGormDB(databaseURL)
	if err != nil {
		log.Fatalf("failed to connect Gorm DB: %v", err)
	}

	if err := database.AutoMigrate(gormDB, &models.User{}, &models.Category{}, &models.Article{}, &models.UploadedImage{}, &models.ArticleBuffer{}, &models.AutomationTopicHistory{}); err != nil {
		log.Fatalf("failed to run database migration: %v", err)
	}

	if err := seed.SeedInitialData(gormDB); err != nil {
		log.Fatalf("failed to seed initial data: %v", err)
	}

	authConfig, err := auth.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("failed to load auth config: %v", err)
	}

	if err := auth.EnsureAdminUser(gormDB, authConfig); err != nil {
		log.Fatalf("failed to ensure admin account: %v", err)
	}

	mediaStorage, err := storage.NewSupabaseStorageFromEnv()
	if err != nil {
		log.Fatalf("failed to initialize supabase storage: %v", err)
	}

	if mediaStorage == nil {
		if err := os.MkdirAll("uploads", 0o755); err != nil {
			log.Fatalf("failed to prepare uploads directory: %v", err)
		}
		log.Printf("supabase storage is not configured; using local uploads directory")
	} else {
		log.Printf("supabase storage is enabled for bucket: %s", mediaStorage.Bucket)
	}

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
		},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	}))
	if mediaStorage == nil {
		router.Static("/uploads", "./uploads")
	}

	articleHandler := handlers.NewArticleHandler(gormDB, mediaStorage)
	authHandler := handlers.NewAuthHandler(gormDB, authConfig)
	routes.RegisterRoutes(router, authConfig, articleHandler, authHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("server listening on %s", fmt.Sprintf(":%s", port))
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
