package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"meangcodes/backend/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createBufferRequest struct {
	TitleHint  string   `json:"titleHint"`
	Topic      string   `json:"topic"`
	References []string `json:"references"`
	Priority   int      `json:"priority"`
	Notes      string   `json:"notes"`
}

type updateBufferRequest struct {
	TitleHint  *string   `json:"titleHint"`
	Topic      *string   `json:"topic"`
	References *[]string `json:"references"`
	Priority   *int      `json:"priority"`
	Status     *string   `json:"status"`
	Notes      *string   `json:"notes"`
}

type generatedReference struct {
	URL              string `json:"url"`
	Reason           string `json:"reason,omitempty"`
	CredibilityScore int    `json:"credibility_score,omitempty"`
}

type recentTopicContext struct {
	Title    string
	Category string
	Key      string
}

type generatedArticleDraft struct {
	Mode            string               `json:"mode"`
	SelectedTopic   string               `json:"selected_topic"`
	ProposedTitle   string               `json:"proposed_title"`
	Slug            string               `json:"slug"`
	MetaDescription string               `json:"meta_description"`
	Keywords        []string             `json:"keywords"`
	Tags            []string             `json:"tags"`
	References      []generatedReference `json:"references"`
	TrendRationale  string               `json:"trend_rationale"`
	ContentOutline  []string             `json:"content_outline"`
	ArticleMarkdown string               `json:"article_markdown"`
	ThumbnailURL    string               `json:"thumbnail_url"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type repairArticleImagesRequest struct {
	Limit  int  `json:"limit"`
	DryRun bool `json:"dryRun"`
}

type automationRunRequest struct {
	PreferredTopics []string `json:"preferredTopics"`
	BlockedTopics   []string `json:"blockedTopics"`
	MaxRepeatCount  int      `json:"maxRepeatCount"`
}

func (h *ArticleHandler) GetBuffers(c *gin.Context) {
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))

	query := h.DB.Model(&models.ArticleBuffer{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var buffers []models.ArticleBuffer
	if err := query.Order("priority ASC").Order("created_at ASC").Find(&buffers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch buffers"})
		return
	}

	items := make([]gin.H, 0, len(buffers))
	for _, item := range buffers {
		items = append(items, bufferToJSON(item))
	}

	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *ArticleHandler) CreateBuffer(c *gin.Context) {
	var req createBufferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	req.Topic = strings.TrimSpace(req.Topic)
	req.TitleHint = strings.TrimSpace(req.TitleHint)
	req.Notes = strings.TrimSpace(req.Notes)
	references := sanitizeReferences(req.References)
	if req.Topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "topic is required"})
		return
	}
	if len(references) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one reference is required"})
		return
	}

	priority := req.Priority
	if priority <= 0 {
		priority = 100
	}

	buffer := models.ArticleBuffer{
		TitleHint:  req.TitleHint,
		Topic:      req.Topic,
		References: serializeReferences(references),
		Priority:   priority,
		Status:     "queued",
		Notes:      req.Notes,
	}

	if err := h.DB.Create(&buffer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create buffer"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": bufferToJSON(buffer)})
}

func (h *ArticleHandler) UpdateBuffer(c *gin.Context) {
	id := c.Param("id")
	var buffer models.ArticleBuffer
	if err := h.DB.First(&buffer, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "buffer item not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch buffer item"})
		return
	}

	var req updateBufferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	updates := map[string]any{}
	if req.TitleHint != nil {
		updates["title_hint"] = strings.TrimSpace(*req.TitleHint)
	}
	if req.Topic != nil {
		topic := strings.TrimSpace(*req.Topic)
		if topic == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "topic cannot be empty"})
			return
		}
		updates["topic"] = topic
	}
	if req.References != nil {
		references := sanitizeReferences(*req.References)
		if len(references) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "references cannot be empty"})
			return
		}
		updates["references"] = serializeReferences(references)
	}
	if req.Priority != nil {
		priority := *req.Priority
		if priority <= 0 {
			priority = 100
		}
		updates["priority"] = priority
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != "queued" && status != "processed" && status != "failed" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "status must be queued, processed, or failed"})
			return
		}
		updates["status"] = status
	}
	if req.Notes != nil {
		updates["notes"] = strings.TrimSpace(*req.Notes)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": bufferToJSON(buffer)})
		return
	}

	if err := h.DB.Model(&buffer).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update buffer item"})
		return
	}

	if err := h.DB.First(&buffer, buffer.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reload buffer item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": bufferToJSON(buffer)})
}

func (h *ArticleHandler) DeleteBuffer(c *gin.Context) {
	id := c.Param("id")
	var buffer models.ArticleBuffer
	if err := h.DB.First(&buffer, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "buffer item not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch buffer item"})
		return
	}

	if err := h.DB.Delete(&buffer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete buffer item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "buffer item deleted"})
}

func (h *ArticleHandler) RunAutomation(c *gin.Context) {
	var req automationRunRequest
	_ = c.ShouldBindJSON(&req)

	mode := "auto"
	var bufferItem *models.ArticleBuffer
	topic := ""
	titleHint := ""
	references := []string{}
	preferredTopics := sanitizeTopicList(req.PreferredTopics)
	blockedTopics := sanitizeTopicList(req.BlockedTopics)
	maxRepeatCount := req.MaxRepeatCount
	if maxRepeatCount <= 0 {
		maxRepeatCount = 2
	}
	if maxRepeatCount > 5 {
		maxRepeatCount = 5
	}

	var queued models.ArticleBuffer
	err := h.DB.Where("status = ?", "queued").Order("priority ASC").Order("created_at ASC").First(&queued).Error
	if err == nil {
		mode = "manual"
		bufferItem = &queued
		topic = queued.Topic
		titleHint = queued.TitleHint
		references = parseReferences(queued.References)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to inspect buffer queue"})
		return
	}

	recentTopics, recentTopicErr := h.loadRecentTopicContext(10)
	if recentTopicErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": recentTopicErr.Error()})
		return
	}

	promptTemplate := loadAutomationPromptTemplate()
	draft, draftErr := h.generateArticleDraft(mode, topic, titleHint, references, promptTemplate, recentTopics, preferredTopics, blockedTopics, maxRepeatCount)
	if draftErr != nil {
		if bufferItem != nil {
			_ = h.DB.Model(bufferItem).Updates(map[string]any{
				"status": "failed",
				"notes":  draftErr.Error(),
			}).Error
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": draftErr.Error()})
		return
	}

	authorID, authorErr := h.pickAuthorID()
	if authorErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": authorErr.Error()})
		return
	}

	categoryID, categoryErr := h.pickCategoryID(draft.SelectedTopic, draft.ProposedTitle)
	if categoryErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": categoryErr.Error()})
		return
	}

	title := strings.TrimSpace(draft.ProposedTitle)
	if title == "" {
		title = strings.TrimSpace(firstNonEmpty(titleHint, topic, draft.SelectedTopic))
	}
	if title == "" {
		title = "Artikel Otomatis Teknologi"
	}

	content := strings.TrimSpace(draft.ArticleMarkdown)
	if content == "" {
		if bufferItem != nil {
			_ = h.DB.Model(bufferItem).Updates(map[string]any{
				"status": "failed",
				"notes":  "generated article content is empty",
			}).Error
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "generated article content is empty"})
		return
	}
	content = normalizeMarkdownImageURLs(content, draft.SelectedTopic, title)

	thumbnail := resolveThumbnailURL(draft.ThumbnailURL, draft.SelectedTopic, title)

	article := models.Article{
		Title:      title,
		Slug:       h.generateUniqueSlug(title),
		Content:    content,
		Thumbnail:  thumbnail,
		ReadTime:   estimateReadTime(content),
		Views:      0,
		Published:  true,
		AuthorID:   authorID,
		CategoryID: categoryID,
	}

	if err := h.DB.Create(&article).Error; err != nil {
		if bufferItem != nil {
			_ = h.DB.Model(bufferItem).Updates(map[string]any{
				"status": "failed",
				"notes":  "failed to create generated article: " + err.Error(),
			}).Error
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create generated article: " + err.Error()})
		return
	}

	if err := h.DB.Preload("Author").Preload("Category").First(&article, article.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "article generated but failed to load relation data"})
		return
	}

	selectedTopic := firstNonEmpty(draft.SelectedTopic, title)
	topicHistory := models.AutomationTopicHistory{
		ArticleID:     article.ID,
		ArticleTitle:  article.Title,
		SelectedTopic: selectedTopic,
		CategoryName:  article.Category.Name,
		TopicKey:      normalizeAutomationTopicKey(selectedTopic, article.Category.Name),
	}
	if err := h.DB.Create(&topicHistory).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "article generated but failed to record topic history"})
		return
	}

	notes := fmt.Sprintf("processed by automation -> slug: %s", article.Slug)
	if bufferItem != nil {
		_ = h.DB.Model(bufferItem).Updates(map[string]any{"status": "processed", "notes": notes}).Error
	} else {
		autoBuffer := models.ArticleBuffer{
			TitleHint:  draft.ProposedTitle,
			Topic:      firstNonEmpty(draft.SelectedTopic, title),
			References: serializeReferences(referenceURLs(draft.References)),
			Priority:   100,
			Status:     "processed",
			Notes:      notes,
		}
		_ = h.DB.Create(&autoBuffer).Error
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"mode":            mode,
			"selectedTopic":   firstNonEmpty(draft.SelectedTopic, topic),
			"references":      draft.References,
			"article":         article,
			"bufferProcessed": bufferItem != nil,
		},
	})
}

func (h *ArticleHandler) RepairArticleImages(c *gin.Context) {
	var req repairArticleImagesRequest
	_ = c.ShouldBindJSON(&req)

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var articles []models.Article
	if err := h.DB.Where("published = ?", true).Order("id ASC").Limit(limit).Find(&articles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch articles for repair"})
		return
	}

	touched := 0
	repaired := 0
	skipped := 0
	items := make([]gin.H, 0, len(articles))

	for _, article := range articles {
		newThumb := resolveThumbnailURL(article.Thumbnail, article.Title, article.Title)
		newContent := normalizeMarkdownImageURLs(article.Content, article.Title, article.Title)

		thumbChanged := strings.TrimSpace(newThumb) != strings.TrimSpace(article.Thumbnail)
		contentChanged := strings.TrimSpace(newContent) != strings.TrimSpace(article.Content)

		if !thumbChanged && !contentChanged {
			skipped++
			continue
		}

		touched++
		items = append(items, gin.H{
			"id":               article.ID,
			"slug":             article.Slug,
			"thumbnailBefore":  article.Thumbnail,
			"thumbnailAfter":   newThumb,
			"thumbnailChanged": thumbChanged,
			"contentChanged":   contentChanged,
		})

		if req.DryRun {
			continue
		}

		updates := map[string]any{}
		if thumbChanged {
			updates["thumbnail"] = newThumb
		}
		if contentChanged {
			updates["content"] = newContent
		}

		if err := h.DB.Model(&article).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update repaired article images"})
			return
		}

		repaired++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"dryRun":    req.DryRun,
			"processed": len(articles),
			"touched":   touched,
			"repaired":  repaired,
			"unchanged": skipped,
			"items":     items,
		},
	})
}

func (h *ArticleHandler) generateArticleDraft(mode string, topic string, titleHint string, references []string, promptTemplate string, recentTopics []recentTopicContext, preferredTopics []string, blockedTopics []string, maxRepeatCount int) (*generatedArticleDraft, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is not configured")
	}

	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = "openrouter/auto"
	}

	endpoint := strings.TrimSpace(os.Getenv("OPENROUTER_API_URL"))
	if endpoint == "" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}

	systemPrompt := "You are an AI content orchestrator for Indonesian tech blog. Output JSON only. No markdown wrapper, no explanation outside JSON."
	diversityPrompt := buildTopicDiversityPrompt(recentTopics, mode, topic, titleHint, references, preferredTopics, blockedTopics, maxRepeatCount)
	userPrompt := fmt.Sprintf(
		"Follow this policy and execute one run:\n\n%s\n\n%s\n\nExecution mode: %s\nTopic hint: %s\nTitle hint: %s\nReferences from manual buffer: %s\n\nReturn strict JSON with fields: mode, selected_topic, proposed_title, slug, meta_description, keywords, tags, references, trend_rationale, content_outline, article_markdown, thumbnail_url, quality_check.",
		promptTemplate,
		diversityPrompt,
		mode,
		topic,
		titleHint,
		strings.Join(references, ", "),
	)

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0.6,
		"response_format": map[string]string{"type": "json_object"},
	}

	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("failed to construct openrouter request")
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "http://localhost:5173")
	req.Header.Set("X-Title", "meangcodes-auto-article")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("failed to call openrouter")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter request failed with status %d", resp.StatusCode)
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.New("failed to parse openrouter response")
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("openrouter returned empty choices")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	draft, parseErr := parseGeneratedDraft(content)
	if parseErr != nil {
		return nil, parseErr
	}

	draft.ProposedTitle = strings.TrimSpace(draft.ProposedTitle)
	draft.SelectedTopic = strings.TrimSpace(draft.SelectedTopic)
	draft.ArticleMarkdown = strings.TrimSpace(draft.ArticleMarkdown)
	if draft.ArticleMarkdown == "" {
		return nil, errors.New("generated article markdown is empty")
	}

	if len(draft.References) == 0 {
		for _, ref := range references {
			draft.References = append(draft.References, generatedReference{URL: ref, Reason: "manual buffer reference", CredibilityScore: 8})
		}
	}

	if shouldRejectRecentTopic(draft, recentTopics, maxRepeatCount, blockedTopics) {
		if retryDraft, retryErr := h.retryDraftWithTopicDiversity(mode, topic, titleHint, references, promptTemplate, recentTopics, preferredTopics, blockedTopics, maxRepeatCount); retryErr == nil {
			draft = retryDraft
		}
	}

	return draft, nil
}

func (h *ArticleHandler) retryDraftWithTopicDiversity(mode string, topic string, titleHint string, references []string, promptTemplate string, recentTopics []recentTopicContext, preferredTopics []string, blockedTopics []string, maxRepeatCount int) (*generatedArticleDraft, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is not configured")
	}

	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = "openrouter/auto"
	}

	endpoint := strings.TrimSpace(os.Getenv("OPENROUTER_API_URL"))
	if endpoint == "" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}

	systemPrompt := "You are an AI content orchestrator for Indonesian tech blog. Output JSON only. No markdown wrapper, no explanation outside JSON."
	diversityPrompt := buildTopicDiversityPrompt(recentTopics, mode, topic, titleHint, references, preferredTopics, blockedTopics, maxRepeatCount)
	userPrompt := fmt.Sprintf(
		"The previous answer repeated a recent topic family. Regenerate with stronger topic diversity.\n\n%s\n\n%s\n\nExecution mode: %s\nTopic hint: %s\nTitle hint: %s\nReferences from manual buffer: %s\n\nReturn strict JSON with fields: mode, selected_topic, proposed_title, slug, meta_description, keywords, tags, references, trend_rationale, content_outline, article_markdown, thumbnail_url, quality_check.",
		promptTemplate,
		diversityPrompt,
		mode,
		topic,
		titleHint,
		strings.Join(references, ", "),
	)

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0.8,
		"response_format": map[string]string{"type": "json_object"},
	}

	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("failed to construct openrouter request")
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "http://localhost:5173")
	req.Header.Set("X-Title", "meangcodes-auto-article")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("failed to call openrouter")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter request failed with status %d", resp.StatusCode)
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.New("failed to parse openrouter response")
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("openrouter returned empty choices")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	draft, parseErr := parseGeneratedDraft(content)
	if parseErr != nil {
		return nil, parseErr
	}

	draft.ProposedTitle = strings.TrimSpace(draft.ProposedTitle)
	draft.SelectedTopic = strings.TrimSpace(draft.SelectedTopic)
	draft.ArticleMarkdown = strings.TrimSpace(draft.ArticleMarkdown)
	if draft.ArticleMarkdown == "" {
		return nil, errors.New("generated article markdown is empty")
	}

	if len(draft.References) == 0 {
		for _, ref := range references {
			draft.References = append(draft.References, generatedReference{URL: ref, Reason: "manual buffer reference", CredibilityScore: 8})
		}
	}

	return draft, nil
}

func buildTopicDiversityPrompt(recentTopics []recentTopicContext, mode string, topic string, titleHint string, references []string, preferredTopics []string, blockedTopics []string, maxRepeatCount int) string {
	lines := []string{
		"Topic diversity rule:",
		fmt.Sprintf("- In auto mode, do not reuse any topic family that already appears %d or more times in the recent published history.", maxRepeatCount),
		"- Prefer a clearly different subject from the recent published history.",
		"- If recent history already repeats a family like serverless, choose another technology family and do not select another serverless angle.",
		"- Keep the article varied across the site: cloud, backend, frontend, security, AI tools, databases, testing, performance, DevOps, developer productivity.",
	}

	if len(preferredTopics) > 0 {
		lines = append(lines, "Preferred topic families to prioritize when picking a fresh angle:")
		for _, item := range preferredTopics {
			lines = append(lines, "- "+item)
		}
	}

	if len(blockedTopics) > 0 {
		lines = append(lines, "Blocked topic families to avoid unless the user explicitly requested them:")
		for _, item := range blockedTopics {
			lines = append(lines, "- "+item)
		}
	}

	if mode == "auto" && strings.TrimSpace(topic) == "" && strings.TrimSpace(titleHint) == "" && len(references) == 0 {
		lines = append(lines, "- AUTO mode is active because no manual inputs were provided. You must choose a fresh topic family that is not a near-duplicate of recent topics.")
	}

	if len(recentTopics) > 0 {
		lines = append(lines, "Recent published topic history (newest first):")
		for index, item := range recentTopics {
			lines = append(lines, fmt.Sprintf("%d. %s | category: %s | key: %s", index+1, item.Title, item.Category, item.Key))
		}

		excluded := repeatedRecentTopicKeys(recentTopics, maxRepeatCount)
		if len(excluded) > 0 {
			lines = append(lines, "Excluded topic families because they repeat too often in recent history:")
			for _, key := range excluded {
				lines = append(lines, "- "+key)
			}
		}
	}

	return strings.Join(lines, "\n")
}

func shouldRejectRecentTopic(draft *generatedArticleDraft, recentTopics []recentTopicContext, maxRepeatCount int, blockedTopics []string) bool {
	if draft == nil || len(recentTopics) == 0 {
		return false
	}

	draftKey := automationTopicFamilySignature(firstNonEmpty(draft.SelectedTopic, draft.ProposedTitle), "")
	if draftKey == "" {
		return false
	}

	for _, blocked := range blockedTopics {
		if draftKey == automationTopicFamilySignature(blocked, "") {
			return true
		}
	}

	for _, key := range repeatedRecentTopicKeys(recentTopics, maxRepeatCount) {
		if key == draftKey {
			return true
		}
	}

	return false
}

func repeatedRecentTopicKeys(recentTopics []recentTopicContext, maxRepeatCount int) []string {
	if maxRepeatCount <= 1 {
		maxRepeatCount = 2
	}

	counts := map[string]int{}
	order := []string{}
	for _, item := range recentTopics {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := counts[key]; !ok {
			order = append(order, key)
		}
		counts[key]++
	}

	repeated := make([]string, 0)
	for _, key := range order {
		if counts[key] >= maxRepeatCount {
			repeated = append(repeated, key)
		}
	}

	return repeated
}

func (h *ArticleHandler) loadRecentTopicContext(limit int) ([]recentTopicContext, error) {
	if limit <= 0 {
		limit = 10
	}

	var histories []models.AutomationTopicHistory
	if err := h.DB.Order("created_at DESC").Limit(limit).Find(&histories).Error; err == nil && len(histories) > 0 {
		contexts := make([]recentTopicContext, 0, len(histories))
		for _, history := range histories {
			title := strings.TrimSpace(history.ArticleTitle)
			category := strings.TrimSpace(history.CategoryName)
			key := automationTopicFamilySignature(firstNonEmpty(history.SelectedTopic, title), category)
			if key == "" {
				continue
			}

			contexts = append(contexts, recentTopicContext{
				Title:    title,
				Category: category,
				Key:      key,
			})
		}

		if len(contexts) > 0 {
			return contexts, nil
		}
	}

	var articles []models.Article
	if err := h.DB.Preload("Category").Where("published = ?", true).Order("created_at DESC").Limit(limit).Find(&articles).Error; err != nil {
		return nil, errors.New("failed to load recent article history")
	}

	contexts := make([]recentTopicContext, 0, len(articles))
	for _, article := range articles {
		title := strings.TrimSpace(article.Title)
		category := strings.TrimSpace(article.Category.Name)
		key := automationTopicFamilySignature(title, category)
		if key == "" {
			continue
		}

		contexts = append(contexts, recentTopicContext{
			Title:    title,
			Category: category,
			Key:      key,
		})
	}

	return contexts, nil
}

func sanitizeTopicList(values []string) []string {
	clean := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := automationTopicFamilySignature(value, "")
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clean = append(clean, value)
	}

	return clean
}

func automationTopicFamilySignature(title string, category string) string {
	combined := strings.ToLower(strings.TrimSpace(category + " " + title))
	combined = strings.NewReplacer(
		"/", " ",
		"|", " ",
		"-", " ",
		"_", " ",
		":", " ",
		",", " ",
		".", " ",
		"!", " ",
		"?", " ",
		"(", " ",
		")", " ",
	).Replace(combined)

	parts := strings.Fields(combined)
	families := []string{}
	seen := map[string]struct{}{}
	for _, part := range parts {
		family := automationTopicFamilyToken(part)
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		families = append(families, family)
	}

	if len(families) == 0 {
		return normalizeAutomationTopicKey(title, category)
	}

	return strings.Join(families, " ")
}

func automationTopicFamilyToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	familyAliases := map[string]string{
		"serverless":       "serverless",
		"lambda":           "serverless",
		"faas":             "serverless",
		"functions":        "serverless",
		"function":         "serverless",
		"container":        "containers",
		"containers":       "containers",
		"containerization": "containers",
		"docker":           "containers",
		"kubernetes":       "containers",
		"k8s":              "containers",
		"cloud":            "cloud",
		"cloud-native":     "cloud",
		"cloudnative":      "cloud",
		"backend":          "backend",
		"frontend":         "frontend",
		"security":         "security",
		"ai":               "ai",
		"database":         "database",
		"databases":        "database",
		"performance":      "performance",
		"devops":           "devops",
		"testing":          "testing",
		"productivity":     "productivity",
	}

	if family, ok := familyAliases[value]; ok {
		return family
	}

	for token, family := range familyAliases {
		if strings.Contains(value, token) {
			return family
		}
	}

	return value
}

func normalizeAutomationTopicKey(title string, category string) string {
	combined := strings.ToLower(strings.TrimSpace(category + " " + title))
	combined = strings.NewReplacer(
		"/", " ",
		"|", " ",
		"-", " ",
		"_", " ",
		":", " ",
		",", " ",
		".", " ",
		"!", " ",
		"?", " ",
	).Replace(combined)

	stopwords := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "atau": {}, "dalam": {}, "the": {}, "to": {}, "of": {}, "for": {},
		"cara": {}, "tips": {}, "panduan": {}, "mengapa": {}, "apa": {}, "untuk": {}, "dengan": {}, "pada": {},
		"andalan": {}, "terbaik": {}, "update": {}, "guide": {}, "tutorial": {}, "build": {}, "membangun": {},
	}

	parts := strings.Fields(combined)
	filtered := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := stopwords[part]; ok {
			continue
		}
		if len(part) < 3 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		filtered = append(filtered, part)
		if len(filtered) >= 5 {
			break
		}
	}

	return strings.Join(filtered, " ")
}

func parseGeneratedDraft(content string) (*generatedArticleDraft, error) {
	candidates := []string{}
	seen := map[string]struct{}{}
	pushCandidate := func(value string) {
		normalized := normalizeModelContent(value)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	}

	pushCandidate(content)
	if unquoted, ok := unquoteJSONString(content); ok {
		pushCandidate(unquoted)
	}

	if extracted := extractJSONObject(content); extracted != "" {
		pushCandidate(extracted)
		if unquoted, ok := unquoteJSONString(extracted); ok {
			pushCandidate(unquoted)
		}
	}

	for _, candidate := range candidates {
		var draft generatedArticleDraft
		if err := json.Unmarshal([]byte(candidate), &draft); err == nil {
			return &draft, nil
		}
	}

	return nil, errors.New("openrouter JSON is invalid")
}

func (h *ArticleHandler) pickAuthorID() (uint, error) {
	var author models.User
	if err := h.DB.Where("role = ?", "admin").Order("id ASC").First(&author).Error; err == nil {
		return author.ID, nil
	}

	if err := h.DB.Order("id ASC").First(&author).Error; err != nil {
		return 0, errors.New("failed to resolve author for generated article")
	}

	return author.ID, nil
}

func (h *ArticleHandler) pickCategoryID(topic string, title string) (uint, error) {
	var categories []models.Category
	if err := h.DB.Order("name ASC").Find(&categories).Error; err != nil {
		return 0, errors.New("failed to resolve categories")
	}
	if len(categories) == 0 {
		return 0, errors.New("no category available")
	}

	probe := strings.ToLower(topic + " " + title)
	for _, item := range categories {
		if strings.Contains(probe, strings.ToLower(item.Name)) || strings.Contains(probe, strings.ToLower(item.Slug)) {
			return item.ID, nil
		}
	}

	for _, item := range categories {
		if strings.Contains(probe, "api") || strings.Contains(probe, "backend") || strings.Contains(probe, "rest") {
			if strings.Contains(strings.ToLower(item.Name), "program") || strings.Contains(strings.ToLower(item.Slug), "program") {
				return item.ID, nil
			}
		}
	}

	return categories[0].ID, nil
}

func bufferToJSON(item models.ArticleBuffer) gin.H {
	return gin.H{
		"id":         item.ID,
		"titleHint":  item.TitleHint,
		"topic":      item.Topic,
		"references": parseReferences(item.References),
		"priority":   item.Priority,
		"status":     item.Status,
		"notes":      item.Notes,
		"createdAt":  item.CreatedAt,
		"updatedAt":  item.UpdatedAt,
	}
}

func sanitizeReferences(references []string) []string {
	clean := make([]string, 0, len(references))
	seen := map[string]struct{}{}
	for _, raw := range references {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(value), "http://") && !strings.HasPrefix(strings.ToLower(value), "https://") {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
	}

	return clean
}

func parseReferences(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}

	var refs []string
	if err := json.Unmarshal([]byte(trimmed), &refs); err != nil {
		return []string{}
	}

	return sanitizeReferences(refs)
}

func serializeReferences(references []string) string {
	clean := sanitizeReferences(references)
	if len(clean) == 0 {
		return "[]"
	}

	data, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}

	return string(data)
}

func referenceURLs(references []generatedReference) []string {
	urls := make([]string, 0, len(references))
	for _, ref := range references {
		if strings.TrimSpace(ref.URL) != "" {
			urls = append(urls, strings.TrimSpace(ref.URL))
		}
	}

	return sanitizeReferences(urls)
}

func extractJSONObject(input string) string {
	trimmed := normalizeModelContent(input)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}

	start := strings.Index(trimmed, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(trimmed); i++ {
		ch := trimmed[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}

		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(trimmed[start : i+1])
			}
		}
	}

	return ""
}

func normalizeModelContent(input string) string {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```JSON")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func unquoteJSONString(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) < 2 {
		return "", false
	}

	if trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
		return "", false
	}

	unquoted, err := strconv.Unquote(trimmed)
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(unquoted), true
}

