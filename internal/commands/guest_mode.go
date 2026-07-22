package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/api/facebook"
	"mybot/internal/api/instagram"
	"mybot/internal/api/tiktok"
	"mybot/internal/api/twiter"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"
)

var (
	cachePeer   tg.InputPeerClass
	cachePeerMu sync.Mutex
)

// InitGuestMode dipanggil dari main.go untuk menyetel peer cache
func InitGuestMode(peer tg.InputPeerClass) {
	cachePeerMu.Lock()
	defer cachePeerMu.Unlock()
	cachePeer = peer
}

func getCachePeer() tg.InputPeerClass {
	cachePeerMu.Lock()
	defer cachePeerMu.Unlock()
	return cachePeer
}

// HandleBotGuestChatQuery adalah handler untuk UpdateBotGuestChatQuery.
func HandleBotGuestChatQuery(ctx context.Context, client *tg.Client, upd *tg.UpdateBotGuestChatQuery, logger *zap.Logger) error {
	msg, ok := upd.Message.(*tg.Message)
	if !ok || msg.Message == "" {
		return nil
	}
	text := msg.Message

	var userID int64
	if msg.FromID != nil {
		if peer, ok := msg.FromID.(*tg.PeerUser); ok {
			userID = peer.UserID
		}
	}
	if userID == 0 && msg.PeerID != nil {
		if peer, ok := msg.PeerID.(*tg.PeerUser); ok {
			userID = peer.UserID
		}
	}

	queryID := upd.QueryID
	url := extractURLFromText(text)
	if url == "" {
		return nil
	}

	logger.Info("Guest query", zap.String("url", url), zap.Int64("user_id", userID))
	log.LogInfo(ctx, "📩 Guest query: "+url)

	inlineMsgID, err := sendGuestInlineResult(ctx, client, queryID, "⏳ Memproses URL, mohon tunggu...")
	if err != nil {
		logger.Error("Gagal kirim inline result", zap.Error(err))
		log.LogError(ctx, "GuestSendInline", err, "query_id="+fmt.Sprintf("%d", queryID))
		return err
	}

	go processGuestTask(ctx, client, inlineMsgID, url, logger)
	return nil
}

func sendGuestInlineResult(ctx context.Context, client *tg.Client, queryID int64, text string) (tg.InputBotInlineMessageIDClass, error) {
	resultID := fmt.Sprintf("guest_%d", time.Now().UnixNano())
	result := &tg.InputBotInlineResult{
		ID:    resultID,
		Type:  "article",
		Title: text,
		SendMessage: &tg.InputBotInlineMessageText{
			Message: text,
		},
	}
	req := &tg.MessagesSetBotGuestChatResultRequest{
		QueryID: queryID,
		Result:  result,
	}
	return client.MessagesSetBotGuestChatResult(ctx, req)
}

func processGuestTask(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, url string, logger *zap.Logger) {
	var err error
	switch {
	case config.IsPlatformURL(url, "tiktok"):
		err = processGuestTikTok(ctx, client, inlineMsgID, url, logger)
	case config.IsPlatformURL(url, "instagram"):
		err = processGuestInstagram(ctx, client, inlineMsgID, url, logger)
	case config.IsPlatformURL(url, "facebook"):
		err = processGuestFacebook(ctx, client, inlineMsgID, url, logger)
	case config.IsPlatformURL(url, "twitter"):
		err = processGuestTwitter(ctx, client, inlineMsgID, url, logger)
	default:
		editGuestInlineText(ctx, client, inlineMsgID, "❌ Platform tidak didukung. \n\nSaat ini hanya mendukung Tiktok, Facebook, Instagram, dan Twitter (X).")
		log.LogWarn(ctx, "GuestUnsupported", "Platform tidak didukung", "url="+url)
		return
	}
	if err != nil {
		logger.Error("Guest download error", zap.Error(err))
		log.LogError(ctx, "GuestProcess", err, "url="+url)
		editGuestInlineText(ctx, client, inlineMsgID, fmt.Sprintf("❌ Gagal mengunduh: %v", err))
	}
}

// --- Proses per platform ---

