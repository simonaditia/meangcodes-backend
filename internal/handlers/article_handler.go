package handlers

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"meangcodes/backend/internal/models"
	"meangcodes/backend/internal/storage"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var slugCleanupRegex = regexp.MustCompile(`[^a-z0-9]+`)

type createArticleRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Thumbnail  string `json:"thumbnail"`
	ReadTime   int    `json:"readTime"`
	AuthorID   uint   `json:"authorId"`
	CategoryID uint   `json:"categoryId"`
	Published  bool   `json:"published"`
}

type updateArticleRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Thumbnail  string `json:"thumbnail"`
	ReadTime   int    `json:"readTime"`
	AuthorID   uint   `json:"authorId"`
	CategoryID uint   `json:"categoryId"`
	Published  bool   `json:"published"`
}

type deleteImageRequest struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

type ArticleHandler struct {
	DB           *gorm.DB
	SupabaseStore *storage.SupabaseStorage
}

const maxUploadSize = 5 << 20 // 5MB

func NewArticleHandler(db *gorm.DB, supabaseStore *storage.SupabaseStorage) *ArticleHandler {
	return &ArticleHandler{DB: db, SupabaseStore: supabaseStore}
}

func (h *ArticleHandler) GetLatestArticles(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("limit"), 12)
	if pageSize > 50 {
		pageSize = 50
	}

	search := strings.TrimSpace(c.Query("search"))
	categorySlug := strings.TrimSpace(c.Query("category"))

	var articles []models.Article
	query := h.DB.Model(&models.Article{}).
		Joins("LEFT JOIN categories ON categories.id = articles.category_id").
		Where("articles.published = ?", true)

	if search != "" {
		likeTerm := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(articles.title) LIKE ? OR LOWER(articles.content) LIKE ?", likeTerm, likeTerm)
	}

	if categorySlug != "" {
		query = query.Where("categories.slug = ?", strings.ToLower(categorySlug))
	}

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count articles"})
		return
	}

	offset := (page - 1) * pageSize
	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
		offset = (page - 1) * pageSize
	}

	if err := query.
		Preload("Author").
		Preload("Category").
		Order("articles.created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&articles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch articles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": articles,
		"pagination": gin.H{
			"page":       page,
			"limit":      pageSize,
			"totalItems": totalItems,
			"totalPages": totalPages,
			"hasNext":    page < totalPages,
			"hasPrev":    page > 1,
		},
	})
}

func (h *ArticleHandler) GetTrendingArticles(c *gin.Context) {
	limit := parsePositiveInt(c.Query("limit"), 6)
	if limit > 20 {
		limit = 20
	}

	periodDays := parsePositiveInt(c.Query("days"), 30)
	if periodDays > 365 {
		periodDays = 365
	}

	since := time.Now().AddDate(0, 0, -periodDays)

	var articles []models.Article
	err := h.DB.
		Preload("Author").
		Preload("Category").
		Where("published = ? AND created_at >= ?", true, since).
		Order("views DESC").
		Order("created_at DESC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch trending articles"})
		return
	}

	if len(articles) == 0 {
		err = h.DB.
			Preload("Author").
			Preload("Category").
			Where("published = ?", true).
			Order("views DESC").
			Order("created_at DESC").
			Limit(limit).
			Find(&articles).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch trending fallback"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": articles,
		"meta": gin.H{
			"windowDays": periodDays,
			"limit":      limit,
		},
	})
}

func (h *ArticleHandler) GetLatestArticlesByCategory(c *gin.Context) {
	categorySlug := strings.ToLower(strings.TrimSpace(c.Param("slug")))
	if categorySlug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category slug is required"})
		return
	}

	var category models.Category
	if err := h.DB.Where("slug = ?", categorySlug).First(&category).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "category not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch category"})
		return
	}

	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("limit"), 9)
	if pageSize > 50 {
		pageSize = 50
	}

	search := strings.TrimSpace(c.Query("search"))

	var articles []models.Article
	query := h.DB.Model(&models.Article{}).
		Where("published = ? AND category_id = ?", true, category.ID)

	if search != "" {
		likeTerm := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(content) LIKE ?", likeTerm, likeTerm)
	}

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count category articles"})
		return
	}

	offset := (page - 1) * pageSize
	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
		offset = (page - 1) * pageSize
	}

	if err := query.
		Preload("Author").
		Preload("Category").
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&articles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch category articles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": articles,
		"category": gin.H{
			"id":   category.ID,
			"name": category.Name,
			"slug": category.Slug,
		},
		"pagination": gin.H{
			"page":       page,
			"limit":      pageSize,
			"totalItems": totalItems,
			"totalPages": totalPages,
			"hasNext":    page < totalPages,
			"hasPrev":    page > 1,
		},
	})
}

