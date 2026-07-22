package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/log"
	"mybot/internal/media"
)

// LoadingContext menyimpan informasi pesan loading
type LoadingContext struct {
	Peer          tg.InputPeerClass
	ProgressMsgID int
	ReplyTo       *tg.InputReplyToMessage
	Platform      string
}

// WithLoading adalah middleware yang menangani logika loading message
// Ia akan mengirim pesan loading, menjalankan handler, lalu mengedit atau menghapus pesan loading
func WithLoading(
	ctx context.Context,
	client *tg.Client,
	msg *tg.Message,
	entities tg.Entities,
	platform string,
	logger *zap.Logger,
	handler func(ctx context.Context, lc *LoadingContext) error,
) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal mendapatkan peer", zap.Error(err))
		log.LogError(ctx, "WithLoading.GetPeer", err, "platform="+platform)
		return err
	}

	var topicID int
	var replyTo *tg.InputReplyToMessage

	// Cek apakah ini grup/channel
	isGroup := true
	if _, ok := msg.PeerID.(*tg.PeerUser); ok {
		isGroup = false
	}

	if isGroup {
		topicID = getTopicID(msg)
		replyTo = buildReplyTo(msg.ID, topicID)
	}

	// Kirim pesan loading
	loadingText := fmt.Sprintf("⏳ Memproses %s, mohon tunggu...", platform)
	progressMsgID, err := sendLoadingMessage(ctx, client, peer, loadingText, replyTo, logger)
	if err != nil {
		logger.Warn("Gagal mengirim pesan loading", zap.Error(err))
		// Tetap lanjutkan meskipun gagal kirim loading
		progressMsgID = 0
	}

	lc := &LoadingContext{
		Peer:          peer,
		ProgressMsgID: progressMsgID,
		ReplyTo:       replyTo,
		Platform:      platform,
	}

	// Jalankan handler utama
	handlerErr := handler(ctx, lc)

	// Hapus pesan loading setelah handler selesai
	if progressMsgID != 0 {
		go deleteLoadingMessage(client, peer, progressMsgID, logger)
	}

	return handlerErr
}

// sendLoadingMessage mengirim pesan loading dan mengembalikan message ID
func sendLoadingMessage(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	text string,
	replyTo *tg.InputReplyToMessage,
	logger *zap.Logger,
) (int, error) {
	randID, err := media.RandomID()
	if err != nil {
		return 0, err
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: randID,
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	// Retry dengan FloodWait handling
	for {
		updates, err := client.MessagesSendMessage(ctx, req)
		if err != nil {
			if d, ok := tgerr.AsFloodWait(err); ok {
				logger.Warn("FloodWait saat kirim loading", zap.Duration("wait", d))
				select {
				case <-time.After(d + time.Second):
					continue
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}
			return 0, err
		}

		msgID, err := media.ExtractMessageID(updates)
		if err != nil {
			return 0, err
		}
		return msgID, nil
	}
}

// deleteLoadingMessage menghapus pesan loading dengan delay
func deleteLoadingMessage(client *tg.Client, peer tg.InputPeerClass, msgID int, logger *zap.Logger) {
	time.Sleep(1 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := deleteGroupMessage(ctx, client, peer, msgID)
	if err != nil {
		logger.Warn("Gagal menghapus pesan loading", zap.Int("msg_id", msgID), zap.Error(err))
	}
}

// EditLoadingMessage mengedit pesan loading menjadi teks baru
func EditLoadingMessage(
	ctx context.Context,
	client *tg.Client,
	peer tg.InputPeerClass,
	msgID int,
	newText string,
	logger *zap.Logger,
) error {
	if msgID == 0 {
		return nil
	}

	// Retry dengan FloodWait handling
	for {
		_, err := client.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
			Peer:    peer,
			ID:      msgID,
			Message: newText,
		})
		if err != nil {
			if d, ok := tgerr.AsFloodWait(err); ok {
				logger.Warn("FloodWait saat edit loading", zap.Duration("wait", d))
				select {
				case <-time.After(d + time.Second):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}
		return nil
	}
}
