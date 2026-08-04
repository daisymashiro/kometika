package commands

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mybot/internal/api"
	"mybot/internal/api/terabox"
	"mybot/internal/config"
	"mybot/internal/media"

	"github.com/gotd/td/telegram/message/markup"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// --- Cache ---
type teraboxSession struct {
	Files       []terabox.TeraboxUniversalData
	MenuMsgID   int
	ActionMsgID int
	Page        int
	Peer        tg.InputPeerClass
	IsOwner     bool
}

var (
	teraboxCache = make(map[string]teraboxSession)
	tbMutex      sync.RWMutex
)

// --- Helper ---
func generateShortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func formatFileNameButton(name string) string {
	const maxRunes = 40
	runes := []rune(name)
	if len(runes) <= maxRunes {
		return name
	}
	ext := getExt(name)
	base := name
	if ext != "" {
		base = base[:len(base)-len(ext)]
	}
	baseRunes := []rune(base)
	head, tail := 24, 8
	if len(baseRunes) <= head+tail {
		return base + ext
	}
	return string(baseRunes[:head]) + "..." + string(baseRunes[len(baseRunes)-tail:]) + ext
}

func getExt(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}
	return ""
}

func parseFileSizeToBytes(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(strings.ToUpper(sizeStr))
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	var value float64
	var unit string
	n, _ := fmt.Sscanf(sizeStr, "%f %s", &value, &unit)
	if n != 2 {
		fmt.Sscanf(sizeStr, "%f%s", &value, &unit)
	}
	switch unit {
	case "KB":
		return int64(value * KB)
	case "MB":
		return int64(value * MB)
	case "GB":
		return int64(value * GB)
	default:
		return 201 * MB
	}
}

// getTeraboxStream adalah custom HTTP downloader khusus untuk mem-bypass pemblokiran CDN Terabox
func getTeraboxStream(ctx context.Context, dlURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "*/*")

	// Timeout dikosongkan agar file besar tidak terputus saat diunduh
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("status HTTP %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// --- Entry point ---
func HandleTerabox(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string) error {
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("terabox") {
		zap.L().Info("Fitur Terabox dinonaktifkan")
		return nil
	}

	// Deteksi ID Pengirim (Support DM dan Grup)
	var userID int64
	if msg.FromID != nil {
		if p, ok := msg.FromID.(*tg.PeerUser); ok {
			userID = p.UserID
		}
	}
	// Fallback jika FromID nil (sering terjadi di Private Chat)
	if userID == 0 && msg.PeerID != nil {
		if p, ok := msg.PeerID.(*tg.PeerUser); ok {
			userID = p.UserID
		}
	}

	ownerIDStr := os.Getenv("OWNER_ID")
	var ownerID int64
	if ownerIDStr != "" {
		ownerID, _ = strconv.ParseInt(ownerIDStr, 10, 64)
	}
	isOwner := (ownerID != 0 && userID == ownerID)

	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		return nil // Abaikan jika bukan DM / Grup biasa (misal command nyasar)
	}

	switch msg.PeerID.(type) {
	case *tg.PeerUser:
		return handleTeraboxPrivate(ctx, client, msg, url, isOwner)
	case *tg.PeerChat, *tg.PeerChannel:
		return handleTeraboxGroup(ctx, client, msg, entities, url, isOwner)
	}
	return nil
}

func handleTeraboxPrivate(ctx context.Context, client *tg.Client, msg *tg.Message, url string, isOwner bool) error {
	peerUser := msg.PeerID.(*tg.PeerUser)
	accessHash, err := GetUserAccessHash(ctx, client, peerUser.UserID)
	if err != nil {
		zap.L().Warn("Gagal dapat access hash Terabox", zap.Error(err))
		return nil
	}
	peer := &tg.InputPeerUser{UserID: peerUser.UserID, AccessHash: accessHash}
	return fetchAndShowTerabox(ctx, client, peer, url, nil, isOwner)
}

func handleTeraboxGroup(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, isOwner bool) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		zap.L().Error("Gagal dapatkan peer grup Terabox", zap.Error(err))
		return err
	}
	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)
	return fetchAndShowTerabox(ctx, client, peer, url, replyTo, isOwner)
}

