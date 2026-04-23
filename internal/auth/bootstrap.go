package auth

import (
	"errors"
	"strings"

	"meangcodes/backend/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func EnsureAdminUser(db *gorm.DB, cfg Config) error {
	email := strings.TrimSpace(strings.ToLower(cfg.AdminEmail))
	password := strings.TrimSpace(cfg.AdminPassword)
	if email == "" || password == "" {
		return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	var user models.User
	err = db.Unscoped().Where("LOWER(email) = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		admin := models.User{
			Name:         cfg.AdminName,
			Email:        email,
			Role:         "admin",
			PasswordHash: string(hash),
			Bio:          "Admin account",
		}
		return db.Create(&admin).Error
	}
	if err != nil {
		return err
	}

	updates := map[string]any{
		"name":          cfg.AdminName,
		"role":          "admin",
		"password_hash": string(hash),
	}
	if user.DeletedAt.Valid {
		updates["deleted_at"] = nil
	}

	return db.Unscoped().Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error
}
