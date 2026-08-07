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
	"mybot/internal/api/douyin"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"
)

// HandleDouyin menangani link Douyin (scraper internal juga melayani
// link Xiaohongshu/RedNote: xhslink.com, xiaohongshu.com, rednote.com).
func HandleDouyin(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	// Deteksi domain via config/domains.go (satu sumber kebenaran)
	if !config.IsPlatformURL(url, "douyin") {
		return nil
	}

	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("douyin") {
		logger.Info("Fitur Douyin dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading (loading otomatis dihapus setelah selesai)
	return WithLoading(ctx, client, msg, entities, "Douyin", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processDouyin(ctx, client, lc, url, logger)
	})
}

func processDouyin(ctx context.Context, client *tg.Client, lc *LoadingContext, url string, logger *zap.Logger) error {
	data, err := douyin.FetchDouyinData(ctx, url)
	if err != nil {
		logger.Warn("Gagal mengambil data Douyin", zap.Error(err))
		log.LogError(ctx, "Douyin.FetchData", err, "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengambil data dari Douyin.", logger)
		}
		return nil
	}

	title := data.Title
	if title == "" {
		title = "Douyin 🍉"
	}

	// Video
	if data.VideoURL != "" {
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh video...", logger)
		}

		if err := sendDouyinVideo(ctx, client, lc.Peer, data, title, lc.ReplyTo, logger); err != nil {
			logger.Error("Gagal kirim video Douyin", zap.Error(err))
			log.LogError(ctx, "Douyin.SendVideo", err, "url="+url, "video_url="+data.VideoURL, "id="+data.ID)
			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim video.", logger)
			}
			return nil
		}
		return nil
	}

	// Foto / album
	switch {
	case len(data.ImageURLs) == 1:
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh foto...", logger)
		}

		if err := sendDouyinSinglePhoto(ctx, client, lc.Peer, data.ImageURLs[0], title, lc.ReplyTo, logger); err != nil {
			logger.Error("Gagal kirim foto Douyin", zap.Error(err))
			log.LogError(ctx, "Douyin.SendPhoto", err, "url="+url, "image_url="+data.ImageURLs[0])
			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim foto.", logger)
			}
			return nil
		}
		return nil

	case len(data.ImageURLs) > 1:
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh album...", logger)
		}

		if err := sendDouyinAlbum(ctx, client, lc.Peer, title, data.ImageURLs, lc.ReplyTo, logger); err != nil {
			logger.Error("Gagal kirim album Douyin", zap.Error(err))
			log.LogError(ctx, "Douyin.SendAlbum", err, "url="+url, fmt.Sprintf("image_count=%d", len(data.ImageURLs)))
			if lc.ProgressMsgID != 0 {
				_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim album foto.", logger)
			}
			return nil
		}
		return nil

	default:
		warnMsg := "Tidak ada media yang dapat diproses dari Douyin"
		logger.Warn(warnMsg, zap.String("url", url))
		log.LogWarn(ctx, "Douyin.NoMedia", warnMsg, "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Tidak ada media yang dapat diproses.", logger)
		}
		return nil
	}
}

func sendDouyinVideo(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, data *douyin.UniversalDouyinData, title string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	stream, _, err := api.GetVideoStream(ctx, data.VideoURL)
	if err != nil {
		return fmt.Errorf("gagal buka stream video Douyin: %w", err)
	}
	defer stream.Close()

	// Deteksi tipe konten otomatis
	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err != nil {
		logger.Warn("Gagal deteksi tipe konten Douyin, fallback ke video/mp4", zap.Error(err))
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

	caption := douyinCaption(title, data.Desc)
	filename := fmt.Sprintf("%s.mp4", data.ID)

	mediaSender := media.NewMediaSender(client)
	if _, err := mediaSender.SendDynamicStream(
		ctx, peer, fullStream, info, filename, caption, nil, replyTo, thumbFile,
	); err != nil {
		return fmt.Errorf("gagal kirim video Douyin: %w", err)
	}

	logger.Info("Video Douyin berhasil dikirim", zap.String("video_id", data.ID))
	return nil
}

func sendDouyinSinglePhoto(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, imageURL, title string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	stream, _, err := api.GetVideoStream(ctx, imageURL)
	if err != nil {
		return fmt.Errorf("gagal buka stream foto Douyin: %w", err)
	}
	defer stream.Close()

	photoReader, err := media.ProcessAndValidateImage(stream, logger)
	if err != nil {
		return fmt.Errorf("gagal proses/konversi foto Douyin: %w", err)
	}

	photoBytes, err := io.ReadAll(photoReader)
	if err != nil {
		return fmt.Errorf("gagal baca foto Douyin: %w", err)
	}

	mediaSender := media.NewMediaSender(client)
	up := uploader.NewUploader(client).WithThreads(1)

	file, err := up.FromBytes(ctx, "douyin.jpg", photoBytes)
	if err != nil {
		return fmt.Errorf("gagal upload foto Douyin: %w", err)
	}

	uploadedMedia, err := mediaSender.UploadPhotoForReuse(ctx, peer, file)
	if err != nil {
		return fmt.Errorf("gagal registrasi foto Douyin: %w", err)
	}

	caption := douyinCaption(title, "")

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
				logger.Warn("FloodWait saat kirim foto Douyin", zap.Duration("wait", d))
				select {
				case <-time.After(d + time.Second):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return fmt.Errorf("gagal kirim foto Douyin: %w", err)
		}
		break
	}

	logger.Info("Foto Douyin berhasil dikirim")
	return nil
}

