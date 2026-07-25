package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/youtube"
	"mybot/internal/media"
)

// HandleMusicCommand menangani perintah .music <url>
func HandleMusicCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return err
	}

	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	// 1. Kirim Pesan Loading
	loadingMsgID, _ := sendLoadingMessage(ctx, client, peer, "🎵 Mengambil data musik dari YouTube", replyTo, logger)

	// 2. Fetch Data dari YouTube
	ytData, err := youtube.FetchYouTubeData(url)
	if err != nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal mengambil data YouTube.", logger)
		return err
	}

	if len(ytData.Audios) == 0 {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Tidak ada format audio yang tersedia.", logger)
		return nil
	}

	// 3. Pilih Audio Kualitas Terbaik (M4A/AAC lebih aman untuk pemutar musik bawaan Telegram)
	var audioTargetURL string
	for _, a := range ytData.Audios {
		format := strings.ToLower(a.Format)
		// Jika Anda sudah menambahkan OriginalFormat di youtube.go, gunakan itu.
		// Jika belum, cek format/url untuk m4a/aac.
		if strings.Contains(format, "m4a") || strings.Contains(format, "aac") {
			audioTargetURL = a.URL
			break
		}
	}

	// Fallback jika M4A tidak ada
	if audioTargetURL == "" {
		audioTargetURL = ytData.Audios[0].URL
	}

	_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "🎧 Mengunduh dan mengirim musik", logger)

	// 4. Buka Aliran Jaringan (Pipe) Langsung ke CDN YouTube (Zero-Disk)
	stream, _, err := api.GetVideoStream(ctx, audioTargetURL)
	if err != nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal membuka aliran audio.", logger)
		return err
	}
	defer stream.Close()

	// 5. Unduh Thumbnail sebagai Cover Album (Opsional)
	var thumbFile tg.InputFileClass
	if ytData.Thumbnail != "" {
		thumbBytes, err := api.GetThumbnail(ctx, ytData.Thumbnail)
		if err == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(client).WithThreads(1)
			thumbFile, _ = up.FromBytes(ctx, "cover.jpg", thumbBytes)
		}
	}

	// 6. RAHASIA: Set Kategori ke Audio agar Telegram merendernya sebagai Music Player
	info := api.ContentTypeInfo{
		MimeType:  "audio/mp4", // M4A menggunakan mime type audio/mp4
		Category:  api.ContentAudio,
		Extension: ".m4a",
	}

	filename := fmt.Sprintf("%s.m4a", ytData.ID)
	caption := fmt.Sprintf("🎵 %s \n\n@Kometika_bot", ytData.Title)

	mediaSender := media.NewMediaSender(client)

	// 7. Kirim secara dinamis (Streaming dari Google langsung ke Telegram)
	_, err = mediaSender.SendDynamicStream(
		ctx,
		peer,
		stream,
		info,
		filename,
		caption,
		nil,
		replyTo,
		thumbFile, // Gambar ini otomatis jadi Cover MP3
	)

	// Hapus pesan loading setelah selesai
	deleteGroupMessage(ctx, client, peer, loadingMsgID)

	if err != nil {
		logger.Error("Gagal mengirim file audio", zap.Error(err))
		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengirim musik ke grup.", replyTo)
		return err
	}

	logger.Info("Musik YouTube berhasil dikirim", zap.String("id", ytData.ID))
	return nil
}
