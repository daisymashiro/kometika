package commands

import (
	"context"
	"time"

	"mybot/internal/api/aceimg"
	"mybot/internal/log"
	"mybot/internal/media"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func HandleAceImg(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	if _, ok := msg.PeerID.(*tg.PeerUser); !ok {
		peer, err := GetPeerFromMessage(ctx, client, msg, entities)
		if err != nil || peer == nil {
			logger.Error("Gagal dapatkan peer grup", zap.Error(err))
			return err
		}
		topicID := getTopicID(msg)
		replyTo := buildReplyTo(msg.ID, topicID)
		return handleAceImgGroup(ctx, client, peer, url, replyTo, logger)
	}

	// ── Private chat ────────────────────────────────────────────────────────
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer private", zap.Error(err))
		log.LogError(ctx, "AceImgGetPeer", err, "url="+url)
		return err
	}

	msgSender := message.NewSender(client)

	// Kirim pesan progress
	progressMsg, err := msgSender.To(peer).Text(ctx, "⏳ Memproses AceImg, mohon tunggu...")
	if err != nil {
		logger.Warn("Gagal kirim progress", zap.Error(err))
	} else {
		progressMsgID, _ := media.ExtractMessageID(progressMsg)
		defer func() {
			if progressMsgID == 0 {
				return
			}
			go func() {
				time.Sleep(1 * time.Second)
				_, _ = client.MessagesDeleteMessages(context.Background(), &tg.MessagesDeleteMessagesRequest{
					Revoke: true,
					ID:     []int{progressMsgID},
				})
			}()
		}()
	}

	// Ekstrak info dari URL
	result, err := aceimg.Extract(url)
	if err != nil {
		logger.Warn("Gagal ekstrak URL AceImg", zap.Error(err))
		log.LogWarn(ctx, "AceImgExtract", err.Error(), "url="+url)
		_, _ = msgSender.To(peer).Text(ctx, "❌ URL AceImg tidak valid.")
		return nil
	}

	mediaSender := media.NewMediaSender(client)

	_, err = mediaSender.SendSmartMedia(
		ctx, peer, result.URL, "", "", nil, nil,
	)

	if err != nil {
		logger.Error("Gagal mengirim media AceImg", zap.Error(err))
		log.LogError(ctx, "AceImgSendMedia", err, "url="+result.URL)
		_, _ = msgSender.To(peer).Text(ctx, "❌ Gagal mengunduh atau mengirim file.")
		return err
	}

	logger.Info("AceImg berhasil dikirim", zap.String("id", result.ID), zap.String("type", result.Type))
	return nil
}

// handleAceImgGroup menangani AceImg di grup/supergroup (tanpa progress sendiri)
func handleAceImgGroup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, url string, replyTo *tg.InputReplyToMessage, logger *zap.Logger) error {
	result, err := aceimg.Extract(url)
	if err != nil {
		logger.Warn("Gagal ekstrak URL AceImg grup", zap.Error(err))
		log.LogWarn(ctx, "AceImgGroupExtract", err.Error(), "url="+url)
		_ = sendGroupText(ctx, client, peer, "❌ URL AceImg tidak valid.", replyTo)
		return nil
	}

	mediaSender := media.NewMediaSender(client)

	_, err = mediaSender.SendSmartMedia(
		ctx, peer, result.URL, "", "", nil, replyTo,
	)

	if err != nil {
		logger.Error("Gagal kirim media AceImg grup", zap.Error(err))
		log.LogError(ctx, "AceImgGroupSend", err, "url="+result.URL)
		_ = sendGroupText(ctx, client, peer, "❌ Gagal mengirim file.", replyTo)
		return err
	}

	logger.Info("AceImg grup berhasil dikirim", zap.String("id", result.ID))
	return nil
}
