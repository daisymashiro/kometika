package facebook

import (
	"fmt"

	"go.uber.org/zap"
)

// FetchFacebookWithFallback mencoba semua scraper secara berurutan hingga mendapatkan video URL yang valid.
func FetchFacebookWithFallback(logger *zap.Logger, videoURL string) (*FacebookUniversalVideoData, error) {
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
	for _, api := range apis {
		logger.Info("Mencoba API Facebook",
			zap.String("api", api.name),
			zap.String("url", videoURL),
		)
		data, err := api.fn(videoURL)
		if err == nil && data != nil && data.VidioURL != "" {
			logger.Info("API Facebook berhasil",
				zap.String("api", api.name),
				zap.String("id", data.ID),
				zap.String("title", data.Title),
			)
			return data, nil
		}
		lastErr = err
		logger.Warn("API Facebook gagal",
			zap.String("api", api.name),
			zap.Error(err),
		)
	}
	return nil, fmt.Errorf("semua API Facebook gagal: %v", lastErr)
}
