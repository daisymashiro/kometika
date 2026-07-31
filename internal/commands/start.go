package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// HandleStart menangani perintah start dengan balasan HTML yang rapi
func HandleStart(ctx context.Context, tgClient *tg.Client, msg *tg.Message, entities tg.Entities, user *tg.User, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, tgClient, msg, entities)
	if err != nil {
		logger.Error("Gagal mendapatkan peer saat HandleStart", zap.Error(err))
		return err
	}

	// ==========================================
	// 1. Ekstrak Nama Pengguna (Cleaner)
	// ==========================================
	var fullName string
	if user != nil {
		// TrimSpace otomatis menghapus spasi berlebih jika firstName atau lastName kosong
		fullName = strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName))
	}
	if fullName == "" {
		fullName = "Pengguna"
	}

	// ==========================================
	// 2. Format Pesan HTML
	// ==========================================
	htmlMsg := fmt.Sprintf(
		`Selamat datang, <b>%s</b>! 👋

Saya adalah <b>Kometika Bot</b>. Saya siap membantu Anda mengunduh media dari berbagai platform.

🌐 <b>Platform yang Didukung:</b>
• TikTok
• Instagram
• Facebook
• Terabox
• YouTube (audio)

📌 <b>Cara Penggunaan:</b>
• <i>Chat Privat:</i> Cukup kirim URL-nya secara langsung.
• <i>Grup / Forum:</i> Gunakan perintah diikuti URL.
  Format: <code>.dl [URL]</code>  (untuk video)
  Format: <code>.play [URL YouTube]</code>

⚡ <b>Prefix yang Tersedia:</b>
<code>.</code> <code>!</code> <code>#</code> <code>/</code>

💡 <b>Contoh:</b>
<code>.dl https://www.tiktok.com/video/123456</code>
<code>.play https://youtu.be/abcdef</code>`,
		fullName,
	)

	sender := message.NewSender(tgClient)
	_, err = sender.To(peer).Reply(msg.ID).StyledText(ctx, html.String(nil, htmlMsg))

	// ==========================================
	// 3. Fallback ke Plain Text (Jika HTML gagal)
	// ==========================================
	if err != nil {
		logger.Warn("Gagal mengirim pesan HTML, fallback ke plain text", zap.Error(err))

		plainMsg := fmt.Sprintf(
			"Selamat datang, %s! 👋\n\nSaya adalah Kometika Bot. Saya siap membantu Anda mengunduh media dari TikTok, Instagram, Facebook, dan Terabox.\n\n• Chat Privat: Cukup kirim URL-nya langsung.\n• Grup: Gunakan format .dl [URL]\n\nContoh: .dl https://www.tiktok.com/video/123456",
			fullName,
		)
		_, err = sender.To(peer).Reply(msg.ID).Text(ctx, plainMsg)
	}

	return err
}
