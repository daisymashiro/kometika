package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/mediafire"
	"mybot/internal/assets"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"
)

// isMediaFireURL memeriksa apakah URL termasuk domain MediaFire
func isMediaFireURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "mediafire.com") || strings.Contains(lower, "mediafires.co")
}

// HandleMediaFire otomatis dipanggil saat URL MediaFire terdeteksi di private chat
func HandleMediaFire(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("mediafire") {
		logger.Info("Fitur MediaFire dinonaktifkan")
		return nil
	}

	if !isMediaFireURL(url) {
		return nil
	}

	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		peer, err := GetPeerFromMessage(ctx, client, msg, entities)
		if err != nil || peer == nil {
			logger.Error("Gagal dapatkan peer grup", zap.Error(err))
			return err
		}
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)
		return handleMediaFireGroup(ctx, client, peer, url, replyTo, logger)
	}

	// ── Private chat ──
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer private", zap.Error(err))
		return err
	}

	msgSender := message.NewSender(client)

	progressMsg, err := msgSender.To(peer).Text(ctx, "⏳ Mencari data sabar, mohon tunggu...")
	if err != nil {
		logger.Warn("Gagal kirim progres", zap.Error(err))
	} else {
		progressMsgID, _ := media.ExtractMessageID(progressMsg)
		defer func() {
			if progressMsgID == 0 {
				return
			}
			go func() {
				time.Sleep(1 * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = client.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
					Revoke: true,
					ID:     []int{progressMsgID},
				})
			}()
		}()
	}

	data, err := mediafire.FetchMediaFireData(url)
	if err != nil {
		logger.Warn("Gagal fetch MediaFire", zap.Error(err))
		log.LogWarn(ctx, "MediaFireFetch", err.Error(), "url="+url)
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengambil data dari MediaFire.")
		return nil
	}

	if data.DirectLink == "" {
		log.LogWarn(ctx, "MediaFireNoDirectLink", "Direct link not found", "url="+url)
		_, _ = msgSender.To(peer).Text(ctx, "❌ Tidak ditemukan tautan unduhan.")
		return nil
	}

	err = sendMediaFireFile(ctx, client, peer, data, nil, logger)
	if err != nil {
		logger.Error("Gagal kirim file MediaFire", zap.Error(err))
		log.LogError(ctx, "MediaFireSendFile", err, "url="+url, "id="+data.ID)
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengirim file.")
		return nil
	}

	logger.Info("File MediaFire berhasil dikirim", zap.String("id", data.ID))
	log.LogInfo(ctx, fmt.Sprintf("✅ MediaFire berhasil dikirim\nID: %s\nJudul: %s", data.ID, data.Title))
	return nil
}

// handleMediaFireGroup digunakan untuk grup/supergroup
func handleMediaFireGroup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	data, err := mediafire.FetchMediaFireData(url)
	if err != nil {
		logger.Warn("Gagal fetch MediaFire grup", zap.Error(err))
		log.LogWarn(ctx, "MediaFireGroupFetch", err.Error(), "url="+url)
		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengambil data MediaFire.", replyTo)
		return nil
	}

	if data.DirectLink == "" {
		log.LogWarn(ctx, "MediaFireGroupNoDirectLink", "Direct link not found", "url="+url)
		_ = sendGroupText(ctx, client, peer, "❌ Tidak ditemukan tautan unduhan.", replyTo)
		return nil
	}

	err = sendMediaFireFile(ctx, client, peer, data, replyTo, logger)
	if err != nil {
		logger.Error("Gagal kirim file MediaFire grup", zap.Error(err))
		log.LogError(ctx, "MediaFireGroupSendFile", err, "url="+url, "id="+data.ID)
		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengirim file.", replyTo)
		return nil
	}

	logger.Info("File MediaFire grup berhasil dikirim", zap.String("id", data.ID))
	log.LogInfo(ctx, fmt.Sprintf("✅ MediaFire grup berhasil dikirim\nID: %s\nJudul: %s", data.ID, data.Title))
	return nil
}

func sendMediaFireFile(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, data *mediafire.MediaFireData, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	stream, _, err := api.GetVideoStream(ctx, data.DirectLink)
	if err != nil {
		return fmt.Errorf("gagal membuka stream: %w", err)
	}
	defer stream.Close()

	caption := fmt.Sprintf("📁 %s\n📦 Ukuran: %s\n\n@Kometika_bot", data.Title, data.Size)

	filename := data.Title
	if !strings.Contains(filename, ".") {
		if idx := strings.LastIndex(data.DirectLink, "."); idx != -1 {
			ext := data.DirectLink[idx:]
			if len(ext) < 6 {
				filename += ext
			}
		}
	}
	if filename == "" {
		filename = fmt.Sprintf("mediafire_%s.bin", data.ID)
	}

	// ─── DETEKSI TIPE KONTEN OTOMATIS ──────────────────────────────────────
	info, fullStream, err := api.DetectAndClassifyStream(stream)
	if err != nil {
		logger.Warn("Gagal deteksi tipe konten, fallback ke dokumen", zap.Error(err))
		info = api.ContentTypeInfo{
			MimeType:  "application/octet-stream",
			Category:  api.ContentDocument,
			Extension: "",
		}
		fullStream = stream
	}

	var thumbFile tg.InputFileClass

	// Gunakan embedded thumbnail
	mediaSender := media.NewMediaSender(client)
	uploadedThumb, err := mediaSender.UploadThumbnail(ctx, assets.DefaultThumbnail)
	if err == nil {
		thumbFile = uploadedThumb
		logger.Info("Thumbnail default embedded berhasil disiapkan")
	} else {
		logger.Warn("Gagal upload thumbnail default embedded", zap.Error(err))
	}

	_, err = mediaSender.SendDynamicStream(
		ctx,
		peer,
		fullStream,
		info,
		filename,
		caption,
		nil, // replyMarkup
		replyTo,
		thumbFile,
	)
	return err
}

// HandleDLCommand tetap sama
func HandleDLCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	if _, ok := msg.PeerID.(*tg.PeerUser); ok {
		return nil
	}

	text := msg.Message
	if !strings.HasPrefix(text, ".dl") {
		return nil
	}

	parts := strings.Fields(text)
	if len(parts) < 2 {
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		_ = sendGroupText(ctx, client, peer, "❌ Gunakan: .dl <url_mediafire>", buildReplyTo(msg.ID, getTopicID(msg)))
		return nil
	}
	url := parts[1]
	if !isMediaFireURL(url) {
		peer, _ := GetPeerFromMessage(ctx, client, msg, entities)
		_ = sendGroupText(ctx, client, peer, "❌ Tautan bukan MediaFire.", buildReplyTo(msg.ID, getTopicID(msg)))
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer untuk .dl", zap.Error(err))
		return err
	}
	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)

	_ = sendGroupText(ctx, client, peer, "⏳ Mengambil file MediaFire...", replyTo)
	return handleMediaFireGroup(ctx, client, peer, url, replyTo, logger)
}
