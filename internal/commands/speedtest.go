package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"github.com/showwin/speedtest-go/speedtest"
	"go.uber.org/zap"

	"mybot/internal/media" // Sesuaikan dengan path media.go milikmu
)

// ======================== KONSTANTA & STRUCT ========================

const speedTestTimeout = 60 * time.Second

type SpeedTestResult struct {
	ServerName    string
	ServerCountry string
	Latency       time.Duration
	DownloadMbps  float64
	UploadMbps    float64
}

// ======================== LOGIKA SPEEDTEST ========================

func runSpeedTestInternal() (*SpeedTestResult, error) {
	client := speedtest.New()

	// 1. Ambil daftar server
	serverList, err := client.FetchServers()
	if err != nil {
		return nil, fmt.Errorf("fetch server list: %w", err)
	}

	// 2. Cari server terdekat (auto)
	targets, err := serverList.FindServer([]int{})
	if err != nil || len(targets) == 0 {
		return nil, fmt.Errorf("tidak ada server yang ditemukan: %w", err)
	}
	s := targets[0]

	// 3. Ping test
	if err := s.PingTest(nil); err != nil {
		return nil, fmt.Errorf("ping test gagal: %w", err)
	}

	// 4. Download test
	if err := s.DownloadTest(); err != nil {
		return nil, fmt.Errorf("download test gagal: %w", err)
	}

	// 5. Upload test
	if err := s.UploadTest(); err != nil {
		return nil, fmt.Errorf("upload test gagal: %w", err)
	}

	// Konversi bytes/sec ke Mbps
	dlMbps := float64(s.DLSpeed) * 8 / 1_000_000
	ulMbps := float64(s.ULSpeed) * 8 / 1_000_000

	return &SpeedTestResult{
		ServerName:    fmt.Sprintf("%s (%s)", s.Name, s.Host),
		ServerCountry: s.Country,
		Latency:       s.Latency,
		DownloadMbps:  dlMbps,
		UploadMbps:    ulMbps,
	}, nil
}

// runSpeedTest menjalankan pengujian dengan batas waktu
func runSpeedTest() (*SpeedTestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), speedTestTimeout)
	defer cancel()

	resultCh := make(chan *SpeedTestResult, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := runSpeedTestInternal()
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	select {
	case res := <-resultCh:
		return res, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("speedtest timeout setelah %v", speedTestTimeout)
	}
}

// formatSpeedTestTelegram merakit teks laporan untuk Telegram
func formatSpeedTestTelegram(result *SpeedTestResult) string {
	var sb strings.Builder

	sb.WriteString("🌐 <b>SPEEDTEST RESULT</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")

	sb.WriteString("📡 <b>Server</b>\n")
	sb.WriteString(fmt.Sprintf("└ <code>%s</code>\n", result.ServerName))
	if result.ServerCountry != "" {
		sb.WriteString(fmt.Sprintf("   🌍 <code>%s</code>\n", result.ServerCountry))
	}
	sb.WriteString("\n")

	sb.WriteString("⏱️ <b>Latency</b>\n")
	sb.WriteString(fmt.Sprintf("└ <code>%s</code>\n\n", result.Latency))

	sb.WriteString("📥 <b>Download</b>\n")
	sb.WriteString(fmt.Sprintf("└ <code>%.2f Mbps</code>\n\n", result.DownloadMbps))

	sb.WriteString("📤 <b>Upload</b>\n")
	sb.WriteString(fmt.Sprintf("└ <code>%.2f Mbps</code>\n", result.UploadMbps))

	return sb.String()
}

// ======================== HANDLER TELEGRAM ========================

// HandleSpeedtest adalah fungsi utama yang dipanggil oleh router/main.go
func HandleSpeedtest(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal mendapatkan peer di HandleSpeedtest", zap.Error(err))
		return err
	}

	sender := message.NewSender(client)

	// 1. Kirim pesan Loading
	loadingMsg, err := sender.To(peer).Reply(msg.ID).StyledText(ctx, html.String(nil, "⏳ <b>Menjalankan Speedtest...</b>\n<i>Mohon tunggu, proses ini memakan waktu sekitar 30-60 detik.</i>"))
	if err != nil {
		logger.Warn("Gagal mengirim pesan loading speedtest", zap.Error(err))
		return err
	}

	// Ekstrak ID pesan loading untuk di-edit nanti
	var loadingMsgID int
	if updates, ok := loadingMsg.(*tg.UpdateShortSentMessage); ok {
		loadingMsgID = updates.ID
	} else {
		loadingMsgID, _ = media.ExtractMessageID(loadingMsg)
	}

	// 2. Jalankan logika speedtest
	result, err := runSpeedTest()
	if err != nil {
		logger.Error("Speedtest error", zap.Error(err))
		if loadingMsgID != 0 {
			_ = media.EditHTML(ctx, client, peer, loadingMsgID, "❌ <b>Gagal melakukan Speedtest.</b>\nCoba beberapa saat lagi.")
		}
		return err
	}

	// 3. Format hasil dan perbarui pesan
	finalHTML := formatSpeedTestTelegram(result)

	if loadingMsgID != 0 {
		err = media.EditHTML(ctx, client, peer, loadingMsgID, finalHTML)
		if err != nil {
			logger.Error("Gagal edit pesan hasil speedtest", zap.Error(err))
			// Fallback: Kirim pesan baru jika edit gagal
			_, _ = sender.To(peer).Reply(msg.ID).StyledText(ctx, html.String(nil, finalHTML))
		}
	}

	logger.Info("Speedtest berhasil diselesaikan", zap.Float64("download_mbps", result.DownloadMbps))
	return nil
}
