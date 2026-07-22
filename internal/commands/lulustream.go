package commands

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"mybot/internal/api"
	"mybot/internal/api/lulustream"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func extractLuluID(link string) string {
	re := regexp.MustCompile(`luluvid\.com/(?:d/)?([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(link)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func HandleLulustream(ctx context.Context, tgClient *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("lulustream") {
		logger.Info("Fitur Lulustream dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading
	return WithLoading(ctx, tgClient, msg, entities, "Lulustream", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processLulustream(ctx, tgClient, lc, url, logger)
	})
}

func processLulustream(ctx context.Context, tgClient *tg.Client, lc *LoadingContext, url string, logger *zap.Logger) error {
	logger.Info("Menerima request LuluStream", zap.String("url", url))

	mediaSender := media.NewMediaSender(tgClient)

	videoID := extractLuluID(url)
	if videoID == "" {
		logger.Warn("Gagal extract videoID dari URL", zap.String("url", url))
		log.LogWarn(ctx, "LulustreamExtractID", "videoID tidak ditemukan", "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "❌ ID Video Lulustream tidak ditemukan.", logger)
		}
		return nil
	}

	// ⏱️ Mulai scrape
	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "🔍 Mengambil informasi video...", logger)
	}

	startScrape := time.Now()
	result, httpClient, err := lulustream.Scrape(videoID)
	logger.Info("Scrape selesai", zap.Duration("durasi", time.Since(startScrape)))
	if err != nil {
		logger.Error("Lulustream scrape error", zap.Error(err))
		log.LogError(ctx, "LulustreamScrape", err, "videoID="+videoID, "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengambil data Lulustream.", logger)
		}
		return nil
	}

	logger.Info("Scraping berhasil", zap.String("title", result.Title), zap.String("id", videoID))
	
	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh video...", logger)
	}

	var thumbFile tg.InputFileClass
	if result.Thumbnail != "" {
		thumbBytes, err := api.GetThumbnail(ctx, result.Thumbnail)
		if err == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(tgClient).WithThreads(2)
			thumbFile, _ = up.FromBytes(ctx, "thumb_lulustream.jpg", thumbBytes)
		} else {
			logger.Warn("Gagal mendownload thumbnail LuluStream", zap.Error(err))
			log.LogWarn(ctx, "LulustreamThumbnail", err.Error(), "url="+result.Thumbnail)
		}
	}

	refererPage := fmt.Sprintf("https://luluvid.com/d/%s_h", videoID)

	// ⏱️ Buka stream
	startStream := time.Now()
	stream, _, err := lulustream.LuluStreamDown(ctx, httpClient, result.DownloadURL, refererPage)
	logger.Info("Stream dibuka", zap.Duration("durasi", time.Since(startStream)))
	if err != nil {
		logger.Error("Gagal membuka stream video LuluStream", zap.Error(err), zap.String("download_url", result.DownloadURL))
		log.LogError(ctx, "LulustreamStream", err, "download_url="+result.DownloadURL, "videoID="+videoID)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "❌ Gagal membuka aliran stream video.", logger)
		}
		return nil
	}
	defer stream.Close()

	caption := fmt.Sprintf("🎥 FileName: %s \n📦 Size: %s \n\n @Kometika_bot", result.Title, result.Filesize)
	filename := fmt.Sprintf("lulustream_%s.mp4", result.ID)

	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err != nil {
		logger.Warn("Gagal mendeteksi tipe konten, fallback ke video/mp4", zap.Error(err))
		info = api.ContentTypeInfo{
			MimeType:  "video/mp4",
			Category:  api.ContentVideo,
			Extension: ".mp4",
		}
		fullStream = stream
	}

	_, err = mediaSender.SendDynamicStream(
		ctx,
		lc.Peer,
		fullStream,
		info,
		filename,
		caption,
		nil,
		lc.ReplyTo,
		thumbFile,
	)

	if err != nil {
		logger.Error("Gagal mengirim video LuluStream", zap.Error(err))
		log.LogError(ctx, "LulustreamUpload", err, "filename="+filename, "videoID="+videoID)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, tgClient, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengirim video ke Telegram.", logger)
		}
		return nil
	}

	logger.Info("Video LuluStream berhasil dikirim", zap.String("video_id", videoID))
	return nil
}
