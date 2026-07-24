package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/cache"
	"mybot/internal/media"
	"mybot/internal/streamer"
)

func HandleYouTubeLiveCallback(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, update *tg.UpdateBotCallbackQuery, logger *zap.Logger) error {
	rawData := string(update.Data)

	if strings.HasPrefix(rawData, "ytstop_") {
		return handleStopLive(ctx, client, peer, msgID, update, rawData, logger)
	}

	if strings.HasPrefix(rawData, "ytplay_") {
		return handlePlayLive(ctx, client, peer, msgID, update, rawData, logger)
	}

	return nil
}

func handlePlayLive(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, update *tg.UpdateBotCallbackQuery, rawData string, logger *zap.Logger) error {
	parts := strings.Split(rawData, "_")

	if len(parts) != 3 {
		media.AnswerCallback(ctx, client, update.QueryID, "Data tombol tidak valid.", true)
		return nil
	}

	videoID := parts[1]
	videoIdx, err := strconv.Atoi(parts[2])
	if err != nil {
		return err
	}

	session, ok := cache.GetLiveSession(videoID)
	if !ok {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Sesi telah kedaluwarsa.", true)
		_ = deleteGroupMessage(ctx, client, peer, msgID) // Bersihkan UI jika telat klik
		return nil
	}

	videoTarget := session.VideoData.Videos[videoIdx]

	var audioTargetURL string
	for _, a := range session.VideoData.Audios {
		if strings.Contains(strings.ToLower(a.Format), "m4a") || strings.Contains(strings.ToLower(a.Format), "aac") {
			audioTargetURL = a.URL
			break
		}
	}
	if audioTargetURL == "" && len(session.VideoData.Audios) > 0 {
		audioTargetURL = session.VideoData.Audios[0].URL
	}
	if audioTargetURL == "" {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Tidak ada stream audio yang tersedia.", true)
		return nil
	}

	rtmpURL := os.Getenv("RTMP_URL")
	rtmpKey := os.Getenv("RTMP_KEY")

	if rtmpURL == "" || rtmpKey == "" {
		errMsg := "❌ Konfigurasi RTMP belum diatur di server bot."
		if editErr := media.EditWithMarkup(ctx, client, peer, msgID, errMsg, nil); editErr != nil {
			logger.Error("Gagal update pesan error RTMP env", zap.Error(editErr))
		}
		return nil
	}

	rtmpEndpoint := fmt.Sprintf("%s/%s", strings.TrimRight(rtmpURL, "/"), rtmpKey)

	queuePos, wasPlaying, errQ := cache.EnqueueStream(cache.QueuedStream{
		VideoID:  videoID,
		VideoURL: videoTarget.URL,
		AudioURL: audioTargetURL,
		Title:    session.VideoData.Title,
		Quality:  videoTarget.Quality,
		MsgID:    msgID,
		Peer:     peer,
	})

	if errQ != nil {
		media.AnswerCallback(ctx, client, update.QueryID, "⚠️ "+errQ.Error(), true)
		return nil
	}

	cache.DeleteLiveSession(videoID)
	media.AnswerCallback(ctx, client, update.QueryID, "Memproses livestream...", false)

	if wasPlaying {
		queuedCaption := fmt.Sprintf("⏳ DITAMBAHKAN KE ANTREAN\n\n📝 Judul: %s\n🎬 Resolusi: %s\n🔢 Posisi Antrean: %d", session.VideoData.Title, videoTarget.Quality, queuePos)
		stopMarkup := &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{
						Text: "❌ Batal Antrean",
						Data: []byte(fmt.Sprintf("ytstop_%s", videoID)),
					},
				}},
			},
		}
		if err := media.EditWithMarkup(ctx, client, peer, msgID, queuedCaption, stopMarkup); err != nil {
			logger.Error("Gagal edit pesan antrean", zap.Error(err))
		}
	} else {
		go processStreamQueueWorker(client, rtmpEndpoint, logger)
	}

	return nil
}

func processStreamQueueWorker(client *tg.Client, rtmpEndpoint string, logger *zap.Logger) {
	for {
		item, ok := cache.DequeueStream()
		if !ok {
			break
		}

		// FIX BUG #3: Tambahkan timeout 2 jam untuk mencegah context leak
		// Jika FFmpeg hang, context akan otomatis di-cancel setelah 2 jam
		streamCtx, cancelStream := context.WithTimeout(context.Background(), 2*time.Hour)
		
		cache.SetCurrentStream(item.VideoID, cancelStream)

		liveCaption := fmt.Sprintf("🔴 LIVE STREAMING SEDANG BERJALAN\n\n📝 Judul: %s\n🎬 Resolusi: %s\n\nSilakan bergabung ke Obrolan Video di atas untuk menonton.", item.Title, item.Quality)
		stopMarkup := &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{
						Text: "⏹ Hentikan Stream",
						Data: []byte(fmt.Sprintf("ytstop_%s", item.VideoID)),
					},
				}},
			},
		}

		_ = media.EditWithMarkup(context.Background(), client, item.Peer, item.MsgID, liveCaption, stopMarkup)

		err := streamer.PushToRTMP(streamCtx, item.VideoURL, item.AudioURL, rtmpEndpoint, logger)

		// Cleanup: Cancel context setelah streaming selesai/error
		cancelStream()

		if err != nil && streamCtx.Err() == nil {
			logger.Error("Stream terputus", zap.Error(err))
			_ = media.EditWithMarkup(context.Background(), client, item.Peer, item.MsgID, "🛑 Stream Terputus karena kesalahan jaringan atau video selesai.", nil)
		} else if streamCtx.Err() == context.DeadlineExceeded {
			logger.Warn("Stream timeout setelah 2 jam", zap.String("video_id", item.VideoID))
			_ = media.EditWithMarkup(context.Background(), client, item.Peer, item.MsgID, "⏱ Stream dihentikan otomatis (timeout 2 jam).", nil)
		} else {
			// === AUTO-CLEANUP SAAT STREAM SELESAI/DISTOP ===
			_ = deleteGroupMessage(context.Background(), client, item.Peer, item.MsgID)
		}

		cache.SetCurrentStream("", nil)
	}
}

func handleStopLive(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, update *tg.UpdateBotCallbackQuery, rawData string, logger *zap.Logger) error {
	videoID := strings.TrimPrefix(rawData, "ytstop_")

	if cache.RemoveFromQueue(videoID) {
		media.AnswerCallback(ctx, client, update.QueryID, "Antrean dibatalkan.", false)
		// === Hapus pesan saat antrean dibatalkan ===
		_ = deleteGroupMessage(ctx, client, peer, msgID)
		return nil
	}

	if cache.IsCurrentStream(videoID) {
		media.AnswerCallback(ctx, client, update.QueryID, "Menghentikan streaming...", false)
		cache.StopCurrentStream()
		// Pesan tidak perlu dihapus di sini, karena otomatis dihapus oleh worker di processStreamQueueWorker
		return nil
	}

	media.AnswerCallback(ctx, client, update.QueryID, "Sesi tidak ditemukan atau sudah selesai.", true)
	// === Bersihkan pesan yang nyangkut ===
	_ = deleteGroupMessage(ctx, client, peer, msgID)
	return nil
}
