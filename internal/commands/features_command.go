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
	sb.WriteString("  <b>Status Fitur Downloader</b>\n\n")

	// Urutkan fitur
	featureList := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	for _, feature := range featureList {
		status := "  Nonaktif"

		// PERBAIKAN: Gunakan fungsi IsEnabled() langsung
		if fm.IsEnabled(feature) {
			status = "  Aktif"
		}

		sb.WriteString(fmt.Sprintf("  %s: %s\n", strings.Title(feature), status))
	}

	sb.WriteString("\nHanya owner yang dapat mengubah status fitur")

	// Pastikan bot melakukan Reply ke pesan awal agar topic/pesan spesifik terdeteksi
	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)
	_, err = msgSender.StyledText(ctx, htmlparser.String(nil, sb.String()))
	return err
}

// HandleListStatusCommand menampilkan status semua fitur (alias untuk /features)
func HandleListStatusCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	return HandleFeaturesCommand(ctx, client, msg, entities, logger)
}

// HandleFeatureOnCommand mengaktifkan fitur (hanya owner)
func HandleFeatureOnCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	if peer == nil {
		return nil
	}

	// Buat sender yang me-reply pesan command
	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)

	// Cek apakah argumen tersedia
	if len(args) < 1 {
		_, _ = msgSender.Text(ctx, "  Penggunaan: /on <nama_fitur>\nContoh: /on tiktok (atau /on tt)")
		return nil
	}

	feature := normalizeFeatureName(args[0])
	fm := config.GetFeatureManager()

	// Validasi nama fitur
	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	isValid := slices.Contains(validFeatures, feature)

	if !isValid {
		_, _ = msgSender.Text(ctx, fmt.Sprintf("  Fitur '%s' tidak ditemukan.\nFitur yang tersedia: %s", feature, strings.Join(validFeatures, ", ")))
		return nil
	}

	fm.Enable(feature)
	logger.Info("Fitur diaktifkan oleh owner", zap.String("feature", feature))
	_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("  Fitur %s telah diaktifkan.", strings.Title(feature))))

	return nil
}

// HandleFeatureOffCommand menonaktifkan fitur (hanya owner)
func HandleFeatureOffCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, args []string, logger *zap.Logger) error {
	peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
	if peer == nil {
		return nil
	}

	// Buat sender yang me-reply pesan command
	msgSender := message.NewSender(client).To(peer).Reply(msg.ID)

	if len(args) < 1 {
		_, _ = msgSender.Text(ctx, "  Penggunaan: /off <nama_fitur>\nContoh: /off tiktok (atau /off tt)")
		return nil
	}

	feature := normalizeFeatureName(args[0])
	fm := config.GetFeatureManager()

	// Validasi nama fitur
	validFeatures := []string{"tiktok", "instagram", "facebook", "twitter", "terabox", "mediafire", "aceimg", "lulustream"}
	isValid := slices.Contains(validFeatures, feature)

	if !isValid {
		_, _ = msgSender.Text(ctx, fmt.Sprintf("  Fitur '%s' tidak ditemukan.\nFitur yang tersedia: %s", feature, strings.Join(validFeatures, ", ")))
		return nil
	}

	fm.Disable(feature)
	logger.Info("Fitur dinonaktifkan oleh owner", zap.String("feature", feature))
	_, _ = msgSender.StyledText(ctx, htmlparser.String(nil, fmt.Sprintf("  Fitur %s telah dinonaktifkan.", strings.Title(feature))))

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
