package commands

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/cache"
	"mybot/internal/config"
	"mybot/internal/media"
)

// GetPeerFromMessage dengan auto-resolve dan cache
func GetPeerFromMessage(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities) (tg.InputPeerClass, error) {
	switch p := msg.PeerID.(type) {
	case *tg.PeerUser:
		if u, exists := entities.Users[p.UserID]; exists {
			return u.AsInputPeer(), nil
		}
		accessHash, err := GetUserAccessHash(ctx, client, p.UserID)
		if err != nil {
			return nil, fmt.Errorf("gagal dapat access hash user %d: %w", p.UserID, err)
		}
		return &tg.InputPeerUser{UserID: p.UserID, AccessHash: accessHash}, nil

	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: p.ChatID}, nil

	case *tg.PeerChannel:
		// 1. Cek entities dulu
		if ch, exists := entities.Channels[p.ChannelID]; exists {
			// Simpan ke cache untuk next time
			cache.SaveChannel(ch.ID, ch.AccessHash, ch.Title, ch.Username)
			return ch.AsInputPeer(), nil
		}

		// 2. Cek cache
		if accessHash, ok := cache.GetChannelAccessHash(p.ChannelID); ok {
			return &tg.InputPeerChannel{
				ChannelID:  p.ChannelID,
				AccessHash: accessHash,
			}, nil
		}

		// 3. Fallback: query ke Telegram
		accessHash, title, username, err := GetChannelInfo(ctx, client, p.ChannelID)
		if err != nil {
			return nil, fmt.Errorf("gagal dapat channel %d: %w", p.ChannelID, err)
		}

		// Simpan ke cache
		cache.SaveChannel(p.ChannelID, accessHash, title, username)

		return &tg.InputPeerChannel{
			ChannelID:  p.ChannelID,
			AccessHash: accessHash,
		}, nil
	}

	return nil, fmt.Errorf("unknown peer type: %T", msg.PeerID)
}

// GetChannelInfo mendapatkan info channel dari Telegram
func GetChannelInfo(ctx context.Context, client *tg.Client, channelID int64) (accessHash int64, title, username string, err error) {
	result, err := client.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: 0, // Bisa 0 jika sudah pernah join
		},
	})
	if err != nil {
		return 0, "", "", fmt.Errorf("ChannelsGetChannels failed: %w", err)
	}

	chats := result.GetChats()
	if len(chats) == 0 {
		return 0, "", "", fmt.Errorf("channel not found")
	}

	channel, ok := chats[0].(*tg.Channel)
	if !ok {
		return 0, "", "", fmt.Errorf("not a channel")
	}

	return channel.AccessHash, channel.Title, channel.Username, nil
}

// getTopicID mengambil topic ID dari pesan forum.
// Mengembalikan 0 jika bukan forum topic atau General topic (id=1).
func getTopicID(msg *tg.Message) int {
	if msg.ReplyTo == nil {
		return 0
	}
	rh, ok := msg.ReplyTo.(*tg.MessageReplyHeader)
	if !ok || !rh.ForumTopic {
		return 0
	}
	// Pesan adalah reply ke pesan lain dalam topic → ReplyToTopID = topic ID
	if topID, ok := rh.GetReplyToTopID(); ok && topID != 0 {
		return topID
	}
	// Pesan langsung di topic (bukan reply ke pesan lain) → ReplyToMsgID IS the topic ID
	if msgID, ok := rh.GetReplyToMsgID(); ok && msgID != 0 {
		return msgID
	}
	return 0
}

// buildReplyTo membuat InputReplyToMessage untuk reply ke pesan tertentu.
// TopMsgID di-set hanya jika topicID > 1 dan berbeda dari replyToMsgID.
func buildReplyTo(replyToMsgID int, topicID int) *tg.InputReplyToMessage {
	r := &tg.InputReplyToMessage{
		ReplyToMsgID: replyToMsgID,
	}
	if topicID > 1 && topicID != replyToMsgID {
		r.SetTopMsgID(topicID)
	}
	return r
}

// deleteGroupMessage menghapus pesan di grup/supergroup/channel.
func deleteGroupMessage(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int) error {
	switch p := peer.(type) {
	case *tg.InputPeerChannel:
		_, err := client.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			ID:      []int{msgID},
		})
		return err
	default:
		_, err := client.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
			Revoke: true,
			ID:     []int{msgID},
		})
		return err
	}
}

