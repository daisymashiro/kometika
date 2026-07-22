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
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/instagram"
	"mybot/internal/cache"
	"mybot/internal/config"
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

	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("instagram") {
		logger.Info("Fitur Instagram dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading
	return WithLoading(ctx, client, msg, entities, "Instagram", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processInstagram(ctx, client, lc, url, logger)
	})
}

func processInstagram(
	ctx context.Context,
	client *tg.Client,
	lc *LoadingContext,
	url string,
	logger *zap.Logger,
) error {
	logger.Info("Memproses Instagram", zap.String("url", url))

	data, err := instagram.FetchInstagramDataWithFallback(url)
	if err != nil {
		logger.Error("Gagal fetch data Instagram", zap.Error(err))
		log.LogError(ctx, "Instagram.FetchInstagramDataWithFallback", err, "url="+url)

		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengambil data Instagram.", logger)
		}
		return nil
	}

	title := trimInstagramTitle(data.Title)

	// Prioritaskan video
	if data.VideoURL != "" {
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh video...", logger)
		}

		err := sendInstagramVideo(
			ctx,
			client,
			lc.Peer,
			data.VideoURL,
			data.AudioURL,
			data.CoverURL,
			data.ID,
			data.Title,
			title,
			lc.ReplyTo,
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

			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim video.", logger)
			}
			return nil
		}

		return nil
	}

	// Proses foto
	switch {
	case len(data.ImageURLs) == 1:
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh foto...", logger)
		}

		err := sendInstagramSinglePhoto(
			ctx,
			client,
			lc.Peer,
			data.ImageURLs[0],
			title,
			lc.ReplyTo,
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

			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim foto ke Telegram.", logger)
			}
			return nil
		}

		return nil

	case len(data.ImageURLs) > 1:
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh album...", logger)
		}

		err = kirimAlbumStreamInstagram(ctx, client, lc.Peer, title, data.ImageURLs, lc.ReplyTo, logger)
		if err != nil {
			logger.Error("Gagal kirim album Instagram", zap.Error(err))
			log.LogError(
				ctx,
				"Instagram.SendAlbum",
				err,
				"url="+url,
				fmt.Sprintf("image_count=%d", len(data.ImageURLs)),
			)

			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim album foto.", logger)
			}
			return nil
		}

		return nil

	default:
		warnMsg := "Tidak ada media yang dapat diproses dari Instagram"
		logger.Warn(warnMsg, zap.String("url", url))
		log.LogWarn(ctx, "Instagram.NoMedia", warnMsg, "url="+url)

		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Tidak ada media yang dapat diproses.", logger)
		}
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

	photoReader, err := media.ProcessAndValidateImage(stream, logger)
	if err != nil {
		return fmt.Errorf("gagal proses/konversi foto Instagram: %w", err)
	}

	photoBytes, err := io.ReadAll(photoReader)
	if err != nil {
		return fmt.Errorf("gagal baca foto Instagram: %w", err)
	}

	mediaSender := media.NewMediaSender(client)
	up := uploader.NewUploader(client).WithThreads(1)

	file, err := up.FromBytes(ctx, "instagram.jpg", photoBytes)
	if err != nil {
		return fmt.Errorf("gagal upload foto Instagram: %w", err)
	}

	uploadedMedia, err := mediaSender.UploadPhotoForReuse(ctx, peer, file)
	if err != nil {
		return fmt.Errorf("gagal registrasi foto Instagram: %w", err)
	}

	caption := fmt.Sprintf("📷 %s\n\n@Kometika_bot", title)

	randID, err := media.RandomID()
	if err != nil {
		return fmt.Errorf("gagal generate random ID: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    uploadedMedia,
		Message:  caption,
		RandomID: randID,
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	// FloodWait handling
	for {
		_, err = client.MessagesSendMedia(ctx, req)
		if err != nil {
			if d, ok := tgerr.AsFloodWait(err); ok {
				logger.Warn("FloodWait saat kirim foto Instagram", zap.Duration("wait", d))
				select {
				case <-time.After(d + time.Second):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return fmt.Errorf("gagal kirim foto Instagram: %w", err)
		}
		break
	}

	logger.Info("Foto Instagram berhasil dikirim")
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
	title string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) error {
	if audioURL != "" && videoID != "" {
		cache.SetAudio(videoID, audioURL, rawTitle, "Instagram Music")
		logger.Info("Audio Instagram disimpan ke cache", zap.String("video_id", videoID))
	}

	stream, _, err := api.GetVideoStream(ctx, videoURL)
	if err != nil {
		return fmt.Errorf("gagal buka stream video Instagram: %w", err)
	}
	defer stream.Close()

	info, fullStream, cleanupFn, err := detectInstagramContentWithFallback(ctx, logger, videoURL, stream)
	if err != nil {
		return err
	}
	if cleanupFn != nil {
		defer cleanupFn()
	}

	thumbFile := uploadInstagramVideoThumb(ctx, client, coverURL, logger)

	replyMarkup := buildInstagramAudioButton(audioURL, videoID)

	caption := fmt.Sprintf("🎬 %s\n\n@Kometika_bot", title)
	filename := fmt.Sprintf("%s.mp4", safeInstagramFileID(videoID))

	mediaSender := media.NewMediaSender(client)
	updates, err := mediaSender.SendDynamicStream(
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

	if replyMarkup != nil && updates != nil {
		scheduleInstagramAudioButtonCleanup(client, peer, updates, videoID, logger)
	}

	logger.Info("Video Instagram berhasil dikirim", zap.String("video_id", videoID))
	return nil
}

func detectInstagramContentWithFallback(
	ctx context.Context,
	logger *zap.Logger,
	videoURL string,
	stream io.ReadCloser,
) (api.ContentTypeInfo, io.Reader, func(), error) {
	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err == nil {
		return info, fullStream, nil, nil
	}

	logger.Warn("Gagal deteksi tipe konten Instagram, fallback ke video/mp4", zap.Error(err))
	log.LogWarn(
		ctx,
		"Instagram.DetectContentFallback",
		"Gagal deteksi tipe konten, fallback ke video/mp4",
		"video_url="+videoURL,
		"error="+err.Error(),
	)

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
		go scheduleAudioCacheCleanup(videoID)
		return
	}

	go func(peerCopy tg.InputPeerClass, msgID int, id string) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		select {
		case <-time.After(2 * time.Minute):
			editCtx, editCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer editCancel()

			for {
				_, err := client.MessagesEditMessage(editCtx, &tg.MessagesEditMessageRequest{
					Peer:        peerCopy,
					ID:          msgID,
					ReplyMarkup: &tg.ReplyKeyboardHide{},
				})
				if err != nil {
					if d, ok := tgerr.AsFloodWait(err); ok {
						logger.Warn("FloodWait saat hapus tombol audio Instagram", zap.Duration("wait", d))
						select {
						case <-time.After(d + time.Second):
							continue
						case <-editCtx.Done():
							logger.Warn("Gagal menghapus tombol audio Instagram (timeout)", zap.Int("msg_id", msgID))
							cache.DeleteAudio(id)
							return
						}
					}

					logger.Warn(
						"Gagal menghapus tombol audio Instagram",
						zap.Int("msg_id", msgID),
						zap.Error(err),
					)

					log.LogWarn(
						editCtx,
						"Instagram.AudioButtonCleanup",
						"Gagal menghapus tombol audio Instagram",
						fmt.Sprintf("msg_id=%d", msgID),
						"id="+id,
						"error="+err.Error(),
					)
					cache.DeleteAudio(id)
					return
				}
				break
			}

			cache.DeleteAudio(id)
			logger.Info(
				"Tombol audio Instagram dihapus otomatis",
				zap.Int("msg_id", msgID),
				zap.String("id", id),
			)

		case <-ctx.Done():
			cache.DeleteAudio(id)
		}
	}(peer, videoMsgID, videoID)
}

func kirimAlbumStreamInstagram(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, title string, imageURLs []string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	return kirimAlbumStream(ctx, client, peer, title, imageURLs, replyTo, logger)
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
