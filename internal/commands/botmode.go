package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/cache"
)

// HandleBotModeCommand menangani command untuk toggle mode bot auto-reply
// Command: .botmode auto|manual|status
// Hanya bisa digunakan oleh owner
func HandleBotModeCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, ownerID int64, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		return err
	}

	// Cek apakah yang eksekusi adalah owner
	var userID int64
	if msg.FromID != nil {
		if p, ok := msg.FromID.(*tg.PeerUser); ok {
			userID = p.UserID
		}
	}

	if userID != ownerID {
		replyTo := buildReplyTo(msg.ID, getTopicID(msg))
		_ = sendGroupText(ctx, client, peer, "❌ Command ini hanya bisa digunakan oleh owner.", replyTo)
		logger.Warn("Unauthorized botmode access", zap.Int64("user_id", userID))
		return nil
	}

	// Parse argumen
	parts := strings.Fields(msg.Message)
	if len(parts) < 2 {
		replyTo := buildReplyTo(msg.ID, getTopicID(msg))
		helpText := `📋 Bot Mode Command

Usage:
• botmode auto - Bot auto-reply saat owner offline
• botmode manual - Bot tidak auto-reply sama sekali
• botmode status - Lihat status bot saat ini

Mode saat ini:` + cache.GetBotMode() + ``

		_ = sendGroupText(ctx, client, peer, helpText, replyTo)
		return nil
	}

	mode := strings.ToLower(parts[1])
	replyTo := buildReplyTo(msg.ID, getTopicID(msg))

	switch mode {
	case "auto":
		cache.SetBotMode("auto")
		response := `✅ Bot Mode: AUTO

Bot akan otomatis membalas pesan customer saat owner offline.

Deteksi online/offline menggunakan:
• Telegram status update (real-time)
• Activity tracking (30 detik grace period)

Bot tidak akan balas jika:
• Owner sedang online
• Owner aktif dalam 30 detik terakhir`

		_ = sendGroupText(ctx, client, peer, response, replyTo)
		logger.Info("Bot mode changed to AUTO", zap.Int64("by_user", userID))

	case "manual":
		cache.SetBotMode("manual")
		response := `🔕 Bot Mode: MANUAL

Bot tidak akan auto-reply sama sekali.

Gunakan mode ini jika:
• Kamu ingin handle semua chat sendiri
• Sedang testing
• Bot auto-reply bermasalah

Untuk mengaktifkan kembali: .botmode auto`

		_ = sendGroupText(ctx, client, peer, response, replyTo)
		logger.Info("Bot mode changed to MANUAL", zap.Int64("by_user", userID))

	case "status":
		statusInfo := cache.GetOwnerStatusInfo()

		onlineStatus := "🔴 OFFLINE"
		if statusInfo["online"].(bool) {
			onlineStatus = "🟢 ONLINE"
		}

		shouldReply := "❌ TIDAK"
		if statusInfo["should_reply"].(bool) {
			shouldReply = "✅ YA"
		}

		response := fmt.Sprintf(`📊 Bot Status

Mode: %s
Owner Status: %s
Last Activity: %v
Bot Should Reply: %s

Penjelasan:
Bot akan auto-reply jika mode AUTO dan owner offline lebih dari 30 detik.`,
			statusInfo["mode"].(string),
			onlineStatus,
			statusInfo["last_activity"],
			shouldReply,
		)

		_ = sendGroupText(ctx, client, peer, response, replyTo)
		logger.Info("Bot status requested", zap.Int64("by_user", userID))

	default:
		response := fmt.Sprintf("❌ Mode tidak valid: %s \n\nGunakan: auto, manual, atau status", mode)
		_ = sendGroupText(ctx, client, peer, response, replyTo)
	}

	return nil
}
