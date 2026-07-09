package log

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message/styling" // Add this import
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

var (
	logPeer   tg.InputPeerClass
	logClient *tg.Client
	logger    *zap.Logger
)

// InitLogger menginisialisasi sistem logging
func InitLogger(peer tg.InputPeerClass, client *tg.Client, log *zap.Logger) {
	logPeer = peer
	logClient = client
	logger = log
}

func isLogReady() bool {
	return logClient != nil && logPeer != nil
}

// sendHTMLMessage mengirim pesan dengan format HTML ke grup log
func sendHTMLMessage(ctx context.Context, opts ...message.StyledTextOption) {
	if !isLogReady() {
		if logger != nil {
			logger.Info("Log dikirim ke console (karena logClient/logPeer belum siap)")
		}
		return
	}
	sender := message.NewSender(logClient)
	// PERUBAHAN: Text diganti dengan StyledText
	_, err := sender.To(logPeer).StyledText(ctx, opts...)
	if err != nil {
		if logger != nil {
			logger.Error("Gagal kirim log HTML", zap.Error(err))
		}
	}
}

// LogError mengirim error ke grup log dengan format HTML + Blockquote asli Telegram
func LogError(ctx context.Context, handler string, err error, extra ...string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Menggunakan slice untuk menggabungkan HTML dan Blockquote
	var opts []message.StyledTextOption

	opts = append(opts, html.String(nil, fmt.Sprintf("<b>🚨 ERROR</b>  <i>%s</i>\n\n", timestamp)))
	opts = append(opts, html.String(nil, fmt.Sprintf("<b>Handler:</b> <code>%s</code>\n", handler)))
	opts = append(opts, html.String(nil, "<b>Error:</b>\n"))

	// PERUBAHAN: Menggunakan styling.Blockquote bawaan gotd/td
	opts = append(opts, styling.Blockquote(fmt.Sprintf("%v", err), false))

	if len(extra) > 0 {
		opts = append(opts, html.String(nil, "\n<b>Extra:</b>\n"))
		for _, e := range extra {
			opts = append(opts, html.String(nil, fmt.Sprintf("• %s\n", e)))
		}
	}

	sendHTMLMessage(ctx, opts...)
}

// LogWarn mengirim warning ke grup log dengan format HTML + Blockquote asli Telegram
func LogWarn(ctx context.Context, handler, warnMsg string, extra ...string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	var opts []message.StyledTextOption

	opts = append(opts, html.String(nil, fmt.Sprintf("<b>⚠️ WARNING</b>  <i>%s</i>\n\n", timestamp)))
	opts = append(opts, html.String(nil, fmt.Sprintf("<b>Handler:</b> <code>%s</code>\n", handler)))
	opts = append(opts, html.String(nil, "<b>Message:</b>\n"))

	// PERUBAHAN: Menggunakan styling.Blockquote bawaan gotd/td
	opts = append(opts, styling.Blockquote(warnMsg, false))

	if len(extra) > 0 {
		opts = append(opts, html.String(nil, "\n<b>Extra:</b>\n"))
		for _, e := range extra {
			opts = append(opts, html.String(nil, fmt.Sprintf("• %s\n", e)))
		}
	}

	sendHTMLMessage(ctx, opts...)
}

// LogInfo mengirim info penting ke grup log
func LogInfo(ctx context.Context, infoMsg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	var opts []message.StyledTextOption
	opts = append(opts, html.String(nil, fmt.Sprintf("<b>ℹ️ INFO</b>  <i>%s</i>\n\n", timestamp)))

	// PERUBAHAN: Menggunakan styling.Blockquote bawaan gotd/td
	opts = append(opts, styling.Blockquote(infoMsg, false))

	sendHTMLMessage(ctx, opts...)
}

// LogReplyToMessage mengirim log sebagai reply ke pesan tertentu (jika ada message ID)
func LogReplyToMessage(ctx context.Context, handler string, err error, replyToMsgID int, extra ...string) {
	if !isLogReady() {
		LogError(ctx, handler, err, extra...)
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	var opts []message.StyledTextOption

	opts = append(opts, html.String(nil, fmt.Sprintf("<b>🚨 ERROR (reply)</b>  <i>%s</i>\n\n", timestamp)))
	opts = append(opts, html.String(nil, fmt.Sprintf("<b>Handler:</b> <code>%s</code>\n", handler)))
	opts = append(opts, html.String(nil, "<b>Error:</b>\n"))

	// PERUBAHAN: Menggunakan styling.Blockquote bawaan gotd/td
	opts = append(opts, styling.Blockquote(fmt.Sprintf("%v", err), false))

	if len(extra) > 0 {
		opts = append(opts, html.String(nil, "\n<b>Extra:</b>\n"))
		for _, e := range extra {
			opts = append(opts, html.String(nil, fmt.Sprintf("• %s\n", e)))
		}
	}

	// Kirim dengan reply ke pesan tertentu
	sender := message.NewSender(logClient)
	// PERUBAHAN: Text diganti dengan StyledText
	_, err2 := sender.To(logPeer).Reply(replyToMsgID).StyledText(ctx, opts...)
	if err2 != nil {
		if logger != nil {
			logger.Error("Gagal kirim log reply", zap.Error(err2))
		}
	}
}
