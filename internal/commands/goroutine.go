package commands

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// HandleGoroutine menampilkan jumlah goroutine aktif dan statistik memori
// Catatan: Fungsi ini tidak melakukan pengecekan akses (private/root) karena sudah dilakukan di main.go
func HandleGoroutine(ctx context.Context, tgClient *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, tgClient, msg, entities)
	if err != nil {
		return err
	}

	// Ambil jumlah goroutine
	jumlahGoroutine := runtime.NumGoroutine()

	// Ambil statistik memori
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Format pesan dengan HTML (support Markdown)
	infoMsg := fmt.Sprintf(
		"🧵 <b>Informasi Goroutine</b>\n\n"+
			"• Goroutine aktif: <code>%d</code>\n"+
			"• Total alokasi memori: <code>%.2f MB</code>\n"+
			"• Memori sistem: <code>%.2f MB</code>\n"+
			"• Jumlah GC: <code>%d</code>",
		jumlahGoroutine,
		float64(memStats.TotalAlloc)/1024/1024,
		float64(memStats.Sys)/1024/1024,
		memStats.NumGC,
	)

	sender := message.NewSender(tgClient)
	_, err = sender.To(peer).Reply(msg.ID).StyledText(ctx, html.String(nil, infoMsg))
	if err != nil {
		// Fallback plain text
		_, err = sender.To(peer).Reply(msg.ID).Text(ctx, infoMsg)
	}
	return err
}
