package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api/youtube"
	"mybot/internal/cache"
	"mybot/internal/media"
)

// HandleYouTubeLive merespons perintah khusus untuk memulai livestreaming ke grup secara LANGSUNG (tanpa menu)
func HandleYouTubeLive(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return fmt.Errorf("gagal mendapatkan peer: %w", err)
	}

	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	// 1. Ekstrak Data dari YouTube
	loadingMsgID, _ := sendLoadingMessage(ctx, client, peer, "🔄 <b>Mengambil data stream YouTube...</b>", replyTo, logger)

	ytData, err := youtube.FetchYouTubeData(url)
	if err != nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Gagal mengambil data YouTube.", logger)
		return err
	}

	if len(ytData.Videos) == 0 {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Tidak ada format video yang tersedia.", logger)
		return nil
	}

	// 2. Ambil Video Terbaik (Index 0 biasanya yang tertinggi)
	videoTarget := ytData.Videos[0]

	// 3. Ambil Audio Terbaik (Dengan Fallback)
	var audioTargetURL string
	for _, a := range ytData.Audios {
		if strings.Contains(strings.ToLower(a.Format), "m4a") || strings.Contains(strings.ToLower(a.Format), "aac") {
			audioTargetURL = a.URL
			break
		}
	}
	if audioTargetURL == "" && len(ytData.Audios) > 0 {
		audioTargetURL = ytData.Audios[0].URL
	}
	if audioTargetURL == "" {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Tidak ada stream audio yang tersedia.", logger)
		return nil
	}

	// 4. Ambil RTMP dari .env
	rtmpURL := os.Getenv("RTMP_URL")
	rtmpKey := os.Getenv("RTMP_KEY")

	if rtmpURL == "" || rtmpKey == "" {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "❌ Konfigurasi RTMP belum diatur di server bot.", logger)
		return nil
	}
	rtmpEndpoint := fmt.Sprintf("%s/%s", strings.TrimRight(rtmpURL, "/"), rtmpKey)

	// 5. Masukkan ke Antrean (Queue Manager)
	queuePos, wasPlaying, errQ := cache.EnqueueStream(cache.QueuedStream{
		VideoID:  ytData.ID,
		VideoURL: videoTarget.URL,
		AudioURL: audioTargetURL,
		Title:    ytData.Title,
		Quality:  videoTarget.Quality + " (Direct)",
		MsgID:    loadingMsgID, // Kita gunakan (dan edit) pesan loading ini
		Peer:     peer,
	})

	if errQ != nil {
		_ = EditLoadingMessage(ctx, client, peer, loadingMsgID, "⚠️ "+errQ.Error(), logger)
		return nil
	}

	// 6. Evaluasi Status Antrean
	if wasPlaying {
		queuedCaption := fmt.Sprintf("⏳ <b>DITAMBAHKAN KE ANTREAN</b>\n\n📝 <b>Judul:</b> %s\n🎬 <b>Resolusi:</b> %s\n🔢 <b>Posisi Antrean:</b> %d", ytData.Title, videoTarget.Quality, queuePos)

		stopMarkup := &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{
						Text: "❌ Batal Antrean",
						Data: []byte(fmt.Sprintf("ytstop_%s", ytData.ID)),
					},
				}},
			},
		}
		_ = media.EditWithMarkup(ctx, client, peer, loadingMsgID, queuedCaption, stopMarkup)
	} else {
		// Jalankan worker pemutar di background
		// Pastikan processStreamQueueWorker di-export menjadi ProcessStreamQueueWorker di youtube_callback.go
		// atau letakkan file ini dalam package yang sama agar bisa saling panggil.
		go processStreamQueueWorker(client, rtmpEndpoint, logger)
	}

	return nil
}

