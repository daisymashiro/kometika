package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/instagram"
	"mybot/internal/cache"
	"mybot/internal/log"
	"mybot/internal/media"
)

func HandleInstagram(
	ctx context.Context,
	client *tg.Client,
	msg *tg.Message,
	entities tg.Entities,
	url string,
	logger *zap.Logger,
) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	lowerURL := strings.ToLower(url)
	if !strings.Contains(lowerURL, "instagram.com") && !strings.Contains(lowerURL, "instagr.am") {
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer Instagram", zap.Error(err))
		log.LogError(ctx, "Instagram.GetPeer", err, "url="+url)
		return err
	}

	// Kalau bukan private chat, proses sebagai group/supergroup.
	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)

		return handleInstagramGroup(ctx, client, peer, url, replyTo, logger)
	}

	// Private chat.
	return handleInstagramCommon(ctx, client, peer, url, nil, logger)
}

func handleInstagramGroup(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	url string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	return handleInstagramCommon(ctx, client, peer, url, replyTo, logger)
}

func handleInstagramCommon(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	url string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) error {
	if logger == nil {
		logger = zap.NewNop()
	}

	progressMsgID := sendInstagramLoading(ctx, client, peer, replyTo, logger)
	defer deleteInstagramLoading(client, progressMsgID, logger)

	logger.Info("Memproses Instagram", zap.String("url", url))
	log.LogInfo(ctx, "Memproses Instagram\nURL: "+url)

	data, err := instagram.FetchInstagramDataWithFallback(url)
	if err != nil {
		logger.Error("Gagal fetch data Instagram", zap.Error(err))
		log.LogError(ctx, "Instagram.FetchInstagramDataWithFallback", err, "url="+url)

		sendInstagramText(ctx, client, peer, "❌ Gagal mengambil data Instagram.", replyTo, logger)
		return nil
	}

	title := trimInstagramTitle(data.Title)

	// Prioritaskan video.
	// Banyak API Instagram kadang mengisi ImageURLs dengan cover video.
	// Kalau foto diproses duluan, bisa salah kirim cover sebagai foto.
	if data.VideoURL != "" {
		err := sendInstagramVideo(
			ctx,
			client,
			peer,
			data.VideoURL,
			data.AudioURL,
			data.CoverURL,
			data.ID,
			data.Title,
			title,
			replyTo,
			logger,
		)
		if err != nil {
			logger.Error("Gagal kirim video Instagram", zap.Error(err))
			log.LogError(
				ctx,
				"Instagram.SendVideo",
				err,
				"url="+url,
				"video_url="+data.VideoURL,
				"id="+data.ID,
			)

			sendInstagramText(ctx, client, peer, "❌ Gagal mengirim video.", replyTo, logger)
			return nil
		}

		return nil
	}

	// Kalau tidak ada video, proses foto.
	switch {
	case len(data.ImageURLs) == 1:
		err := sendInstagramSinglePhoto(
			ctx,
			client,
			peer,
			data.ImageURLs[0],
			title,
			replyTo,
			logger,
		)
		if err != nil {
			logger.Error("Gagal kirim foto Instagram", zap.Error(err))
			log.LogError(
				ctx,
				"Instagram.SendSinglePhoto",
				err,
				"url="+url,
				"image_url="+data.ImageURLs[0],
			)

			sendInstagramText(ctx, client, peer, "❌ Gagal mengirim foto ke Telegram.", replyTo, logger)
			return nil
		}

		return nil

	case len(data.ImageURLs) > 1:
		if replyTo != nil {
			err = kirimAlbumStreamGroup(ctx, client, peer, title, data.ImageURLs, replyTo, logger)
		} else {
			err = kirimAlbumStream(ctx, client, peer, title, data.ImageURLs, logger)
		}

		if err != nil {
			logger.Error("Gagal kirim album Instagram", zap.Error(err))
			log.LogError(
				ctx,
				"Instagram.SendAlbum",
				err,
				"url="+url,
				fmt.Sprintf("image_count=%d", len(data.ImageURLs)),
			)

			sendInstagramText(ctx, client, peer, "❌ Gagal mengirim album foto.", replyTo, logger)
			return nil
		}

		return nil

	default:
		warnMsg := "Tidak ada media yang dapat diproses dari Instagram"
		logger.Warn(warnMsg, zap.String("url", url))
		log.LogWarn(ctx, "Instagram.NoMedia", warnMsg, "url="+url)

		sendInstagramText(ctx, client, peer, "❌ Tidak ada media yang dapat diproses.", replyTo, logger)
		return nil
	}
}

