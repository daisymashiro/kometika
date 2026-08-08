package commands

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"mybot/internal/config"

	"github.com/gotd/td/telegram/message"
	htmlparser "github.com/gotd/td/telegram/message/html"
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

// normalizeFeatureName mengubah alias (fb, ig, tt) menjadi nama fitur penuh
func normalizeFeatureName(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "fb":
		return "facebook"
	case "ig":
		return "instagram"
	case "tt":
		return "tiktok"
	case "tw", "x":
		return "twitter"
	case "lulu":
		return "lulustream"
	case "tera":
		return "terabox"
	case "dy":
		return "douyin"
	}
	return f
}

func HandleFeaturesCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer untuk command features", zap.Error(err))
		return err
	}

	fm := config.GetFeatureManager()

	var sb strings.Builder
	sb.WriteString("<b>📋 Status Fitur Downloader</b>\n\n")

	featureList := []string{"tiktok", "instagram", "facebook", "twitter", "douyin", "terabox", "mediafire", "aceimg", "lulustream", "droplink"}
	for _, feature := range featureList {
		statusIcon := "❌ Nonaktif"
		if fm.IsEnabled(feature) {
			statusIcon = "✅ Aktif"
		}
		// Rata kiri: nama fitur maks 14 karakter, lalu spasi, lalu status
		sb.WriteString(fmt.Sprintf("<code>%-14s</code> %s\n", strings.Title(feature), statusIcon))
	}

	sb.WriteString("\n🔒 Hanya owner yang dapat mengubah status fitur.")

	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)
	_, err = msgSender.StyledText(ctx, htmlparser.String(nil, sb.String()))
	return err
}

func HandleListStatusCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	return HandleFeaturesCommand(ctx, client, msg, entities, logger)
}

func HandleFeatureOnCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	if peer == nil {
		return nil
	}
	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)

	if len(args) < 1 {
		_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, "⚠️ <b>Penggunaan:</b> /on &lt;nama_fitur&gt;\nContoh: <code>/on tiktok</code> atau <code>/on tt</code>"))
		return nil
	}

	feature := normalizeFeatureName(args[0])
	fm := config.GetFeatureManager()

	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "douyin", "terabox", "mediafire", "aceimg", "lulustream", "droplink"}
	if !slices.Contains(validFeatures, feature) {
		_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("❌ Fitur <b>%s</b> tidak ditemukan.\nFitur yang tersedia: <code>%s</code>", feature, strings.Join(validFeatures, ", "))))
		return nil
	}

	fm.Enable(feature)
	logger.Info("Fitur diaktifkan oleh owner", zap.String("feature", feature))
	_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("✅ Fitur <b>%s</b> telah <u>diaktifkan</u>.", strings.Title(feature))))
	return nil
}

func HandleFeatureOffCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	if peer == nil {
		return nil
	}
	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)

	if len(args) < 1 {
		_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, "⚠️ <b>Penggunaan:</b> /off &lt;nama_fitur&gt;\nContoh: <code>/off tiktok</code> atau <code>/off tt</code>"))
		return nil
	}

	feature := normalizeFeatureName(args[0])
	fm := config.GetFeatureManager()

	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "douyin", "terabox", "mediafire", "aceimg", "lulustream", "droplink"}
	if !slices.Contains(validFeatures, feature) {
		_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("❌ Fitur <b>%s</b> tidak ditemukan.\nFitur yang tersedia: <code>%s</code>", feature, strings.Join(validFeatures, ", "))))
		return nil
	}

	fm.Disable(feature)
	logger.Info("Fitur dinonaktifkan oleh owner", zap.String("feature", feature))
	_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("❌ Fitur <b>%s</b> telah <u>dinonaktifkan</u>.", strings.Title(feature))))
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
