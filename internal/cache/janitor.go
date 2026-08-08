package cache

import (
	"sync"
	"time"
)

// Janitor pembersih berkala untuk semua store ber-TTL.
// Tanpa ini, entri yang tak pernah di-Get lagi (audio, live session)
// dan channel lama akan menumpuk selamanya.
var (
	janitorOnce sync.Once
	janitorStop chan struct{}
)

// StartJanitor menjalankan pembersih TTL tiap 10 menit.
// Panggil sekali di main setelah inisialisasi.
func StartJanitor() {
	janitorOnce.Do(func() {
		stop := make(chan struct{})
		janitorStop = stop
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					pruneAudio()
					pruneLive()
					ClearOldChannels()
				case <-stop:
					return
				}
			}
		}()
	})
}

// StopJanitor menghentikan janitor (dipanggil saat shutdown).
func StopJanitor() {
	if janitorStop != nil {
		close(janitorStop)
	}
}

func pruneAudio() {
	now := time.Now()
	audioStore.Range(func(key, value any) bool {
		if info, ok := value.(AudioInfo); ok && now.After(info.ExpiresAt) {
			audioStore.Delete(key)
		}
		return true
	})
}

func pruneLive() {
	now := time.Now()
	liveStore.Range(func(key, value any) bool {
		if session, ok := value.(*LiveSession); ok && now.After(session.ExpiresAt) {
			liveStore.Delete(key)
		}
		return true
	})
}