// --- Core logic ---
func fetchAndShowTerabox(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, isOwner bool) error {
	loadingReq := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  "⏳ Mengambil data Terabox, mohon tunggu...",
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

	files, err := terabox.FetchTeraboxUniversal(url)
	if err != nil || len(files) == 0 {
		zap.L().Warn("Fetch Terabox gagal", zap.Error(err))
		if loadingMsgID != 0 {
			_ = media.EditHTML(ctx, client, peer, loadingMsgID, "❌ Gagal mengambil data Terabox atau link tidak valid.")
		}
		return nil
	}
	zap.L().Info("Berhasil fetch Terabox", zap.Int("total_files", len(files)))

	if loadingMsgID != 0 {
		deleteGroupMessage(ctx, client, peer, loadingMsgID)
	}

	reqID := generateShortID()
	markup := buildTeraboxFileMarkup(files, reqID, 0, isOwner)

	plainText := fmt.Sprintf("✅ Data Terabox Ditemukan!\nTotal File: %d\n\nPilih file untuk diunduh:", len(files))
	menuReq := &tg.MessagesSendMessageRequest{
		Peer:        peer,
		Message:     plainText,
		RandomID:    time.Now().UnixNano(),
		ReplyMarkup: markup,
	}
	if replyTo != nil {
		menuReq.SetReplyTo(replyTo)
	}

	menuUpdates, err := client.MessagesSendMessage(ctx, menuReq)
	var menuMsgID int
	if err == nil && menuUpdates != nil {
		menuMsgID, _ = media.ExtractMessageID(menuUpdates)

		tbMutex.Lock()
		teraboxCache[reqID] = teraboxSession{
			Files:       files,
			MenuMsgID:   menuMsgID,
			ActionMsgID: 0,
			Page:        0,
			Peer:        peer,
			IsOwner:     isOwner,
		}
		tbMutex.Unlock()

		time.AfterFunc(5*time.Minute+30*time.Second, func() {
			tbMutex.Lock()
			sess, exists := teraboxCache[reqID]
			delete(teraboxCache, reqID)
			tbMutex.Unlock()

			if exists {
				if sess.MenuMsgID != 0 {
					deleteGroupMessage(context.Background(), client, sess.Peer, sess.MenuMsgID)
				}
				if sess.ActionMsgID != 0 {
					deleteGroupMessage(context.Background(), client, sess.Peer, sess.ActionMsgID)
				}
				zap.L().Info("Pesan menu/action Terabox dihapus otomatis", zap.String("reqID", reqID))
			}
		})
	}
	return nil
}

func buildTeraboxFileMarkup(files []terabox.TeraboxUniversalData, reqID string, page int, isOwner bool) *tg.ReplyInlineMarkup {
	var rows []tg.KeyboardButtonRow
	start := page * 8
	end := min(start+8, len(files))

	for i := start; i < end; i++ {
		btnText := fmt.Sprintf("📁 %s", formatFileNameButton(files[i].FileName))
		cbData := fmt.Sprintf("tb_dl_%s_%d", reqID, i)
		rows = append(rows, tg.KeyboardButtonRow{
			Buttons: []tg.KeyboardButtonClass{
				markup.Callback(btnText, []byte(cbData), markup.StyleBgSuccess()),
			},
		})
	}

	var navButtons []tg.KeyboardButtonClass
	if page > 0 {
		navButtons = append(navButtons, markup.Callback("⬅️ Prev", fmt.Appendf(nil, "tb_page_%s_%d", reqID, page-1), markup.StyleBgSuccess()))
	}
	navButtons = append(navButtons, markup.Callback("❌ Tutup", fmt.Appendf(nil, "tb_close_%s", reqID), markup.StyleBgSuccess()))
	if end < len(files) {
		navButtons = append(navButtons, markup.Callback("Next ➡️", fmt.Appendf(nil, "tb_page_%s_%d", reqID, page+1), markup.StyleBgSuccess()))
	}
	rows = append(rows, tg.KeyboardButtonRow{Buttons: navButtons})

	if isOwner {
		rows = append(rows, tg.KeyboardButtonRow{
			Buttons: []tg.KeyboardButtonClass{
				markup.Callback("📦 UNDUH SEMUA (ZIP)", []byte(fmt.Sprintf("tb_zipdl_%s", reqID)), markup.StyleBgPrimary()),
			},
		})
	}

	return &tg.ReplyInlineMarkup{Rows: rows}
}

