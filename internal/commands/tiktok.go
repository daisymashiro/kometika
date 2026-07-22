package commands

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/tiktok"
	"mybot/internal/cache"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"
)

func HandleTikTok(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	if !strings.Contains(url, "tiktok.com") && !strings.Contains(url, "vm.tiktok.com") {
		return nil
	}

	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("tiktok") {
		logger.Info("Fitur TikTok dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading
	return WithLoading(ctx, client, msg, entities, "TikTok", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processTikTok(ctx, client, lc, url, logger)
	})
}

func processTikTok(ctx context.Context, client *tg.Client, lc *LoadingContext, url string, logger *zap.Logger) error {
	mediaSender := media.NewMediaSender(client)

	data, stream, err := tiktok.FetchTikTokDataWithFallback(ctx, logger, url, func(apiName string, failErr error) {
		logger.Warn("API Gagal (Otomatis Skip)", zap.String("api", apiName), zap.Error(failErr))
		log.LogWarn(ctx, "TikTokAPI_"+apiName, failErr.Error(), "url="+url)
	})

	if err != nil {
		logger.Warn("Semua API gagal", zap.Error(err))
		log.LogError(ctx, "TikTok.FetchData", err, "url="+url)
		
		// Edit pesan loading menjadi error
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengambil data dari TikTok.", logger)
		}
		return nil
	}

	if stream != nil {
		defer stream.Close()
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	// Album handler
	if data.IsAlbum && len(data.ImageURLs) > 0 {
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh album...", logger)
		}
		
		err = kirimAlbumStream(ctx, client, lc.Peer, title, data.ImageURLs, lc.ReplyTo, logger)
		if err != nil {
			logger.Error("Gagal kirim album", zap.Error(err))
			log.LogError(ctx, "TikTok.SendAlbum", err, "url="+url)
			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim foto.", logger)
			}
		}
		return nil
	}

	if data.VideoURL == "" {
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Tidak ditemukan URL video.", logger)
		}
		return nil
	}

	// Simpan audio ke cache
	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Tiktok Music")
		logger.Info("Audio disimpan ke cache", zap.String("video_id", data.ID))
	}

	// Update pesan loading
	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh video...", logger)
	}

	// Deteksi tipe konten otomatis
	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err != nil {
		logger.Warn("Gagal deteksi tipe konten, fallback ke video/mp4", zap.Error(err))
		info = api.ContentTypeInfo{
			MimeType:  "video/mp4",
			Category:  api.ContentVideo,
			Extension: ".mp4",
		}
		fullStream = stream
	}

	// Persiapan thumbnail
	var thumbFile tg.InputFileClass
	if data.CoverURL != "" {
		thumbBytes, err := api.GetThumbnail(ctx, data.CoverURL)
		if err == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(client).WithThreads(1)
			thumbFile, _ = up.FromBytes(ctx, "thumb.jpg", thumbBytes)
		}
		if thumbFile == nil {
			logger.Warn("Gagal upload thumbnail, video akan dikirim tanpa thumbnail")
		}
	}

	var replyMarkup tg.ReplyMarkupClass
	if data.AudioURL != "" && data.ID != "" {
		replyMarkup = &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{Text: "Unduh Audio (MP3)", Data: fmt.Appendf([]byte{}, "mp3_%s", data.ID)},
				}},
			},
		}
	}

	caption := fmt.Sprintf("%s\n\n@Kometika_bot", title)
	filename := fmt.Sprintf("%s.mp4", data.ID)

	// Kirim dengan dynamic stream
	videoMsgUpdates, err := mediaSender.SendDynamicStream(
		ctx, lc.Peer, fullStream, info, filename, caption, replyMarkup, lc.ReplyTo, thumbFile,
	)
	if err != nil {
		logger.Error("Gagal kirim video", zap.Error(err))
		log.LogError(ctx, "TikTok.SendVideo", err, "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim video.", logger)
		}
		return nil
	}

	var videoMsgID int
	if videoMsgUpdates != nil {
		if id, err := media.ExtractMessageID(videoMsgUpdates); err == nil {
			videoMsgID = id
		}
	}

	logger.Info("Video TikTok berhasil dikirim", zap.String("video_id", data.ID), zap.Int("msg_id", videoMsgID))

	// Cleanup tombol audio setelah 2 menit dengan context-aware goroutine
	if videoMsgID != 0 && data.AudioURL != "" && data.ID != "" {
		go scheduleAudioButtonCleanup(client, lc.Peer, videoMsgID, data.ID, logger)
	} else if data.AudioURL != "" && data.ID != "" {
		go scheduleAudioCacheCleanup(data.ID)
	}

	return nil
}

