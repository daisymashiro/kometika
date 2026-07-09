package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// APIConfig untuk setiap endpoint
type APIConfig struct {
	URL       string
	FieldName string
	Name      string
}

// Daftar API shortener
var shortenerAPIs = []APIConfig{
	{URL: "https://api-shorturl.nayumiwandi.workers.dev/", FieldName: "short_url", Name: "nayumiwandi"},
	{URL: "https://apiv1.layanan-yumi.workers.dev/", FieldName: "short_url", Name: "layanan-yumi"},
	{URL: "https://apishorturl2.yumidev.workers.dev/", FieldName: "short_url", Name: "yumidev"},
	{URL: "https://apishorturl3.benigof977.workers.dev/", FieldName: "short_url", Name: "benigof977"},
	{URL: "https://www.urlfy.org/api/v1/shorten", FieldName: "shortUrl", Name: "urlfy"},
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// ShortenWithFallback memendekkan URL dengan fallback API
func ShortenWithFallback(longURL string) (string, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("%d", startTime.UnixNano())

	zap.L().Info("Starting URL shortening process",
		zap.String("request_id", requestID),
		zap.String("long_url", maskURL(longURL)),
	)

	if longURL == "" {
		zap.L().Error("URL tidak boleh kosong", zap.String("request_id", requestID))
		return "", errors.New("URL tidak boleh kosong")
	}

	payload := map[string]string{"url": longURL}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		zap.L().Error("Failed to marshal JSON", zap.String("request_id", requestID), zap.Error(err))
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	var lastError error
	var apiErrors []map[string]interface{}

	for idx, api := range shortenerAPIs {
		zap.L().Debug("Trying API",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.Int("attempt", idx+1),
		)

		shortURL, err := tryShortener(api, jsonPayload, requestID)
		if err == nil && shortURL != "" {
			zap.L().Info("URL shortened successfully",
				zap.String("request_id", requestID),
				zap.String("api_name", api.Name),
				zap.String("short_url", shortURL),
				zap.Duration("duration", time.Since(startTime)),
				zap.Int("attempts", idx+1),
			)
			return shortURL, nil
		}

		lastError = err
		apiErrors = append(apiErrors, map[string]interface{}{
			"api":   api.Name,
			"error": err.Error(),
			"time":  time.Now().Format(time.RFC3339),
		})
		zap.L().Warn("API failed", zap.String("request_id", requestID), zap.String("api_name", api.Name), zap.Error(err))
	}

	zap.L().Error("All shortener APIs failed",
		zap.String("request_id", requestID),
		zap.Duration("total_duration", time.Since(startTime)),
		zap.Any("api_errors", apiErrors),
		zap.Error(lastError),
	)
	return "", errors.New("semua API gagal memendekkan URL")
}

// tryShortener mencoba satu API
func tryShortener(api APIConfig, payload []byte, requestID string) (string, error) {
	startTime := time.Now()

	resp, err := httpClient.Post(api.URL, "application/json", bytes.NewReader(payload))
	if err != nil {
		zap.L().Error("HTTP request failed",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.Error(err),
			zap.Duration("duration", time.Since(startTime)),
		)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		zap.L().Warn("Unexpected status code",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.Int("status_code", resp.StatusCode),
		)
		return "", fmt.Errorf("status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		zap.L().Error("Failed to read response body",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.Error(err),
		)
		return "", err
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		zap.L().Error("Failed to unmarshal JSON",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.Error(err),
		)
		return "", err
	}

	shortURL, ok := respData[api.FieldName].(string)
	if !ok || shortURL == "" {
		zap.L().Error("Field not found or empty",
			zap.String("request_id", requestID),
			zap.String("api_name", api.Name),
			zap.String("field_name", api.FieldName),
		)
		return "", errors.New("field tidak ditemukan atau kosong")
	}

	return shortURL, nil
}

// maskURL untuk privasi
func maskURL(url string) string {
	if len(url) > 50 {
		return url[:50] + "..."
	}
	return url
}

