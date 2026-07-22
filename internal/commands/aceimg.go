package commands

import (
	"context"

	"mybot/internal/api/aceimg"
	"mybot/internal/config"
	"mybot/internal/log"
	"mybot/internal/media"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func HandleAceImg(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, url string, logger *zap.Logger) error {
	// Cek feature toggle
	fm := config.GetFeatureManager()
	if !fm.IsEnabled("aceimg") {
		logger.Info("Fitur AceImg dinonaktifkan")
		return nil
	}

	// Gunakan middleware WithLoading
	return WithLoading(ctx, client, msg, entities, "AceImg", logger, func(ctx context.Context, lc *LoadingContext) error {
		return processAceImg(ctx, client, lc, url, logger)
	})
}

func processAceImg(ctx context.Context, client *tg.Client, lc *LoadingContext, url string, logger *zap.Logger) error {
	// Ekstrak info dari URL
	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "🔍 Mengekstrak URL AceImg...", logger)
	}

	result, err := aceimg.Extract(url)
	if err != nil {
		logger.Warn("Gagal ekstrak URL AceImg", zap.Error(err))
		log.LogWarn(ctx, "AceImgExtract", err.Error(), "url="+url)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ URL AceImg tidak valid.", logger)
		}
		return nil
	}

	if lc.ProgressMsgID != 0 {
		_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "📥 Mengunduh media...", logger)
	}

	mediaSender := media.NewMediaSender(client)

	_, err = mediaSender.SendSmartMedia(
		ctx, lc.Peer, result.URL, "", "", nil, lc.ReplyTo,
	)

	if err != nil {
		logger.Error("Gagal mengirim media AceImg", zap.Error(err))
		log.LogError(ctx, "AceImgSendMedia", err, "url="+result.URL)
		if lc.ProgressMsgID != 0 {
			_ = EditLoadingMessage(ctx, client, lc.Peer, lc.ProgressMsgID, "❌ Gagal mengunduh atau mengirim file.", logger)
		}
		return nil
	}

	logger.Info("AceImg berhasil dikirim", zap.String("id", result.ID), zap.String("type", result.Type))
	return nil
}
