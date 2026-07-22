package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/api/facebook"
	"mybot/internal/cache"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"
)

// HandleFacebook adalah entry point utama untuk tautan Facebook.
func HandleFacebook(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	lowerURL := strings.ToLower(url)

	if !strings.Contains(lowerURL, "facebook.com") &&
		!strings.Contains(lowerURL, "fb.watch") &&
		!strings.Contains(lowerURL, "fb.gg") &&
		!strings.Contains(lowerURL, "fb.com") {
		return nil
	}

	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("facebook") {
		logger.Info("Fitur Facebook dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading
	return WithLoading(ctx, client, msg, entities, "Facebook", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processFacebook(ctx, client, lc, url, logger)
	})
}

func processFacebook(ctx context.Context, client *tg.Client, lc *LoadingContext, url string, logger *zap.Logger) error {
	logger.Info("Memproses Facebook", zap.String("url", url))

	// Mengambil data menggunakan sistem fallback
	data, err := facebook.FetchFacebookWithFallback(logger, url)
	if err != nil {
		logger.Warn("Gagal fetch data Facebook", zap.Error(err))
		log.LogError(ctx, "FacebookFetch", err, "url="+url)
		
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengambil data dari Facebook.", logger)
		}
		return nil
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	if data.VidioURL == "" {
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Tidak ditemukan URL video.", logger)
		}
		return nil
	}

	// Simpan Audio ke Cache jika tersedia dari scraper
	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Facebook Music")
		logger.Info("Audio Facebook berhasil disimpan ke cache", zap.String("video_id", data.ID))
	}

	// Update pesan loading
	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh video...", logger)
	}

	// Menyusun tombol inline MP3 jika audio URL valid
	var replyMarkup tg.ReplyMarkupClass
	if data.AudioURL != "" && data.ID != "" {
		replyMarkup = &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{
						Text: "🎵 Unduh Audio (MP3)",
						Data: []byte(fmt.Sprintf("mp3_%s", data.ID)),
					},
				}},
			},
		}
	}

	caption := fmt.Sprintf("📘 %s\n\n@Kometika_bot", title)

	mediaSender := media.NewMediaSender(client)
	videoMsgUpdates, err := mediaSender.SendSmartMedia(
		ctx, lc.Peer, data.VidioURL, data.CoverURL, caption, replyMarkup, lc.ReplyTo,
	)
	if err != nil {
		logger.Error("Gagal mengirim video Facebook", zap.Error(err))
		log.LogError(ctx, "Facebook.SendVideo", err, "url="+url)
		
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim video.", logger)
		}
		return nil
	}

	// Siklus pembersihan tombol inline otomatis setelah 2 menit dengan context-aware
	if replyMarkup != nil && videoMsgUpdates != nil {
		videoMsgID, err := media.ExtractMessageID(videoMsgUpdates)
		if err == nil && videoMsgID != 0 {
			go scheduleFacebookAudioButtonCleanup(client, lc.Peer, videoMsgID, data.ID, logger)
		} else {
			go scheduleAudioCacheCleanup(data.ID)
		}
	}

	logger.Info("Video Facebook sukses terkirim", zap.String("video_id", data.ID))
	return nil
}

// scheduleFacebookAudioButtonCleanup dengan context-aware cleanup
func scheduleFacebookAudioButtonCleanup(client *tg.Client, peer tg.InputPeerClass, msgID int, videoID string, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	select {
	case <-time.After(2 * time.Minute):
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
					logger.Warn("FloodWait saat hapus tombol audio Facebook", zap.Duration("wait", d))
					select {
					case <-time.After(d + time.Second):
						continue
					case <-editCtx.Done():
						logger.Warn("Gagal menghapus tombol audio Facebook (timeout)", zap.Int("msg_id", msgID))
						cache.DeleteAudio(videoID)
						return
					}
				}
				logger.Warn("Gagal menghapus tombol audio Facebook", zap.Int("msg_id", msgID), zap.Error(err))
				cache.DeleteAudio(videoID)
				return
			}
			break
		}

		cache.DeleteAudio(videoID)
		logger.Info("Tombol audio Facebook dihapus otomatis", zap.Int("msg_id", msgID))

	case <-ctx.Done():
		cache.DeleteAudio(videoID)
	}
}
