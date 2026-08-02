package commands

import (
	"context"
	"os"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message/markup"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"

	"mybot/internal/media"
)

// HandleHelpCommand menangani perintah help dan mengirimkan panduan bot
func HandleHelpCommand(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, user *tg.User, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal dapatkan peer untuk command help", zap.Error(err))
		return err
	}

	// Mengambil OWNER_ID dari .env untuk mengarahkan tombol ke akun Admin
	ownerID := os.Getenv("OWNER_ID")
	adminURL := "tg://user?id=" + ownerID

	// Susunan Teks HTML untuk Bantuan
	htmlMsg := `<b>🛠 Panduan Penggunaan Kometika Bot</b>

<b>🚀 Fitur & Platform yang Didukung:</b>
• <b>TikTok</b> (Video, Album Foto, Audio)
• <b>Instagram</b> (Reels, Post, IGTV, Album)
• <b>Facebook</b> (Video/Reels)
• <b>Twitter / X</b> (Video, Foto, Album)
• <b>Terabox</b> (Download Video/File)
• <b>YouTube</b> (Unduh Musik dengan perintah <code>play</code>)
• <b>LuluStream, MediaFire, AceImg</b>

<b>⌨️ Pemanggilan Perintah:</b>
Bot ini mendukung multiple prefix. Anda bisa menggunakan salah satu dari simbol awalan berikut:
<code>.</code>  <code>/</code>  <code>!</code>  <code>#</code>
<i>Contoh:</i> <code>.dl [link]</code> atau <code>/status</code> atau <code>!ping</code>

💡 <i><b>Catatan:</b> Di Private Chat, Anda cukup mengirim link secara langsung tanpa perintah.</i>
📌 <b>jangan spam !!!</b>
`

	// Membuat tombol inline menggunakan package markup
	replyMarkup := markup.InlineKeyboard(
		markup.Row(
			markup.URL("💬 Hubungi Admin", adminURL, markup.StyleBgPrimary()),
		),
	)

	sender := message.NewSender(client)

	// Mengirimkan pesan dengan HTML dan Markup (Tombol)
	sentMsg, err := sender.To(peer).Reply(msg.ID).Markup(replyMarkup).StyledText(ctx, html.String(nil, htmlMsg))
	if err != nil {
		logger.Error("Gagal mengirim pesan help", zap.Error(err))
		return err
	}

	// Mengekstrak Message ID dari pesan yang baru saja terkirim
	msgID, errExt := media.ExtractMessageID(sentMsg)
	if errExt == nil && msgID != 0 {
		// Menjalankan goroutine di background untuk menghapus tombol setelah 5 menit
		go scheduleHelpButtonCleanup(client, peer, msgID, logger)
	}

	return nil
}

// scheduleHelpButtonCleanup bertugas menghapus tombol (ReplyMarkup) setelah 5 menit
func scheduleHelpButtonCleanup(client *tg.Client, peer tg.InputPeerClass, msgID int, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute) // Timeout pelindung
	defer cancel()

	select {
	case <-time.After(5 * time.Minute): // Tunggu selama 5 menit
		editCtx, editCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer editCancel()

		for {
			// Mengirim request EditMessage dengan ReplyKeyboardHide (menghapus tombol)
			_, err := client.MessagesEditMessage(editCtx, &tg.MessagesEditMessageRequest{
				Peer:        peer,
				ID:          msgID,
				ReplyMarkup: &tg.ReplyKeyboardHide{},
			})

			if err != nil {
				// Handle proteksi Spam/FloodWait dari Telegram
				if d, ok := tgerr.AsFloodWait(err); ok {
					logger.Warn("FloodWait saat hapus tombol help", zap.Duration("wait", d))
					select {
					case <-time.After(d + time.Second):
						continue // Coba lagi
					case <-editCtx.Done():
						logger.Warn("Gagal menghapus tombol help (timeout)")
						return
					}
				}
				logger.Warn("Gagal menghapus tombol help", zap.Error(err))
				return
			}
			logger.Info("Tombol menu help dihapus otomatis", zap.Int("msg_id", msgID))
			break // Berhasil dihapus
		}
	case <-ctx.Done(): // Jika bot mati tiba-tiba sebelum 5 menit
		return
	}
}