func sendInstagramSinglePhoto(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	imageURL string,
	title string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) error {
	stream, _, err := api.GetVideoStream(ctx, imageURL)
	if err != nil {
		return fmt.Errorf("gagal buka stream foto Instagram: %w", err)
	}
	defer stream.Close()

	// Convert ke JPEG standar yang ramah MTProto.
	photoReader, err := media.ProcessAndValidateImage(stream, logger)
	if err != nil {
		return fmt.Errorf("gagal proses/konversi foto Instagram: %w", err)
	}

	// Ambil bytes foto ke RAM
	photoBytes, err := io.ReadAll(photoReader)
	if err != nil {
		return fmt.Errorf("gagal membaca buffer foto ke RAM: %w", err)
	}

	logger.Info("Mengunggah foto Instagram langsung dari RAM bytes", zap.Int("bytes", len(photoBytes)))

	// 1. Buat instance uploader bawaan gotd (seperti yang Anda pakai di thumbnail video)
	up := uploader.NewUploader(client).WithThreads(1)

	// 2. Upload langsung dari slice bytes RAM
	uploadedFile, err := up.FromBytes(ctx, "instagram.jpg", photoBytes)
	if err != nil {
		return fmt.Errorf("gagal upload raw bytes ke Telegram: %w", err)
	}

	caption := fmt.Sprintf("📸 %s\n\n@Kometika_bot", title)

	// 3. Daftarkan file yang sudah terupload sebagai input media foto
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    &tg.InputMediaUploadedPhoto{File: uploadedFile},
		Message:  caption,
		RandomID: time.Now().UnixNano(),
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	// 4. Kirim ke Telegram
	_, err = client.MessagesSendMedia(ctx, req)
	if err != nil {
		return fmt.Errorf("gagal mengeksekusi MessagesSendMedia: %w", err)
	}

	return nil
}

func sendInstagramVideo(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	videoURL string,
	audioURL string,
	coverURL string,
	videoID string,
	rawTitle string,
	captionTitle string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) error {
	if audioURL != "" && videoID != "" {
		cache.SetAudio(videoID, audioURL, rawTitle, "Instagram Music")

		logger.Info("Audio Instagram disimpan ke cache", zap.String("id", videoID))
		log.LogInfo(
			ctx,
			fmt.Sprintf(
				"Audio Instagram disimpan ke cache\nID: %s\nSource: Instagram Music",
				videoID,
			),
		)
	}

	stream, _, err := api.GetVideoStream(ctx, videoURL)
	if err != nil {
		return fmt.Errorf("gagal buka stream video Instagram: %w", err)
	}

	info, fullStream, closeStream, err := detectInstagramVideoStream(ctx, stream, videoURL, logger)
	if closeStream != nil {
		defer closeStream()
	}
	if err != nil {
		return err
	}

	if info.Category != api.ContentVideo {
		return fmt.Errorf(
			"URL video tidak mengembalikan video valid: mime=%s category=%s",
			info.MimeType,
			info.Category,
		)
	}

	if info.MimeType == "" {
		info.MimeType = "video/mp4"
	}

	ext := info.Extension
	if ext == "" {
		ext = ".mp4"
	}

	fileID := safeInstagramFileID(videoID)
	filename := fmt.Sprintf("instagram_%s%s", fileID, ext)

	thumbFile := uploadInstagramVideoThumb(ctx, client, coverURL, logger)

	replyMarkup := buildInstagramAudioButton(audioURL, videoID)

	caption := fmt.Sprintf("📽️ %s\n\n@Kometika_bot", captionTitle)

	mediaSender := media.NewMediaSender(client)

	videoMsgUpdates, err := mediaSender.SendDynamicStream(
		ctx,
		peer,
		fullStream,
		info,
		filename,
		caption,
		replyMarkup,
		replyTo,
		thumbFile,
	)
	if err != nil {
		return fmt.Errorf("gagal kirim video Instagram: %w", err)
	}

	if replyMarkup != nil && videoID != "" {
		scheduleInstagramAudioButtonCleanup(client, peer, videoMsgUpdates, videoID, logger)
	}

	return nil
}

