package cache

import (
	"sync"
	"time"
)

// OwnerStatus menyimpan status online/offline owner untuk business bot
type OwnerStatus struct {
	mu           sync.RWMutex
	isOnline     bool
	lastActivity time.Time
	mode         string // "auto" atau "manual"
}

var ownerStatus = &OwnerStatus{
	isOnline: false,
	mode:     "auto", // Default: bot otomatis deteksi
}

// SetOwnerOnline mengatur status online owner
func SetOwnerOnline(online bool) {
	ownerStatus.mu.Lock()
	defer ownerStatus.mu.Unlock()
	ownerStatus.isOnline = online
	if online {
		ownerStatus.lastActivity = time.Now()
	}
}

// IsOwnerOnline mengecek apakah owner sedang online
func IsOwnerOnline() bool {
	ownerStatus.mu.RLock()
	defer ownerStatus.mu.RUnlock()
	return ownerStatus.isOnline
}

// GetOwnerLastActivity mengembalikan waktu terakhir owner aktif
func GetOwnerLastActivity() time.Time {
	ownerStatus.mu.RLock()
	defer ownerStatus.mu.RUnlock()
	return ownerStatus.lastActivity
}

// UpdateOwnerActivity memperbarui timestamp aktivitas owner
func UpdateOwnerActivity() {
	ownerStatus.mu.Lock()
	defer ownerStatus.mu.Unlock()
	ownerStatus.lastActivity = time.Now()
}

// SetBotMode mengatur mode bot: "auto" (deteksi otomatis) atau "manual" (bot tidak balas)
func SetBotMode(mode string) {
	ownerStatus.mu.Lock()
	defer ownerStatus.mu.Unlock()
	ownerStatus.mode = mode
}

// GetBotMode mengembalikan mode bot saat ini
func GetBotMode() string {
	ownerStatus.mu.RLock()
	defer ownerStatus.mu.RUnlock()
	return ownerStatus.mode
}

// ShouldBotReply menentukan apakah bot harus membalas berdasarkan status owner
// Logika:
// 1. Jika mode "manual" → bot tidak balas
// 2. Jika owner online → bot tidak balas
// 3. Jika owner aktif dalam 30 detik terakhir → bot tidak balas
// 4. Jika owner offline > 30 detik → bot balas
func ShouldBotReply() bool {
	ownerStatus.mu.RLock()
	defer ownerStatus.mu.RUnlock()

	// Mode manual: bot tidak balas sama sekali
	if ownerStatus.mode == "manual" {
		return false
	}

	// Owner sedang online: bot tidak balas
	if ownerStatus.isOnline {
		return false
	}

	// Owner baru saja aktif (dalam 30 detik): bot tidak balas
	// Ini untuk mencegah collision saat owner baru logout
	timeSinceActive := time.Since(ownerStatus.lastActivity)
	if timeSinceActive < 60*time.Second {
		return false
	}

	// Owner sudah lama offline: bot boleh balas
	return true
}

// GetOwnerStatusInfo mengembalikan informasi lengkap status owner untuk debugging
func GetOwnerStatusInfo() map[string]interface{} {
	ownerStatus.mu.RLock()
	online := ownerStatus.isOnline
	lastActivity := ownerStatus.lastActivity
	mode := ownerStatus.mode
	ownerStatus.mu.RUnlock()

	return map[string]interface{}{
		"online":        online,
		"last_activity": lastActivity,
		"mode":          mode,
		"should_reply":  ShouldBotReply(),
	}
}
