package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mybot/internal/config"
	"mybot/internal/log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// ======================== STATE ========================

var (
	businessStateMu sync.RWMutex
	businessState   = make(map[string]*BusinessState)
)

var keywordReplacer = strings.NewReplacer(
	",", " ", ".", " ", "!", " ", "?", " ", ";", " ", ":", " ",
)

type BusinessState struct {
	ConnectionID string
	DCID         int
	Rights       *tg.BusinessBotRights
	UserID       int64
}

func SetBusinessState(connID string, state *BusinessState) {
	businessStateMu.Lock()
	defer businessStateMu.Unlock()
	businessState[connID] = state
}

func GetBusinessState(connID string) (*BusinessState, bool) {
	businessStateMu.RLock()
	defer businessStateMu.RUnlock()
	state, ok := businessState[connID]
	return state, ok
}

func randomDelay(ctx context.Context, minSec, maxSec int) error {
	d := time.Duration(minSec+rand.Intn(maxSec-minSec+1)) * time.Second
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func RemoveBusinessState(connID string) {
	businessStateMu.Lock()
	defer businessStateMu.Unlock()
	delete(businessState, connID)
}

// ======================== STICKER CONFIG ========================

const (
	StickerSetName = "te4_lei3_xi1_ya4_by_fStikBot" // ganti sesuai set_name
	StickerEmoji   = "🤠"                            // ganti sesuai emoji
)

// ======================== KEYWORD MAP ========================

var keywordMap = map[string]bool{
	"halo":            true,
	"yumi":            true,
	"ruri":            true,
	"hai":             true,
	"hi":              true,
	"hello":           true,
	"hallo":           true,
	"helo":            true,
	"hlo":             true,
	"hy":              true,
	"yi":              true,
	"oy":              true,
	"p":               true,
	"ping":            true,
	"test":            true,
	"selamat":         true,
	"pagi":            true,
	"siang":           true,
	"sore":            true,
	"malam":           true,
	"assalamualaikum": true,
	"waalaikumsalam":  true,
	"slmt":            true,
	"pgi":             true,
	"bro":             true,
	"sis":             true,
	"boss":            true,
	"bos":             true,
	"min":             true,
	"kak":             true,
	"ko":              true,
	"mbak":            true,
	"mas":             true,
	"dek":             true,
	"ndu":             true,
	"tengkiu":         true,
	"makasih":         true,
	"thx":             true,
	"thanks":          true,
	"hey":             true,
	"good":            true,
	"morning":         true,
	"afternoon":       true,
	"evening":         true,
	"greetings":       true,
	"yo":              true,
	"sup":             true,
	"what's up":       true,
	"howdy":           true,
	"halo kak":        true,
	"halo min":        true,
	"hai admin":       true,
	"permisi":         true,
	"mau tanya":       true,
	"boleh tanya":     true,
	"nanya dong":      true,
	"gan":             true,
	"agya":            true,
	"om":              true,
	"beb":             true,
}

// ======================== AI CONFIG (GROQ) ========================

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash:generateContent"

func getGeminiAPIKey() string {
	return os.Getenv("GEMINI_API_KEY")
}

func generateAIReply(ctx context.Context, userMessage string) (string, error) {
	apiKey := getGeminiAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY tidak ditemukan di environment")
	}

	systemPrompt := `
Kamu adalah Asisten AI yang ceria, ramah, dan profesional untuk bot Telegram. Kamu juga berjiwa Wibu dan sangat suka anime.

Tugasmu adalah membalas pesan masuk dari pengguna, terutama saat mereka menyapa atau mengirim pertanyaan singkat.

Aturan utama yang harus kamu patuhi:
1. Gunakan Bahasa Indonesia yang santun, alami, dan hangat. Jangan terdengar seperti robot kaku.
2. Balasan harus SINGKAT, maksimal 60 kata.
3. Jangan pernah mengulang kalimat yang sama persis. Buatlah variasi jawaban secara acak. Contohnya: "Eh, halo! Silakan tunggu admin online, ya :3", atau jika ada yang bertanya "beb ngapain?", kamu bisa membalas "Duh, beb lagi offline nih. Ini pesan otomatis dari bot!". Jangan lupa untuk selalu mengingatkan bahwa kamu adalah bot dan pesan akan dibalas saat admin online.
4. JANGAN mengajukan pertanyaan balik yang rumit atau membutuhkan jawaban panjang, karena ini pesan otomatis dan pengguna mungkin hanya ingin menyapa.
5. Jangan memberikan informasi spesifik tentang lokasi, jam buka, atau nomor admin, karena kamu hanya asisten otomatis.
6. Jika pesan pengguna berisi pertanyaan teknis, rumit, atau meminta bantuan khusus, balas dengan ramah bahwa pesan akan diteruskan ke admin dan akan direspon segera.
7. Jangan membalas dengan kata-kata negatif, kasar, atau defensif. Selalu jaga nada bicara yang menyenangkan.
8. Jika pengguna hanya mengirim stiker, kamu cukup membalas dengan stiker balasan (ini sudah diatur oleh sistem, kamu tidak perlu menulis balasan teks untuk stiker).
9. Kamu boleh menambahkan emoji dan karakter lucu seperti :3, -_-, '_', atau :v.

Tujuan utama kamu adalah membuat pengguna merasa dihargai dan diperhatikan, serta memberikan kesan pertama yang baik, meskipun hanya dengan balasan singkat.
`

	// Gabungkan system prompt dengan pesan user
	fullPrompt := systemPrompt + "\n\nPesan dari pengguna: " + userMessage

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": fullPrompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gagal encode JSON: %w", err)
	}

	url := geminiBaseURL + "?key=" + apiKey
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("gagal buat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second} // Gemini mungkin lebih lambat, beri timeout lebih longgar
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gagal panggil Gemini: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gagal baca response: %w", err)
	}

	// Struct untuk response Gemini
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("gagal parse JSON: %w, body: %s", err, string(body))
	}

	if result.Error.Message != "" {
		return "", fmt.Errorf("error dari Gemini: %s", result.Error.Message)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini tidak mengembalikan teks")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}

// ======================== FUNGSI UTAMA ========================

func BusinessMessageHandler(ctx context.Context, tgClient *tg.Client, update *tg.UpdateBotNewBusinessMessage, entities tg.Entities, logger *zap.Logger) error {
	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}
	connID := update.ConnectionID
	msgID := msg.ID

	state, _ := GetBusinessState(connID)

	if state != nil && msg.FromID != nil {
		if peerUser, ok := msg.FromID.(*tg.PeerUser); ok && peerUser.UserID == state.UserID {
			logger.Debug("Message from business owner, ignored",
				zap.Int64("owner_id", state.UserID),
				zap.String("conn_id", connID),
			)
			return nil
		}
	}

	// 1. CEK STIKER (harus sebelum guard teks kosong)
	if isSticker(msg) {
		state, _ := GetBusinessState(connID)
		peer, err := GetPeerFromMessage(ctx, tgClient, msg, entities)
		if err != nil {
			logger.Error("Gagal dapat peer untuk stiker", zap.Error(err))
			log.LogError(ctx, "BusinessMessageHandler.GetPeer", err,
				"conn_id: "+connID,
				"msg_id: "+strconv.Itoa(msgID),
			)
			return nil
		}

		// Mark as read
		if state != nil && state.Rights != nil && state.Rights.ReadMessages {
			if err := markAsRead(ctx, tgClient, connID, peer, msgID); err != nil {
				logger.Error("Gagal mark as read stiker", zap.Error(err))
				log.LogError(ctx, "BusinessMessageHandler.markAsRead", err,
					"conn_id: "+connID,
					"msg_id: "+strconv.Itoa(msgID),
				)
			}
		}

		// Balas dengan stiker
		if state != nil && state.Rights != nil && state.Rights.Reply {
			if err := randomDelay(ctx, 2, 8); err != nil {
				return err
			}
			if err := sendBusinessSticker(ctx, tgClient, connID, peer, msgID); err != nil {
				logger.Error("Gagal kirim balasan stiker", zap.Error(err))
				log.LogError(ctx, "BusinessMessageHandler.replySticker", err,
					"conn_id: "+connID,
					"msg_id: "+strconv.Itoa(msgID),
				)
			}
		}
		return nil
	}

	// 2. Jika teks kosong (bukan stiker) → abaikan
	if msg.Message == "" {
		return nil
	}
	text := msg.Message

	peer, err := GetPeerFromMessage(ctx, tgClient, msg, entities)
	if err != nil {
		logger.Error("Gagal dapat peer", zap.Error(err))
		log.LogError(ctx, "BusinessMessageHandler.GetPeer", err,
			"conn_id: "+connID,
			"msg_id: "+strconv.Itoa(msgID),
		)
		return nil
	}

	logger.Info("Business message received",
		zap.String("text", text),
		zap.String("conn_id", connID),
	)

	if state == nil {
		logger.Warn("State is nil, cannot reply", zap.String("conn_id", connID))
	} else if state.Rights == nil {
		logger.Warn("Rights is nil, cannot reply", zap.String("conn_id", connID))
	} else {
		logger.Info("Rights status",
			zap.Bool("can_read", state.Rights.ReadMessages),
			zap.Bool("can_reply", state.Rights.Reply),
			zap.String("conn_id", connID),
		)
	}

	// Mark as read selalu (jika hak tersedia)
	if state != nil && state.Rights != nil && state.Rights.ReadMessages {
		if err := markAsRead(ctx, tgClient, connID, peer, msgID); err != nil {
			logger.Error("Gagal mark as read", zap.Error(err))
			log.LogError(ctx, "BusinessMessageHandler.markAsRead", err,
				"conn_id: "+connID,
				"msg_id: "+strconv.Itoa(msgID),
			)
		}
	}

	shouldReply := !config.IsSupportedURL(text) && !isCommand(text) && matchKeyword(text)

	logger.Info("Reply decision (AI)",
		zap.Bool("should_reply", shouldReply),
		zap.String("conn_id", connID),
	)

	if shouldReply {
		if state != nil && state.Rights != nil && state.Rights.Reply {
			aiReply, err := generateAIReply(ctx, text)

			var finalReply string
			if err != nil {
				logger.Warn("AI gagal, pakai fallback", zap.Error(err))
				finalReply = "Maaf, AI sedang sibuk. Pesan akan dibalas nanti oleh admin."
			} else {
				finalReply = aiReply
				logger.Info("AI reply generated", zap.String("reply", finalReply))
			}

			if err := randomDelay(ctx, 3, 10); err != nil {
				return err
			}

			if err := sendBusinessReply(ctx, tgClient, connID, peer, msgID, finalReply); err != nil {
				logger.Error("Gagal kirim balasan AI", zap.Error(err))
				log.LogError(ctx, "BusinessMessageHandler.sendReply", err,
					"conn_id: "+connID,
					"msg_id: "+strconv.Itoa(msgID),
					"reply: "+finalReply,
				)
			} else {
				logger.Info("AI Reply sent successfully", zap.String("conn_id", connID))
			}
		} else {
			logger.Warn("Reply skipped: no rights or state",
				zap.Bool("state_exists", state != nil),
				zap.Bool("can_reply", state != nil && state.Rights != nil && state.Rights.Reply),
				zap.String("conn_id", connID),
			)
		}
	}
	return nil
}

