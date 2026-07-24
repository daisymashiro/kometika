package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"mybot/internal/log"

	"go.uber.org/zap"
)

// ======================== AI PROVIDER INTERFACE ========================

type AIProvider interface {
	Name() string
	Generate(ctx context.Context, prompt string) (string, error)
}

// ======================== CUSTOM ERROR UNTUK RATE LIMIT ========================

type RateLimitError struct {
	RetryAfter int  // detik (dari API)
	IsDaily    bool // true = kuota harian habis (tidak perlu retry di hari yang sama)
	Msg        string
}

func (e RateLimitError) Error() string {
	return e.Msg
}

// ======================== PROVIDER CHAIN ========================

var (
	aiProviderChain []AIProvider
	aiProviderOnce  sync.Once
)

// InitAIProviders baca env dan daftarkan provider dengan multi-key fallback
// Urutan: Gemini(k1→k2→k3) → OpenRouter → Groq
func InitAIProviders() {
	aiProviderOnce.Do(func() {
		aiProviderChain = nil

		// Gemini — multiple API keys
		for _, key := range readMultiKeys("GEMINI_API_KEY") {
			aiProviderChain = append(aiProviderChain, &GeminiProvider{apiKey: key})
		}

		// OpenRouter — multiple API keys
		for _, key := range readMultiKeys("OPENROUTER_API_KEY") {
			aiProviderChain = append(aiProviderChain, &OpenRouterProvider{apiKey: key})
		}

		// Groq — multiple API keys
		for _, key := range readMultiKeys("GROQ_API_KEY") {
			aiProviderChain = append(aiProviderChain, &GroqProvider{apiKey: key})
		}
	})
}

// readMultiKeys baca API_KEY, API_KEY_2, API_KEY_3, ... sampai kosong
func readMultiKeys(prefix string) []string {
	var keys []string
	if k := os.Getenv(prefix); k != "" {
		keys = append(keys, k)
	}
	for i := 2; i <= 10; i++ {
		env := fmt.Sprintf("%s_%d", prefix, i)
		if k := os.Getenv(env); k != "" {
			keys = append(keys, k)
		} else {
			break
		}
	}
	return keys
}

// GenerateWithFallback coba tiap provider urut, balik hasil pertama yg sukses.
// Sekarang mendukung:
// - Exponential backoff untuk Gemini (45-70 detik dasar, dikali 2^attempt)
// - Penundaan sesuai retryDelay dari API untuk provider lain (OpenRouter/Groq)
func GenerateWithFallback(ctx context.Context, prompt string, logger *zap.Logger) (string, string, error) {
	InitAIProviders()

	if len(aiProviderChain) == 0 {
		return "", "", fmt.Errorf("tidak ada AI provider terdaftar. Isi GEMINI_API_KEY / OPENROUTER_API_KEY / GROQ_API_KEY di .env")
	}

	var lastErr error

	for _, p := range aiProviderChain {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		default:
		}

		// Maksimal 2 percobaan untuk provider ini (jika 429 dan bukan daily)
		maxAttempts := 2
		for attempt := 0; attempt < maxAttempts; attempt++ {
			result, err := p.Generate(ctx, prompt)
			if err == nil {
				logger.Info("AI reply sukses",
					zap.String("provider", p.Name()),
				)
				return result, p.Name(), nil
			}

			// Cek apakah error adalah RateLimitError (khusus 429)
			if rlErr, ok := err.(RateLimitError); ok {
				if rlErr.IsDaily {
					// Kuota harian habis → skip provider ini (tidak ada gunanya retry di hari yang sama)
					logger.Warn("Kuota harian habis, skip provider",
						zap.String("provider", p.Name()),
						zap.String("reason", rlErr.Msg),
					)
					break // keluar dari loop attempt, lanjut ke provider berikutnya
				}

				// =======================================================
				// KHUSUS GEMINI: Exponential Backoff + Random 45-70 detik
				// =======================================================
				if strings.Contains(p.Name(), "Gemini") {
					// Base wait: 45-70 detik (random)
					baseWait := 45 + rand.Intn(26) // 45 sampai 70
					// Exponential backoff: 1x, 2x, 4x (sesuai attempt)
					waitSeconds := baseWait * (1 << attempt) // shift left = kali 2^attempt

					if attempt < maxAttempts-1 {
						logger.Warn("Gemini rate limit, menerapkan exponential backoff",
							zap.String("provider", p.Name()),
							zap.Int("attempt", attempt+1),
							zap.Int("wait_seconds", waitSeconds),
							zap.Int("base_wait", baseWait),
						)
						select {
						case <-time.After(time.Duration(waitSeconds) * time.Second):
							// tunggu selesai, lanjut retry
							continue
						case <-ctx.Done():
							return "", "", ctx.Err()
						}
					} else {
						// Attempt terakhir gagal
						lastErr = err
						logger.Warn("Gemini gagal setelah retry terakhir",
							zap.String("provider", p.Name()),
							zap.Error(err),
						)
					}
				} else {
					// =======================================================
					// PROVIDER LAIN (OpenRouter, Groq): pakai retryDelay dari API
					// =======================================================
					waitSeconds := rlErr.RetryAfter
					if waitSeconds <= 0 {
						waitSeconds = 60 // default 60 detik
					}
					if attempt < maxAttempts-1 {
						logger.Warn("Rate limit, tunggu sebelum retry",
							zap.String("provider", p.Name()),
							zap.Int("wait_seconds", waitSeconds),
						)
						select {
						case <-time.After(time.Duration(waitSeconds) * time.Second):
							continue
						case <-ctx.Done():
							return "", "", ctx.Err()
						}
					} else {
						lastErr = err
						logger.Warn("Provider gagal setelah retry",
							zap.String("provider", p.Name()),
							zap.Error(err),
						)
					}
				}
			} else {
				// Error non-rate-limit (400, 500, dll) → langsung skip provider ini
				lastErr = err
				logger.Warn("AI provider gagal, coba next",
					zap.String("provider", p.Name()),
					zap.Error(err),
				)
				log.LogWarn(ctx, "AIProviderFallback",
					fmt.Sprintf("Provider %s gagal: %v", p.Name(), err),
					"next: ...",
				)
				break // keluar dari loop attempt
			}
		}
	}

	return "", "", fmt.Errorf("semua AI provider gagal. error terakhir: %w", lastErr)
}