var markdownImageRegex = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func normalizeMarkdownImageURLs(content string, topic string, title string) string {
	if strings.TrimSpace(content) == "" {
		return content
	}

	index := 0
	return markdownImageRegex.ReplaceAllStringFunc(content, func(match string) string {
		parts := markdownImageRegex.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}

		alt := strings.TrimSpace(parts[1])
		src := strings.TrimSpace(parts[2])
		if isValidHTTPURL(src) && !isKnownIrrelevantImageURL(src) && isReachableImageURL(src) {
			return match
		}

		index++
		fallback := buildRelevantImageURL(topic, title, alt, index, 1280, 720)
		if alt == "" {
			alt = "illustration"
		}

		return fmt.Sprintf("![%s](%s)", alt, fallback)
	})
}

func resolveThumbnailURL(raw string, topic string, title string) string {
	value := strings.TrimSpace(raw)
	if isValidHTTPURL(value) && !isKnownIrrelevantImageURL(value) && isReachableImageURL(value) {
		return value
	}

	return buildRelevantImageURL(topic, title, "thumbnail", 0, 1200, 630)
}

func buildRelevantImageURL(topic string, title string, hint string, index int, width int, height int) string {
	return buildGeneratedTopicIllustrationURL(topic, title, hint, index, width, height)
}