// sendGroupText mengirim pesan teks sebagai reply di grup/topic.
func sendGroupText(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, text string, replyTo *tg.InputReplyToMessage) error {
	randID, err := media.RandomID()
	if err != nil {
		// fallback jika error (sangat jarang)
		randID = time.Now().UnixNano()
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: randID,
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err = client.MessagesSendMessage(ctx, req)
	return err
}

// HandleGroupDL menangani command /dl <url> di grup/supergroup (termasuk forum topic).
func HandleGroupDL(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer grup", zap.Error(err))
		return err
	}

	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)

	if config.IsTeraboxLink(url) { // <-- Ubah menjadi config.IsTeraboxLink
		return HandleTerabox(ctx, client, msg, entities, url)
	}

	// Route ke handler yang sesuai (handler akan mengelola loading message sendiri)
	switch {
	case config.IsPlatformURL(url, "tiktok"):
		return HandleTikTok(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "instagram"):
		return HandleInstagram(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "lulustream"):
		return HandleLulustream(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "facebook"):
		return HandleFacebook(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "douyin"):
		return HandleDouyin(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "mediafire"):
		return HandleMediaFire(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "aceimg"):
		return HandleAceImg(ctx, client, msg, entities, url, logger)
	case config.IsPlatformURL(url, "twitter"):
		return HandleTwitter(ctx, client, msg, entities, url, logger)
	default:
		if err := sendGroupText(ctx, client, peer, "❌ URL tidak dikenali. Platform yang didukung:\n• TikTok\n• Instagram\n• Facebook\n• Douyin\n• Twitter\n• Lulustream\n• Terabox\n• MediaFire\n• AceImg", replyTo); err != nil {
			logger.Error("Gagal kirim pesan error", zap.Error(err))
		}
		return fmt.Errorf("unsupported URL: %s", url)
	}
}

// kirimAlbumStreamGroup mengirim album foto dengan reply support untuk grup/topic.
func kirimAlbumStreamGroup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, title string, imageURLs []string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	if len(imageURLs) == 0 {
		logger.Warn("Tidak ada gambar untuk dikirim")
		return fmt.Errorf("no images to send")
	}

	mediaSender := media.NewMediaSender(client)

	const maxAlbumSize = 10
	batches := splitIntoBatches(imageURLs, maxAlbumSize)
	httpClient := &http.Client{
		Timeout: 2 * time.Minute,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	for batchIdx, batch := range batches {
		logger.Info("Processing batch",
			zap.Int("batch", batchIdx+1),
			zap.Int("total_batches", len(batches)),
			zap.Int("images_in_batch", len(batch)),
		)

		readers := make([]io.Reader, 0, len(batch))
		filenames := make([]string, 0, len(batch))
		captions := make([]string, 0, len(batch))

		for i, imgURL := range batch {
			// Download gambar
			req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
			if err != nil {
				logger.Warn("Gagal buat request gambar",
					zap.String("url", imgURL),
					zap.Error(err),
				)
				continue
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				logger.Warn("Gagal download gambar",
					zap.String("url", imgURL),
					zap.Error(err),
				)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				logger.Warn("Gagal baca body gambar", zap.Error(err))
				continue
			}

			// Konversi WebP ke JPEG jika perlu
			if isWebP(body) {
				convertedBody, err := convertWebPToJPEG(body, logger)
				if err != nil {
					logger.Warn("Gagal konversi WebP",
						zap.String("url", imgURL),
						zap.Error(err),
					)
					continue
				}
				body = convertedBody
			}

			readers = append(readers, bytes.NewReader(body))
			filenames = append(filenames, fmt.Sprintf("img_%d_%d.jpg", batchIdx, i))

			// Caption hanya di foto pertama
			if batchIdx == 0 && i == 0 {
				captions = append(captions, fmt.Sprintf("🖼️ %s\n\n@Kometika_bot", title))
			} else {
				captions = append(captions, "")
			}
		}

		if len(readers) == 0 {
			logger.Warn("Tidak ada gambar valid untuk batch",
				zap.Int("batch", batchIdx+1),
			)
			continue
		}

		// Reply hanya untuk batch pertama
		var batchReplyTo *tg.InputReplyToMessage
		if batchIdx == 0 {
			batchReplyTo = replyTo
		}

		// Kirim album
		if err := mediaSender.SendPhotoAlbumStream(ctx, peer, readers, filenames, captions, batchReplyTo); err != nil {
			logger.Error("Gagal kirim album batch",
				zap.Int("batch", batchIdx+1),
				zap.Error(err),
			)
			// Continue ke batch berikutnya
			continue
		}

		logger.Info("Batch berhasil dikirim",
			zap.Int("batch", batchIdx+1),
		)

		// Delay antar batch untuk avoid flood
		if batchIdx < len(batches)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}

// isWebP mengecek apakah data adalah format WebP
func isWebP(data []byte) bool {
	return len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WEBP"
}

// convertWebPToJPEG mengkonversi WebP ke JPEG
func convertWebPToJPEG(data []byte, logger *zap.Logger) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode webp: %w", err)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}

	logger.Debug("WebP converted to JPEG",
		zap.Int("original_size", len(data)),
		zap.Int("converted_size", buf.Len()),
	)

	return buf.Bytes(), nil
}
