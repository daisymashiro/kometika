package tiktok

import (
	"context"
	"fmt"
	"io"

	"go.uber.org/zap"
	"mybot/internal/api" // [TAMBAH] Pastikan import package api kamu
)

// [UBAH] Signature fungsi sekarang mengembalikan stream (io.ReadCloser) dan menerima onFail
func FetchTikTokDataWithFallback(ctx context.Context, logger *zap.Logger, tiktokURL string, onFail func(apiName string, err error)) (*UniversalTikTokData, io.ReadCloser, error) {
	apis := []struct {
		name string
		fn   func(string) (*UniversalTikTokData, error)
	}{
		{"MusicallDown", FetchMusicallDown},
		{"Ssstik", ScrapeTikTokUniversal}, // Sesuaikan jika namamu beda
		{"TikWM", FetchTikWM},
		{"Puruboy", FetchPuruboyTikTok},
		{"NexRay", FetchNexRay},
	}

	var lastErr error
	for _, apiItem := range apis {
		logger.Info("Mencoba API TikTok",
			zap.String("api", apiItem.name),
			zap.String("url", tiktokURL),
		)
		data, err := apiItem.fn(tiktokURL)

		if err == nil && data != nil {
			// Jika Album (Foto), langsung anggap sukses karena tidak butuh stream video
			if data.IsAlbum && len(data.ImageURLs) > 0 {
				logger.Info("API TikTok berhasil (Album)", zap.String("api", apiItem.name))
				return data, nil, nil
			}

			// [TAMBAH] Jika Video, kita tes buka stream-nya di sini
			if data.VideoURL != "" {
				stream, _, streamErr := api.GetVideoStream(ctx, data.VideoURL)
				if streamErr == nil {
					logger.Info("API TikTok berhasil & Stream Terbuka", zap.String("api", apiItem.name))
					return data, stream, nil
				}

				// Jika CDN memutus koneksi (error stream), catat dan lempar ke callback
				lastErr = streamErr
				if onFail != nil {
					onFail(apiItem.name, streamErr)
				}
				continue // Lanjut coba API berikutnya
			}
		}

		// Jika gagal scraping data
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("data tidak lengkap")
		}

		// [TAMBAH] Lempar error ke callback
		if onFail != nil {
			onFail(apiItem.name, lastErr)
		}
	}
	return nil, nil, fmt.Errorf("semua API TikTok gagal: %v", lastErr)
}