func buildImageKeywords(values ...string) []string {
	cleaner := regexp.MustCompile(`[^a-z0-9\s-]+`)
	seen := map[string]struct{}{}
	keywords := []string{"technology", "software"}

	for _, value := range values {
		normalized := strings.ToLower(cleaner.ReplaceAllString(value, " "))
		for _, piece := range strings.Fields(normalized) {
			if len(piece) < 3 {
				continue
			}
			if _, ok := seen[piece]; ok {
				continue
			}
			seen[piece] = struct{}{}
			keywords = append(keywords, piece)
			if len(keywords) >= 8 {
				return keywords
			}
		}
	}

	return keywords
}

func buildFallbackImageURL(topic string, title string, index int, width int, height int) string {
	seedParts := []string{strings.TrimSpace(topic), strings.TrimSpace(title)}
	if index > 0 {
		seedParts = append(seedParts, strconv.Itoa(index))
	}

	seed := strings.ToLower(strings.Join(seedParts, "-"))
	seed = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(seed, "-")
	seed = strings.Trim(seed, "-")
	if seed == "" {
		seed = "meangcodes-tech"
	}

	return fmt.Sprintf("https://picsum.photos/seed/%s/%d/%d", seed, width, height)
}

func buildGeneratedTopicIllustrationURL(topic string, title string, hint string, index int, width int, height int) string {
	mainTitle := strings.TrimSpace(firstNonEmpty(title, topic, "Artikel Teknologi"))
	subtitle := strings.TrimSpace(firstNonEmpty(topic, hint, "Automasi Konten"))
	if index > 0 {
		subtitle = fmt.Sprintf("%s • Visual %d", subtitle, index)
	}

	if len(mainTitle) > 76 {
		mainTitle = mainTitle[:76] + "..."
	}
	if len(subtitle) > 96 {
		subtitle = subtitle[:96] + "..."
	}

	mainTitle = sanitizeSVGText(mainTitle)
	subtitle = sanitizeSVGText(subtitle)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s"><defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0%%" stop-color="#0f172a"/><stop offset="100%%" stop-color="#155e75"/></linearGradient></defs><rect width="100%%" height="100%%" fill="url(#bg)"/><rect x="40" y="40" width="%d" height="%d" rx="22" fill="rgba(15,23,42,0.50)" stroke="rgba(255,255,255,0.18)"/><text x="72" y="130" fill="#c7d2fe" font-family="Manrope,Segoe UI,sans-serif" font-size="28" font-weight="700">MEANGCODES</text><text x="72" y="188" fill="#ffffff" font-family="Manrope,Segoe UI,sans-serif" font-size="44" font-weight="800">%s</text><text x="72" y="246" fill="#bae6fd" font-family="Manrope,Segoe UI,sans-serif" font-size="26">%s</text></svg>`, width, height, width, height, mainTitle, width-80, height-80, mainTitle, subtitle)

	return "data:image/svg+xml;utf8," + url.QueryEscape(svg)
}

func sanitizeSVGText(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)

	return replacer.Replace(strings.TrimSpace(value))
}

func isValidHTTPURL(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	host := strings.TrimSpace(parsed.Host)
	if host == "" {
		return false
	}

	return !isDisallowedImageHost(host)
}

func isDisallowedImageHost(host string) bool {
	clean := strings.ToLower(strings.TrimSpace(host))
	if strings.Contains(clean, ":") {
		if h, _, err := net.SplitHostPort(clean); err == nil {
			clean = h
		} else {
			clean = strings.Split(clean, ":")[0]
		}
	}
	clean = strings.TrimPrefix(clean, "www.")

	disallowed := []string{
		"example.com",
		"source.unsplash.com",
		"placeholder.com",
		"via.placeholder.com",
		"placehold.co",
		"dummyimage.com",
		"fakeimg.pl",
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
	}

	for _, blocked := range disallowed {
		if clean == blocked || strings.HasSuffix(clean, "."+blocked) {
			return true
		}
	}

	return false
}

func isKnownIrrelevantImageURL(raw string) bool {
	probe := strings.ToLower(strings.TrimSpace(raw))
	if probe == "" {
		return false
	}

	blockedPatterns := []string{
		"loremflickr.com/cache/resized/defaultimage",
		"defaultimage.small",
		"defaultimage_",
	}

	for _, pattern := range blockedPatterns {
		if strings.Contains(probe, pattern) {
			return true
		}
	}

	return false
}

func isReachableImageURL(raw string) bool {
	client := &http.Client{Timeout: 8 * time.Second}

	requestHead, err := http.NewRequest(http.MethodHead, raw, nil)
	if err == nil {
		if response, headErr := client.Do(requestHead); headErr == nil {
			defer response.Body.Close()
			if response.Request != nil && response.Request.URL != nil {
				if isKnownIrrelevantImageURL(response.Request.URL.String()) {
					return false
				}
			}
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
				if strings.HasPrefix(contentType, "image/") {
					return true
				}
				if response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusNotImplemented && contentType != "" {
					return false
				}
			}
		}
	}

	requestGet, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return false
	}
	requestGet.Header.Set("Range", "bytes=0-1024")

	response, err := client.Do(requestGet)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		if isKnownIrrelevantImageURL(response.Request.URL.String()) {
			return false
		}
	}

	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return false
	}

	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	return strings.HasPrefix(contentType, "image/")
}

func loadAutomationPromptTemplate() string {
	paths := []string{
		"../fiturotomatis.txt",
		"fiturotomatis.txt",
		"../../fiturotomatis.txt",
	}

	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(content)) != "" {
			return string(content)
		}
	}

	return "Use manual buffer first. If empty, find trending technology topic and generate complete article in Indonesian with references and image plan."
}