func processGuestTikTok(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, url string, logger *zap.Logger) error {
	editGuestInlineText(ctx, client, inlineMsgID, "🔍 Mendeteksi TikTok...")
	data, stream, err := tiktok.FetchTikTokDataWithFallback(ctx, logger, url, nil)
	if err != nil {
		return err
	}
	if stream != nil {
		defer stream.Close()
	}
	if data.VideoURL == "" {
		return fmt.Errorf("tidak ada video URL")
	}
	if data.IsAlbum && len(data.ImageURLs) > 1 {
		editGuestInlineText(ctx, client, inlineMsgID, "❌ Album tidak didukung di Guest Mode.")
		return nil
	}

	editGuestInlineText(ctx, client, inlineMsgID, "📥 Mengunduh video...")
	info := api.ContentTypeInfo{
		MimeType:  "video/mp4",
		Category:  api.ContentVideo,
		Extension: ".mp4",
	}
	filename := fmt.Sprintf("%s.mp4", data.ID)
	fileID, accessHash, fileRef, err := uploadMediaToCacheWithRetry(ctx, client, data.VideoURL, info, filename, data.CoverURL, 3)
	if err != nil {
		return err
	}
	caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
	if len(caption) > 200 {
		caption = caption[:200]
	}
	inputMedia := &tg.InputMediaDocument{
		ID: &tg.InputDocument{
			ID:            fileID,
			AccessHash:    accessHash,
			FileReference: fileRef,
		},
	}
	return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
}

func processGuestInstagram(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, url string, logger *zap.Logger) error {
	editGuestInlineText(ctx, client, inlineMsgID, "🔍 Mendeteksi Instagram...")
	data, err := instagram.FetchInstagramDataWithFallback(url)
	if err != nil {
		return err
	}
	if len(data.ImageURLs) > 1 {
		editGuestInlineText(ctx, client, inlineMsgID, "❌ Album tidak didukung di Guest Mode.")
		return nil
	}
	if data.VideoURL != "" {
		editGuestInlineText(ctx, client, inlineMsgID, "📥 Mengunduh video...")
		info := api.ContentTypeInfo{
			MimeType:  "video/mp4",
			Category:  api.ContentVideo,
			Extension: ".mp4",
		}
		filename := fmt.Sprintf("%s.mp4", data.ID)
		fileID, accessHash, fileRef, err := uploadMediaToCacheWithRetry(ctx, client, data.VideoURL, info, filename, data.CoverURL, 3)
		if err != nil {
			return err
		}
		caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
		if len(caption) > 400 {
			caption = caption[:400]
		}
		inputMedia := &tg.InputMediaDocument{
			ID: &tg.InputDocument{
				ID:            fileID,
				AccessHash:    accessHash,
				FileReference: fileRef,
			},
		}
		return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
	}
	if len(data.ImageURLs) == 1 {
		imgURL := data.ImageURLs[0]
		editGuestInlineText(ctx, client, inlineMsgID, "📥 Mengunduh foto...")
		fileID, accessHash, fileRef, err := uploadImageToCacheWithRetry(ctx, client, imgURL, 3)
		if err != nil {
			return err
		}
		caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
		if len(caption) > 200 {
			caption = caption[:200]
		}
		inputMedia := &tg.InputMediaPhoto{
			ID: &tg.InputPhoto{
				ID:            fileID,
				AccessHash:    accessHash,
				FileReference: fileRef,
			},
		}
		return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
	}
	return fmt.Errorf("tidak ada media")
}

func processGuestFacebook(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, url string, logger *zap.Logger) error {
	editGuestInlineText(ctx, client, inlineMsgID, "🔍 Mendeteksi Facebook...")
	data, err := facebook.FetchFacebookWithFallback(logger, url)
	if err != nil {
		return err
	}
	if data.VidioURL == "" {
		return fmt.Errorf("tidak ada video URL")
	}
	editGuestInlineText(ctx, client, inlineMsgID, "📥 Mengunduh video...")
	info := api.ContentTypeInfo{
		MimeType:  "video/mp4",
		Category:  api.ContentVideo,
		Extension: ".mp4",
	}
	filename := fmt.Sprintf("%s.mp4", data.ID)
	fileID, accessHash, fileRef, err := uploadMediaToCacheWithRetry(ctx, client, data.VidioURL, info, filename, data.CoverURL, 3)
	if err != nil {
		return err
	}
	caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
	if len(caption) > 200 {
		caption = caption[:200]
	}
	inputMedia := &tg.InputMediaDocument{
		ID: &tg.InputDocument{
			ID:            fileID,
			AccessHash:    accessHash,
			FileReference: fileRef,
		},
	}
	return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
}

