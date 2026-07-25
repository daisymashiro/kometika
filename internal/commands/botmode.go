package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/cache"
	"mybot/internal/media"
)

// getBotModeStatusText menghasilkan teks status dinamis yang bisa dipakai berulang
func getBotModeStatusText() string {
	statusInfo := cache.GetOwnerStatusInfo()
	onlineStatus := "🔴 OFFLINE"
	if statusInfo["online"].(bool) {
		onlineStatus = "🟢 ONLINE"
	}

	shouldReply := "❌ TIDAK"
	if statusInfo["should_reply"].(bool) {
		shouldReply = "✅ YA"
	}

	return fmt.Sprintf(`ℹ️ Status Bot Auto-Reply

Mode saat ini: %s
Status Owner: %s
Aktivitas Terakhir: %v
Bot Akan Membalas: %s

Pilih mode operasi bot di bawah ini:`,
		strings.ToUpper(statusInfo["mode"].(string)), onlineStatus, statusInfo["last_activity"], shouldReply)
}

// buildBotModeMarkup membuat tombol inline 🤖 Auto dan 🔧 Manual
func buildBotModeMarkup() *tg.ReplyInlineMarkup {
	return &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{
			{
				Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonCallback{Text: "🤖 Auto", Data: []byte("botmode_auto")},
					&tg.KeyboardButtonCallback{Text: "🔧 Manual", Data: []byte("botmode_manual")},
				},
			},
		},
	}
}

// HandleBotModeCommand menangani command utama (.botmode)
func HandleBotModeCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, ownerID int64, logger *zap.Logger) error {
	// 1. PASTIKAN HANYA DI PRIVATE CHAT
	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		if peer != nil {
			sender := message.NewSender(client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "🚫 Command ini hanya dapat digunakan di Private Chat (DM) dengan bot.")
		}
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return err
	}

	// 2. CEK AKSES OWNER
	var userID int64
	if p, ok := msg.PeerID.(*tg.PeerUser); ok {
		userID = p.UserID
	} else if msg.FromID != nil {
		if pFrom, ok := msg.FromID.(*tg.PeerUser); ok {
			userID = pFrom.UserID
		}
	}

	if userID != ownerID {
		sender := message.NewSender(client)
		_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "❌ Command ini hanya bisa digunakan oleh owner.")
		logger.Warn("Unauthorized botmode access", zap.Int64("user_id", userID))
		return nil
	}

	// 3. KIRIM PESAN DENGAN TOMBOL INLINE
	req := &tg.MessagesSendMessageRequest{
		Peer:        peer,
		Message:     getBotModeStatusText(),
		RandomID:    time.Now().UnixNano(),
		ReplyTo:     &tg.InputReplyToMessage{ReplyToMsgID: msg.ID},
		ReplyMarkup: buildBotModeMarkup(),
	}

	updates, err := client.MessagesSendMessage(ctx, req)
	if err != nil {
		logger.Error("Gagal mengirim menu botmode", zap.Error(err))
		return err
	}

	// 4. EKSTRAK MSG ID & JALANKAN PEMBERSIHAN OTOMATIS (2 MENIT)
	if updates != nil {
		msgID, errExt := media.ExtractMessageID(updates)
		if errExt == nil && msgID != 0 {
			go scheduleBotModeButtonCleanup(client, peer, msgID, logger)
		}
	}

	return nil
}

// HandleBotModeCallback menangani aksi saat tombol diklik
func HandleBotModeCallback(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, query *tg.UpdateBotCallbackQuery, logger *zap.Logger) error {
	data := string(query.Data)

	switch data {
	case "botmode_auto":
		cache.SetBotMode("auto")
		media.AnswerCallback(ctx, client, query.QueryID, "🤖 Mode diubah ke AUTO", false)
		logger.Info("Bot mode changed to AUTO via button")

	case "botmode_manual":
		cache.SetBotMode("manual")
		media.AnswerCallback(ctx, client, query.QueryID, "🔧 Mode diubah ke MANUAL", false)
		logger.Info("Bot mode changed to MANUAL via button")
	}

	newText := getBotModeStatusText()
	markup := buildBotModeMarkup()

	_, err := client.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:        peer,
		ID:          msgID,
		Message:     newText,
		ReplyMarkup: markup,
	})

	if err != nil {
		if tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
			return nil
		}
		logger.Warn("Gagal mengedit pesan botmode", zap.Error(err))
	}
	return nil
}

// scheduleBotModeButtonCleanup menghapus tombol seletah 2 menit menggunakan context-aware cleanup
func scheduleBotModeButtonCleanup(client *tg.Client, peer tg.InputPeerClass, msgID int, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	select {
	case <-time.After(2 * time.Minute):
		// Waktu 2 menit habis, mulai eksekusi penghapusan tombol
		editCtx, editCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer editCancel()

		for {
			// Mengirim request edit dengan ReplyKeyboardHide (menghilangkan tombol inline)
			_, err := client.MessagesEditMessage(editCtx, &tg.MessagesEditMessageRequest{
				Peer:        peer,
				ID:          msgID,
				ReplyMarkup: &tg.ReplyKeyboardHide{},
			})

			if err != nil {
				// Tangani pembatasan spam (FloodWait) dari Telegram
				if d, ok := tgerr.AsFloodWait(err); ok {
					logger.Warn("FloodWait saat hapus tombol botmode", zap.Duration("wait", d))
					select {
					case <-time.After(d + time.Second):
						continue // Coba lagi setelah cooldown
					case <-editCtx.Done():
						logger.Warn("Gagal menghapus tombol botmode (timeout)", zap.Int("msg_id", msgID))
						return
					}
				}
				logger.Warn("Gagal menghapus tombol botmode", zap.Int("msg_id", msgID), zap.Error(err))
				return
			}

			break // Berhasil dihapus, keluar dari loop
		}

		logger.Info("Tombol menu botmode dihapus otomatis", zap.Int("msg_id", msgID))

	case <-ctx.Done():
		// Terjadi pembatalan context secara eksternal
		return
	}
}