// ======================== GEMINI ========================

type GeminiProvider struct {
	apiKey string
	label  string
}

func (p *GeminiProvider) Name() string {
	if p.label != "" {
		return p.label
	}
	return "Gemini"
}

func (p *GeminiProvider) Generate(ctx context.Context, userMessage string) (string, error) {
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.0-flash"
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, p.apiKey)
	sysPrompt := getSystemPrompt()
	fullPrompt := sysPrompt + "\n\nPesan dari pengguna: " + userMessage

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": fullPrompt},
				},
			},
		},
	}

	body, statusCode, err := doHTTPPostWithStatus(ctx, url, payload)
	if err != nil {
		return "", fmt.Errorf("Gemini HTTP: %w", err)
	}

	// Jika status 429, parsing error untuk mendapatkan retryDelay dan jenis kuota
	if statusCode == 429 {
		retryAfter, isDaily, parseErr := parseGeminiError(body)
		if parseErr != nil {
			// Jika gagal parsing, tetap buat RateLimitError dengan default 60 detik
			return "", RateLimitError{
				RetryAfter: 60,
				IsDaily:    false,
				Msg:        fmt.Sprintf("Gemini HTTP 429 (parse error): %s", string(body)),
			}
		}
		return "", RateLimitError{
			RetryAfter: retryAfter,
			IsDaily:    isDaily,
			Msg:        fmt.Sprintf("Gemini HTTP 429: %s", string(body)),
		}
	}

	// Status lainnya (400, 500, dll.) kita treat sebagai error biasa
	if statusCode >= 400 {
		return "", fmt.Errorf("Gemini HTTP %d: %s", statusCode, string(body))
	}

	// Parse response sukses
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("Gemini parse JSON: %w, body: %s", err, string(body))
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("Gemini: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini: response kosong")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// parseGeminiError membaca body error HTTP 429 dari Gemini dan mengembalikan:
// - retryAfter dalam detik (0 jika tidak ada)
// - isDaily true jika kuota harian habis
// - error jika parsing gagal
func parseGeminiError(body []byte) (retryAfter int, isDaily bool, err error) {
	var errResp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Type       string `json:"@type"`
				RetryDelay string `json:"retryDelay,omitempty"` // misal "14s"
				Violations []struct {
					QuotaMetric string `json:"quotaMetric"`
					QuotaId     string `json:"quotaId"`
				} `json:"violations"`
			} `json:"details"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err != nil {
		return 0, false, fmt.Errorf("unmarshal error response: %w", err)
	}

	// Cari retryDelay di detail
	for _, detail := range errResp.Error.Details {
		if detail.RetryDelay != "" {
			if d, err := time.ParseDuration(detail.RetryDelay); err == nil {
				retryAfter = int(d.Seconds())
				break
			}
		}
	}

	// Cek apakah ada violation dengan quotaId mengandung "PerDay"
	for _, detail := range errResp.Error.Details {
		for _, v := range detail.Violations {
			if strings.Contains(v.QuotaId, "PerDay") {
				isDaily = true
				break
			}
		}
	}

	// Default ke 60 detik jika tidak ditemukan
	if retryAfter <= 0 {
		retryAfter = 60
	}

	return retryAfter, isDaily, nil
}

// ======================== OPENROUTER ========================

type OpenRouterProvider struct {
	apiKey string
}

func (p *OpenRouterProvider) Name() string { return "OpenRouter" }

func (p *OpenRouterProvider) Generate(ctx context.Context, userMessage string) (string, error) {
	const model = "openai/gpt-4o"
	url := "https://openrouter.ai/api/v1/chat/completions"
	sysPrompt := getSystemPrompt()

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": userMessage},
		},
		"max_tokens":  300,
		"temperature": 0.8,
		"stream":      false,
	}

	body, statusCode, err := doHTTPPostWithStatusAndKey(ctx, url, payload, "Authorization", "Bearer "+p.apiKey)
	if err != nil {
		return "", fmt.Errorf("OpenRouter HTTP: %w", err)
	}
	if statusCode >= 400 {
		return "", fmt.Errorf("OpenRouter HTTP %d: %s", statusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("OpenRouter parse JSON: %w, body: %s", err, string(body))
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("OpenRouter: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("OpenRouter: tidak ada choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// ======================== GROQ ========================

type GroqProvider struct {
	apiKey string
}

func (p *GroqProvider) Name() string { return "Groq" }

func (p *GroqProvider) Generate(ctx context.Context, userMessage string) (string, error) {
	const model = "llama-3.1-8b-instant"
	url := "https://api.groq.com/openai/v1/chat/completions"
	sysPrompt := getSystemPrompt()

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": userMessage},
		},
		"max_tokens":  300,
		"temperature": 0.8,
		"stream":      false,
	}

	body, statusCode, err := doHTTPPostWithStatusAndKey(ctx, url, payload, "Authorization", "Bearer "+p.apiKey)
	if err != nil {
		return "", fmt.Errorf("Groq HTTP: %w", err)
	}
	if statusCode >= 400 {
		return "", fmt.Errorf("Groq HTTP %d: %s", statusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("Groq parse JSON: %w, body: %s", err, string(body))
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("Groq: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("Groq: tidak ada choices")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// ======================== SYSTEM PROMPT ========================

func getSystemPrompt() string {
	return `