func processGuestTwitter(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, url string, logger *zap.Logger) error {
	editGuestInlineText(ctx, client, inlineMsgID, "🔍 Mendeteksi Twitter...")
	data, err := twiter.FetchTwitterWithFallback(ctx, url)
	if err != nil {
		return err
	}
	if data.DownloadURL == "" {
		return fmt.Errorf("tidak ada media URL")
	}
	if data.IsAlbum && len(data.ImageURLs) > 1 {
		editGuestInlineText(ctx, client, inlineMsgID, "❌ Album tidak didukung di Guest Mode.")
		return nil
	}
	editGuestInlineText(ctx, client, inlineMsgID, "📥 Mengunduh media...")
	info := api.ContentTypeInfo{
		MimeType:  "video/mp4",
		Category:  api.ContentVideo,
		Extension: ".mp4",
	}
	filename := fmt.Sprintf("%s.mp4", data.ID)
	fileID, accessHash, fileRef, err := uploadMediaToCacheWithRetry(ctx, client, data.DownloadURL, info, filename, data.CoverURL, 3)
	if err != nil {
		// Coba sebagai gambar
		fileID, accessHash, fileRef, err2 := uploadImageToCacheWithRetry(ctx, client, data.DownloadURL, 3)
		if err2 != nil {
			return err
		}
		caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
		if len(caption) > 200 {
			caption = caption[:200]
		}
		inputMedia := &tg.InputMediaPhoto{
			ID: &tg.InputPhoto{
				ID:            fileID,
				AccessHash:    accessHash,
				FileReference: fileRef,
			},
		}
		return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
	}
	caption := fmt.Sprintf("%s\n\n@Kometika_bot", data.Title)
	if len(caption) > 400 {
		caption = caption[:400]
	}
	inputMedia := &tg.InputMediaDocument{
		ID: &tg.InputDocument{
			ID:            fileID,
			AccessHash:    accessHash,
			FileReference: fileRef,
		},
	}
	return editGuestInlineMedia(ctx, client, inlineMsgID, inputMedia, caption)
}

// uploadMediaToCacheWithRetry dengan context-aware retry (FIXED)
func uploadMediaToCacheWithRetry(ctx context.Context, client *tg.Client, mediaURL string, info api.ContentTypeInfo, filename, thumbURL string, maxRetries int) (int64, int64, []byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fileID, accessHash, fileRef, err := uploadMediaToCache(ctx, client, mediaURL, info, filename, thumbURL)
		if err == nil {
			return fileID, accessHash, fileRef, nil
		}
		lastErr = err
		
		// Context-aware sleep menggunakan select
		retryDelay := time.Duration(attempt) * 500 * time.Millisecond
		select {
		case <-time.After(retryDelay):
			// Lanjutkan retry
		case <-ctx.Done():
			// Context dibatalkan, hentikan retry
			return 0, 0, nil, ctx.Err()
		}
	}
	return 0, 0, nil, fmt.Errorf("setelah %d percobaan: %w", maxRetries, lastErr)
}

// uploadImageToCacheWithRetry dengan context-aware retry (FIXED)
func uploadImageToCacheWithRetry(ctx context.Context, client *tg.Client, imageURL string, maxRetries int) (int64, int64, []byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fileID, accessHash, fileRef, err := uploadImageToCache(ctx, client, imageURL)
		if err == nil {
			return fileID, accessHash, fileRef, nil
		}
		lastErr = err
		
		// Context-aware sleep menggunakan select
		retryDelay := time.Duration(attempt) * 500 * time.Millisecond
		select {
		case <-time.After(retryDelay):
			// Lanjutkan retry
		case <-ctx.Done():
			// Context dibatalkan, hentikan retry
			return 0, 0, nil, ctx.Err()
		}
	}
	return 0, 0, nil, fmt.Errorf("setelah %d percobaan: %w", maxRetries, lastErr)
}

