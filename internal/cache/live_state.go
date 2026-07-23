package cache

import (
	"context"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"mybot/internal/api/youtube"
)

// LiveSession menyimpan data state untuk fitur YouTube Livestream
type LiveSession struct {
	VideoData *youtube.VideoData
	URL       string
	Cancel    context.CancelFunc
	GroupCall tg.InputGroupCallClass // Menyimpan instance Group Call untuk cleanup (PhoneDiscardGroupCall)
	ExpiresAt time.Time
}

var (
	liveStore sync.Map
	liveTTL   = 15 * time.Minute // Waktu tunggu user menekan resolusi sebelum cache hangus
)

// SetLiveSession menyimpan data pre-fetch YouTube saat command .play dipanggil
func SetLiveSession(videoID string, data *youtube.VideoData, url string) {
	liveStore.Store(videoID, &LiveSession{
		VideoData: data,
		URL:       url,
		ExpiresAt: time.Now().Add(liveTTL),
	})
}

// GetLiveSession mengambil data pre-fetch berdasarkan Video ID
func GetLiveSession(videoID string) (*LiveSession, bool) {
	val, ok := liveStore.Load(videoID)
	if !ok {
		return nil, false
	}

	session := val.(*LiveSession)

	// Hapus cache otomatis jika sudah melewati batas waktu tunggu
	if time.Now().After(session.ExpiresAt) {
		liveStore.Delete(videoID)
		return nil, false
	}

	return session, true
}

// SetLiveActive menandai bahwa stream sedang berjalan, menyimpan fungsi Cancel FFmpeg,
// dan menyimpan objek InputGroupCall untuk keperluan mematikan Voice Chat Telegram
func SetLiveActive(videoID string, cancel context.CancelFunc, groupCall tg.InputGroupCallClass) {
	if session, ok := GetLiveSession(videoID); ok {
		session.Cancel = cancel
		session.GroupCall = groupCall
		session.ExpiresAt = time.Now().Add(24 * time.Hour) // Perpanjang masa aktif cache selama stream berjalan
		liveStore.Store(videoID, session)
	}
}

// StopLiveStream mengeksekusi trigger penghentian (FFmpeg) dan membersihkan cache
func StopLiveStream(videoID string) {
	if session, ok := GetLiveSession(videoID); ok {
		// Panggil CancelFunc jika tersedia untuk mematikan goroutine exec FFmpeg
		if session.Cancel != nil {
			session.Cancel()
		}
		// Hapus sepenuhnya dari memory
		liveStore.Delete(videoID)
	}
}
