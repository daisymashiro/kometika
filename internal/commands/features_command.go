package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"mybot/internal/config"

	htmlparser "github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// GetOwnerID mendapatkan owner ID dari environment variable
func GetOwnerID() int64 {
	ownerIDStr := os.Getenv("OWNER_ID")
	if ownerIDStr == "" {
		return 0
	}
	ownerID, err := strconv.ParseInt(ownerIDStr, 10, 64)
	if err != nil {
		return 0
	}
	return ownerID
}

// IsOwner memeriksa apakah user adalah owner
func IsOwner(userID int64) bool {
	ownerID := GetOwnerID()
	return ownerID != 0 && userID == ownerID
}

// HandleFeaturesCommand menampilkan status semua fitur
func HandleFeaturesCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer untuk command features", zap.Error(err))
		return err
	}

	fm := config.GetFeatureManager()
	features := fm.GetAll()

	var sb strings.Builder
	sb.WriteString("📋 <b>Status Fitur Downloader</b>\n\n")

	// Urutkan fitur
	featureList := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	
	for _, feature := range featureList {
		status := "❌ Nonaktif"
		if enabled, exists := features[feature]; exists && enabled {
			status = "✅ Aktif"
		}
		sb.WriteString(fmt.Sprintf("• <b>%s</b>: %s\n", strings.Title(feature), status))
	}

	sb.WriteString("\n<i>Hanya owner yang dapat mengubah status fitur</i>")

	msgSender := message.NewSender(client)
	_, err = msgSender.To(peer).StyledText(ctx, htmlparser.String(nil, sb.String()))
	return err
}

// HandleListStatusCommand menampilkan status semua fitur (alias untuk /features)
func HandleListStatusCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	return HandleFeaturesCommand(ctx, client, msg, entities, logger)
}

// HandleFeatureOnCommand mengaktifkan fitur (hanya owner)
func HandleFeatureOnCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	// Cek apakah user adalah owner
	userID := GetUserIDFromMessage(msg)
	if !IsOwner(userID) {
		logger.Warn("Non-owner mencoba mengaktifkan fitur", zap.Int64("user_id", userID))
		
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		if peer != nil {
			msgSender := message.NewSender(client)
			_, _ = msgSender.To(peer).Text(ctx, "❌ Hanya owner yang dapat menggunakan command ini.")
		}
		return nil
	}

	if len(args) < 2 {
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		if peer != nil {
			msgSender := message.NewSender(client)
			_, _ = msgSender.To(peer).Text(ctx, "ℹ️ Penggunaan: /on <nama_fitur>\nContoh: /on tiktok")
		}
		return nil
	}

	feature := strings.ToLower(args[1])
	fm := config.GetFeatureManager()
	
	// Validasi nama fitur
	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	isValid := false
	for _, f := range validFeatures {
		if f == feature {
			isValid = true
			break
		}
	}

	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	msgSender := message.NewSender(client)

	if !isValid {
		if peer != nil {
			_, _ = msgSender.To(peer).Text(ctx, fmt.Sprintf("❌ Fitur '%s' tidak ditemukan.\nFitur yang tersedia: %s", feature, strings.Join(validFeatures, ", ")))
		}
		return nil
	}

	fm.Enable(feature)
	logger.Info("Fitur diaktifkan oleh owner", zap.String("feature", feature), zap.Int64("owner_id", userID))

	if peer != nil {
		_, _ = msgSender.To(peer).StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("✅ Fitur <b>%s</b> telah diaktifkan.", strings.Title(feature))))
	}

	return nil
}

// HandleFeatureOffCommand menonaktifkan fitur (hanya owner)
func HandleFeatureOffCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	// Cek apakah user adalah owner
	userID := GetUserIDFromMessage(msg)
	if !IsOwner(userID) {
		logger.Warn("Non-owner mencoba menonaktifkan fitur", zap.Int64("user_id", userID))
		
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		if peer != nil {
			msgSender := message.NewSender(client)
			_, _ = msgSender.To(peer).Text(ctx, "❌ Hanya owner yang dapat menggunakan command ini.")
		}
		return nil
	}

	if len(args) < 2 {
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		if peer != nil {
			msgSender := message.NewSender(client)
			_, _ = msgSender.To(peer).Text(ctx, "ℹ️ Penggunaan: /off <nama_fitur>\nContoh: /off tiktok")
		}
		return nil
	}

	feature := strings.ToLower(args[1])
	fm := config.GetFeatureManager()
	
	// Validasi nama fitur
	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	isValid := false
	for _, f := range validFeatures {
		if f == feature {
			isValid = true
			break
		}
	}

	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	msgSender := message.NewSender(client)

	if !isValid {
		if peer != nil {
			_, _ = msgSender.To(peer).Text(ctx, fmt.Sprintf("❌ Fitur '%s' tidak ditemukan.\nFitur yang tersedia: %s", feature, strings.Join(validFeatures, ", ")))
		}
		return nil
	}

	fm.Disable(feature)
	logger.Info("Fitur dinonaktifkan oleh owner", zap.String("feature", feature), zap.Int64("owner_id", userID))

	if peer != nil {
		_, _ = msgSender.To(peer).StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("🚫 Fitur <b>%s</b> telah dinonaktifkan.", strings.Title(feature))))
	}

	return nil
}

// GetUserIDFromMessage mendapatkan user ID dari message
func GetUserIDFromMessage(msg *tg.Message) int64 {
	if msg.FromID == nil {
		return 0
	}

	switch from := msg.FromID.(type) {
	case *tg.PeerUser:
		return from.UserID
	default:
		return 0
	}
}
