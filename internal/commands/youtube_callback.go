package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/cache"
	"mybot/internal/media"
	"mybot/internal/streamer"
)

// HandleYouTubeLiveCallback adalah entry point untuk callback query YouTube Live
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

// handlePlayLive menangani saat user memilih resolusi video dan memulai streaming
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

	// 1. Ambil data dari State Cache
	session, ok := cache.GetLiveSession(videoID)
	if !ok {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Sesi telah kedaluwarsa. Silakan kirim ulang command .play", true)
		return nil
	}

	if session.Cancel != nil {
		media.AnswerCallback(ctx, client, update.QueryID, "⚠️ Stream untuk video ini sedang berjalan.", true)
		return nil
	}

	if videoIdx >= len(session.VideoData.Videos) {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Resolusi tidak ditemukan.", true)
		return nil
	}

	videoTarget := session.VideoData.Videos[videoIdx]

	// 2. Ambil Audio Terbaik (Dengan Fallback)
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
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Tidak ada stream audio yang tersedia dari video ini.", true)
		return nil
	}

	// 3. Ambil RTMP Endpoint dari Environment Variables (.env)
	rtmpURL := os.Getenv("RTMP_URL")
	rtmpKey := os.Getenv("RTMP_KEY")

	if rtmpURL == "" || rtmpKey == "" {
		errMsg := "❌ Konfigurasi RTMP belum diatur. Pastikan Admin telah mengatur `RTMP_URL` dan `RTMP_KEY` di server bot."
		if editErr := media.EditWithMarkup(ctx, client, peer, msgID, errMsg, nil); editErr != nil {
			logger.Error("Gagal update pesan error RTMP env", zap.Error(editErr))
		}
		return nil
	}

	// Gabungkan URL dan Key (Pastikan tidak ada double slash)
	rtmpURL = strings.TrimRight(rtmpURL, "/")
	rtmpEndpoint := fmt.Sprintf("%s/%s", rtmpURL, rtmpKey)

	media.AnswerCallback(ctx, client, update.QueryID, "Memulai Livestream...", false)

	// 4. Edit Pesan (Hapus Tombol & Beri Status Live)
	liveCaption := fmt.Sprintf("🔴 LIVE STREAMING SEDANG BERJALAN\n\n📝 Judul: %s\n🎬 Resolusi: %s\n\nSilakan bergabung ke Obrolan Video di bagian atas grup untuk menonton.", session.VideoData.Title, videoTarget.Quality)

	stopMarkup := &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{
			{Buttons: []tg.KeyboardButtonClass{
				&tg.KeyboardButtonCallback{
					Text: "⏹ Hentikan Stream",
					Data: []byte(fmt.Sprintf("ytstop_%s", videoID)),
				},
			}},
		},
	}

	if err := media.EditWithMarkup(ctx, client, peer, msgID, liveCaption, stopMarkup); err != nil {
		logger.Error("Gagal mengedit pesan menjadi status live", zap.Error(err))
	}

	// 5. Jalankan Zero-Disk Muxer di Background
	streamCtx, cancelStream := context.WithCancel(context.Background())

	// Simpan context cancel ke state cache (groupCall kita isi nil karena bot tidak bisa close call)
	cache.SetLiveActive(videoID, cancelStream, nil)

	go func() {
		defer cache.StopLiveStream(videoID)
		err := streamer.PushToRTMP(streamCtx, videoTarget.URL, audioTargetURL, rtmpEndpoint, logger)

		if err != nil && streamCtx.Err() == nil {
			logger.Error("Stream terputus", zap.Error(err))
			_ = media.EditWithMarkup(context.Background(), client, peer, msgID, "🛑 Stream Terputus karena kesalahan jaringan atau video selesai.", nil)
		} else {
			_ = media.EditWithMarkup(context.Background(), client, peer, msgID, "⏹ Stream video telah selesai atau dihentikan.", nil)
		}
	}()

	return nil
}

// handleStopLive menangani saat user menekan tombol "Hentikan Stream"
func handleStopLive(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, update *tg.UpdateBotCallbackQuery, rawData string, logger *zap.Logger) error {
	videoID := strings.TrimPrefix(rawData, "ytstop_")

	_, ok := cache.GetLiveSession(videoID)
	if !ok {
		media.AnswerCallback(ctx, client, update.QueryID, "Sesi tidak ditemukan atau sudah berhenti.", true)
		return nil
	}

	media.AnswerCallback(ctx, client, update.QueryID, "Menghentikan streaming...", false)

	// Hentikan FFmpeg Muxer (Cleanup Resource STB)
	cache.StopLiveStream(videoID)

	if err := media.EditWithMarkup(ctx, client, peer, msgID, "⏹ Livestream telah dihentikan secara manual.", nil); err != nil {
		logger.Warn("Gagal mengedit pesan stop stream", zap.Error(err))
	}

	return nil
}
