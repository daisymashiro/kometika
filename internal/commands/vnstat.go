package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/media" // Sesuaikan dengan path media.go milikmu
)

// Struct vnstat
type VnstatOutput struct {
	JsonVersion string `json:"jsonversion"`
	Interfaces  []struct {
		Name    string `json:"name"`
		Traffic struct {
			Day []struct {
				Date struct {
					Year  int `json:"year"`
					Month int `json:"month"`
					Day   int `json:"day"`
				} `json:"date"`
				Rx int64 `json:"rx"`
				Tx int64 `json:"tx"`
			} `json:"day"`
			Month []struct {
				Date struct {
					Year  int `json:"year"`
					Month int `json:"month"`
				} `json:"date"`
				Rx int64 `json:"rx"`
				Tx int64 `json:"tx"`
			} `json:"month"`
			Year []struct {
				Date struct {
					Year int `json:"year"`
				} `json:"date"`
				Rx int64 `json:"rx"`
				Tx int64 `json:"tx"`
			} `json:"year"`
			Total struct {
				Rx int64 `json:"rx"`
				Tx int64 `json:"tx"`
			} `json:"total"`
		} `json:"traffic"`
	} `json:"interfaces"`
}

func getVnstatUsage() (*VnstatOutput, error) {
	cmd := exec.Command("vnstat", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gagal menjalankan vnstat: %w", err)
	}

	var data VnstatOutput
	err = json.Unmarshal(output, &data)
	if err != nil {
		return nil, fmt.Errorf("gagal parse JSON vnstat: %w", err)
	}

	return &data, nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// HandleVnstat menangani perintah untuk mengecek penggunaan internet
func HandleVnstat(ctx context.Context, client *tg.Client, msg *tg.Message, entities tg.Entities, logger *zap.Logger) error {
	peer, err := GetPeerFromMessage(ctx, client, msg, entities)
	if err != nil || peer == nil {
		logger.Error("Gagal mendapatkan peer di HandleVnstat", zap.Error(err))
		return err
	}

	sender := message.NewSender(client)

	// Kirim pesan loading sebentar
	loadingMsg, _ := sender.To(peer).Reply(msg.ID).Text(ctx, "⏳ Membaca data vnstat...")

	usage, err := getVnstatUsage()
	if err != nil {
		logger.Error("Error getVnstatUsage", zap.Error(err))
		if loadingUpdates, ok := loadingMsg.(*tg.UpdateShortSentMessage); ok {
			_ = media.EditHTML(ctx, client, peer, loadingUpdates.ID, "❌ <b>Gagal membaca vnstat.</b> Pastikan vnstat terinstal di sistem.")
		}
		return err
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>Statistik Penggunaan Jaringan (vnStat)</b>\n\n")

	for _, iface := range usage.Interfaces {
		sb.WriteString(fmt.Sprintf("🌐 <b>Interface:</b> <code>%s</code>\n", iface.Name))

		// 1. DATA HARIAN
		days := iface.Traffic.Day
		nDays := len(days)
		if nDays > 0 {
			today := days[nDays-1]
			sb.WriteString(fmt.Sprintf("📌 <b>Hari Ini (%d-%02d-%02d):</b>\n", today.Date.Year, today.Date.Month, today.Date.Day))
			sb.WriteString(fmt.Sprintf("   ⬇️ DL: <code>%-10s</code>\n   ⬆️ UL: <code>%-10s</code>\n   🔄 Tot: <code>%s</code>\n", formatBytes(today.Rx), formatBytes(today.Tx), formatBytes(today.Rx+today.Tx)))
		}
		if nDays > 1 {
			yesterday := days[nDays-2]
			sb.WriteString(fmt.Sprintf("📌 <b>Kemarin (%d-%02d-%02d):</b>\n", yesterday.Date.Year, yesterday.Date.Month, yesterday.Date.Day))
			sb.WriteString(fmt.Sprintf("   ⬇️ DL: <code>%-10s</code>\n   ⬆️ UL: <code>%-10s</code>\n   🔄 Tot: <code>%s</code>\n", formatBytes(yesterday.Rx), formatBytes(yesterday.Tx), formatBytes(yesterday.Rx+yesterday.Tx)))
		}

		// 2. DATA BULANAN
		months := iface.Traffic.Month
		nMonths := len(months)
		if nMonths > 0 {
			thisMonth := months[nMonths-1]
			sb.WriteString(fmt.Sprintf("\n📅 <b>Bulan Ini (%d-%02d):</b>\n", thisMonth.Date.Year, thisMonth.Date.Month))
			sb.WriteString(fmt.Sprintf("   ⬇️ DL: <code>%-10s</code>\n   ⬆️ UL: <code>%-10s</code>\n   🔄 Tot: <code>%s</code>\n", formatBytes(thisMonth.Rx), formatBytes(thisMonth.Tx), formatBytes(thisMonth.Rx+thisMonth.Tx)))
		}
		if nMonths > 1 {
			lastMonth := months[nMonths-2]
			sb.WriteString(fmt.Sprintf("📅 <b>Bulan Lalu (%d-%02d):</b>\n", lastMonth.Date.Year, lastMonth.Date.Month))
			sb.WriteString(fmt.Sprintf("   ⬇️ DL: <code>%-10s</code>\n   ⬆️ UL: <code>%-10s</code>\n   🔄 Tot: <code>%s</code>\n", formatBytes(lastMonth.Rx), formatBytes(lastMonth.Tx), formatBytes(lastMonth.Rx+lastMonth.Tx)))
		}

		// 3. DATA TAHUNAN & TOTAL
		years := iface.Traffic.Year
		nYears := len(years)
		if nYears > 0 {
			thisYear := years[nYears-1]
			sb.WriteString(fmt.Sprintf("\n🌍 <b>Total Tahun Ini (%d):</b>\n", thisYear.Date.Year))
			sb.WriteString(fmt.Sprintf("   🔄 Tot: <code>%s</code>\n", formatBytes(thisYear.Rx+thisYear.Tx)))
		}

		total := iface.Traffic.Total
		sb.WriteString("🚀 <b>Total Seluruh Waktu:</b>\n")
		sb.WriteString(fmt.Sprintf("   🔄 Tot: <code>%s</code>\n\n", formatBytes(total.Rx+total.Tx)))
		sb.WriteString("〰️〰️〰️〰️〰️〰️〰️〰️〰️\n")
	}

	finalMsg := sb.String()

	// Menghapus pesan loading
	loadingMsgID, err := media.ExtractMessageID(loadingMsg)
	if err == nil && loadingMsgID != 0 {
		_, _ = client.MessagesDeleteMessages(context.Background(), &tg.MessagesDeleteMessagesRequest{
			Revoke: true,
			ID:     []int{loadingMsgID},
		})
	}

	// Mengirimkan hasil akhir dengan format HTML
	_, err = sender.To(peer).Reply(msg.ID).StyledText(ctx, html.String(nil, finalMsg))
	if err != nil {
		logger.Error("Gagal mengirim hasil vnstat", zap.Error(err))
		return err
	}

	logger.Info("Data vnstat berhasil dikirim")
	return nil
}
