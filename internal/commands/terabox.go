package commands

import (
	"context"
	"crypto/rand"
	"fmt"
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
	"mybot/internal/log"
)

// ─── Cache ───────────────────────────────────────────────────────────────────

type teraboxSession struct {
	Files       []terabox.TeraboxUniversalData
	MenuMsgID   int
	ActionMsgID int
	Page        int
	Peer        tg.InputPeerClass
}

var (
	teraboxCache = make(map[string]teraboxSession)
	tbMutex      sync.RWMutex
)

// ─── Helper ──────────────────────────────────────────────────────────────────

func generateShortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// formatFileNameButton memendekkan nama file untuk tombol inline keyboard.
// Kepala + ekor nama asli dipertahankan, dan ekstensi (format) selalu terlihat.
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
	return string(baseRunes[:head]) + "…" + string(baseRunes[len(baseRunes)-tail:]) + ext
}

// getExt mengembalikan ekstensi file (termasuk titik) atau string kosong.
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

// ─── Entry point ─────────────────────────────────────────────────────────────

func HandleTerabox(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string) error {
	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("terabox") {
		zap.L().Info("Fitur Terabox dinonaktifkan")
		return nil
	}
	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		// Jika bukan Private Chat (artinya Grup/Supergroup/Channel),
		// langsung kembalikan nil agar bot mengabaikan tanpa error/pesan apapun.
		return nil
	}
	switch msg.PeerID.(type) {
	case *tg.PeerUser:
		return handleTeraboxPrivate(ctx, client, msg, url)
	case *tg.PeerChat, *tg.PeerChannel:
		return handleTeraboxGroup(ctx, client, msg, entities, url)
	}
	return nil
}

func handleTeraboxPrivate(ctx context.Context, client *tg.Client, msg *tg.Message, url string) error {
	peerUser := msg.PeerID.(*tg.PeerUser)
	accessHash, err := GetUserAccessHash(ctx, client, peerUser.UserID)
	if err != nil {
		zap.L().Warn("Gagal dapat access hash Terabox", zap.Error(err))
		return nil
	}
	peer := &tg.InputPeerUser{UserID: peerUser.UserID, AccessHash: accessHash}
	return fetchAndShowTerabox(ctx, client, peer, url, nil)
}

func handleTeraboxGroup(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		zap.L().Error("Gagal dapatkan peer grup Terabox", zap.Error(err))
		return err
	}
	topicID := getTopicID(msg)
	replyTo := buildReplyTo(msg.ID, topicID)
	return fetchAndShowTerabox(ctx, client, peer, url, replyTo)
}

// ─── Core logic ──────────────────────────────────────────────────────────────

func fetchAndShowTerabox(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage) error {
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
	// hapus pesan load
	if loadingMsgID != 0 {
		deleteGroupMessage(ctx, client, peer, loadingMsgID)
	}

	reqID := generateShortID()
	markup := buildTeraboxFileMarkup(files, reqID, 0)
	plainText := fmt.Sprintf("🗂 Data Terabox Ditemukan!\nTotal File: %d\n\nPilih file untuk diunduh:", len(files))

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
	}

	// 5. Simpan ke cache
	tbMutex.Lock()
	teraboxCache[reqID] = teraboxSession{
		Files:       files,
		MenuMsgID:   menuMsgID,
		ActionMsgID: 0,
		Page:        0,
		Peer:        peer,
	}
	tbMutex.Unlock()

	// 6. Timer 5m30s: hapus pesan menu dan action, bersihkan cache
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

	return nil
}

// buildTeraboxFileMarkup membuat inline keyboard daftar file dengan pagination.
func buildTeraboxFileMarkup(files []terabox.TeraboxUniversalData, reqID string, page int) *tg.ReplyInlineMarkup {
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

	return &tg.ReplyInlineMarkup{Rows: rows}
}