func sendTeraboxMenu(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, reqID string, page int) error {
	tbMutex.RLock()
	sess, exists := teraboxCache[reqID]
	tbMutex.RUnlock()

	if !exists {
		return media.EditHTML(ctx, client, peer, msgID, "⏳ Sesi Terabox telah kadaluarsa. Silakan kirim ulang link.")
	}

	fm := buildTeraboxFileMarkup(sess.Files, reqID, page, sess.IsOwner)
	plainText := fmt.Sprintf("✅ Data Terabox Ditemukan!\nTotal File: %d\nHalaman: %d\n\nPilih file untuk diunduh:", len(sess.Files), page+1)

	if err := media.EditWithMarkup(ctx, client, peer, msgID, plainText, fm); err != nil {
		return err
	}

	tbMutex.Lock()
	if s, ok := teraboxCache[reqID]; ok {
		s.Page = page
		teraboxCache[reqID] = s
	}
	tbMutex.Unlock()
	return nil
}

func sendNewTeraboxMenu(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, reqID string, page int, replyTo *tg.InputReplyToMessage) (int, error) {
	tbMutex.RLock()
	sess, exists := teraboxCache[reqID]
	tbMutex.RUnlock()

	if !exists {
		return 0, fmt.Errorf("sesi tidak ditemukan")
	}

	fm := buildTeraboxFileMarkup(sess.Files, reqID, page, sess.IsOwner)
	plainText := fmt.Sprintf("✅ Data Terabox Ditemukan!\nTotal File: %d\nHalaman: %d\n\nPilih file untuk diunduh:", len(sess.Files), page+1)

	reqSend := &tg.MessagesSendMessageRequest{
		Peer:        peer,
		Message:     plainText,
		RandomID:    time.Now().UnixNano(),
		ReplyMarkup: fm,
	}
	if replyTo != nil {
		reqSend.SetReplyTo(replyTo)
	}

	updates, err := client.MessagesSendMessage(ctx, reqSend)
	if err != nil {
		return 0, err
	}
	msgID, _ := media.ExtractMessageID(updates)
	return msgID, nil
}

func buildActionMarkup(reqID string, index int, hasStream bool) *tg.ReplyInlineMarkup {
	var buttons []tg.KeyboardButtonClass
	if hasStream {
		buttons = append(buttons, markup.Callback("▶️ Stream", fmt.Appendf(nil, "tb_stream_%s_%d", reqID, index), markup.StyleBgSuccess()))
	}
	buttons = append(buttons, markup.Callback("⬇️ Download", fmt.Appendf(nil, "tb_down_%s_%d", reqID, index), markup.StyleBgSuccess()))
	buttons = append(buttons, markup.Callback("🔙 Kembali", fmt.Appendf(nil, "tb_back_%s", reqID), markup.StyleBgDanger()))
	return &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: buttons}}}
}

