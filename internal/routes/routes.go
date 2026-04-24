package routes

import (
	"time"

	"meangcodes/backend/internal/auth"
	"meangcodes/backend/internal/handlers"
	"meangcodes/backend/internal/middleware"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func RegisterRoutes(router *gin.Engine, authConfig auth.Config, articleHandler *handlers.ArticleHandler, authHandler *handlers.AuthHandler) {
	loginLimiter := middleware.NewIPRateLimiter(rate.Every(6*time.Second), 5, 30*time.Minute)
	uploadLimiter := middleware.NewIPRateLimiter(rate.Every(3*time.Second), 8, 30*time.Minute)

	router.GET("/health", handlers.Health(articleHandler.DB))

	api := router.Group("/api")
	{
		authRoutes := api.Group("/auth")
		{
			authRoutes.POST("/login", loginLimiter.Middleware(), authHandler.Login)
			authRoutes.GET("/me", middleware.AuthRequired(authConfig), authHandler.Me)
		}

		api.GET("/articles", articleHandler.GetLatestArticles)
		api.GET("/homepage", articleHandler.GetHomepageBundle)
		api.GET("/homepage/bundle", articleHandler.GetHomepageBundle)
		api.GET("/articles/trending", articleHandler.GetTrendingArticles)
		api.GET("/articles/:slug", articleHandler.GetArticleBySlug)
		api.GET("/articles/:slug/related", articleHandler.GetRelatedArticles)
		api.GET("/categories/:slug/articles", articleHandler.GetLatestArticlesByCategory)
		api.GET("/categories", articleHandler.GetCategories)
		api.GET("/authors", articleHandler.GetAuthors)

		admin := api.Group("")
		admin.Use(middleware.AuthRequired(authConfig), middleware.RequireRole("admin"))
		{
			admin.POST("/articles", articleHandler.CreateArticle)
			admin.PATCH("/articles/:slug", articleHandler.UpdateArticle)
			admin.DELETE("/articles/:slug", articleHandler.DeleteArticle)
			admin.POST("/uploads", uploadLimiter.Middleware(), articleHandler.UploadImage)
			admin.DELETE("/uploads", uploadLimiter.Middleware(), articleHandler.DeleteImage)
		}
	}
}