func (h *ArticleHandler) GetHomepageBundle(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("limit"), 9)
	if pageSize > 50 {
		pageSize = 50
	}

	search := strings.TrimSpace(c.Query("search"))
	categorySlug := strings.TrimSpace(c.Query("category"))

	trendingLimit := parsePositiveInt(c.Query("trendingLimit"), 7)
	if trendingLimit > 20 {
		trendingLimit = 20
	}

	trendingDays := parsePositiveInt(c.Query("trendingDays"), 45)
	if trendingDays > 365 {
		trendingDays = 365
	}

	sectionCategoryLimit := parsePositiveInt(c.Query("sectionCategoryLimit"), 3)
	if sectionCategoryLimit > 8 {
		sectionCategoryLimit = 8
	}

	sectionArticleLimit := parsePositiveInt(c.Query("sectionArticleLimit"), 3)
	if sectionArticleLimit > 8 {
		sectionArticleLimit = 8
	}

	baseQuery := h.DB.Model(&models.Article{}).
		Joins("LEFT JOIN categories ON categories.id = articles.category_id").
		Where("articles.published = ?", true)

	if search != "" {
		likeTerm := "%" + strings.ToLower(search) + "%"
		baseQuery = baseQuery.Where("LOWER(articles.title) LIKE ? OR LOWER(articles.content) LIKE ?", likeTerm, likeTerm)
	}

	if categorySlug != "" {
		baseQuery = baseQuery.Where("categories.slug = ?", strings.ToLower(categorySlug))
	}

	var totalItems int64
	if err := baseQuery.Count(&totalItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count homepage latest articles"})
		return
	}

	offset := (page - 1) * pageSize
	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
		offset = (page - 1) * pageSize
	}

	var latest []models.Article
	if err := baseQuery.
		Preload("Author").
		Preload("Category").
		Order("articles.created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&latest).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch homepage latest articles"})
		return
	}

	var categories []models.Category
	if err := h.DB.Order("name ASC").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch homepage categories"})
		return
	}

	trendingSince := time.Now().AddDate(0, 0, -trendingDays)
	var trending []models.Article
	err := h.DB.
		Preload("Author").
		Preload("Category").
		Where("published = ? AND created_at >= ?", true, trendingSince).
		Order("views DESC").
		Order("created_at DESC").
		Limit(trendingLimit).
		Find(&trending).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch homepage trending articles"})
		return
	}

	if len(trending) == 0 {
		err = h.DB.
			Preload("Author").
			Preload("Category").
			Where("published = ?", true).
			Order("views DESC").
			Order("created_at DESC").
			Limit(trendingLimit).
			Find(&trending).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch homepage trending fallback"})
			return
		}
	}

	type homepageSection struct {
		Category models.Category  `json:"category"`
		Articles []models.Article `json:"articles"`
	}

	sections := make([]homepageSection, 0)
	for _, category := range categories {
		if len(sections) >= sectionCategoryLimit {
			break
		}

		var sectionArticles []models.Article
		if err := h.DB.
			Preload("Author").
			Preload("Category").
			Where("published = ? AND category_id = ?", true, category.ID).
			Order("created_at DESC").
			Limit(sectionArticleLimit).
			Find(&sectionArticles).Error; err != nil {
			continue
		}

		if len(sectionArticles) == 0 {
			continue
		}

		sections = append(sections, homepageSection{
			Category: category,
			Articles: sectionArticles,
		})
	}

	var featured *models.Article
	if len(trending) > 0 {
		featured = &trending[0]
	} else if len(latest) > 0 {
		featured = &latest[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"featured":  featured,
			"latest":    latest,
			"trending":  trending,
			"categories": categories,
			"sections":  sections,
			"pagination": gin.H{
				"page":       page,
				"limit":      pageSize,
				"totalItems": totalItems,
				"totalPages": totalPages,
				"hasNext":    page < totalPages,
				"hasPrev":    page > 1,
			},
		},
		"meta": gin.H{
			"trendingWindowDays": trendingDays,
			"trendingLimit":      trendingLimit,
			"sectionCategoryLimit": sectionCategoryLimit,
			"sectionArticleLimit":  sectionArticleLimit,
		},
	})
}