// --- Callback handler ---
func HandleTeraboxCallback(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, update *tg.UpdateBotCallbackQuery, logger *zap.Logger) error {
	if len(update.Data) == 0 {
		return nil
	}
	rawData := string(update.Data)
	parts := strings.Split(rawData, "_")
	if len(parts) < 3 {
		media.AnswerCallback(ctx, client, update.QueryID, "Data tidak valid", true)
		return nil
	}

	action := parts[1]
	reqID := parts[2]

	if action == "close" {
		tbMutex.Lock()
		_, exists := teraboxCache[reqID]
		if exists {
			delete(teraboxCache, reqID)
		}
		tbMutex.Unlock()
		deleteGroupMessage(ctx, client, peer, msgID)
		media.AnswerCallback(ctx, client, update.QueryID, "Menu ditutup", false)
		return nil
	}

	tbMutex.RLock()
	sess, exists := teraboxCache[reqID]
	tbMutex.RUnlock()

	if !exists {
		media.AnswerCallback(ctx, client, update.QueryID, "⏳ Sesi kadaluarsa. Kirim ulang link.", true)
		return nil
	}

	if action == "page" {
		if len(parts) < 4 {
			return nil
		}
		pageVal, _ := strconv.Atoi(parts[3])
		_ = sendTeraboxMenu(ctx, client, peer, msgID, reqID, pageVal)
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		return nil
	}

	if action == "back" {
		if sess.ActionMsgID != 0 {
			deleteGroupMessage(ctx, client, peer, sess.ActionMsgID)
		}
		newMenuID, err := sendNewTeraboxMenu(ctx, client, peer, reqID, sess.Page, nil)
		if err != nil {
			media.AnswerCallback(ctx, client, update.QueryID, "Gagal memuat menu", true)
			return nil
		}
		tbMutex.Lock()
		if s, ok := teraboxCache[reqID]; ok {
			s.MenuMsgID = newMenuID
			s.ActionMsgID = 0
			teraboxCache[reqID] = s
		}
		tbMutex.Unlock()
		if msgID != 0 && msgID != newMenuID {
			deleteGroupMessage(ctx, client, peer, msgID)
		}
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		return nil
	}

	// --- HANDLER DOWNLOAD ZIP (KHUSUS OWNER) ---
	if action == "zipdl" {
		ownerIDStr := os.Getenv("OWNER_ID")
		var ownerID int64
		if ownerIDStr != "" {
			ownerID, _ = strconv.ParseInt(ownerIDStr, 10, 64)
		}

		if ownerID == 0 || update.UserID != ownerID {
			media.AnswerCallback(ctx, client, update.QueryID, "❌ Hanya owner yang dapat menggunakan fitur ini.", true)
			return nil
		}

		media.AnswerCallback(ctx, client, update.QueryID, "Memproses unduhan paralel & ZIP...", false)

		tbMutex.Lock()
		delete(teraboxCache, reqID)
		tbMutex.Unlock()

		go processZipDownload(client, peer, sess.Files, reqID, msgID, logger)
		return nil
	}

	if len(parts) < 4 {
		media.AnswerCallback(ctx, client, update.QueryID, "Data tidak lengkap", true)
		return nil
	}

	indexVal, _ := strconv.Atoi(parts[3])
	if indexVal < 0 || indexVal >= len(sess.Files) {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ File tidak ditemukan.", true)
		return nil
	}
	fileTarget := sess.Files[indexVal]

	if action == "dl" {
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		if sess.MenuMsgID != 0 {
			deleteGroupMessage(ctx, client, peer, sess.MenuMsgID)
		}

		htmlCaption := fmt.Sprintf("File Name: %s \n\nPilih aksi:", fileTarget.FileName)
		markup := buildActionMarkup(reqID, indexVal, fileTarget.StreamURL != "")
		var actionMsgID int

		if fileTarget.Thumbnail != "" {
			data, err := api.GetThumbnail(ctx, fileTarget.Thumbnail)
			if err != nil {
				reqSend := &tg.MessagesSendMessageRequest{
					Peer:        peer,
					Message:     htmlCaption,
					RandomID:    time.Now().UnixNano(),
					ReplyMarkup: markup,
				}
				updates, _ := client.MessagesSendMessage(ctx, reqSend)
				actionMsgID, _ = media.ExtractMessageID(updates)
			} else {
				up := uploader.NewUploader(client)
				inputFile, err := up.FromBytes(ctx, "thumbnail.jpg", data)
				if err != nil {
					reqSend := &tg.MessagesSendMessageRequest{
						Peer:        peer,
						Message:     htmlCaption,
						RandomID:    time.Now().UnixNano(),
						ReplyMarkup: markup,
					}
					updates, _ := client.MessagesSendMessage(ctx, reqSend)
					actionMsgID, _ = media.ExtractMessageID(updates)
				} else {
					req := &tg.MessagesSendMediaRequest{
						Peer:        peer,
						RandomID:    time.Now().UnixNano(),
						Media:       &tg.InputMediaUploadedPhoto{File: inputFile},
						Message:     htmlCaption,
						ReplyMarkup: markup,
					}
					updates, _ := client.MessagesSendMedia(ctx, req)
					actionMsgID, _ = media.ExtractMessageID(updates)
				}
			}
		} else {
			reqSend := &tg.MessagesSendMessageRequest{
				Peer:        peer,
				Message:     htmlCaption,
				RandomID:    time.Now().UnixNano(),
				ReplyMarkup: markup,
			}
			updates, _ := client.MessagesSendMessage(ctx, reqSend)
			actionMsgID, _ = media.ExtractMessageID(updates)
		}

		tbMutex.Lock()
		if s, ok := teraboxCache[reqID]; ok {
			s.MenuMsgID = 0
			s.ActionMsgID = actionMsgID
			teraboxCache[reqID] = s
		}
		tbMutex.Unlock()
		return nil
	}

	if action == "stream" {
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		if fileTarget.StreamURL == "" {
			_ = media.SendHTML(ctx, client, peer, "❌ Tidak ada URL stream untuk file ini.")
			return nil
		}
		shortURL, err := api.ShortenWithFallback(fileTarget.StreamURL)
		if err != nil {
			shortURL = fileTarget.StreamURL
		}
		htmlMsg := fmt.Sprintf("🎬 %s\n\n@Kometika_bot", fileTarget.FileName)
		streamMarkup := markup.InlineKeyboard(markup.Row(markup.URL("▶️ Buka Stream", shortURL, markup.StyleBgSuccess())))
		reqSend := &tg.MessagesSendMessageRequest{
			Peer:        peer,
			Message:     htmlMsg,
			RandomID:    time.Now().UnixNano(),
			ReplyMarkup: streamMarkup,
		}
		_, _ = client.MessagesSendMessage(ctx, reqSend)
		return nil
	}

	if action == "down" {
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		downloadURL := fileTarget.DownloadURL
		if downloadURL == "" {
			_ = media.SendHTML(ctx, client, peer, "❌ Download URL tidak tersedia untuk file ini.")
			return nil
		}
		sizeBytes := parseFileSizeToBytes(fileTarget.FileSize)
		const maxSize = 400 * 1024 * 1024 // 400 MB

		if sizeBytes < maxSize {
			// Memanggil getTeraboxStream secara native alih-alih API Stream
			stream, err := getTeraboxStream(ctx, downloadURL)
			if err != nil {
				zap.L().Error("Gagal stream file Terabox", zap.Error(err), zap.String("url", downloadURL))
				_ = media.SendHTML(ctx, client, peer, "❌ Gagal mengambil stream file.")
				return nil
			}
			defer stream.Close()

			info, fullStream, _ := api.DetectAndClassifyStream(stream)
			if info.Category == api.ContentUnknown || info.Category == "" {
				info = api.ContentTypeInfo{
					MimeType:  "application/octet-stream",
					Category:  api.ContentDocument,
					Extension: "",
				}
			}

			var thumbFile tg.InputFileClass
			if fileTarget.Thumbnail != "" {
				thumbData, err := api.GetThumbnail(ctx, fileTarget.Thumbnail)
				if err == nil && len(thumbData) > 0 {
					upThumb := uploader.NewUploader(client)
					thumbFile, _ = upThumb.FromBytes(ctx, "thumb_terabox.jpg", thumbData)
				}
			}

			captionPlain := fmt.Sprintf("📄 %s\n\n@Kometika_bot", fileTarget.FileName)
			mediaSender := media.NewMediaSender(client)
			_, err = mediaSender.SendDynamicStream(
				ctx, peer, fullStream, info, fileTarget.FileName, captionPlain, nil, nil, thumbFile,
			)
			if err != nil {
				_ = media.SendHTML(ctx, client, peer, "❌ Gagal mengirim file.")
			}
		} else {
			shortURL, err := api.ShortenWithFallback(downloadURL)
			if err != nil {
				shortURL = downloadURL
			}
			htmlMsg := fmt.Sprintf("📄 %s\n\n⚖️ Ukuran: %s\n\n@Kometika_bot", fileTarget.FileName, fileTarget.FileSize)
			downMarkup := markup.InlineKeyboard(markup.Row(markup.URL("⬇️ Unduh File", shortURL, markup.StyleBgSuccess())))
			reqSend := &tg.MessagesSendMessageRequest{
				Peer:        peer,
				Message:     htmlMsg,
				RandomID:    time.Now().UnixNano(),
				ReplyMarkup: downMarkup,
			}
			_, _ = client.MessagesSendMessage(ctx, reqSend)
		}
		return nil
	}

	return nil
}