// douyinCaption menyusun caption judul + deskripsi (jika ada), lalu
// memotongnya aman di batas karakter Telegram.
func douyinCaption(title, desc string) string {
	var sb strings.Builder
	sb.WriteString(title)
	if desc != "" {
		sb.WriteString("\n\n")
		sb.WriteString(desc)
	}
	sb.WriteString("\n\n@Kometika_bot")

	caption := sb.String()
	if len(caption) > 1000 {
		caption = string([]rune(caption)[:1000]) + "..."
	}
	return caption
}

// sendDouyinAlbum mengirim album foto. Setiap gambar diproses lewat
// ProcessAndValidateImageBytes (konversi WebP/PNG/GIF -> JPEG, resize, dan
// validasi dimensi) sebelum dikirim, karena CDN XHS bisa mengembalikan
// WebP atau gambar bersudut ekstrem yang ditolak Telegram
// (PHOTO_INVALID_DIMENSIONS).
func sendDouyinAlbum(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, title string, imageURLs []string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	if len(imageURLs) == 0 {
		return nil
	}

	mediaSender := media.NewMediaSender(client)

	const maxAlbumSize = 10
	batches := splitIntoBatches(imageURLs, maxAlbumSize)
	totalBatches := len(batches)
	log.LogInfo(ctx, fmt.Sprintf("Douyin.AlbumStart: images=%d batches=%d", len(imageURLs), totalBatches))

	for batchIdx, batch := range batches {
		readers := make([]io.Reader, 0, len(batch))
		filenames := make([]string, 0, len(batch))
		captions := make([]string, 0, len(batch))

		for _, imgURL := range batch {
			stream, _, err := api.GetVideoStream(ctx, imgURL)
			if err != nil {
				logger.Warn("Gagal buka stream gambar Douyin", zap.String("url", imgURL), zap.Error(err))
				log.LogWarn(ctx, "Douyin.AlbumOpen", "Gagal membuka stream gambar Douyin", "url="+imgURL, "error="+err.Error())
				continue
			}
			jpegBytes, convErr := media.ProcessAndValidateImageBytes(stream, logger)
			stream.Close()
			if convErr != nil {
				logger.Warn("Gagal konversi gambar Douyin", zap.String("url", imgURL), zap.Error(convErr))
				log.LogWarn(ctx, "Douyin.AlbumConvert", "Gagal konversi gambar Douyin ke JPEG", "url="+imgURL, "error="+convErr.Error())
				continue
			}

			readers = append(readers, bytes.NewReader(jpegBytes))
			filenames = append(filenames, fmt.Sprintf("douyin_%d_%d.jpg", batchIdx, len(filenames)))
			if len(captions) == 0 {
				captions = append(captions, fmt.Sprintf("🖼️ %s\n\n@Kometika_bot", title))
			} else {
				captions = append(captions, "")
			}
		}

		if len(readers) == 0 {
			logger.Warn("Tidak ada gambar valid untuk batch Douyin", zap.Int("batch", batchIdx))
			log.LogWarn(ctx, "Douyin.AlbumEmptyBatch", "Tidak ada gambar valid di batch Douyin", fmt.Sprintf("batch=%d", batchIdx))
			continue
		}

		if err := mediaSender.SendPhotoAlbumStream(ctx, peer, readers, filenames, captions, replyTo); err != nil {
			log.LogWarn(ctx, "Douyin.AlbumSend", "Gagal kirim batch album Douyin", fmt.Sprintf("batch=%d total_batches=%d", batchIdx+1, totalBatches), "error="+err.Error())
			return fmt.Errorf("kirim album Douyin batch ke-%d gagal: %w", batchIdx+1, err)
		}

		log.LogInfo(ctx, fmt.Sprintf("Douyin.AlbumBatchOK: batch=%d total_batches=%d images=%d", batchIdx+1, totalBatches, len(batch)))

		if batchIdx < len(batches)-1 {
			time.Sleep(1 * time.Second)
		}
	}
	return nil
}
