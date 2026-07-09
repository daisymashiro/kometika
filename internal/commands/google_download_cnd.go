package commands

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
	"mybot/internal/media"
)

func HandleGDNCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	text := msg.Message
	lowerText := strings.ToLower(text)

	prefixes := []string{".gdn", "/gdn", "!gdn", "#gdn"}
	validPrefix := false
	for _, p := range prefixes {
		if strings.HasPrefix(lowerText, p) {
			validPrefix = true
			break
		}
	}

	if !validPrefix {
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer di GDN", zap.Error(err))
		return err
	}
	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)

	parts := strings.Fields(text)
	if len(parts) < 2 {
		_ = sendGroupText(ctx, client, peer, "❌ Format salah.\nGunakan: .gdn url", replyTo)
		return nil
	}
	targetURL := parts[1]

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		_ = sendGroupText(ctx, client, peer, "❌ URL tidak valid. Harus diawali http:// atau https://", replyTo)
		return nil
	}

	msgSender := message.NewSender(client)
	var progressMsgID int
	if _, isUser := msg.PeerID.(*tg.PeerUser); isUser {
		pMsg, err := msgSender.To(peer).Text(ctx, "⏳ Mengunduh stream dari CDN, mohon tunggu...")
		if err == nil {
			progressMsgID, _ = media.ExtractMessageID(pMsg)
		}
	} else {
		pMsg, err := msgSender.To(peer).Reply(msg.ID).Text(ctx, "⏳ Mengunduh stream dari CDN, mohon tunggu...")
		if err == nil {
			progressMsgID, _ = media.ExtractMessageID(pMsg)
		}
	}

	defer func() {
		if progressMsgID != 0 {
			go func() {
				time.Sleep(2 * time.Second) // Memberi jeda waktu agar user sempat membaca status
				delCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if errDel := deleteGroupMessage(delCtx, client, peer, progressMsgID); errDel != nil {
					logger.Debug("Gagal menghapus pesan progress GDN (mungkin sudah terhapus)", zap.Error(errDel))
				}
			}()
		}
	}()

	var stream io.ReadCloser
	var httpContentType string // Variabel penampung Content-Type cadangan jika deteksi biner buntu
	isM3U8 := strings.Contains(strings.ToLower(targetURL), ".m3u8")

	if isM3U8 {
		var errM3U8 error
		// FIX BUG 1: Mengirimkan pointer logger ke FFmpeg compiler
		stream, errM3U8 = api.GetM3U8ToMP4Stream(ctx, targetURL, logger)
		if errM3U8 != nil {
			logger.Error("Gagal inisialisasi FFmpeg untuk M3U8", zap.Error(errM3U8))
			log.LogError(ctx, "GDN_M3U8Open", errM3U8, "url="+targetURL)
			_ = sendGroupText(ctx, client, peer, "❌ Gagal memproses M3U8 Stream melalui FFmpeg.", replyTo)
			return nil
		}
	} else {
		var errStream error
		// FIX BUG 3: Membaca stream menggunakan fungsi baru pembaca Header HTTP
		stream, _, httpContentType, errStream = api.GetVideoStreamWithHeader(ctx, targetURL)
		if errStream != nil {
			logger.Error("Gagal membuka URL CDN", zap.Error(errStream))
			log.LogError(ctx, "GDN_StreamOpen", errStream, "url="+targetURL)
			_ = sendGroupText(ctx, client, peer, "❌ Gagal membuka koneksi ke URL tersebut.", replyTo)
			return nil
		}
	}
	defer stream.Close()

	var info api.ContentTypeInfo
	var fullStream io.Reader

	if isM3U8 {
		info = api.ContentTypeInfo{
			MimeType:  "video/mp4",
			Category:  api.ContentVideo,
			Extension: ".mp4",
		}
		fullStream = stream
	} else {
		var errDetect error
		info, fullStream, errDetect = api.DetectAndClassifyStream(stream)

		// FIX BUG 3: Jika magic number gagal mendeteksi (menghasilkan binary/unknown) atau terjadi error pembacaan awal
		if errDetect != nil || info.Category == api.ContentUnknown || info.Category == api.ContentBinary {
			// Periksa apakah HTTP Content-Type bawaan CDN valid untuk dipergunakan
			if httpContentType != "" && !strings.Contains(httpContentType, "octet-stream") && !strings.Contains(httpContentType, "application/json") {
				info = api.GetContentTypeInfo(httpContentType)
				logger.Info("Magic number gagal, fallback ke HTTP Header sukses", zap.String("mime", info.MimeType))
			} else {
				// Paksa asumsi aman ke Video MP4 jika tipe konten benar-benar tidak terbaca namun dipanggil via .gdn
				info = api.ContentTypeInfo{
					MimeType:  "video/mp4",
					Category:  api.ContentVideo,
					Extension: ".mp4",
				}
				logger.Info("Fallback total diaktifkan: Dipaksa menjadi video/mp4")
			}
		}
	}

	filename := "gdn_download"
	parsedURL, errURL := url.Parse(targetURL)
	if errURL == nil {
		baseName := path.Base(parsedURL.Path)
		if baseName != "" && baseName != "/" && baseName != "." && len(baseName) < 50 {
			filename = baseName
		}
	}
	if !strings.Contains(filename, ".") {
		filename += info.Extension
	}

	var thumbFile tg.InputFileClass
	if info.Category == api.ContentVideo {
		defaultThumbURL := "https://i.pinimg.com/736x/54/62/78/5462782f3870037d8e3378df6719c00b.jpg"
		thumbBytes, errThumb := api.GetThumbnail(ctx, defaultThumbURL)
		if errThumb == nil && len(thumbBytes) > 0 {
			up := uploader.NewUploader(client).WithThreads(1)
			thumbFile, _ = up.FromBytes(ctx, "default_thumb.jpg", thumbBytes)
		}
	}

	caption := fmt.Sprintf("📥 Google Download CDN\n📦 File: %s\n\n@Kometika_bot", filename)

	mediaSender := media.NewMediaSender(client)
	_, err = mediaSender.SendDynamicStream(
		ctx,
		peer,
		fullStream,
		info,
		filename,
		caption,
		nil,
		replyTo,
		thumbFile,
	)

	if err != nil {
		logger.Error("Gagal mengirim file GDN", zap.Error(err))
		// Integrasi penuh dengan internal log milikmu menggunakan format Blockquote Telegram asli
		log.LogError(ctx, "GDN_SendFile", err, "url="+targetURL, "file="+filename, "mime_type="+info.MimeType)

		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengirim file dari CDN. Koneksi terputus atau file terlalu besar.", replyTo)
		return nil
	}

	logger.Info("File GDN berhasil dikirim", zap.String("url", targetURL), zap.String("file", filename))
	log.LogInfo(ctx, fmt.Sprintf("✅ GDN Berhasil Mengunduh\nFile: %s\nURL: %s", filename, targetURL))

	return nil
}
