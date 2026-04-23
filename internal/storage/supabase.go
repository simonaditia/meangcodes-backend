package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type SupabaseStorage struct {
	BaseURL        string
	ServiceRoleKey string
	Bucket         string
	PublicBaseURL  string
	HTTPClient     *http.Client
}

func NewSupabaseStorageFromEnv() (*SupabaseStorage, error) {
	enabled := isTruthy(os.Getenv("SUPABASE_STORAGE_ENABLED"))
	baseURL := strings.TrimSpace(os.Getenv("SUPABASE_URL"))
	serviceRoleKey := strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY"))
	bucket := strings.TrimSpace(os.Getenv("SUPABASE_STORAGE_BUCKET"))

	if baseURL == "" && serviceRoleKey == "" && bucket == "" {
		return nil, nil
	}

	if !enabled {
		if baseURL == "" || serviceRoleKey == "" {
			return nil, nil
		}
	}

	if baseURL == "" || serviceRoleKey == "" {
		return nil, fmt.Errorf("SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY are required when SUPABASE_STORAGE_ENABLED=true")
	}

	if bucket == "" {
		bucket = "uploads"
	}

	cleanBaseURL := strings.TrimRight(baseURL, "/")
	publicBaseURL := fmt.Sprintf("%s/storage/v1/object/public/%s", cleanBaseURL, bucket)

	return &SupabaseStorage{
		BaseURL:        cleanBaseURL,
		ServiceRoleKey: serviceRoleKey,
		Bucket:         bucket,
		PublicBaseURL:  publicBaseURL,
		HTTPClient:     &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (s *SupabaseStorage) UploadObject(ctx context.Context, objectKey string, reader io.Reader, contentType string) (string, error) {
	if strings.TrimSpace(objectKey) == "" {
		return "", fmt.Errorf("object key is required")
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	uploadURL := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.BaseURL, s.Bucket, encodeObjectKey(objectKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+s.ServiceRoleKey)
	req.Header.Set("apikey", s.ServiceRoleKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "false")

	res, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("supabase upload failed (%d): %s", res.StatusCode, string(raw))
	}

	return s.PublicURL(objectKey), nil
}

func (s *SupabaseStorage) DeleteObject(ctx context.Context, objectKey string) error {
	if strings.TrimSpace(objectKey) == "" {
		return fmt.Errorf("object key is required")
	}

	deleteURL := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.BaseURL, s.Bucket, encodeObjectKey(objectKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.ServiceRoleKey)
	req.Header.Set("apikey", s.ServiceRoleKey)

	res, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(res.Body)
		return fmt.Errorf("supabase delete failed (%d): %s", res.StatusCode, string(raw))
	}

	return nil
}

func (s *SupabaseStorage) PublicURL(objectKey string) string {
	trimmed := strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	return fmt.Sprintf("%s/%s", s.PublicBaseURL, trimmed)
}

func (s *SupabaseStorage) ObjectKeyFromInput(raw string) (string, bool) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", false
	}

	if strings.HasPrefix(input, s.PublicBaseURL+"/") {
		return strings.TrimPrefix(input, s.PublicBaseURL+"/"), true
	}

	parsed, err := url.Parse(input)
	if err == nil {
		publicPrefix := fmt.Sprintf("/storage/v1/object/public/%s/", s.Bucket)
		if strings.HasPrefix(parsed.Path, publicPrefix) {
			return strings.TrimPrefix(parsed.Path, publicPrefix), true
		}
	}

	if !strings.Contains(input, "://") && !strings.HasPrefix(input, "/") {
		return strings.TrimLeft(input, "/"), true
	}

	return "", false
}

func BuildObjectKey(folder string, fileName string) string {
	cleanFolder := strings.Trim(strings.TrimSpace(folder), "/")
	if cleanFolder == "" {
		cleanFolder = "articles"
	}

	monthPath := time.Now().UTC().Format("2006/01")
	return path.Join(cleanFolder, monthPath, strings.TrimLeft(strings.TrimSpace(fileName), "/"))
}

func encodeObjectKey(objectKey string) string {
	parts := strings.Split(strings.TrimLeft(strings.TrimSpace(objectKey), "/"), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		encoded = append(encoded, url.PathEscape(part))
	}

	return strings.Join(encoded, "/")
}

func isTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