func detectInstagramVideoStream(
	ctx context.Context,
	stream interface {
		Read([]byte) (int, error)
		Close() error
	},
	videoURL string,
	logger *zap.Logger,
) (api.ContentTypeInfo, interface {
	Read([]byte) (int, error)
}, func(), error) {
	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err == nil {
		return info, fullStream, func() {
			_ = stream.Close()
		}, nil
	}

	// Penting untuk zero disk:
	// Kalau DetectAndClassifyStream gagal, stream bisa sudah terbaca sebagian.
	// Jangan upload stream yang sama karena bisa terpotong/korup.
	// Solusi: close lalu fetch ulang dari URL.
	logger.Warn("Gagal deteksi tipe konten video, fetch ulang dan fallback ke video/mp4", zap.Error(err))
	log.LogWarn(
		ctx,
		"Instagram.DetectVideoContent",
		"Gagal deteksi tipe konten video, mencoba fetch ulang dan fallback ke video/mp4",
		"video_url="+videoURL,
		"error="+err.Error(),
	)

	_ = stream.Close()

	stream2, _, err2 := api.GetVideoStream(ctx, videoURL)
	if err2 != nil {
		return api.ContentTypeInfo{}, nil, nil, fmt.Errorf("gagal fetch ulang video setelah gagal deteksi: %w", err2)
	}

	fallbackInfo := api.ContentTypeInfo{
		MimeType:  "video/mp4",
		Category:  api.ContentVideo,
		Extension: ".mp4",
	}

	return fallbackInfo, stream2, func() {
		_ = stream2.Close()
	}, nil
}

func uploadInstagramVideoThumb(
	ctx context.Context,
	client *tg.Client,
	coverURL string,
	logger *zap.Logger,
) tg.InputFileClass {
	if coverURL == "" {
		return nil
	}

	thumbBytes, err := api.GetThumbnail(ctx, coverURL)
	if err != nil || len(thumbBytes) == 0 {
		if err != nil {
			logger.Warn("Gagal ambil thumbnail Instagram", zap.Error(err))
			log.LogWarn(
				ctx,
				"Instagram.ThumbnailFetch",
				"Gagal mengambil thumbnail Instagram",
				"cover_url="+coverURL,
				"error="+err.Error(),
			)
		}
		return nil
	}

	// Karena kamu sudah punya converter image sendiri.
	// Ini tetap zero disk.
	convertedThumbReader, err := media.ProcessAndValidateImage(bytes.NewReader(thumbBytes), logger)
	if err != nil {
		logger.Warn("Gagal convert thumbnail Instagram ke JPEG, mencoba upload thumbnail asli", zap.Error(err))
		log.LogWarn(
			ctx,
			"Instagram.ThumbnailConvert",
			"Gagal convert thumbnail Instagram ke JPEG, mencoba upload thumbnail asli",
			"cover_url="+coverURL,
			"error="+err.Error(),
		)
	} else {
		convertedBytes, readErr := io.ReadAll(convertedThumbReader)
		if readErr == nil && len(convertedBytes) > 0 {
			thumbBytes = convertedBytes
		}
	}

	up := uploader.NewUploader(client).WithThreads(1)

	thumbFile, err := up.FromBytes(ctx, "thumb.jpg", thumbBytes)
	if err != nil {
		logger.Warn("Gagal upload thumbnail Instagram", zap.Error(err))
		log.LogWarn(
			ctx,
			"Instagram.ThumbnailUpload",
			"Gagal upload thumbnail Instagram",
			"cover_url="+coverURL,
			"error="+err.Error(),
		)
		return nil
	}

	return thumbFile
}

