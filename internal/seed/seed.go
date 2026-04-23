package seed

import (
	"fmt"
	"strings"

	"meangcodes/backend/internal/models"

	"gorm.io/gorm"
)

func SeedInitialData(db *gorm.DB) error {
	var articleCount int64
	if err := db.Model(&models.Article{}).Count(&articleCount).Error; err != nil {
		return err
	}

	if articleCount > 0 {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		users := []models.User{
			{
				Name:  "Simon Aditia",
				Email: "simon@example.com",
				Role:  "viewer",
				Bio:   "Full-stack developer yang fokus di React, Go, dan arsitektur API modern.",
			},
			{
				Name:  "Rania Putri",
				Email: "rania@example.com",
				Role:  "viewer",
				Bio:   "Penulis keamanan aplikasi web dan praktik secure coding untuk tim engineering.",
			},
		}
		if err := tx.Create(&users).Error; err != nil {
			return err
		}

		categories := []models.Category{
			{Name: "Programming", Slug: "programming", Description: "Tips coding, pattern, dan best practice software engineering."},
			{Name: "Security", Slug: "security", Description: "Praktik keamanan aplikasi, API hardening, dan vulnerability awareness."},
			{Name: "DevOps", Slug: "devops", Description: "CI/CD, observability, deployment, dan workflow engineering."},
		}
		if err := tx.Create(&categories).Error; err != nil {
			return err
		}

		templates := []struct {
			Title      string
			Slug       string
			Content    string
			Thumbnail  string
			ReadTime   int
			Views      int64
			AuthorIdx  int
			CategoryIx int
		}{
			{
				Title:      "Menyusun Arsitektur React Modular Untuk Blog Skala Menengah",
				Slug:       "arsitektur-react-modular-blog-skala-menengah",
				Content:    sampleArticleContent("arsitektur frontend", "React"),
				Thumbnail:  "https://images.unsplash.com/photo-1518770660439-4636190af475",
				ReadTime:   8,
				Views:      1240,
				AuthorIdx:  0,
				CategoryIx: 0,
			},
			{
				Title:      "Audit Endpoint API: Checklist Keamanan Dasar Untuk Tim Startup",
				Slug:       "audit-endpoint-api-checklist-keamanan-dasar",
				Content:    sampleArticleContent("api security", "Gin + PostgreSQL"),
				Thumbnail:  "https://images.unsplash.com/photo-1555949963-aa79dcee981c",
				ReadTime:   10,
				Views:      2010,
				AuthorIdx:  1,
				CategoryIx: 1,
			},
			{
				Title:      "Strategi CI/CD Untuk Tim Kecil Dengan Kualitas Release Stabil",
				Slug:       "strategi-cicd-untuk-tim-kecil",
				Content:    sampleArticleContent("deployment workflow", "DevOps"),
				Thumbnail:  "https://images.unsplash.com/photo-1461749280684-dccba630e2f6",
				ReadTime:   7,
				Views:      980,
				AuthorIdx:  0,
				CategoryIx: 2,
			},
			{
				Title:      "Mendesain Query Gorm Agar Tetap Cepat Saat Data Tumbuh",
				Slug:       "optimasi-query-gorm-saat-data-tumbuh",
				Content:    sampleArticleContent("query optimization", "Gorm"),
				Thumbnail:  "https://images.unsplash.com/photo-1516116216624-53e697fedbea",
				ReadTime:   9,
				Views:      1520,
				AuthorIdx:  0,
				CategoryIx: 0,
			},
			{
				Title:      "Threat Modeling Praktis Untuk Fitur Login dan Session",
				Slug:       "threat-modeling-praktis-login-session",
				Content:    sampleArticleContent("auth security", "OWASP"),
				Thumbnail:  "https://images.unsplash.com/photo-1516321497487-e288fb19713f",
				ReadTime:   11,
				Views:      2305,
				AuthorIdx:  1,
				CategoryIx: 1,
			},
		}

		articles := make([]models.Article, 0, len(templates))
		for _, t := range templates {
			articles = append(articles, models.Article{
				Title:      t.Title,
				Slug:       t.Slug,
				Content:    t.Content,
				Thumbnail:  t.Thumbnail,
				ReadTime:   t.ReadTime,
				Views:      t.Views,
				Published:  true,
				AuthorID:   users[t.AuthorIdx].ID,
				CategoryID: categories[t.CategoryIx].ID,
			})
		}

		if err := tx.Create(&articles).Error; err != nil {
			return err
		}

		return nil
	})
}

func sampleArticleContent(topic string, stack string) string {
	blocks := []string{
		fmt.Sprintf("Artikel ini membahas %s dengan pendekatan praktis dan siap dipakai untuk proyek produksi.", topic),
		fmt.Sprintf("Kita menggunakan konteks stack %s agar contoh lebih relevan dengan kebutuhan tim web modern.", stack),
		"Fokus utama ada pada desain struktur data, konsistensi API, dan pengalaman baca yang nyaman untuk pengguna.",
		"Setiap bagian disusun bertahap dari konsep, implementasi, hingga checklist validasi sebelum dirilis.",
		"Di akhir artikel, kamu mendapatkan rangkuman langkah prioritas yang bisa langsung dieksekusi minggu ini.",
	}

	return strings.Join(blocks, "\n\n")
}