// sendTeraboxMenu dipakai untuk navigasi halaman (edit pesan yang ada).
func sendTeraboxMenu(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, reqID string, page int) error {
	tbMutex.RLock()
	sess, exists := teraboxCache[reqID]
	tbMutex.RUnlock()
	if !exists {
		return media.EditHTML(ctx, client, peer, msgID, "❌ Sesi Terabox telah kadaluarsa. Silakan kirim ulang link.")
	}
	fm := buildTeraboxFileMarkup(sess.Files, reqID, page)
	plainText := fmt.Sprintf("🗂 Data Terabox Ditemukan!\nTotal File: %d\nHalaman: %d\n\nPilih file untuk diunduh:", len(sess.Files), page+1)
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
	fm := buildTeraboxFileMarkup(sess.Files, reqID, page)
	plainText := fmt.Sprintf("🗂 Data Terabox Ditemukan!\nTotal File: %d\nHalaman: %d\n\nPilih file untuk diunduh:", len(sess.Files), page+1)
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

// buildActionMarkup membuat keyboard untuk aksi (Stream, Download, Kembali)
func buildActionMarkup(reqID string, index int, hasStream bool) *tg.ReplyInlineMarkup {
	var buttons []tg.KeyboardButtonClass
	if hasStream {
		buttons = append(buttons, markup.Callback("▶️ Stream", fmt.Appendf(nil, "tb_stream_%s_%d", reqID, index), markup.StyleBgSuccess()))
	}
	buttons = append(buttons,
		markup.Callback("⬇️ Download", fmt.Appendf(nil, "tb_down_%s_%d", reqID, index), markup.StyleBgSuccess()),
		markup.Callback("🔙 Kembali", fmt.Appendf(nil, "tb_back_%s", reqID), markup.StyleBgDanger()),
	)
	return &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: buttons}}}
}

