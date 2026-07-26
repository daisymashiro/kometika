package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/kkdai/youtube/v2"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
	"mybot/internal/media"
)

func HandleMusicCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, targetUrl string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return err
	}
	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	loadingMsgID, _ := sendLoadingMessage(ctx, client, peer, "⏳ Mengambil data musik dari YouTube...", replyTo, logger)

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	customTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	httpClient := &http.Client{
		Transport: customTransport,
		Timeout:   60 * time.Second,
	}

	ytClient := youtube.Client{
		HTTPClient: httpClient, // Masukkan custom client ke sini
	}

	video, err := ytClient.GetVideo(targetUrl)
	if err != nil {
		logger.Error("Gagal mengambil data video YouTube", zap.Error(err))
		log.LogError(ctx, "YoutubeMusic_GetVideo", err, "url="+targetUrl)

		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal mengambil data dari YouTube.", logger)
		return err
	}

	var audioFormat *youtube.Format
	formats := video.Formats.WithAudioChannels()

	for i, f := range formats {
		if strings.Contains(f.MimeType, "audio/mp4") {
			audioFormat = &formats[i]
			break
		}
	}

	if audioFormat == nil && len(formats) > 0 {
		audioFormat = &formats[0]
	}

	if audioFormat == nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Tidak ada format audio yang tersedia.", logger)
		return nil
	}

	_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "⏳ Mengunduh dan mengirim musik...", logger)

	stream, _, err := ytClient.GetStream(video, audioFormat)
	if err != nil {
		logger.Error("Gagal membuka aliran stream", zap.Error(err))
		log.LogError(ctx, "YoutubeMusic_GetStream", err, "url="+targetUrl)

		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal membuka aliran stream audio.", logger)
		return err
	}
	defer stream.Close()

	info := api.ContentTypeInfo{
		MimeType:  "audio/mp4",
		Category:  api.ContentAudio,
		Extension: ".m4a",
	}

	filename := fmt.Sprintf("%s.m4a", video.ID)
	caption := fmt.Sprintf("🎵 %s \n\n@Kometika_bot", video.Title)

	mediaSender := media.NewMediaSender(client)

	_, err = mediaSender.SendDynamicStream(
		ctx, peer, stream, info, filename, caption, nil, replyTo, nil,
	)

	// Hapus pesan loading
	deleteGroupMessage(ctx, client, peer, loadingMsgID)

	if err != nil {
		logger.Error("Gagal mengirim file audio ke Telegram", zap.Error(err))
		log.LogError(ctx, "YoutubeMusic_UploadToTG", err, "url="+targetUrl)

		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengirim musik ke grup. Koneksi terputus.", replyTo)
		return err
	}

	logger.Info("Musik YouTube berhasil dikirim", zap.String("id", video.ID))
	return nil
}
