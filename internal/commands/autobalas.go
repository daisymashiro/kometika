package commands

import (
	"context"
	"fmt"
	"math/rand"
	"mybot/internal/cache"
	"mybot/internal/config"
	"mybot/internal/log"
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

	// ===== CEK STATUS OWNER SEBELUM PROSES PESAN =====
	// Jika owner online atau mode manual, bot tidak proses pesan
	if !cache.ShouldBotReply() {
		logger.Info("Bot skip business message: owner online atau mode manual",
			zap.String("conn_id", connID),
			zap.Bool("owner_online", cache.IsOwnerOnline()),
			zap.String("bot_mode", cache.GetBotMode()),
		)
		return nil
	}
	// ===== END CEK STATUS OWNER =====

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

	shouldReply := !config.IsSupportedURL(text) && !isCommand(text)

	logger.Info("Reply decision (AI)",
		zap.Bool("should_reply", shouldReply),
		zap.String("conn_id", connID),
	)

	if shouldReply {

		if state != nil && state.Rights != nil && state.Rights.Reply {
			aiCtx, aiCancel := context.WithTimeout(ctx, 35*time.Second)
			defer aiCancel()
			aiReply, provider, err := GenerateWithFallback(aiCtx, text, logger)

			var finalReply string
			if err != nil {
				logger.Warn("Semua AI provider gagal, pakai fallback", zap.Error(err))
				finalReply = "Maaf, AI sedang sibuk. Pesan akan dibalas nanti oleh admin."
			} else {
				finalReply = aiReply
				logger.Info("AI reply generated",
					zap.String("provider", provider),
					zap.String("reply", finalReply),
				)
			}

			if err := randomDelay(ctx, 20, 45); err != nil {
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

func isCommand(text string) bool {
	prefixes := []string{"/", ".", "!", "#"}
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
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
