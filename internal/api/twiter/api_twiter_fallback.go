package twiter

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// TwitterUniversalData adalah struktur standar untuk hasil scraping Twitter
// Support single video, single photo, atau multiple photos (album)
type TwitterUniversalData struct {
	ID          string   `json:"id"`          // ID unik dari konten
	Title       string   `json:"title"`       // Judul konten
	DownloadURL string   `json:"downloadUrl"` // Link unduhan (video atau foto pertama)
	CoverURL    string   `json:"coverUrl"`    // URL thumbnail/cover
	MediaType   string   `json:"mediaType"`   // "video", "photo", "album"
	VideoURL    string   `json:"videoUrl"`    // Hanya jika video (untuk clarity)
	ImageURLs   []string `json:"imageUrls"`   // Hanya jika photo/album (bisa multiple)
	IsAlbum     bool     `json:"isAlbum"`     // True jika lebih dari 1 foto
}

// log adalah logger global untuk package twitter
var log *zap.Logger

// SetLogger digunakan oleh main untuk menginjeksi logger ke package ini
func SetLogger(l *zap.Logger) {
	log = l
}

// helper logging internal (aman jika log nil)
func logInfo(msg string, fields ...zap.Field) {
	if log != nil {
		log.Info(msg, fields...)
	}
}

func logWarn(msg string, fields ...zap.Field) {
	if log != nil {
		log.Warn(msg, fields...)
	}
}

func logError(msg string, fields ...zap.Field) {
	if log != nil {
		log.Error(msg, fields...)
	}
}

// FetchTwitterWithFallback mencoba semua scraper Twitter secara berurutan hingga mendapatkan data yang valid
func FetchTwitterWithFallback(ctx context.Context, twitterURL string) (*TwitterUniversalData, error) {
	logInfo("Memulai proses scraping Twitter dengan fallback",
		zap.String("url", twitterURL),
	)

	// Daftar API dalam urutan prioritas
	apis := []struct {
		name string
		fn   func(context.Context, string) (*TwitterUniversalData, error)
	}{
		{"FXTwitter API", FetchTweetData},
		{"Siputzx API", ScrapeTwitter},
		// Tambahkan scraper lain di sini sesuai kebutuhan
		// {"API Name", FetchTwitterFromAPI},
	}

	var lastErr error
	for _, api := range apis {
		logInfo("Mencoba API Twitter",
			zap.String("api", api.name),
			zap.String("url", twitterURL),
		)

		data, err := api.fn(ctx, twitterURL)

		// Cek sukses dan memiliki data yang valid
		if err == nil && data != nil && (data.VideoURL != "" || len(data.ImageURLs) > 0) {
			logInfo("API Twitter berhasil",
				zap.String("api", api.name),
				zap.String("id", data.ID),
				zap.String("title", data.Title),
				zap.String("media_type", data.MediaType),
				zap.Int("image_count", len(data.ImageURLs)),
			)
			return data, nil
		}

		// Jika err nil tapi data tidak valid, buat error sendiri
		if err == nil {
			err = fmt.Errorf("API %s returned no valid content (no video or images)", api.name)
		}
		lastErr = err
		logWarn("API Twitter gagal",
			zap.String("api", api.name),
			zap.Error(err),
		)
	}

	logError("Semua API Twitter gagal",
		zap.String("url", twitterURL),
		zap.Error(lastErr),
	)

	return nil, fmt.Errorf("semua API Twitter gagal: %w", lastErr)
}
