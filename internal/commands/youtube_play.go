package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/youtube"
	"mybot/internal/cache"
	"mybot/internal/media"
)

// HandlePlayCommand menangani perintah `.play <url>` di grup
func HandlePlayCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return err
	}

	// Pastikan ini berjalan di Grup / Supergroup / Channel
	if _, isUser := msg.PeerID.(*tg.PeerUser); isUser {
		_ = sendGroupText(ctx, client, peer, "❌ Command `.play` hanya dapat digunakan di dalam Grup.", nil)
		return nil
	}

	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	// 1. Ekstrak Metadata YouTube
	loadingMsgID, _ := sendLoadingMessage(ctx, client, peer, "🔄 Mengambil data stream YouTube.", replyTo, logger)

	ytData, err := youtube.FetchYouTubeData(url)
	if err != nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal mengambil data YouTube.", logger)
		return err
	}

	if len(ytData.Videos) == 0 {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Tidak ada format video yang tersedia.", logger)
		return nil
	}

	// 2. Simpan Data ke Cache
	cache.SetLiveSession(ytData.ID, ytData, url)

	// 3. Bangun Inline Keyboard berdasarkan Resolusi
	var rows []tg.KeyboardButtonRow
	for i, vid := range ytData.Videos {
		btnText := fmt.Sprintf("🎬 %s (%s)", vid.Quality, vid.Format)
		cbData := fmt.Sprintf("ytplay_%s_%d", ytData.ID, i)

		btn := &tg.KeyboardButtonCallback{
			Text: btnText,
			Data: []byte(cbData),
		}

		if i%2 == 0 {
			rows = append(rows, tg.KeyboardButtonRow{Buttons: []tg.KeyboardButtonClass{btn}})
		} else {
			lastIdx := len(rows) - 1
			rows[lastIdx].Buttons = append(rows[lastIdx].Buttons, btn)
		}
	}
	replyMarkup := &tg.ReplyInlineMarkup{Rows: rows}

	// 4. Unduh Thumbnail
	var thumbFile tg.InputFileClass
	thumbBytes, err := api.GetThumbnail(ctx, ytData.Thumbnail)
	if err == nil && len(thumbBytes) > 0 {
		up := uploader.NewUploader(client).WithThreads(1)
		thumbFile, _ = up.FromBytes(ctx, fmt.Sprintf("thumb_%s.jpg", ytData.ID), thumbBytes)
	}

	// 5. Hapus pesan loading dan kirim panel UI
	deleteGroupMessage(ctx, client, peer, loadingMsgID)

	caption := fmt.Sprintf("▶️ Siap Memutar Livestream\n\n📝 Judul: %s\n⏱ Durasi: %d detik\n\nSilakan pilih resolusi video di bawah ini untuk memulai siaran:", ytData.Title, ytData.Duration)

	mediaReq := &tg.MessagesSendMediaRequest{
		Peer:        peer,
		Media:       &tg.InputMediaUploadedPhoto{File: thumbFile},
		Message:     caption,
		RandomID:    time.Now().UnixNano(),
		ReplyMarkup: replyMarkup,
	}
	if replyTo != nil {
		mediaReq.SetReplyTo(replyTo)
	}

	updates, err := client.MessagesSendMedia(ctx, mediaReq)
	if err != nil {
		logger.Error("Gagal mengirim panel play", zap.Error(err))
		return err
	}

	menuMsgID, errExt := media.ExtractMessageID(updates)
	if errExt == nil && menuMsgID != 0 {
		time.AfterFunc(15*time.Minute, func() {
			if _, ok := cache.GetLiveSession(ytData.ID); ok {
				_ = deleteGroupMessage(context.Background(), client, peer, menuMsgID)
				cache.DeleteLiveSession(ytData.ID)
				logger.Info("Panel YouTube otomatis dihapus agar chat bersih", zap.String("id", ytData.ID))
			}
		})
	}
	return nil
}