// ─── Callback handler ────────────────────────────────────────────────────────

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

	// ── close ──
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

	// Cek sesi masih ada
	tbMutex.RLock()
	sess, exists := teraboxCache[reqID]
	tbMutex.RUnlock()
	if !exists {
		media.AnswerCallback(ctx, client, update.QueryID, "❌ Sesi kadaluarsa. Kirim ulang link.", true)
		return nil
	}

	// ── page ── (navigasi halaman)
	if action == "page" {
		if len(parts) < 4 {
			media.AnswerCallback(ctx, client, update.QueryID, "Data tidak lengkap", true)
			return nil
		}
		pageVal, _ := strconv.Atoi(parts[3])
		_ = sendTeraboxMenu(ctx, client, peer, msgID, reqID, pageVal)
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		return nil
	}

	// ── back ── (kembali ke menu file dari pesan aksi)
	if action == "back" {
		if sess.ActionMsgID != 0 {
			deleteGroupMessage(ctx, client, peer, sess.ActionMsgID)
		}
		newMenuID, err := sendNewTeraboxMenu(ctx, client, peer, reqID, sess.Page, nil)
		if err != nil {
			media.AnswerCallback(ctx, client, update.QueryID, "Gagal memuat menu", true)
			return nil
		}
		// Update cache: MenuMsgID = newMenuID, ActionMsgID = 0
		tbMutex.Lock()
		if s, ok := teraboxCache[reqID]; ok {
			s.MenuMsgID = newMenuID
			s.ActionMsgID = 0
			s.Page = sess.Page
			teraboxCache[reqID] = s
		}
		tbMutex.Unlock()
		// Hapus pesan lama (menu sebelumnya yang mungkin masih ada)
		if msgID != 0 && msgID != newMenuID {
			deleteGroupMessage(ctx, client, peer, msgID)
		}
		media.AnswerCallback(ctx, client, update.QueryID, "", false)
		return nil
	}

	// Aksi yang memerlukan index file (dl, stream, down)
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

	// ── dl: user memilih file dari menu → tampilkan aksi (thumbnail + tombol) dan hapus menu lama
	if action == "dl" {
		media.AnswerCallback(ctx, client, update.QueryID, "", false)

		if sess.MenuMsgID != 0 {
			deleteGroupMessage(ctx, client, peer, sess.MenuMsgID)
		}

		htmlCaption := fmt.Sprintf("File Name: %s \n\nPilih aksi:", fileTarget.FileName)
		markup := buildActionMarkup(reqID, indexVal, fileTarget.StreamURL != "")

		logger.Info("File target",
			zap.String("filename", fileTarget.FileName),
			zap.String("stream_url", fileTarget.StreamURL),
			zap.String("download_url", fileTarget.DownloadURL),
		)

		var actionMsgID int

		// Kirim thumbnail (jika ada) sebagai foto + keyboard, atau kirim teks biasa + keyboard
		if fileTarget.Thumbnail != "" {
			data, err := api.GetThumbnail(ctx, fileTarget.Thumbnail)
			if err != nil {
				logger.Warn("Gagal download thumbnail", zap.String("url", fileTarget.Thumbnail), zap.Error(err))
				reqSend := &tg.MessagesSendMessageRequest{
					Peer:        peer,
					Message:     htmlCaption,
					RandomID:    time.Now().UnixNano(),
					ReplyMarkup: markup,
				}
				updates, err := client.MessagesSendMessage(ctx, reqSend)
				if err == nil {
					actionMsgID, _ = media.ExtractMessageID(updates)
				}
			} else {
				up := uploader.NewUploader(client)
				inputFile, err := up.FromBytes(ctx, "thumbnail.jpg", data)
				if err != nil {
					logger.Warn("Gagal upload thumbnail", zap.Error(err))
					reqSend := &tg.MessagesSendMessageRequest{
						Peer:        peer,
						Message:     htmlCaption,
						RandomID:    time.Now().UnixNano(),
						ReplyMarkup: markup,
					}
					updates, err := client.MessagesSendMessage(ctx, reqSend)
					if err == nil {
						actionMsgID, _ = media.ExtractMessageID(updates)
					}
				} else {
					req := &tg.MessagesSendMediaRequest{
						Peer:        peer,
						RandomID:    time.Now().UnixNano(),
						Media:       &tg.InputMediaUploadedPhoto{File: inputFile},
						Message:     htmlCaption,
						ReplyMarkup: markup,
					}
					updates, err := client.MessagesSendMedia(ctx, req)
					if err != nil {
						logger.Warn("Gagal kirim foto thumbnail", zap.Error(err))
					} else {
						actionMsgID, _ = media.ExtractMessageID(updates)
					}
				}
			}
		} else {
			// Tidak ada thumbnail, kirim pesan teks
			reqSend := &tg.MessagesSendMessageRequest{
				Peer:        peer,
				Message:     htmlCaption,
				RandomID:    time.Now().UnixNano(),
				ReplyMarkup: markup,
			}
			updates, err := client.MessagesSendMessage(ctx, reqSend)
			if err == nil {
				actionMsgID, _ = media.ExtractMessageID(updates)
			}
		}

		// Update cache: set MenuMsgID = 0 (sudah dihapus), ActionMsgID = actionMsgID
		tbMutex.Lock()
		if s, ok := teraboxCache[reqID]; ok {
			s.MenuMsgID = 0
			s.ActionMsgID = actionMsgID
			teraboxCache[reqID] = s
		}
		tbMutex.Unlock()
		return nil
	}

	// ── stream ──
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
		htmlMsg := fmt.Sprintf("▶️ %s\n\n@Kometika_bot", fileTarget.FileName)
		streamMarkup := markup.InlineKeyboard(markup.Row(markup.URL("🔗 Buka Stream", shortURL, markup.StyleBgSuccess())))
		reqSend := &tg.MessagesSendMessageRequest{
			Peer:        peer,
			Message:     htmlMsg,
			RandomID:    time.Now().UnixNano(),
			ReplyMarkup: streamMarkup,
		}
		if _, err := client.MessagesSendMessage(ctx, reqSend); err != nil {
			logger.Warn("Gagal kirim tombol stream", zap.Error(err))
		}
		return nil
	}

	// ── down ──
	if action == "down" {
		media.AnswerCallback(ctx, client, update.QueryID, "", false)

		downloadURL := fileTarget.DownloadURL
		if fileTarget.StreamURL != "" {
			downloadURL = fileTarget.StreamURL
		}

		sizeBytes := parseFileSizeToBytes(fileTarget.FileSize)
		const maxSize = 400 * 1024 * 1024 // 400 MB

		if sizeBytes < maxSize {
			// Ambil stream
			stream, _, err := api.GetVideoStream(ctx, downloadURL)
			if err != nil {
				logger.Error("Gagal stream file", zap.Error(err))
				log.LogError(ctx, "TeraboxStream", err, "filename="+fileTarget.FileName, "url="+downloadURL)
				_ = media.SendHTML(ctx, client, peer, "❌ Gagal mengambil stream file.")
				return nil
			}
			defer stream.Close()

			// ═══ DETEKSI TIPE KONTEN OTOMATIS ═══
			info, fullStream, err := api.DetectAndClassifyStream(stream)
			if err != nil {
				logger.Warn("Gagal deteksi tipe konten, fallback ke dokumen", zap.Error(err))
				// Fallback: gunakan info default untuk dokumen
				info = api.ContentTypeInfo{
					MimeType:  "application/octet-stream",
					Category:  api.ContentDocument,
					Extension: "",
				}
				fullStream = stream // stream asli (tanpa 512 byte) – data awal mungkin hilang
			}

			// ═══ PERSIAPAN THUMBNAIL ═══
			var thumbFile tg.InputFileClass
			if fileTarget.Thumbnail != "" {
				thumbData, err := api.GetThumbnail(ctx, fileTarget.Thumbnail)
				if err == nil && len(thumbData) > 0 {
					upThumb := uploader.NewUploader(client)
					thumbFile, _ = upThumb.FromBytes(ctx, "thumb_terabox.jpg", thumbData)
					logger.Info("Thumbnail berhasil disiapkan", zap.String("filename", fileTarget.FileName))
				} else {
					logger.Warn("Gagal download thumbnail untuk file", zap.String("filename", fileTarget.FileName), zap.Error(err))
				}
			}

			captionPlain := fmt.Sprintf("📁 %s\n\n@Kometika_bot", fileTarget.FileName)

			// ═══ KIRIM DENGAN DYNAMIC STREAM ═══
			mediaSender := media.NewMediaSender(client)
			_, err = mediaSender.SendDynamicStream(
				ctx,
				peer,
				fullStream,
				info,
				fileTarget.FileName,
				captionPlain,
				nil, // replyMarkup
				nil, // replyTo (tidak digunakan di sini)
				thumbFile,
			)

			if err != nil {
				logger.Error("Gagal upload ke Telegram", zap.Error(err))
				_ = media.SendHTML(ctx, client, peer, "❌ Gagal mengirim file.")
			} else {
				logger.Info("Upload sukses", zap.String("filename", fileTarget.FileName))
			}
		} else {
			// File > 400 MB: kirim D-URL pendek sebagai tombol
			shortURL, err := api.ShortenWithFallback(downloadURL)
			if err != nil {
				shortURL = downloadURL
			}
			htmlMsg := fmt.Sprintf("⬇️ %s\n\n📦 Ukuran: %s\n\n@Kometika_bot", fileTarget.FileName, fileTarget.FileSize)
			downMarkup := markup.InlineKeyboard(markup.Row(markup.URL("🔗 Unduh File", shortURL, markup.StyleBgSuccess())))
			reqSend := &tg.MessagesSendMessageRequest{
				Peer:        peer,
				Message:     htmlMsg,
				RandomID:    time.Now().UnixNano(),
				ReplyMarkup: downMarkup,
			}
			if _, err := client.MessagesSendMessage(ctx, reqSend); err != nil {
				logger.Warn("Gagal kirim tombol unduh", zap.Error(err))
			}
		}
		return nil
	}

	return nil
}
