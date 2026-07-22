package api

import (
	"sync"
	"time"
)

// CircuitBreaker mengelola status API dengan cooldown otomatis
type CircuitBreaker struct {
	mu            sync.RWMutex
	failures      map[string]int
	lastFailTime  map[string]time.Time
	threshold     int
	cooldownTime  time.Duration
}

// NewCircuitBreaker membuat instance baru circuit breaker
func NewCircuitBreaker(threshold int, cooldownTime time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failures:     make(map[string]int),
		lastFailTime: make(map[string]time.Time),
		threshold:    threshold,
		cooldownTime: cooldownTime,
	}
}

// CanAttempt memeriksa apakah API boleh dicoba
func (cb *CircuitBreaker) CanAttempt(apiName string) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	failures, exists := cb.failures[apiName]
	if !exists {
		return true
	}

	// Jika gagal >= threshold, cek cooldown
	if failures >= cb.threshold {
		lastFail, ok := cb.lastFailTime[apiName]
		if !ok {
			return true
		}
		// Jika masih dalam cooldown, skip
		if time.Since(lastFail) < cb.cooldownTime {
			return false
		}
		// Cooldown selesai, reset counter
		return true
	}

	return true
}

// RecordSuccess mencatat keberhasilan API (reset counter)
func (cb *CircuitBreaker) RecordSuccess(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.failures, apiName)
	delete(cb.lastFailTime, apiName)
}

// RecordFailure mencatat kegagalan API
func (cb *CircuitBreaker) RecordFailure(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures[apiName]++
	cb.lastFailTime[apiName] = time.Now()
}

// Reset mereset semua counter (untuk debugging)
func (cb *CircuitBreaker) Reset(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.failures, apiName)
	delete(cb.lastFailTime, apiName)
}

// GetStatus mengembalikan status API
func (cb *CircuitBreaker) GetStatus(apiName string) (failures int, inCooldown bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	failures = cb.failures[apiName]
	if failures >= cb.threshold {
		if lastFail, ok := cb.lastFailTime[apiName]; ok {
			inCooldown = time.Since(lastFail) < cb.cooldownTime
		}
	}
	return
}