func buildInstagramAudioButton(audioURL string, videoID string) tg.ReplyMarkupClass {
	if audioURL == "" || videoID == "" {
		return nil
	}

	return &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{
			{
				Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{
						Text: "🎵 Unduh Audio (MP3)",
						Data: []byte(fmt.Sprintf("mp3_%s", videoID)),
					},
				},
			},
		},
	}
}

func scheduleInstagramAudioButtonCleanup(
	client *tg.Client,
	peer tg.InputPeerClass,
	updates tg.UpdatesClass,
	videoID string,
	logger *zap.Logger,
) {
	videoMsgID, err := media.ExtractMessageID(updates)
	if err != nil || videoMsgID == 0 {
		go func() {
			time.Sleep(2 * time.Minute)
			cache.DeleteAudio(videoID)
		}()

		return
	}

	go func(peerCopy tg.InputPeerClass, msgID int, id string) {
		time.Sleep(2 * time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_, err := client.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
			Peer:        peerCopy,
			ID:          msgID,
			ReplyMarkup: &tg.ReplyKeyboardHide{},
		})
		if err != nil {
			logger.Warn(
				"Gagal menghapus tombol audio Instagram",
				zap.Int("msg_id", msgID),
				zap.Error(err),
			)

			log.LogWarn(
				ctx,
				"Instagram.AudioButtonCleanup",
				"Gagal menghapus tombol audio Instagram",
				fmt.Sprintf("msg_id=%d", msgID),
				"id="+id,
				"error="+err.Error(),
			)
		}

		cache.DeleteAudio(id)

		logger.Info(
			"Tombol audio Instagram dihapus otomatis",
			zap.Int("msg_id", msgID),
			zap.String("id", id),
		)
	}(peer, videoMsgID, videoID)
}

func sendInstagramLoading(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) int {
	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  "⏳ Memproses Instagram, mohon tunggu...",
		RandomID: time.Now().UnixNano(),
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	updates, err := client.MessagesSendMessage(ctx, req)
	if err != nil {
		logger.Warn("Gagal kirim pesan loading Instagram", zap.Error(err))
		log.LogWarn(
			ctx,
			"Instagram.LoadingMessage",
			"Gagal mengirim pesan loading Instagram",
			"error="+err.Error(),
		)
		return 0
	}

	msgID, err := media.ExtractMessageID(updates)
	if err != nil {
		logger.Warn("Gagal extract message ID loading Instagram", zap.Error(err))
		return 0
	}

	return msgID
}

func deleteInstagramLoading(client *tg.Client, progressMsgID int, logger *zap.Logger) {
	if progressMsgID == 0 {
		return
	}

	go func() {
		time.Sleep(1 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := client.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
			Revoke: true,
			ID:     []int{progressMsgID},
		})
		if err != nil {
			logger.Warn(
				"Gagal hapus pesan loading Instagram",
				zap.Int("msg_id", progressMsgID),
				zap.Error(err),
			)
		}
	}()
}

func sendInstagramText(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	text string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) {
	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: time.Now().UnixNano(),
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err := client.MessagesSendMessage(ctx, req)
	if err != nil {
		logger.Warn("Gagal kirim pesan teks Instagram", zap.Error(err))
		log.LogWarn(
			ctx,
			"Instagram.SendText",
			"Gagal mengirim pesan teks Instagram",
			"text="+text,
			"error="+err.Error(),
		)
	}
}

func trimInstagramTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Instagram"
	}

	runes := []rune(title)
	if len(runes) > 400 {
		return string(runes[:400]) + "..."
	}

	return title
}

func safeInstagramFileID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Sprintf("%d", time.Now().Unix())
	}

	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)

	return replacer.Replace(id)
}

func readAllSmall(r interface {
	Read([]byte) (int, error)
}) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}

		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}
