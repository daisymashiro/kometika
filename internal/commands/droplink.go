package commands

import (
	"context"
	"fmt"
	"time"

	"droplink"

	"mybot/internal/config"
	"mybot/internal/media"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// HandleDroplink unlocks a droplink.co short link via headless Chromium
// (droplink package) and sends the final destination URL as a reply.
// Hanya diproses di private chat (DM); link di grup/channel diabaikan.
func HandleDroplink(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string) error {
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("droplink") {
		return nil
	}

	// Batasan: hanya bot private chat (DM), bukan grup/channel.
	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return fmt.Errorf("gagal dapatkan peer: %w", err)
	}
	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)

	send := func(text string) error {
		req := &tg.MessagesSendMessageRequest{
			Peer:     peer,
			Message:  text,
			RandomID: time.Now().UnixNano(),
		}
		if replyTo != nil {
			req.SetReplyTo(replyTo)
		}
		_, err := client.MessagesSendMessage(ctx, req)
		return err
	}

	code, err := droplink.ParseCode(url)
	if err != nil {
		return send("❌ Link droplink tidak valid: " + url)
	}

	// Pesan loading — dihapus setelah hasil (link asli / error) terkirim.
	loadingReq := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  "⏳ Membuka link droplink, tunggu ±30 detik...",
		RandomID: time.Now().UnixNano(),
	}
	if replyTo != nil {
		loadingReq.SetReplyTo(replyTo)
	}
	loadingUpdates, _ := client.MessagesSendMessage(ctx, loadingReq)
	var loadingMsgID int
	if loadingUpdates != nil {
		loadingMsgID, _ = media.ExtractMessageID(loadingUpdates)
	}
	defer func() {
		if loadingMsgID != 0 {
			_ = deleteGroupMessage(ctx, client, peer, loadingMsgID)
		}
	}()

	res, err := droplink.Unlock(ctx, code, droplink.Options{
		Mode: droplink.ModeFast,
		Logf: func(format string, args ...any) {
			zap.L().Info("droplink: " + fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		return send("❌ Gagal membuka droplink: " + err.Error())
	}

	if res.URL != "" {
		msgText := fmt.Sprintf("✅ Link berhasil dibuka (t = %.0fs)\n%s", res.Elapsed.Seconds(), res.URL)
		if res.Title != "" {
			msgText += "\n📄 " + res.Title
		}
		return send(msgText)
	}
	return send("⚠️ Link tidak berhasil diekstrak.\nTip: coba lagi nanti.")
}