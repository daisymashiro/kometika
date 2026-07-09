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

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/tiktok"
	"mybot/internal/cache"
	"mybot/internal/log"
	"mybot/internal/media"
)

func HandleTikTok(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	if !strings.Contains(url, "tiktok.com") && !strings.Contains(url, "vm.tiktok.com") {
		return nil
	}

	mediaSender := media.NewMediaSender(client)

	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		peer, err := GetPeerFromMessage(ctx, client, msg, entities)
		if err != nil || peer == nil {
			return err
		}
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)
		return handleTikTokGroup(ctx, client, peer, url, replyTo, logger)
	}

	// ── Private chat ──
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil {
		logger.Error("Gagal dapatkan peer", zap.Error(err))
		return err
	}

	msgSender := message.NewSender(client)

	progressMsg, err := msgSender.To(peer).Text(ctx, "⏳ Memproses TikTok, mohon tunggu...")
	if err != nil {
		logger.Warn("Gagal kirim pesan progress", zap.Error(err))
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

	data, stream, err := tiktok.FetchTikTokDataWithFallback(ctx, logger, url, func(apiName string, failErr error) {
		logger.Warn("API Gagal (Otomatis Skip)", zap.String("api", apiName), zap.Error(failErr))
		log.LogWarn(ctx, "TikTokAPI_"+apiName, failErr.Error(), "url="+url)
	})

	if err != nil {
		logger.Warn("Semua API gagal", zap.Error(err))
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengambil data dari TikTok.")
		return nil
	}

	if stream != nil {
		defer stream.Close()
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	if data.IsAlbum && len(data.ImageURLs) > 0 {
		err = kirimAlbumStream(ctx, client, peer, title, data.ImageURLs, logger)
		if err != nil {
			logger.Error("Gagal kirim album", zap.Error(err))
			_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengirim foto.")
		}
		return nil
	}

	if data.VideoURL == "" {
		_, _ = msgSender.To(peer).Text(ctx, "❌ Tidak ditemukan URL video.")
		return nil
	}

	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Tiktok Music")
		logger.Info("Audio disimpan ke cache", zap.String("video_id", data.ID))
	}

	// ─── DETEKSI TIPE KONTEN OTOMATIS ──────────────────────────────────────
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

	// ─── PERSIAPAN THUMBNAIL ──────────────────────────────────────────────
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

	// ─── KIRIM DENGAN DYNAMIC STREAM ──────────────────────────────────────
	videoMsgUpdates, err := mediaSender.SendDynamicStream(
		ctx, peer, fullStream, info, filename, caption, replyMarkup, nil, thumbFile,
	)
	if err != nil {
		logger.Error("Gagal kirim video", zap.Error(err))
		_, _ = msgSender.To(peer).Text(ctx, "Gagal mengirim video.")
		return nil
	}

	var videoMsgID int
	if videoMsgUpdates != nil {
		if id, err := media.ExtractMessageID(videoMsgUpdates); err == nil {
			videoMsgID = id
		}
	}

	logger.Info("Video TikTok berhasil dikirim", zap.String("video_id", data.ID), zap.Int("msg_id", videoMsgID))

	if videoMsgID != 0 && data.AudioURL != "" && data.ID != "" {
		go func(peerCopy tg.InputPeerClass, msgID int) {
			time.Sleep(2 * time.Minute)
			_, err := client.MessagesEditMessage(context.Background(), &tg.MessagesEditMessageRequest{
				Peer:        peerCopy,
				ID:          msgID,
				ReplyMarkup: &tg.ReplyKeyboardHide{},
			})
			if err != nil {
				logger.Warn("Gagal hapus tombol audio", zap.Error(err))
			}
			cache.DeleteAudio(data.ID)
		}(peer, videoMsgID)
	} else if data.AudioURL != "" && data.ID != "" {
		go func(videoID string) {
			time.Sleep(2 * time.Minute)
			cache.DeleteAudio(videoID)
		}(data.ID)
	}

	return nil
}

// handleTikTokGroup menangani TikTok di grup/supergroup dengan reply support.
func handleTikTokGroup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	data, stream, err := tiktok.FetchTikTokDataWithFallback(ctx, logger, url, func(apiName string, failErr error) {
		logger.Warn("API Grup Gagal (Otomatis Skip)", zap.String("api", apiName), zap.Error(failErr))
	})

	if err != nil {
		logger.Warn("Gagal fetch data TikTok grup", zap.Error(err))
		sendGroupText(ctx, client, peer, "❌ Gagal mengambil data dari TikTok.", replyTo)
		return nil
	}

	if stream != nil {
		defer stream.Close()
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	if data.IsAlbum && len(data.ImageURLs) > 0 {
		if err := kirimAlbumStreamGroup(ctx, client, peer, title, data.ImageURLs, replyTo, logger); err != nil {
			logger.Error("Gagal kirim album TikTok grup", zap.Error(err))
			sendGroupText(ctx, client, peer, "❌ Gagal mengirim foto.", replyTo)
		}
		return nil
	}

	if data.VideoURL == "" {
		sendGroupText(ctx, client, peer, "❌ Tidak ditemukan URL video.", replyTo)
		return nil
	}

	if data.AudioURL != "" && data.ID != "" {
		cache.SetAudio(data.ID, data.AudioURL, data.Title, "Tiktok Music")
		logger.Info("Audio disimpan ke cache (grup)", zap.String("video_id", data.ID))
	}

	// ─── DETEKSI TIPE KONTEN OTOMATIS ──────────────────────────────────────
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

	var thumbFile tg.InputFileClass
	if data.CoverURL != "" {
		thumbBytes, err := api.GetThumbnail(ctx, data.CoverURL)
		if err == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(client).WithThreads(1)
			thumbFile, _ = up.FromBytes(ctx, "thumb.jpg", thumbBytes)
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

	sender := media.NewMediaSender(client)
	videoMsgUpdates, err := sender.SendDynamicStream(
		ctx, peer, fullStream, info, filename, caption, replyMarkup, replyTo, thumbFile,
	)
	if err != nil {
		logger.Error("Gagal kirim video TikTok grup", zap.Error(err))
		sendGroupText(ctx, client, peer, "❌ Gagal mengirim video.", replyTo)
		return nil
	}

	var videoMsgID int
	if videoMsgUpdates != nil {
		if id, err := media.ExtractMessageID(videoMsgUpdates); err == nil {
			videoMsgID = id
		}
	}

	logger.Info("Video TikTok grup berhasil dikirim", zap.String("video_id", data.ID), zap.Int("msg_id", videoMsgID))

	if videoMsgID != 0 && data.AudioURL != "" && data.ID != "" {
		go func(peerCopy tg.InputPeerClass, msgID int) {
			time.Sleep(2 * time.Minute)
			_, _ = client.MessagesEditMessage(context.Background(), &tg.MessagesEditMessageRequest{
				Peer:        peerCopy,
				ID:          msgID,
				ReplyMarkup: &tg.ReplyKeyboardHide{},
			})
			cache.DeleteAudio(data.ID)
		}(peer, videoMsgID)
	} else if data.AudioURL != "" && data.ID != "" {
		go func(videoID string) {
			time.Sleep(2 * time.Minute)
			cache.DeleteAudio(videoID)
		}(data.ID)
	}

	return nil
}

// kirimAlbumStream (private) tetap menggunakan SendPhotoAlbumStream
func kirimAlbumStream(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, title string, imageURLs []string, logger *zap.Logger) error {
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

		if err := mediaSender.SendPhotoAlbumStream(ctx, peer, readers, filenames, captions, nil); err != nil {
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