func (h *ArticleHandler) GetArticleBySlug(c *gin.Context) {
	slug := c.Param("slug")
	var article models.Article

	err := h.DB.
		Preload("Author").
		Preload("Category").
		Where("slug = ? AND published = ?", slug, true).
		First(&article).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch article"})
		return
	}

	// Increment views asynchronously to avoid blocking the detail response.
	go h.DB.Model(&models.Article{}).Where("id = ?", article.ID).Update("views", gorm.Expr("views + 1"))

	c.JSON(http.StatusOK, gin.H{
		"data": article,
	})
}

func (h *ArticleHandler) GetRelatedArticles(c *gin.Context) {
	slug := c.Param("slug")
	limit := parsePositiveInt(c.Query("limit"), 4)
	if limit > 12 {
		limit = 12
	}

	var article models.Article
	if err := h.DB.Where("slug = ? AND published = ?", slug, true).First(&article).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch base article"})
		return
	}

	titleTerms := strings.Fields(strings.ToLower(article.Title))
	var related []models.Article
	query := h.DB.
		Preload("Author").
		Preload("Category").
		Where("published = ? AND id <> ?", true, article.ID)

	if article.CategoryID != 0 {
		query = query.Where("category_id = ?", article.CategoryID)
	}

	if len(titleTerms) > 0 {
		keyword := "%" + titleTerms[0] + "%"
		query = query.Order(gorm.Expr("CASE WHEN LOWER(title) LIKE ? THEN 0 ELSE 1 END", keyword))
	}

	if err := query.Order("views DESC").Order("created_at DESC").Limit(limit).Find(&related).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch related articles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": related})
}

func (h *ArticleHandler) GetCategories(c *gin.Context) {
	var categories []models.Category
	if err := h.DB.Order("name ASC").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch categories"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": categories})
}

func (h *ArticleHandler) GetAuthors(c *gin.Context) {
	var authors []models.User
	if err := h.DB.Order("name ASC").Find(&authors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch authors"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": authors})
}

func (h *ArticleHandler) CreateArticle(c *gin.Context) {
	var req createArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Thumbnail = strings.TrimSpace(req.Thumbnail)

	if req.Title == "" || req.Content == "" || req.AuthorID == 0 || req.CategoryID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title, content, authorId, and categoryId are required"})
		return
	}

	var author models.User
	if err := h.DB.First(&author, req.AuthorID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "author not found"})
		return
	}

	var category models.Category
	if err := h.DB.First(&category, req.CategoryID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category not found"})
		return
	}

	slug := h.generateUniqueSlug(req.Title)
	readTime := req.ReadTime
	if readTime <= 0 {
		readTime = estimateReadTime(req.Content)
	}

	article := models.Article{
		Title:      req.Title,
		Slug:       slug,
		Content:    req.Content,
		Thumbnail:  req.Thumbnail,
		ReadTime:   readTime,
		Views:      0,
		Published:  req.Published,
		AuthorID:   req.AuthorID,
		CategoryID: req.CategoryID,
	}

	if err := h.DB.Create(&article).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create article"})
		return
	}

	h.trackUploadedImages(collectArticleUploadPaths(article.Content, article.Thumbnail))

	if err := h.DB.Preload("Author").Preload("Category").First(&article, article.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "article created but failed to load relation data"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": article})
}

func (h *ArticleHandler) UpdateArticle(c *gin.Context) {
	slug := c.Param("slug")

	var article models.Article
	if err := h.DB.Where("slug = ?", slug).First(&article).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch article"})
		return
	}

	var req updateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Thumbnail = strings.TrimSpace(req.Thumbnail)

	if req.Title == "" || req.Content == "" || req.AuthorID == 0 || req.CategoryID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title, content, authorId, and categoryId are required"})
		return
	}

	var author models.User
	if err := h.DB.First(&author, req.AuthorID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "author not found"})
		return
	}

	var category models.Category
	if err := h.DB.First(&category, req.CategoryID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category not found"})
		return
	}

	originalPaths := collectArticleUploadPaths(article.Content, article.Thumbnail)

	readTime := req.ReadTime
	if readTime <= 0 {
		readTime = estimateReadTime(req.Content)
	}

	article.Title = req.Title
	article.Content = req.Content
	article.Thumbnail = req.Thumbnail
	article.ReadTime = readTime
	article.AuthorID = req.AuthorID
	article.CategoryID = req.CategoryID
	article.Published = req.Published

	if err := h.DB.Save(&article).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update article"})
		return
	}

	currentPaths := collectArticleUploadPaths(article.Content, article.Thumbnail)
	h.trackUploadedImages(currentPaths)
	h.cleanupOrphanedImages(diffPaths(originalPaths, currentPaths), article.ID)

	if err := h.DB.Preload("Author").Preload("Category").First(&article, article.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "article updated but failed to load relation data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": article})
}