// uploadMediaToCache dengan atribut berdasarkan info dan thumbnail
func uploadMediaToCache(ctx context.Context, client *tg.Client, mediaURL string, info api.ContentTypeInfo, filename, thumbURL string) (int64, int64, []byte, error) {
	stream, _, err := api.GetVideoStream(ctx, mediaURL)
	if err != nil {
		return 0, 0, nil, err
	}
	defer stream.Close()

	peer := getCachePeer()
	if peer == nil {
		var err2 error
		peer, err2 = getSelfPeer(ctx, client)
		if err2 != nil {
			return 0, 0, nil, err2
		}
	}

	up := uploader.NewUploader(client).WithThreads(1)
	file, err := up.FromReader(ctx, filename, stream)
	if err != nil {
		return 0, 0, nil, err
	}

	var thumbFile tg.InputFileClass
	if thumbURL != "" {
		thumbBytes, err := api.GetThumbnail(ctx, thumbURL)
		if err == nil && len(thumbBytes) > 0 {
			thumbFile, _ = up.FromBytes(ctx, "thumb.jpg", thumbBytes)
		}
	}

	doc := &tg.InputMediaUploadedDocument{
		File:     file,
		MimeType: info.MimeType,
	}
	if thumbFile != nil {
		doc.SetThumb(thumbFile)
	}

	var attrs []tg.DocumentAttributeClass
	switch info.Category {
	case api.ContentVideo:
		videoAttr := &tg.DocumentAttributeVideo{}
		if info.MimeType == "video/mp4" {
			videoAttr.SupportsStreaming = true
		}
		attrs = append(attrs, videoAttr)
	case api.ContentAudio:
		attrs = append(attrs, &tg.DocumentAttributeAudio{})
	}
	attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: filename})
	doc.Attributes = attrs

	randID, err := media.RandomID()
	if err != nil {
		return 0, 0, nil, err
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    doc,
		Message:  "temp",
		RandomID: randID,
	}
	updates, err := client.MessagesSendMedia(ctx, req)
	if err != nil {
		return 0, 0, nil, err
	}

	switch v := updates.(type) {
	case *tg.Updates:
		for _, update := range v.Updates {
			if msg, ok := update.(*tg.UpdateNewMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if docMedia, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := docMedia.Document.(*tg.Document); ok {
							return doc.ID, doc.AccessHash, doc.FileReference, nil
						}
					}
				}
			}
			if msg, ok := update.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if docMedia, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := docMedia.Document.(*tg.Document); ok {
							return doc.ID, doc.AccessHash, doc.FileReference, nil
						}
					}
				}
			}
		}
	case *tg.UpdatesCombined:
		for _, update := range v.Updates {
			if msg, ok := update.(*tg.UpdateNewMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if docMedia, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := docMedia.Document.(*tg.Document); ok {
							return doc.ID, doc.AccessHash, doc.FileReference, nil
						}
					}
				}
			}
			if msg, ok := update.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if docMedia, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := docMedia.Document.(*tg.Document); ok {
							return doc.ID, doc.AccessHash, doc.FileReference, nil
						}
					}
				}
			}
		}
	case *tg.UpdateShortSentMessage:
		if docMedia, ok := v.Media.(*tg.MessageMediaDocument); ok {
			if doc, ok := docMedia.Document.(*tg.Document); ok {
				return doc.ID, doc.AccessHash, doc.FileReference, nil
			}
		}
	default:
		return 0, 0, nil, fmt.Errorf("tipe updates tidak didukung: %T", updates)
	}
	return 0, 0, nil, fmt.Errorf("tidak dapat menemukan document ID")
}

