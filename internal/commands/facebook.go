package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api/facebook"
	"mybot/internal/cache"
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

	mediaSender := media.NewMediaSender(client)

	// Jika BUKAN private chat -> dialihkan ke group handler khusus
	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		peer, err := GetPeerFromMessage(ctx, client, msg, entities)
		if err != nil || peer == nil {
			return err
		}
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)
		return handleFacebookGroup(ctx, client, peer, url, replyTo, logger)
	}

	// ── PRIVATE CHAT LOGIC ──────────────────────────────────────────────────
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal mendapatkan peer private chat", zap.Error(err))
		log.LogError(ctx, "FacebookGetPeer", err, "url="+url)
		return err
	}

	msgSender := message.NewSender(client)

	// 1. Kirim pesan kemajuan (Loading)
	progressMsg, err := msgSender.To(peer).Text(ctx, "⏳ Memproses Facebook, mohon tunggu...")
	if err != nil {
		logger.Warn("Gagal mengirim pesan progress", zap.Error(err))
	} else {
		progressMsgID, _ := media.ExtractMessageID(progressMsg)
		defer func() {
			if progressMsgID == 0 {
				return
			}
			go func() {
				time.Sleep(1 * time.Second)
				_, _ = client.MessagesDeleteMessages(context.Background(), &tg.MessagesDeleteMessagesRequest{
					Revoke: true,
					ID:     []int{progressMsgID},
				})
			}()
		}()
	}

	logger.Info("Memproses Facebook (Private Chat)", zap.String("url", url))

	// 2. Mengambil data menggunakan sistem fallback
	data, err := facebook.FetchFacebookWithFallback(logger, url)
	if err != nil {
		logger.Warn("Gagal fetch data Facebook", zap.Error(err))
		log.LogError(ctx, "FacebookFetch", err, "url="+url)
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengambil data dari Facebook.")
		return nil
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	if data.VidioURL == "" {
		_, _ = msgSender.To(peer).Text(ctx, "❌ Tidak ditemukan URL video.")
		return nil
	}

	// 3. Simpan Audio ke Cache jika tersedia dari scraper
	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Facebook Music")
		logger.Info("Audio Facebook berhasil disimpan ke cache", zap.String("video_id", data.ID))
	}

	// 4. Menyusun tombol inline MP3 jika audio URL valid
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

	videoMsgUpdates, err := mediaSender.SendSmartMedia(
		ctx, peer, data.VidioURL, data.CoverURL, caption, replyMarkup, nil,
	)
	if err != nil {
		logger.Error("Gagal mengirim video Facebook", zap.Error(err))
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengirim video.")
		return nil
	}

	// 6. Siklus pembersihan tombol inline otomatis setelah 2 menit
	if replyMarkup != nil && videoMsgUpdates != nil {
		videoMsgID, err := media.ExtractMessageID(videoMsgUpdates)
		if err == nil && videoMsgID != 0 {
			go func(peerCopy tg.InputPeerClass, msgID int) {
				time.Sleep(2 * time.Minute)
				_, _ = client.MessagesEditMessage(context.Background(), &tg.MessagesEditMessageRequest{
					Peer:        peerCopy,
					ID:          msgID,
					ReplyMarkup: &tg.ReplyKeyboardHide{},
				})
				cache.DeleteAudio(data.ID)
				logger.Info("Tombol audio Facebook dihapus otomatis", zap.Int("msg_id", msgID))
			}(peer, videoMsgID)
		} else {
			go func(videoID string) {
				time.Sleep(2 * time.Minute)
				cache.DeleteAudio(videoID)
			}(data.ID)
		}
	}

	logger.Info("Video Facebook sukses terkirim (Private)", zap.String("video_id", data.ID))
	return nil
}

// handleFacebookGroup menangani pemrosesan tautan Facebook di Group / Supergroup.
func handleFacebookGroup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	logger.Info("Memproses Facebook (Group Chat)", zap.String("url", url))

	mediaSender := media.NewMediaSender(client)

	data, err := facebook.FetchFacebookWithFallback(logger, url)
	if err != nil {
		logger.Warn("Gagal fetch data Facebook grup", zap.Error(err))
		sendGroupText(ctx, client, peer, "❌ Gagal mengambil data dari Facebook.", replyTo)
		return nil
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	if data.VidioURL == "" {
		sendGroupText(ctx, client, peer, "❌ Tidak ditemukan URL video.", replyTo)
		return nil
	}

	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Facebook Music")
		logger.Info("Audio Facebook disimpan ke cache (Group)", zap.String("video_id", data.ID))
	}

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

	// 🚀 MENGGUNAKAN SMART PIPELINE BARU DI GROUP (Dengan parameter replyTo)
	videoMsgUpdates, err := mediaSender.SendSmartMedia(
		ctx, peer, data.VidioURL, data.CoverURL, caption, replyMarkup, replyTo,
	)
	if err != nil {
		logger.Error("Gagal mengirim video Facebook di grup", zap.Error(err))
		sendGroupText(ctx, client, peer, "❌ Gagal mengirim video.", replyTo)
		return nil
	}

	if replyMarkup != nil && videoMsgUpdates != nil {
		videoMsgID, err := media.ExtractMessageID(videoMsgUpdates)
		if err == nil && videoMsgID != 0 {
			go func(peerCopy tg.InputPeerClass, msgID int) {
				time.Sleep(2 * time.Minute)
				_, _ = client.MessagesEditMessage(context.Background(), &tg.MessagesEditMessageRequest{
					Peer:        peerCopy,
					ID:          msgID,
					ReplyMarkup: &tg.ReplyKeyboardHide{},
				})
				cache.DeleteAudio(data.ID)
			}(peer, videoMsgID)
		} else {
			go func(videoID string) {
				time.Sleep(2 * time.Minute)
				cache.DeleteAudio(videoID)
			}(data.ID)
		}
	}

	logger.Info("Video Facebook berhasil terkirim ke grup", zap.String("video_id", data.ID))
	return nil
}