// --- LOGIKA UNDUH SEMUA (ZIP) UNTUK OWNER ---
func processZipDownload(client *tg.Client, peer tg.InputPeerClass, files []terabox.TeraboxUniversalData, reqID string, menuMsgID int, logger *zap.Logger) {
	ctx := context.Background()

	_ = media.EditHTML(ctx, client, peer, menuMsgID, "⏳ <b>Mendownload file ke server...</b>\nProses mungkin memakan waktu tergantung jumlah dan ukuran file.")

	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("terabox_%s_*", reqID))
	if err != nil {
		logger.Error("Gagal membuat direktori temporary", zap.Error(err))
		_ = media.EditHTML(ctx, client, peer, menuMsgID, "❌ Gagal menginisialisasi penyimpanan di server.")
		return
	}
	defer os.RemoveAll(tmpDir)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	var dlErrors []string
	var mu sync.Mutex

	for _, f := range files {
		if f.DownloadURL == "" {
			mu.Lock()
			dlErrors = append(dlErrors, fmt.Sprintf("%s (No URL)", f.FileName))
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(file terabox.TeraboxUniversalData) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			destPath := filepath.Join(tmpDir, file.FileName)
			err := downloadFileToPath(ctx, file.DownloadURL, destPath)
			if err != nil {
				mu.Lock()
				dlErrors = append(dlErrors, fmt.Sprintf("%s (%v)", file.FileName, err))
				mu.Unlock()
			}
		}(f)
	}

	wg.Wait()

	if len(dlErrors) == len(files) {
		logger.Error("Semua file gagal diunduh", zap.Strings("errors", dlErrors))
		_ = media.EditHTML(ctx, client, peer, menuMsgID, "❌ Gagal mendownload seluruh file.")
		return
	}

	_ = media.EditHTML(ctx, client, peer, menuMsgID, "📦 <b>Mengkompresi file menjadi ZIP...</b>")

	zipFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("TeraboxBatch_%s.zip", reqID))
	defer os.Remove(zipFilePath)

	err = createZipFromDir(tmpDir, zipFilePath)
	if err != nil {
		logger.Error("Gagal membuat ZIP", zap.Error(err))
		_ = media.EditHTML(ctx, client, peer, menuMsgID, "❌ Gagal membuat file ZIP.")
		return
	}

	_ = media.EditHTML(ctx, client, peer, menuMsgID, "🚀 <b>Mengunggah ZIP ke Telegram...</b>")

	up := uploader.NewUploader(client).WithThreads(2)
	uploadFile, err := up.FromPath(ctx, zipFilePath)
	if err != nil {
		logger.Error("Gagal unggah ke Telegram", zap.Error(err))
		_ = media.EditHTML(ctx, client, peer, menuMsgID, "❌ Gagal mengunggah ZIP ke Telegram (Batas file atau kesalahan jaringan).")
		return
	}

	successCount := len(files) - len(dlErrors)
	caption := fmt.Sprintf("📦 <b>Terabox Batch Download</b>\n\n✅ Berhasil: %d/%d File\n\n@Kometika_bot", successCount, len(files))

	docReq := &tg.MessagesSendMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaUploadedDocument{
			File:     uploadFile,
			MimeType: "application/zip",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: fmt.Sprintf("Terabox_%s.zip", reqID)},
			},
		},
		Message:  caption,
		RandomID: time.Now().UnixNano(),
	}

	_, err = client.MessagesSendMedia(ctx, docReq)
	if err != nil {
		logger.Error("Gagal kirim media doc ZIP", zap.Error(err))
		_ = media.EditHTML(ctx, client, peer, menuMsgID, "❌ Terjadi kesalahan saat mengirim dokumen.")
		return
	}

	_ = deleteGroupMessage(ctx, client, peer, menuMsgID)
}

func downloadFileToPath(ctx context.Context, url string, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "terabox;1.34.0.4;PC;PC-Windows;10.0.19045;WindowsTeraBox")

	client := &http.Client{} // Tanpa timeout agar file besar tidak terpotong
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("status HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func createZipFromDir(sourceDir, zipFilePath string) error {
	zipFile, err := os.Create(zipFilePath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = filepath.Base(path)
		header.Method = zip.Deflate

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}
