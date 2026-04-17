package main

import (
	"log"
	"os"
	"strings"

	"meangcodes/backend/internal/auth"
	"meangcodes/backend/internal/database"
	"meangcodes/backend/internal/handlers"
	"meangcodes/backend/internal/models"
	"meangcodes/backend/internal/routes"
	"meangcodes/backend/internal/seed"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	gormDB, err := database.NewGormDB(databaseURL)
	if err != nil {
		log.Fatalf("failed to connect Gorm DB: %v", err)
	}

	if err := database.AutoMigrate(gormDB, &models.User{}, &models.Category{}, &models.Article{}, &models.UploadedImage{}); err != nil {
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

	if err := os.MkdirAll("uploads", 0o755); err != nil {
		log.Fatalf("failed to prepare uploads directory: %v", err)
	}

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
		},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	}))
	router.Static("/uploads", "./uploads")

	articleHandler := handlers.NewArticleHandler(gormDB)
	authHandler := handlers.NewAuthHandler(gormDB, authConfig)
	routes.RegisterRoutes(router, authConfig, articleHandler, authHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
