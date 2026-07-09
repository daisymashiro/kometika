package commands

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"mybot/internal/api"
	"mybot/internal/api/lulustream"
	"mybot/internal/log"
	"mybot/internal/media"

	"github.com/gotd/td/telegram/message"
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
	peer, err := GetPeerFromMessage(ctx, tgClient, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer untuk Lulustream", zap.Error(err))
		log.LogError(ctx, "LulustreamGetPeer", err, "url="+url)
		return err
	}

	msgSender := message.NewSender(tgClient)
	mediaSender := media.NewMediaSender(tgClient)

	var replyTo *tg.InputReplyToMessage
	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		topicID := getTopicID(msg)
		replyTo = buildReplyTo(msg.ID, topicID)
	}
	logger.Info("Menerima request LuluStream", zap.String("url", url))

	var progressMsgID int
	if replyTo != nil {
		reqProgress := &tg.MessagesSendMessageRequest{
			Peer:     peer,
			Message:  "⏳ Memproses LuluStream, mohon tunggu...",
			RandomID: time.Now().UnixNano(),
		}
		reqProgress.SetReplyTo(replyTo)
		progressUpdates, err := tgClient.MessagesSendMessage(ctx, reqProgress)
		if err == nil && progressUpdates != nil {
			progressMsgID, _ = media.ExtractMessageID(progressUpdates)
		}
	} else {
		progressMsg, err := msgSender.To(peer).Reply(msg.ID).Text(ctx, "⏳ Memproses LuluStream, mohon tunggu...")
		if err == nil {
			progressMsgID, _ = media.ExtractMessageID(progressMsg)
		}
	}

	defer func() {
		if progressMsgID != 0 {
			go func() {
				time.Sleep(1 * time.Second)
				deleteGroupMessage(context.Background(), tgClient, peer, progressMsgID)
			}()
		}
	}()

	videoID := extractLuluID(url)
	if videoID == "" {
		logger.Warn("Gagal extract videoID dari URL", zap.String("url", url))
		log.LogWarn(ctx, "LulustreamExtractID", "videoID tidak ditemukan", "url="+url)
		_ = media.SendHTML(ctx, tgClient, peer, "❌ ID Video Lulustream tidak ditemukan.")
		return nil
	}

	// ⏱️ Mulai scrape
	startScrape := time.Now()
	result, httpClient, err := lulustream.Scrape(videoID)
	logger.Info("Scrape selesai", zap.Duration("durasi", time.Since(startScrape)))
	if err != nil {
		logger.Error("Lulustream scrape error", zap.Error(err))
		log.LogError(ctx, "LulustreamScrape", err, "videoID="+videoID, "url="+url)
		_ = media.SendHTML(ctx, tgClient, peer, "❌ Gagal mengambil data Lulustream.")
		return err
	}

	logger.Info("Scraping berhasil", zap.String("title", result.Title), zap.String("id", videoID))
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
		_ = media.SendHTML(ctx, tgClient, peer, "❌ Gagal membuka aliran stream video. Server menolak akses.")
		return err
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
		peer,
		fullStream,
		info,
		filename,
		caption,
		nil,
		replyTo,
		thumbFile,
	)

	if err != nil {
		logger.Error("Gagal mengirim video LuluStream", zap.Error(err))
		log.LogError(ctx, "LulustreamUpload", err, "filename="+filename, "videoID="+videoID)
		_ = media.SendHTML(ctx, tgClient, peer, "❌ Gagal mengirim video ke Telegram.")
		return err
	}
	logger.Info("Video LuluStream berhasil dikirim", zap.String("video_id", videoID))
	return nil
}
