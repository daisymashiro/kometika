package commands

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/twiter"
	"mybot/internal/log"
	"mybot/internal/media"
)

// HandleTwitter adalah entry point utama untuk tautan Twitter/X.
func HandleTwitter(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	lowerURL := strings.ToLower(url)

	// Validasi domain Twitter
	if !strings.Contains(lowerURL, "twitter.com") && !strings.Contains(lowerURL, "x.com") && !strings.Contains(lowerURL, "t.co") {
		return nil
	}

	// Deteksi apakah pesan dari Grup/Forum
	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		peer, err := GetPeerFromMessage(ctx, client, msg, entities)
		if err != nil || peer == nil {
			logger.Error("Gagal mendapatkan peer Twitter grup", zap.Error(err))
			return err
		}
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)
		return handleTwitterCommon(ctx, client, peer, url, replyTo, true, logger)
	}

	// PRIVATE CHAT LOGIC
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal mendapatkan peer private chat Twitter", zap.Error(err))
		log.LogError(ctx, "TwitterGetPeer", err, "url="+url)
		return err
	}

	return handleTwitterCommon(ctx, client, peer, url, nil, false, logger)
}

// handleTwitterCommon menyatukan logika DM dan Grup agar tidak ada duplikasi kode
func handleTwitterCommon(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, isGroup bool, logger *zap.Logger) error {
	logger.Info("Memproses Twitter", zap.String("url", url), zap.Bool("isGroup", isGroup))
	log.LogInfo(ctx, "💬 Memproses Twitter\nURL: "+url)

	msgSender := message.NewSender(client)
	var progressMsgID int

	// 1. Kirim pesan progres hanya di Private Chat
	if !isGroup {
		progressMsg, err := msgSender.To(peer).Text(ctx, "⏳ Memproses Twitter, mohon tunggu...")
		if err == nil {
			progressMsgID, _ = media.ExtractMessageID(progressMsg)
			defer func() {
				if progressMsgID != 0 {
					go func() {
						time.Sleep(1 * time.Second)
						_, _ = client.MessagesDeleteMessages(context.Background(), &tg.MessagesDeleteMessagesRequest{
							Revoke: true,
							ID:     []int{progressMsgID},
						})
					}()
				}
			}()
		} else {
			logger.Warn("Gagal mengirim pesan progress", zap.Error(err))
		}
	}

	// 2. Mengambil data menggunakan sistem fallback
	data, err := twiter.FetchTwitterWithFallback(ctx, url)
	if err != nil {
		logger.Warn("Gagal fetch data Twitter", zap.Error(err))
		log.LogError(ctx, "TwitterFetch", err, "url="+url)
		msgErr := "❌ Gagal mengambil data dari Twitter."
		if isGroup {
			_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
		} else {
			_, _ = msgSender.To(peer).Text(ctx, msgErr)
		}
		return nil
	}

	title := data.Title
	if len(title) > 400 {
		title = title[:400] + "..."
	}

	// ===============================================
	// 3. LOGIKA ALBUM (Banyak Media)
	// ===============================================
	if data.IsAlbum && len(data.ImageURLs) > 1 {
		err = kirimAlbumStreamGroup(ctx, client, peer, title, data.ImageURLs, replyTo, logger)
		if err != nil {
			logger.Error("Gagal kirim album Twitter", zap.Error(err))
			msgErr := "❌ Gagal mengirim foto album."
			if isGroup {
				_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
			} else {
				_, _ = msgSender.To(peer).Text(ctx, msgErr)
			}
		} else {
			logger.Info("Album Twitter berhasil dikirim", zap.String("id", data.ID))
		}
		return nil
	}

	// ===============================================
	// 4. Buka Stream URL (Untuk Single Video/Photo)
	// ===============================================
	if data.DownloadURL == "" {
		logger.Warn("URL download kosong", zap.String("id", data.ID))
		msgErr := "❌ Tidak ditemukan URL media."
		if isGroup {
			_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
		} else {
			_, _ = msgSender.To(peer).Text(ctx, msgErr)
		}
		log.LogWarn(ctx, "TwitterNoDownloadURL", "Download URL kosong untuk ID: "+data.ID, "url="+url)
		return nil
	}

	stream, _, err := api.GetVideoStream(ctx, data.DownloadURL)
	if err != nil {
		logger.Error("Gagal download stream Twitter", zap.Error(err))
		log.LogError(ctx, "TwitterStreamDown", err, "media_url="+data.DownloadURL)
		msgErr := "❌ Gagal mengunduh media."
		if isGroup {
			_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
		} else {
			_, _ = msgSender.To(peer).Text(ctx, msgErr)
		}
		return nil
	}
	defer stream.Close()

	// 5. Deteksi tipe konten otomatis
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

	caption := fmt.Sprintf("%s\n\n@Kometika_bot", title)

	// ===============================================
	// 6A. LOGIKA SINGLE PHOTO (In-Memory RAM Convert)
	// ===============================================
	if info.Category == api.ContentImage {
		// Validasi dan konversi ke format standar Telegram (menghindari PHOTO_SAVE_FILE_INVALID)
		photoReader, err := media.ProcessAndValidateImage(fullStream, logger)

		if err != nil {
			logger.Error("Gagal konversi gambar Twitter", zap.Error(err))
			return err
		}

		// Muat ke RAM untuk diunggah langsung via bytes
		photoBytes, err := io.ReadAll(photoReader)
		if err != nil {
			logger.Error("Gagal membaca buffer foto", zap.Error(err))
			return err
		}

		up := uploader.NewUploader(client).WithThreads(1)
		uploadedFile, err := up.FromBytes(ctx, "twitter_photo.jpg", photoBytes)
		if err != nil {
			logger.Error("Gagal upload raw bytes gambar", zap.Error(err))
			return err
		}

		req := &tg.MessagesSendMediaRequest{
			Peer:     peer,
			Media:    &tg.InputMediaUploadedPhoto{File: uploadedFile},
			Message:  caption,
			RandomID: time.Now().UnixNano(),
		}
		if replyTo != nil {
			req.SetReplyTo(replyTo)
		}

		_, err = client.MessagesSendMedia(ctx, req)
		if err != nil {
			logger.Error("Gagal kirim gambar Twitter via MessagesSendMedia", zap.Error(err))
			msgErr := "❌ Gagal mengirim gambar."
			if isGroup {
				_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
			} else {
				_, _ = msgSender.To(peer).Text(ctx, msgErr)
			}
			return err
		}

		logger.Info("Single Photo Twitter berhasil dikirim", zap.String("id", data.ID))
		log.LogInfo(ctx, fmt.Sprintf("📸 Twitter Photo Downloaded\nTitle: %s\nTweet ID: %s", title, data.ID))
		return nil
	}

	// ===============================================
	// 6B. LOGIKA SINGLE VIDEO (Dynamic Streaming Zero-Disk)
	// ===============================================
	var thumbFile tg.InputFileClass
	if data.CoverURL != "" {
		thumbBytes, err := api.GetThumbnail(ctx, data.CoverURL)
		if err == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(client).WithThreads(1)
			thumbFile, _ = up.FromBytes(ctx, "thumb.jpg", thumbBytes)
		}
	}

	filename := fmt.Sprintf("%s%s", data.ID, info.Extension)
	mediaSender := media.NewMediaSender(client)

	videoMsgUpdates, err := mediaSender.SendDynamicStream(
		ctx, peer, fullStream, info, filename, caption, nil, replyTo, thumbFile,
	)

	if err != nil {
		logger.Error("Gagal kirim video Twitter", zap.Error(err))
		log.LogError(ctx, "TwitterSendMedia", err, "tweet_id="+data.ID)
		msgErr := "❌ Gagal mengirim video."
		if isGroup {
			_ = sendGroupText(ctx, client, peer, msgErr, replyTo)
		} else {
			_, _ = msgSender.To(peer).Text(ctx, msgErr)
		}
		return nil
	}

	var videoMsgID int
	if videoMsgUpdates != nil {
		if id, err := media.ExtractMessageID(videoMsgUpdates); err == nil {
			videoMsgID = id
		}
	}

	logger.Info("Video Twitter berhasil dikirim", zap.String("id", data.ID), zap.Int("msg_id", videoMsgID))
	log.LogInfo(ctx, fmt.Sprintf("🎥 Twitter Video Downloaded\nTitle: %s\nTweet ID: %s\nMsg ID: %d", title, data.ID, videoMsgID))
	return nil
}
