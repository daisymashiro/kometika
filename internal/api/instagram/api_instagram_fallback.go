package instagram

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
)

var igBreaker = api.NewCircuitBreaker(3, 5*time.Minute)

func FetchInstagramDataWithFallback(ctx context.Context, instaURL string) (*UniversalInstagramData, error) {
	logger := zap.L()

	apis := []struct {
		name string
		fn   func(string) (*UniversalInstagramData, error)
	}{
		{"FASTDL", FetchFastdlApp},
		{"FastVidioSave", FetchFastVidioSave},
		{"DanzyAPI", FetchInstagramFromDanzy},
		{"Download Gram ORG", FetchInstagramFromDownloadGram},
		{"Downr Downloader", FetchDownr},
		{"Siputzx_fastdl", FetchInstagramFromFastdl},
		{"IgramFetch", FetchIgram}, // igram gagal vidio
		{"FetchIgramStory", FetchIgramStory},
		{"NexRay", FetchInstagramFromNexRay}, //nexray gagal membaca url setelah dalam fetch /?
	}

	var lastErr error
	for _, apiItem := range apis {

		if !igBreaker.CanAttempt(apiItem.name) {
			failures, state, cooldownEnds := igBreaker.GetStatus(apiItem.name)
			sisaWaktu := time.Until(cooldownEnds).Round(time.Second)

			logger.Warn("API Instagram di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Any("state", state),
				zap.Duration("sisa_waktu", sisaWaktu),
			)

			logMsg := fmt.Sprintf("API %s sedang diblokir. State: %d. Coba lagi dalam %s", apiItem.name, state, sisaWaktu)
			log.LogWarn(ctx, "Instagram_CircuitBreaker", logMsg, fmt.Sprintf("failures=%d", failures))
			continue
		}

		logger.Info("Mencoba API Instagram",
			zap.String("api", apiItem.name),
			zap.String("url", instaURL),
		)

		data, err := apiItem.fn(instaURL)

		if err == nil && data != nil && (data.VideoURL != "" || len(data.ImageURLs) > 0) {
			logger.Info("API Instagram berhasil",
				zap.String("api", apiItem.name),
				zap.Bool("has_video", data.VideoURL != ""),
				zap.Int("image_count", len(data.ImageURLs)),
			)
			igBreaker.RecordSuccess(apiItem.name)
			return data, nil
		}

		if err == nil {
			err = fmt.Errorf("API %s returned no valid content (video empty and no images)", apiItem.name)
		}
		lastErr = err

		igBreaker.RecordFailure(apiItem.name)

		logger.Warn("API Instagram gagal",
			zap.String("api", apiItem.name),
			zap.Error(err),
		)

		log.LogError(ctx, "Instagram_API_Gagal", err, "api="+apiItem.name, "url="+instaURL)
	}

	log.LogError(ctx, "Instagram_Semua_API_Gagal", lastErr, "url="+instaURL)
	return nil, fmt.Errorf("all APIs failed: last error: %w", lastErr)
}

func ResetCircuitBreaker(apiName string) {
	igBreaker.Reset(apiName)
}
