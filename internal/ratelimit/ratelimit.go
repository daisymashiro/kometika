package ratelimit

import (
	"sync"
	"time"
)

// ======================== RATE LIMITER ========================
// Token Bucket Algorithm untuk membatasi request per user
// Mencegah spam dan abuse

type UserLimiter struct {
	mu         sync.Mutex
	buckets    map[int64]*TokenBucket
	capacity   int           // Maksimal token
	refillRate time.Duration // Waktu untuk refill 1 token
}

type TokenBucket struct {
	tokens     int
	lastRefill time.Time
}

// NewUserLimiter membuat instance baru rate limiter
// capacity: berapa banyak request yang diperbolehkan
// refillRate: berapa lama waktu untuk menambah 1 token
// Contoh: NewUserLimiter(10, 1*time.Minute) = 10 request per menit
func NewUserLimiter(capacity int, refillRate time.Duration) *UserLimiter {
	return &UserLimiter{
		buckets:    make(map[int64]*TokenBucket),
		capacity:   capacity,
		refillRate: refillRate,
	}
}

// Allow memeriksa apakah user diperbolehkan melakukan request
// Return true jika masih ada token, false jika sudah habis (rate limited)
func (ul *UserLimiter) Allow(userID int64) bool {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	bucket, ok := ul.buckets[userID]
	if !ok {
		// User baru, buat bucket dengan token penuh
		bucket = &TokenBucket{
			tokens:     ul.capacity,
			lastRefill: time.Now(),
		}
		ul.buckets[userID] = bucket
	}

	// Refill tokens berdasarkan waktu yang berlalu
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed / ul.refillRate)
	
	if tokensToAdd > 0 {
		bucket.tokens = min(bucket.tokens+tokensToAdd, ul.capacity)
		bucket.lastRefill = bucket.lastRefill.Add(time.Duration(tokensToAdd) * ul.refillRate)
	}

	// Cek apakah user masih punya token
	if bucket.tokens > 0 {
		bucket.tokens--
		return true
	}
	
	return false // Rate limited
}

// GetRemainingTokens mengembalikan sisa token user
func (ul *UserLimiter) GetRemainingTokens(userID int64) int {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	bucket, ok := ul.buckets[userID]
	if !ok {
		return ul.capacity
	}

	// Refill tokens berdasarkan waktu yang berlalu
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed / ul.refillRate)
	
	if tokensToAdd > 0 {
		bucket.tokens = min(bucket.tokens+tokensToAdd, ul.capacity)
		bucket.lastRefill = bucket.lastRefill.Add(time.Duration(tokensToAdd) * ul.refillRate)
	}

	return bucket.tokens
}

// Reset mereset rate limiter untuk user tertentu
func (ul *UserLimiter) Reset(userID int64) {
	ul.mu.Lock()
	defer ul.mu.Unlock()
	delete(ul.buckets, userID)
}

// CleanupOldBuckets membersihkan bucket yang sudah tidak aktif > 1 jam
func (ul *UserLimiter) CleanupOldBuckets() int {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	toDelete := make([]int64, 0)
	cutoff := time.Now().Add(-1 * time.Hour)

	for userID, bucket := range ul.buckets {
		if bucket.lastRefill.Before(cutoff) {
			toDelete = append(toDelete, userID)
		}
	}

	for _, userID := range toDelete {
		delete(ul.buckets, userID)
	}

	return len(toDelete)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
