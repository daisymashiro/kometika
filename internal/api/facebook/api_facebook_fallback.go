package facebook

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
)

var fbBreaker = api.NewCircuitBreaker(3, 5*time.Minute)

func FetchFacebookWithFallback(ctx context.Context, logger *zap.Logger, videoURL string) (*FacebookUniversalVideoData, error) {
	apis := []struct {
		name string
		fn   func(string) (*FacebookUniversalVideoData, error)
	}{
		{"Get VidFB", FetchFacebookGetVidFB},
		{"FGet Io", FetchFacebookFGet},
		{"FlyDev", FetchFacebookFlyDev},
		{"Facebook Vercel", FetchFacebookVercel},
		{"Siputzx", GetFacebookVideoData},
	}

	var lastErr error
	for _, apiItem := range apis {
		if !fbBreaker.CanAttempt(apiItem.name) {
			failures, state, cooldownEnds := fbBreaker.GetStatus(apiItem.name)
			sisaWaktu := time.Until(cooldownEnds).Round(time.Second)

			logger.Warn("API Facebook di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Any("state", state),
				zap.Duration("sisa_waktu", sisaWaktu),
			)

			logMsg := fmt.Sprintf("API %s sedang diblokir. State: %d. Coba lagi dalam %s", apiItem.name, state, sisaWaktu)
			log.LogWarn(ctx, "Facebook_CircuitBreaker", logMsg, fmt.Sprintf("failures=%d", failures))
			continue
		}

		logger.Info("Mencoba API Facebook",
			zap.String("api", apiItem.name),
			zap.String("url", videoURL),
		)

		data, err := apiItem.fn(videoURL)

		if err == nil && data != nil && data.VidioURL != "" {
			logger.Info("API Facebook berhasil",
				zap.String("api", apiItem.name),
				zap.String("id", data.ID),
				zap.String("title", data.Title),
			)
			fbBreaker.RecordSuccess(apiItem.name)
			return data, nil
		}

		if err == nil {
			err = fmt.Errorf("API %s mengembalikan URL video kosong", apiItem.name)
		}
		lastErr = err

		fbBreaker.RecordFailure(apiItem.name)

		logger.Warn("API Facebook gagal",
			zap.String("api", apiItem.name),
			zap.Error(err),
		)

		log.LogError(ctx, "Facebook_API_Gagal", err, "api="+apiItem.name, "url="+videoURL)
	}

	log.LogError(ctx, "Facebook_Semua_API_Gagal", lastErr, "url="+videoURL)
	return nil, fmt.Errorf("semua API Facebook gagal: %v", lastErr)
}

func ResetCircuitBreaker(apiName string) {
	fbBreaker.Reset(apiName)
}