// uploadImageToCache
func uploadImageToCache(ctx context.Context, client *tg.Client, imageURL string) (int64, int64, []byte, error) {
	stream, _, err := api.GetVideoStream(ctx, imageURL)
	if err != nil {
		return 0, 0, nil, err
	}
	defer stream.Close()

	// Baca stream sepenuhnya ke buffer sebelum divalidasi
	body, err := io.ReadAll(stream)
	if err != nil {
		return 0, 0, nil, err
	}

	convertedBody, err := media.ProcessAndValidateImageBytes(bytes.NewReader(body), nil)
	if err == nil && len(convertedBody) > 0 {
		body = convertedBody
	}

	peer := getCachePeer()
	if peer == nil {
		var err2 error
		peer, err2 = getSelfPeer(ctx, client)
		if err2 != nil {
			return 0, 0, nil, err2
		}
	}

	up := uploader.NewUploader(client).WithThreads(1)
	file, err := up.FromBytes(ctx, "temp.jpg", body)
	if err != nil {
		return 0, 0, nil, err
	}

	randID, err := media.RandomID()
	if err != nil {
		return 0, 0, nil, err
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    &tg.InputMediaUploadedPhoto{File: file},
		Message:  "temp",
		RandomID: randID,
	}
	updates, err := client.MessagesSendMedia(ctx, req)
	if err != nil {
		return 0, 0, nil, err
	}

	switch v := updates.(type) {
	case *tg.Updates:
		for _, update := range v.Updates {
			if msg, ok := update.(*tg.UpdateNewMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if photoMedia, ok := m.Media.(*tg.MessageMediaPhoto); ok {
						if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
							return photo.ID, photo.AccessHash, photo.FileReference, nil
						}
					}
				}
			}
			if msg, ok := update.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if photoMedia, ok := m.Media.(*tg.MessageMediaPhoto); ok {
						if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
							return photo.ID, photo.AccessHash, photo.FileReference, nil
						}
					}
				}
			}
		}
	case *tg.UpdatesCombined:
		for _, update := range v.Updates {
			if msg, ok := update.(*tg.UpdateNewMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if photoMedia, ok := m.Media.(*tg.MessageMediaPhoto); ok {
						if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
							return photo.ID, photo.AccessHash, photo.FileReference, nil
						}
					}
				}
			}
			if msg, ok := update.(*tg.UpdateNewChannelMessage); ok {
				if m, ok := msg.Message.(*tg.Message); ok {
					if photoMedia, ok := m.Media.(*tg.MessageMediaPhoto); ok {
						if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
							return photo.ID, photo.AccessHash, photo.FileReference, nil
						}
					}
				}
			}
		}
	case *tg.UpdateShortSentMessage:
		if photoMedia, ok := v.Media.(*tg.MessageMediaPhoto); ok {
			if photo, ok := photoMedia.Photo.(*tg.Photo); ok {
				return photo.ID, photo.AccessHash, photo.FileReference, nil
			}
		}
	default:
		return 0, 0, nil, fmt.Errorf("tipe updates tidak didukung: %T", updates)
	}
	return 0, 0, nil, fmt.Errorf("tidak dapat menemukan photo ID")
}

// getSelfPeer
func getSelfPeer(ctx context.Context, client *tg.Client) (tg.InputPeerClass, error) {
	users, err := client.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil || len(users) == 0 {
		return nil, fmt.Errorf("cannot get self user")
	}
	u, ok := users[0].(*tg.User)
	if !ok {
		return nil, fmt.Errorf("self user not valid")
	}
	return &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
}

// --- Edit inline message ---
func editGuestInlineText(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, text string) {
	req := &tg.MessagesEditInlineBotMessageRequest{
		ID: inlineMsgID,
	}
	req.Message = text
	_, _ = client.MessagesEditInlineBotMessage(ctx, req)
}

func editGuestInlineMedia(ctx context.Context, client *tg.Client, inlineMsgID tg.InputBotInlineMessageIDClass, media tg.InputMediaClass, caption string) error {
	req := &tg.MessagesEditInlineBotMessageRequest{
		ID:    inlineMsgID,
		Media: media,
	}
	if caption != "" {
		req.Message = caption
	}
	_, err := client.MessagesEditInlineBotMessage(ctx, req)
	return err
}

// --- Utility ---
func extractURLFromText(text string) string {
	words := strings.Fields(text)
	for _, w := range words {
		if strings.HasPrefix(w, "http://") || strings.HasPrefix(w, "https://") {
			return w
		}
	}
	return ""
}
