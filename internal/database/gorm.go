package database

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func NewGormDB(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

func AutoMigrate(db *gorm.DB, models ...interface{}) error {
	return db.AutoMigrate(models...)
}
