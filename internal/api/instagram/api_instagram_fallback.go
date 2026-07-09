package instagram

import (
	"fmt"
	"go.uber.org/zap"
)

func FetchInstagramDataWithFallback(instaURL string) (*UniversalInstagramData, error) {
	logger := zap.L()

	apis := []struct {
		name string
		fn   func(string) (*UniversalInstagramData, error)
	}{

		{"Download Gram ORG", FetchInstagramFromDownloadGram},
		{"Downr Downloader", FetchDownr},
		{"Siputzx_fastdl", FetchInstagramFromFastdl},
		//{"InstagramGraphql", FetchInstagram},
		{"IgramFetch", FetchIgram}, // igram gagal vidio
		{"FetchIgramStory", FetchIgramStory},
		{"NexRay", FetchInstagramFromNexRay}, //nexray gagal membaca url setelah dalam fetch /?
		{"DanzyAPI", FetchInstagramFromDanzy},
		{"FastVidioSave", FetchFastVidioSave},
	}

	var lastErr error
	for _, api := range apis {
		logger.Info("Mencoba API Instagram",
			zap.String("api", api.name),
			zap.String("url", instaURL),
		)

		data, err := api.fn(instaURL)

		// Cek sukses dan memiliki konten
		if err == nil && data != nil && (data.VideoURL != "" || len(data.ImageURLs) > 0) {
			logger.Info("API Instagram berhasil",
				zap.String("api", api.name),
				zap.Bool("has_video", data.VideoURL != ""),
				zap.Int("image_count", len(data.ImageURLs)),
			)
			return data, nil
		}

		// Jika err nil tapi data tidak valid, buat error sendiri
		if err == nil {
			err = fmt.Errorf("API %s returned no valid content (video empty and no images)", api.name)
		}
		lastErr = err
		logger.Warn("API Instagram gagal",
			zap.String("api", api.name),
			zap.Error(err),
		)
	}
	return nil, fmt.Errorf("all APIs failed: last error: %w", lastErr)
}
