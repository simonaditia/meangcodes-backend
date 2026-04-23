package auth

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
	AdminName     string
	TokenTTL      time.Duration
}

type Claims struct {
	Role  string `json:"role"`
	Email string `json:"email"`
	Name  string `json:"name"`
	jwt.RegisteredClaims
}

func LoadConfigFromEnv() (Config, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}

	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminEmail == "" || adminPassword == "" {
		return Config{}, errors.New("ADMIN_EMAIL and ADMIN_PASSWORD are required for bootstrap admin account")
	}

	adminName := os.Getenv("ADMIN_NAME")
	if adminName == "" {
		adminName = "Admin"
	}

	tokenTTL := 24 * time.Hour
	if value := os.Getenv("JWT_TTL_HOURS"); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed > 0 {
			tokenTTL = time.Duration(parsed) * time.Hour
		}
	}

	return Config{
		JWTSecret:     jwtSecret,
		AdminEmail:    adminEmail,
		AdminPassword: adminPassword,
		AdminName:     adminName,
		TokenTTL:      tokenTTL,
	}, nil
}

func GenerateToken(cfg Config) (string, time.Time, error) {
	return GenerateTokenForUser(cfg, cfg.AdminEmail, cfg.AdminName, "admin")
}

func GenerateTokenForUser(cfg Config, email string, name string, role string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(cfg.TokenTTL)

	claims := Claims{
		Role:  role,
		Email: email,
		Name:  name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

func ParseToken(tokenString string, cfg Config) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}

		return []byte(cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
