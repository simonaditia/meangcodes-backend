package handlers

import (
	"net/http"
	"strings"

	"meangcodes/backend/internal/auth"
	"meangcodes/backend/internal/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthHandler struct {
	DB     *gorm.DB
	Config auth.Config
}

func NewAuthHandler(db *gorm.DB, cfg auth.Config) *AuthHandler {
	return &AuthHandler{DB: db, Config: cfg}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	password := req.Password
	if email == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	var user models.User
	if err := h.DB.Where("LOWER(email) = ?", email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if strings.TrimSpace(user.PasswordHash) == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token, expiresAt, err := auth.GenerateTokenForUser(h.Config, user.Email, user.Name, normalizeRole(user.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"accessToken": token,
			"expiresAt":   expiresAt,
			"user": gin.H{
				"name":  user.Name,
				"email": user.Email,
				"role":  normalizeRole(user.Role),
			},
		},
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	claimsAny, exists := c.Get("authClaims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	claims, ok := claimsAny.(*auth.Claims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"user": gin.H{
				"name":  claims.Name,
				"email": claims.Email,
				"role":  claims.Role,
			},
		},
	})
}

func normalizeRole(role string) string {
	clean := strings.ToLower(strings.TrimSpace(role))
	if clean == "" {
		return "viewer"
	}

	return clean
}
