package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"mybot/internal/api/youtube"
)

// LiveSession untuk menyimpan data pre-fetch sebelum tombol resolusi ditekan
type LiveSession struct {
	VideoData *youtube.VideoData
	URL       string
	ExpiresAt time.Time
}

// QueuedStream menyimpan data lengkap yang siap untuk dikirim ke FFmpeg
type QueuedStream struct {
	VideoID  string
	VideoURL string
	AudioURL string
	Title    string
	Quality  string
	MsgID    int
	Peer     tg.InputPeerClass
}

var (
	// Store untuk Pre-fetch (Sesi menu tombol resolusi)
	liveStore sync.Map
	liveTTL   = 15 * time.Minute

	// Store untuk Antrean (Queue System)
	queueMu     sync.Mutex
	streamQueue []QueuedStream
	isPlaying   bool
	currentCtx  context.CancelFunc
	currentVid  string
)

// ================== PRE-FETCH SESSION ==================

func SetLiveSession(videoID string, data *youtube.VideoData, url string) {
	liveStore.Store(videoID, &LiveSession{
		VideoData: data,
		URL:       url,
		ExpiresAt: time.Now().Add(liveTTL),
	})
}

func GetLiveSession(videoID string) (*LiveSession, bool) {
	val, ok := liveStore.Load(videoID)
	if !ok {
		return nil, false
	}
	session := val.(*LiveSession)
	if time.Now().After(session.ExpiresAt) {
		liveStore.Delete(videoID)
		return nil, false
	}
	return session, true
}

func DeleteLiveSession(videoID string) {
	liveStore.Delete(videoID)
}

// ================== QUEUE MANAGER ==================

// EnqueueStream menambah stream ke antrean, mengembalikan (posisi, apakah_sedang_mutar, error)
func EnqueueStream(item QueuedStream) (int, bool, error) {
	queueMu.Lock()
	defer queueMu.Unlock()

	// Cegah duplikasi video yang sama dimainkan bersamaan
	if currentVid == item.VideoID {
		return 0, true, fmt.Errorf("video ini sedang diputar")
	}
	for _, q := range streamQueue {
		if q.VideoID == item.VideoID {
			return 0, true, fmt.Errorf("video ini sudah ada di dalam antrean")
		}
	}

	wasPlaying := isPlaying
	streamQueue = append(streamQueue, item)

	// Jika tidak ada yang main, tandai sebagai main
	if !isPlaying {
		isPlaying = true
	}

	return len(streamQueue), wasPlaying, nil
}

// DequeueStream mengambil stream selanjutnya. Jika kosong, mengembalikan false
func DequeueStream() (*QueuedStream, bool) {
	queueMu.Lock()
	defer queueMu.Unlock()

	if len(streamQueue) == 0 {
		isPlaying = false
		currentVid = ""
		currentCtx = nil
		return nil, false
	}
	item := streamQueue[0]
	streamQueue = streamQueue[1:] // Hapus dari antrean
	return &item, true
}

// SetCurrentStream menyetel state stream yang sedang berjalan di FFmpeg
func SetCurrentStream(vidID string, cancel context.CancelFunc) {
	queueMu.Lock()
	defer queueMu.Unlock()
	currentVid = vidID
	currentCtx = cancel
}

// IsCurrentStream mengecek apakah videoID sedang dimainkan saat ini
func IsCurrentStream(vidID string) bool {
	queueMu.Lock()
	defer queueMu.Unlock()
	return currentVid == vidID
}

// StopCurrentStream mematikan FFmpeg yang sedang berjalan
// FIX BUG #4: Panggil cancel() di luar lock untuk menghindari deadlock
func StopCurrentStream() {
	queueMu.Lock()
	cancel := currentCtx
	currentCtx = nil
	queueMu.Unlock()
	
	// Call cancel outside the lock untuk menghindari deadlock
	// jika cancel() memanggil callback yang butuh lock queueMu
	if cancel != nil {
		cancel()
	}
}

// RemoveFromQueue menghapus video dari antrean jika user membatalkannya sebelum diputar
func RemoveFromQueue(vidID string) bool {
	queueMu.Lock()
	defer queueMu.Unlock()
	for i, item := range streamQueue {
		if item.VideoID == vidID {
			streamQueue = append(streamQueue[:i], streamQueue[i+1:]...)
			return true
		}
	}
	return false
}