func (h *ArticleHandler) DeleteArticle(c *gin.Context) {
	slug := c.Param("slug")

	var article models.Article
	if err := h.DB.Where("slug = ?", slug).First(&article).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch article"})
		return
	}

	paths := collectArticleUploadPaths(article.Content, article.Thumbnail)
	if err := h.DB.Delete(&article).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete article"})
		return
	}

	h.cleanupOrphanedImages(paths, article.ID)

	c.JSON(http.StatusOK, gin.H{"message": "article deleted", "deletedAt": time.Now().UTC()})
}

func (h *ArticleHandler) UploadImage(c *gin.Context) {
	fileHeader, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image file is required"})
		return
	}

	if fileHeader.Size > maxUploadSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file size exceeds 5MB"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded file"})
		return
	}
	defer file.Close()

	header := make([]byte, 512)
	bytesRead, readErr := file.Read(header)
	if readErr != nil && readErr != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to inspect uploaded file"})
		return
	}

	contentType := http.DetectContentType(header[:bytesRead])
	ext, valid := allowedImageExtension(contentType)
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported image type"})
		return
	}

	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to rewind uploaded file"})
		return
	}

	if h.SupabaseStore != nil {
		storedName := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), randomToken(6), ext)
		objectKey := storage.BuildObjectKey("articles", storedName)

		buffer := bytes.NewBuffer(nil)
		if _, err := io.Copy(buffer, file); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read uploaded image"})
			return
		}

		publicURL, err := h.SupabaseStore.UploadObject(c.Request.Context(), objectKey, bytes.NewReader(buffer.Bytes()), contentType)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload image to supabase storage"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"data": gin.H{
				"url":  publicURL,
				"path": publicURL,
			},
		})
		return
	}

	if err := os.MkdirAll("uploads", 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare upload directory"})
		return
	}

	storedName := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), randomToken(6), ext)
	targetPath := filepath.Join("uploads", storedName)
	if err := c.SaveUploadedFile(fileHeader, targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store uploaded image"})
		return
	}

	publicPath := "/uploads/" + storedName
	h.trackUploadedImages([]string{publicPath})

	c.JSON(http.StatusCreated, gin.H{
		"data": gin.H{
			"url":  buildFileURL(c, publicPath),
			"path": publicPath,
		},
	})
}

func (h *ArticleHandler) DeleteImage(c *gin.Context) {
	var req deleteImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	path := normalizeUploadPath(firstNonEmpty(strings.TrimSpace(req.Path), strings.TrimSpace(req.URL)))
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image path or url is required"})
		return
	}

	if !isUploadPath(path) {
		if h.SupabaseStore != nil {
			if objectKey, ok := h.SupabaseStore.ObjectKeyFromInput(firstNonEmpty(strings.TrimSpace(req.Path), strings.TrimSpace(req.URL))); ok {
				if err := h.SupabaseStore.DeleteObject(c.Request.Context(), objectKey); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "failed to delete supabase image"})
					return
				}

				c.JSON(http.StatusOK, gin.H{"message": "image deleted", "path": objectKey})
				return
			}
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "only uploaded images can be deleted"})
		return
	}

	h.cleanupOrphanedImages([]string{path}, 0)
	c.JSON(http.StatusOK, gin.H{"message": "image cleanup attempted", "path": path})
}

func (h *ArticleHandler) generateUniqueSlug(title string) string {
	base := slugify(title)
	if base == "" {
		base = "untitled-article"
	}

	current := base
	sequence := 2
	for {
		var count int64
		h.DB.Model(&models.Article{}).Where("slug = ?", current).Count(&count)
		if count == 0 {
			return current
		}

		current = fmt.Sprintf("%s-%d", base, sequence)
		sequence++
	}
}