// ======================== DETEKSI STIKER ========================

func isSticker(msg *tg.Message) bool {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return false
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok {
		return false
	}
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeSticker); ok {
			return true
		}
	}
	return false
}

// ======================== GET STIKER DOCUMENT ========================

func getStickerDocumentByEmoji(ctx context.Context, tgClient *tg.Client, setName, emoji string) (*tg.Document, error) {
	req := &tg.MessagesGetStickerSetRequest{
		Stickerset: &tg.InputStickerSetShortName{ShortName: setName},
	}
	res, err := tgClient.MessagesGetStickerSet(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get sticker set: %w", err)
	}

	m, ok := res.AsModified()
	if !ok {
		return nil, fmt.Errorf("sticker set %q tidak dapat dibaca (not modified)", setName)
	}

	for _, docInterface := range m.Documents {
		doc, ok := docInterface.(*tg.Document)
		if !ok {
			continue
		}
		for _, attr := range doc.Attributes {
			if stickerAttr, ok := attr.(*tg.DocumentAttributeSticker); ok {
				if stickerAttr.Alt == emoji {
					return doc, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("sticker dengan emoji %q tidak ditemukan di set %q", emoji, setName)
}

// ======================== KIRIM STIKER ========================

func sendBusinessSticker(ctx context.Context, tgClient *tg.Client, connID string, peer tg.InputPeerClass, replyToMsgID int) error {
	doc, err := getStickerDocumentByEmoji(ctx, tgClient, StickerSetName, StickerEmoji)
	if err != nil {
		return fmt.Errorf("ambil dokumen stiker: %w", err)
	}

	randomID, err := crypto.RandInt64(crypto.DefaultRand())
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}

	media := &tg.InputMediaDocument{
		ID: doc.AsInput(),
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		RandomID: randomID,
		ReplyTo:  &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID},
	}

	wrapped := &tg.InvokeWithBusinessConnectionRequest{
		ConnectionID: connID,
		Query:        req,
	}

	var box tg.UpdatesBox
	return tgClient.Invoker().Invoke(ctx, wrapped, &box)
}

// ======================== LOGIKA REPLY TEKS ========================

func determineReply(text string) (string, bool) {
	if config.IsSupportedURL(text) || isCommand(text) {
		return "", false
	}
	if matchKeyword(text) {
		return "Chat akan di balas kalau saya Online, pesan otomatis.", true
	}
	return "", false
}

func isCommand(text string) bool {
	prefixes := []string{"/", ".", "!", "#"}
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// matchKeyword mendukung pencocokan kata tunggal maupun frasa (multi-word)
func matchKeyword(text string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	// 1. Cek per kata
	for _, word := range strings.Fields(cleaned) {
		if keywordMap[word] {
			return true
		}
	}
	// 2. Cek frasa (key yang mengandung spasi)
	for key := range keywordMap {
		if strings.Contains(key, " ") && strings.Contains(cleaned, key) {
			return true
		}
	}
	return false
}

// ======================== API WRAPPER ========================

func markAsRead(ctx context.Context, tgClient *tg.Client, connID string, peer tg.InputPeerClass, maxID int) error {
	req := &tg.MessagesReadHistoryRequest{
		Peer:  peer,
		MaxID: maxID,
	}
	wrapped := &tg.InvokeWithBusinessConnectionRequest{
		ConnectionID: connID,
		Query:        req,
	}
	var result tg.MessagesAffectedMessages
	return tgClient.Invoker().Invoke(ctx, wrapped, &result)
}

func sendBusinessReply(ctx context.Context, tgClient *tg.Client, connID string, peer tg.InputPeerClass, replyToMsgID int, text string) error {
	randomID, err := crypto.RandInt64(crypto.DefaultRand())
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}
	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		ReplyTo:  &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID},
		RandomID: randomID,
	}
	wrapped := &tg.InvokeWithBusinessConnectionRequest{
		ConnectionID: connID,
		Query:        req,
	}
	var box tg.UpdatesBox
	return tgClient.Invoker().Invoke(ctx, wrapped, &box)
}
