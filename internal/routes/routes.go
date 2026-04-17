package routes

import (
	"meangcodes/backend/internal/auth"
	"meangcodes/backend/internal/handlers"
	"meangcodes/backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(router *gin.Engine, authConfig auth.Config, articleHandler *handlers.ArticleHandler, authHandler *handlers.AuthHandler) {
	api := router.Group("/api")
	{
		authRoutes := api.Group("/auth")
		{
			authRoutes.POST("/login", authHandler.Login)
			authRoutes.GET("/me", middleware.AuthRequired(authConfig), authHandler.Me)
		}

		api.GET("/articles", articleHandler.GetLatestArticles)
		api.GET("/articles/:slug", articleHandler.GetArticleBySlug)
		api.GET("/articles/:slug/related", articleHandler.GetRelatedArticles)
		api.GET("/categories", articleHandler.GetCategories)
		api.GET("/authors", articleHandler.GetAuthors)

		admin := api.Group("")
		admin.Use(middleware.AuthRequired(authConfig), middleware.RequireRole("admin"))
		{
			admin.POST("/articles", articleHandler.CreateArticle)
			admin.PATCH("/articles/:slug", articleHandler.UpdateArticle)
			admin.DELETE("/articles/:slug", articleHandler.DeleteArticle)
			admin.POST("/uploads", articleHandler.UploadImage)
			admin.DELETE("/uploads", articleHandler.DeleteImage)
		}
	}
}
