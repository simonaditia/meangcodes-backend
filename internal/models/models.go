package models

import "time"

import "gorm.io/gorm"

type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"size:120;not null" json:"name"`
	Email        string         `gorm:"size:150;uniqueIndex;not null" json:"email"`
	Role         string         `gorm:"size:20;index;not null;default:viewer" json:"role"`
	PasswordHash string         `gorm:"size:255;not null;default:''" json:"-"`
	Bio          string         `gorm:"type:text" json:"bio,omitempty"`
	Avatar       string         `gorm:"size:255" json:"avatar,omitempty"`
	Articles     []Article      `gorm:"foreignKey:AuthorID" json:"articles,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

type Category struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:80;uniqueIndex;not null" json:"name"`
	Slug        string         `gorm:"size:100;uniqueIndex;not null" json:"slug"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Articles    []Article      `gorm:"foreignKey:CategoryID" json:"articles,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type Article struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	Title      string         `gorm:"size:180;not null" json:"title"`
	Slug       string         `gorm:"size:200;uniqueIndex;not null" json:"slug"`
	Content    string         `gorm:"type:text;not null" json:"content"`
	Thumbnail  string         `gorm:"size:255" json:"thumbnail,omitempty"`
	ReadTime   int            `gorm:"not null" json:"readTime"`
	Views      int64          `gorm:"default:0;index;not null" json:"views"`
	Published  bool           `gorm:"default:true;index;not null" json:"published"`
	AuthorID   uint           `gorm:"index;not null" json:"authorId"`
	CategoryID uint           `gorm:"index;not null" json:"categoryId"`
	Author     User           `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"author"`
	Category   Category       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"category"`
	CreatedAt  time.Time      `gorm:"index" json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

type UploadedImage struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Path      string         `gorm:"size:255;uniqueIndex;not null" json:"path"`
	MimeType  string         `gorm:"size:100;not null" json:"mimeType"`
	Size      int64          `gorm:"not null" json:"size"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