// scheduleAudioButtonCleanup dengan context-aware cleanup
func scheduleAudioButtonCleanup(client *tg.Client, peer tg.InputPeerClass, msgID int, videoID string, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	select {
	case <-time.After(2 * time.Minute):
		// Timeout cleanup selesai, edit tombol
		editCtx, editCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer editCancel()

		for {
			_, err := client.MessagesEditMessage(editCtx, &tg.MessagesEditMessageRequest{
				Peer:        peer,
				ID:          msgID,
				ReplyMarkup: &tg.ReplyKeyboardHide{},
			})
			if err != nil {
				if d, ok := tgerr.AsFloodWait(err); ok {
					logger.Warn("FloodWait saat hapus tombol audio", zap.Duration("wait", d))
					select {
					case <-time.After(d + time.Second):
						continue
					case <-editCtx.Done():
						logger.Warn("Gagal menghapus tombol audio (timeout)", zap.Int("msg_id", msgID))
						cache.DeleteAudio(videoID)
						return
					}
				}
				logger.Warn("Gagal menghapus tombol audio", zap.Int("msg_id", msgID), zap.Error(err))
				cache.DeleteAudio(videoID)
				return
			}
			break
		}

		cache.DeleteAudio(videoID)
		logger.Info("Tombol audio dihapus otomatis", zap.Int("msg_id", msgID))

	case <-ctx.Done():
		// Context dibatalkan
		cache.DeleteAudio(videoID)
	}
}

// scheduleAudioCacheCleanup dengan context-aware cleanup
func scheduleAudioCacheCleanup(videoID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	select {
	case <-time.After(2 * time.Minute):
		cache.DeleteAudio(videoID)
	case <-ctx.Done():
		cache.DeleteAudio(videoID)
	}
}

// kirimAlbumStream dengan support replyTo untuk grup
func kirimAlbumStream(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, title string, imageURLs []string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	if len(imageURLs) == 0 {
		return nil
	}

	mediaSender := media.NewMediaSender(client)

	const maxAlbumSize = 10
	batches := splitIntoBatches(imageURLs, maxAlbumSize)
	httpClient := &http.Client{Timeout: 40 * time.Second}

	for batchIdx, batch := range batches {
		readers := make([]io.Reader, 0, len(batch))
		filenames := make([]string, 0, len(batch))
		captions := make([]string, 0, len(batch))

		for i, imgURL := range batch {
			req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
			if err != nil {
				logger.Warn("Gagal buat request", zap.Error(err))
				continue
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				logger.Warn("Gagal download gambar", zap.String("url", imgURL), zap.Error(err))
				continue
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				logger.Warn("Gagal baca body gambar", zap.Error(err))
				continue
			}

			// Convert WebP ke JPEG
			if len(body) >= 12 && string(body[0:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
				img, _, err := image.Decode(bytes.NewReader(body))
				if err != nil {
					logger.Warn("Gagal decode WebP", zap.Error(err))
					continue
				}
				var buf bytes.Buffer
				if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
					logger.Warn("Gagal encode WebP ke JPEG", zap.Error(err))
					continue
				}
				body = buf.Bytes()
			}

			readers = append(readers, bytes.NewReader(body))
			filenames = append(filenames, fmt.Sprintf("tiktok_%d_%d.jpg", batchIdx, i))
			if batchIdx == 0 && i == 0 {
				captions = append(captions, fmt.Sprintf("🖼️ %s\n\n@Kometika_bot", title))
			} else {
				captions = append(captions, "")
			}
		}

		if len(readers) == 0 {
			logger.Warn("Tidak ada gambar untuk batch", zap.Int("batch", batchIdx))
			continue
		}

		if err := mediaSender.SendPhotoAlbumStream(ctx, peer, readers, filenames, captions, replyTo); err != nil {
			logger.Error("Gagal kirim album batch", zap.Int("batch", batchIdx), zap.Error(err))
		}

		if batchIdx < len(batches)-1 {
			time.Sleep(1 * time.Second)
		}
	}
	return nil
}

func splitIntoBatches(items []string, size int) [][]string {
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := min(i+size, len(items))
		batches = append(batches, items[i:end])
	}
	return batches
}
