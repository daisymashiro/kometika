package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api/youtube"
	"mybot/internal/cache"
	"mybot/internal/streamer"
)

// HandleYouTubeLive merespons perintah khusus untuk memulai livestreaming ke grup secara langsung
func HandleYouTubeLive(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return fmt.Errorf("gagal mendapatkan peer: %w", err)
	}

	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	// 1. Ekstrak Data dari YouTube
	_ = sendGroupText(ctx, client, peer, "🔄 Mengambil data stream YouTube.", replyTo)
	ytData, err := youtube.FetchYouTubeData(url)
	if err != nil {
		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengambil data YouTube.", replyTo)
		return err
	}

	if len(ytData.Videos) == 0 || len(ytData.Audios) == 0 {
		return fmt.Errorf("ketersediaan video atau audio tidak lengkap")
	}

	// Filter: RTMP FLV mensyaratkan Audio AAC (M4A) dan Video MP4 (H264).
	var videoURL, audioURL string
	for _, v := range ytData.Videos {
		if strings.Contains(strings.ToLower(v.Format), "mp4") {
			videoURL = v.URL
			break // Ambil kualitas yang diranking paling atas oleh vidssave
		}
	}
	for _, a := range ytData.Audios {
		if strings.Contains(strings.ToLower(a.Format), "m4a") || strings.Contains(strings.ToLower(a.Format), "aac") {
			audioURL = a.URL
			break
		}
	}

	if videoURL == "" || audioURL == "" {
		_ = sendGroupText(ctx, client, peer, "❌ Format MP4/M4A tidak ditemukan. RTMP membutuhkan codec H264/AAC.", replyTo)
		return fmt.Errorf("format tidak didukung untuk zero-copy RTMP")
	}

	// 2. Inisialisasi Group Call RTMP di Telegram
	_ = sendGroupText(ctx, client, peer, "📡 Membuka Group Call dan Endpoint RTMP.", replyTo)

	callUpdates, err := client.PhoneCreateGroupCall(ctx, &tg.PhoneCreateGroupCallRequest{
		Peer:       peer,
		RtmpStream: true,
		RandomID:   rand.Int(),
	})
	if err != nil {
		logger.Error("Gagal membuat Group Call", zap.Error(err))
		return err
	}

	// Ekstrak InputGroupCall agar 'callUpdates' terpakai
	var groupCall tg.InputGroupCallClass
	if updates, ok := callUpdates.(*tg.Updates); ok {
		for _, u := range updates.Updates {
			if callUpdate, ok := u.(*tg.UpdateGroupCall); ok {
				if call, ok := callUpdate.Call.(*tg.GroupCall); ok {
					groupCall = &tg.InputGroupCall{
						ID:         call.ID,
						AccessHash: call.AccessHash,
					}
					break
				}
			}
		}
	}

	rtmpData, err := client.PhoneGetGroupCallStreamRtmpURL(ctx, &tg.PhoneGetGroupCallStreamRtmpURLRequest{
		Peer:      peer,
		Revoke:    false,
		LiveStory: false,
	})
	if err != nil {
		logger.Error("Gagal mendapatkan URL RTMP", zap.Error(err))
		return err
	}
	rtmpEndpoint := fmt.Sprintf("%s/%s", rtmpData.URL, rtmpData.Key)

	streamCtx, cancelStream := context.WithCancel(context.Background())

	// 3. Masukkan ke cache (Wajib SetLiveSession dulu sebelum SetLiveActive untuk direct command)
	cache.SetLiveSession(ytData.ID, ytData, url)
	cache.SetLiveActive(ytData.ID, cancelStream, groupCall)

	go func() {
		defer cache.StopLiveStream(ytData.ID)
		err := streamer.PushToRTMP(streamCtx, videoURL, audioURL, rtmpEndpoint, logger)
		if err != nil {
			logger.Error("Stream gagal di tengah jalan", zap.Error(err))
		}
	}()

	caption := fmt.Sprintf("▶️ Livestream Dimulai!\n\n🎬 Judul: %s\n\nSilakan bergabung ke Voice Chat grup untuk menonton.", ytData.Title)
	_ = sendGroupText(ctx, client, peer, caption, replyTo)

	return nil
}