Kamu adalah Asisten AI yang ceria, ramah, dan profesional untuk bot Telegram. Kamu juga berjiwa Wibu dan sangat suka anime.

Tugasmu adalah membalas pesan masuk dari pengguna, terutama saat mereka menyapa atau mengirim pertanyaan singkat.

Aturan utama yang harus kamu patuhi:
1. Gunakan Bahasa Indonesia yang santun, alami, dan hangat. Jangan terdengar seperti robot kaku.
2. Balasan harus SINGKAT, maksimal 60 kata.
3. Jangan pernah mengulang kalimat yang sama persis. Buatlah variasi jawaban secara acak. Contohnya: "Eh, halo! Silakan tunggu admin online, ya :3", atau jika ada yang bertanya "beb ngapain?", kamu bisa membalas "Duh, beb lagi offline nih. Ini pesan otomatis dari bot!". Jangan lupa untuk selalu mengingatkan bahwa kamu adalah bot dan pesan akan dibalas saat admin online.
4. JANGAN mengajukan pertanyaan balik yang rumit atau membutuhkan jawaban panjang, karena ini pesan otomatis dan pengguna mungkin hanya ingin menyapa.
5. Jangan memberikan informasi spesifik tentang lokasi, jam buka, atau nomor admin, karena kamu hanya asisten otomatis.
6. Jika pesan pengguna berisi pertanyaan teknis, rumit, atau meminta bantuan khusus, balas dengan ramah bahwa pesan akan diteruskan ke admin dan akan direspon segera.
7. Jangan membalas dengan kata-kata negatif, kasar, atau defensif. Selalu jaga nada bicara yang menyenangkan.
8. Kamu boleh menambahkan emoji dan karakter lucu seperti :3, -_-, '_', atau :v.

Tujuan utama kamu adalah membuat pengguna merasa dihargai dan diperhatikan, serta memberikan kesan pertama yang baik, meskipun hanya dengan balasan singkat.
`
}

// ======================== HTTP HELPERS (DENGAN STATUS CODE) ========================

// doHTTPPostWithStatus mengembalikan body dan status code
func doHTTPPostWithStatus(ctx context.Context, url string, payload interface{}) ([]byte, int, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encode JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, fmt.Errorf("buat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("baca response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// doHTTPPostWithStatusAndKey untuk provider yang pakai header Authorization
func doHTTPPostWithStatusAndKey(ctx context.Context, url string, payload interface{}, header, headerValue string) ([]byte, int, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encode JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, fmt.Errorf("buat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, headerValue)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("baca response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// Fungsi lama masih dipertahankan untuk kompatibilitas (jika ada yang pakai)
func doHTTPPost(ctx context.Context, url string, payload interface{}) ([]byte, error) {
	body, _, err := doHTTPPostWithStatus(ctx, url, payload)
	return body, err
}

func doHTTPPostWithKey(ctx context.Context, url string, payload interface{}, header, headerValue string) ([]byte, error) {
	body, _, err := doHTTPPostWithStatusAndKey(ctx, url, payload, header, headerValue)
	return body, err
}