func slugify(input string) string {
	formatted := strings.ToLower(strings.TrimSpace(input))
	formatted = slugCleanupRegex.ReplaceAllString(formatted, "-")
	formatted = strings.Trim(formatted, "-")
	return formatted
}

func estimateReadTime(content string) int {
	words := len(strings.Fields(content))
	if words == 0 {
		return 1
	}

	minutes := words / 200
	if words%200 != 0 {
		minutes++
	}
	if minutes < 1 {
		minutes = 1
	}

	return minutes
}

func allowedImageExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func randomToken(size int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}

	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}

	return string(b)
}

func buildFileURL(c *gin.Context, path string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = strings.TrimSpace(strings.Split(forwardedProto, ",")[0])
	}

	return fmt.Sprintf("%s://%s%s", scheme, c.Request.Host, path)
}

func (h *ArticleHandler) trackUploadedImages(paths []string) {
	for _, path := range uniquePaths(paths) {
		if !isUploadPath(path) {
			continue
		}

		var image models.UploadedImage
		err := h.DB.Unscoped().Where("path = ?", path).First(&image).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}

		fullPath := filePathFromUploadPath(path)
		var size int64
		if stat, statErr := os.Stat(fullPath); statErr == nil {
			size = stat.Size()
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = h.DB.Create(&models.UploadedImage{
				Path:     path,
				MimeType: mimeTypeFromExtension(filepath.Ext(path)),
				Size:     size,
			}).Error
			continue
		}

		if image.DeletedAt.Valid {
			_ = h.DB.Unscoped().Model(&models.UploadedImage{}).Where("id = ?", image.ID).Update("deleted_at", nil).Error
		}

		_ = h.DB.Model(&models.UploadedImage{}).Where("id = ?", image.ID).Updates(map[string]any{
			"mime_type": mimeTypeFromExtension(filepath.Ext(path)),
			"size":      size,
		}).Error
	}
}

func (h *ArticleHandler) cleanupOrphanedImages(paths []string, excludeArticleID uint) {
	for _, path := range uniquePaths(paths) {
		if !isUploadPath(path) {
			continue
		}

		db := h.DB.Model(&models.Article{}).Where("deleted_at IS NULL").
			Where("thumbnail = ? OR thumbnail LIKE ? OR content LIKE ?", path, "%"+path+"%", "%"+path+"%")
		if excludeArticleID != 0 {
			db = db.Where("id <> ?", excludeArticleID)
		}

		var refs int64
		if err := db.Count(&refs).Error; err != nil || refs > 0 {
			continue
		}

		var image models.UploadedImage
		if err := h.DB.Where("path = ?", path).First(&image).Error; err == nil {
			_ = h.DB.Delete(&image).Error
		}

		if err := os.Remove(filePathFromUploadPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
	}
}

func collectArticleUploadPaths(content string, thumbnail string) []string {
	paths := make([]string, 0)
	thumbnailPath := normalizeUploadPath(thumbnail)
	if isUploadPath(thumbnailPath) {
		paths = append(paths, thumbnailPath)
	}

	matches := regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`).FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		path := normalizeUploadPath(strings.TrimSpace(match[1]))
		if isUploadPath(path) {
			paths = append(paths, path)
		}
	}

	return uniquePaths(paths)
}

func diffPaths(oldPaths []string, newPaths []string) []string {
	newSet := make(map[string]struct{}, len(newPaths))
	for _, path := range newPaths {
		newSet[path] = struct{}{}
	}

	removed := make([]string, 0)
	for _, path := range oldPaths {
		if _, ok := newSet[path]; !ok {
			removed = append(removed, path)
		}
	}

	return uniquePaths(removed)
}

func uniquePaths(paths []string) []string {
	set := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := set[path]; ok {
			continue
		}
		set[path] = struct{}{}
		result = append(result, path)
	}

	return result
}

func normalizeUploadPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "/uploads/") {
		return trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err == nil && strings.HasPrefix(parsed.Path, "/uploads/") {
		return parsed.Path
	}

	return ""
}

func isUploadPath(path string) bool {
	return strings.HasPrefix(path, "/uploads/")
}

func filePathFromUploadPath(path string) string {
	name := strings.TrimPrefix(path, "/uploads/")
	return filepath.Join("uploads", name)
}

func mimeTypeFromExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func parsePositiveInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}
