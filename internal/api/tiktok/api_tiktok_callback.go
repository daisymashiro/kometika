package tiktok

import (
	"context"
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
)

var (
	tiktokBreaker = api.NewCircuitBreaker(3, 5*time.Minute)
)

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
		if !tiktokBreaker.CanAttempt(apiItem.name) {
			failures, state, cooldownEnds := tiktokBreaker.GetStatus(apiItem.name)

			sisaWaktu := time.Until(cooldownEnds).Round(time.Second)

			logger.Warn("API TikTok di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Any("state", state),
				zap.Duration("sisa_waktu", sisaWaktu),
			)

			logMsg := fmt.Sprintf("API %s sedang diblokir. State: %d. Coba lagi dalam %s", apiItem.name, state, sisaWaktu)
			log.LogWarn(ctx, "TikTok_CircuitBreaker", logMsg, fmt.Sprintf("failures=%d", failures))
			continue
		}

		logger.Info("Mencoba API TikTok", zap.String("api", apiItem.name), zap.String("url", tiktokURL))
		data, err := apiItem.fn(tiktokURL)

		if err == nil && data != nil {
			if data.IsAlbum && len(data.ImageURLs) > 0 {
				logger.Info("API TikTok berhasil (Album)", zap.String("api", apiItem.name))
				tiktokBreaker.RecordSuccess(apiItem.name)
				return data, nil, nil
			}

			if data.VideoURL != "" {
				stream, _, streamErr := api.GetVideoStream(ctx, data.VideoURL)
				if streamErr == nil {
					logger.Info("API TikTok berhasil & Stream Terbuka", zap.String("api", apiItem.name))
					tiktokBreaker.RecordSuccess(apiItem.name)
					return data, stream, nil
				}

				lastErr = streamErr
				tiktokBreaker.RecordFailure(apiItem.name)

				log.LogError(ctx, "TikTok_StreamGagal", streamErr, "api="+apiItem.name, "video_url="+data.VideoURL)

				if onFail != nil {
					onFail(apiItem.name, streamErr)
				}
				continue
			}
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("data tidak lengkap")
		}

		tiktokBreaker.RecordFailure(apiItem.name)

		log.LogError(ctx, "TikTok_API_Gagal", lastErr, "api="+apiItem.name, "url="+tiktokURL)

		if onFail != nil {
			onFail(apiItem.name, lastErr)
		}
	}

	log.LogError(ctx, "TikTok_Semua_API_Gagal", lastErr, "url="+tiktokURL)
	return nil, nil, fmt.Errorf("semua API TikTok gagal: %v", lastErr)
}

func ResetCircuitBreaker(apiName string) {
	tiktokBreaker.Reset(apiName)
}
