package tiktok

import (
	"context"
	"fmt"
	"io"

	"go.uber.org/zap"
	"mybot/internal/api"
)

var (
	// Circuit breaker global untuk TikTok APIs
	tiktokBreaker = api.NewCircuitBreaker(3, 5*60) // 3 kegagalan, cooldown 5 menit
)

// FetchTikTokDataWithFallback menggunakan circuit breaker untuk skip API yang gagal beruntun
func FetchTikTokDataWithFallback(ctx context.Context, logger *zap.Logger, tiktokURL string, onFail func(apiName string, err error)) (*UniversalTikTokData, io.ReadCloser, error) {
	apis := []struct {
		name string
		fn   func(string) (*UniversalTikTokData, error)
	}{
		{"MusicallDown", FetchMusicallDown},
		{"Ssstik", ScrapeTikTokUniversal},
		{"TikWM", FetchTikWM},
		{"Puruboy", FetchPuruboyTikTok},
		{"NexRay", FetchNexRay},
	}

	var lastErr error
	for _, apiItem := range apis {
		// Cek circuit breaker
		if !tiktokBreaker.CanAttempt(apiItem.name) {
			failures, inCooldown := tiktokBreaker.GetStatus(apiItem.name)
			logger.Warn("API TikTok di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Bool("in_cooldown", inCooldown),
			)
			continue
		}

		logger.Info("Mencoba API TikTok",
			zap.String("api", apiItem.name),
			zap.String("url", tiktokURL),
		)
		data, err := apiItem.fn(tiktokURL)

		if err == nil && data != nil {
			// Jika Album (Foto), langsung anggap sukses
			if data.IsAlbum && len(data.ImageURLs) > 0 {
				logger.Info("API TikTok berhasil (Album)", zap.String("api", apiItem.name))
				tiktokBreaker.RecordSuccess(apiItem.name)
				return data, nil, nil
			}

			// Jika Video, tes buka stream
			if data.VideoURL != "" {
				stream, _, streamErr := api.GetVideoStream(ctx, data.VideoURL)
				if streamErr == nil {
					logger.Info("API TikTok berhasil & Stream Terbuka", zap.String("api", apiItem.name))
					tiktokBreaker.RecordSuccess(apiItem.name)
					return data, stream, nil
				}

				// Jika CDN memutus koneksi (error stream)
				lastErr = streamErr
				tiktokBreaker.RecordFailure(apiItem.name)
				if onFail != nil {
					onFail(apiItem.name, streamErr)
				}
				continue
			}
		}

		// Jika gagal scraping data
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("data tidak lengkap")
		}

		tiktokBreaker.RecordFailure(apiItem.name)
		if onFail != nil {
			onFail(apiItem.name, lastErr)
		}
	}
	return nil, nil, fmt.Errorf("semua API TikTok gagal: %v", lastErr)
}

// ResetCircuitBreaker mereset circuit breaker untuk API tertentu (untuk debugging)
func ResetCircuitBreaker(apiName string) {
	tiktokBreaker.Reset(apiName)
}
