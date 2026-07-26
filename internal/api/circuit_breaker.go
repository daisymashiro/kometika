package api

import (
	"sync"
	"time"
)

// State represents the circuit breaker state for a single API
type State int

const (
	StateClosed   State = iota // normal, requests allowed
	StateOpen                  // tripped, requests blocked
	StateHalfOpen              // testing, limited requests allowed
)

// CircuitBreaker manages API health with automatic cooldown
type CircuitBreaker struct {
	mu           sync.RWMutex
	failures     map[string]int
	lastFailTime map[string]time.Time
	states       map[string]State
	threshold    int
	cooldownTime time.Duration
}

// NewCircuitBreaker creates a new circuit breaker instance.
//
// Usage:
//
//	cb := NewCircuitBreaker(3, 5*time.Minute)  // ✅ correct
//	cb := NewCircuitBreaker(3, 5*60)           // ❌ 300ns, not 5 minutes
func NewCircuitBreaker(threshold int, cooldownTime time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failures:     make(map[string]int),
		lastFailTime: make(map[string]time.Time),
		states:       make(map[string]State),
		threshold:    threshold,
		cooldownTime: cooldownTime,
	}
}

// CanAttempt checks if the API can be tried.
// Returns true if: state is Closed, or state is HalfOpen (trial request).
// Returns false if: state is Open and still in cooldown.
func (cb *CircuitBreaker) CanAttempt(apiName string) bool {
	cb.mu.Lock() // Need write lock for state transition
	defer cb.mu.Unlock()

	state, exists := cb.states[apiName]
	if !exists {
		cb.states[apiName] = StateClosed
		return true
	}

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		lastFail, ok := cb.lastFailTime[apiName]
		if !ok {
			// Corrupted state, reset to Closed
			cb.resetAPI(apiName)
			return true
		}

		if time.Since(lastFail) >= cb.cooldownTime {
			// Cooldown finished → transition to HalfOpen
			cb.states[apiName] = StateHalfOpen
			return true
		}

		// Still in cooldown
		return false

	case StateHalfOpen:
		// Allow one trial request at a time.
		// If you want stricter control, use a separate flag to track
		// in-flight trial requests.
		return true

	default:
		return true
	}
}

// RecordSuccess resets the failure counter and closes the circuit.
func (cb *CircuitBreaker) RecordSuccess(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.resetAPI(apiName)
}

// RecordFailure increments the failure counter and potentially trips the circuit.
func (cb *CircuitBreaker) RecordFailure(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures[apiName]++
	cb.lastFailTime[apiName] = time.Now()

	state := cb.states[apiName]

	switch state {
	case StateClosed:
		if cb.failures[apiName] >= cb.threshold {
			cb.states[apiName] = StateOpen
		}
	case StateHalfOpen:
		// Trial request failed → back to Open
		cb.states[apiName] = StateOpen
		// Keep failure count, don't reset so we know it's bad
	}
}

// Reset clears all state for a specific API (for debugging/admin).
func (cb *CircuitBreaker) Reset(apiName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.resetAPI(apiName)
}

// resetAPI is the internal reset helper (caller must hold write lock).
func (cb *CircuitBreaker) resetAPI(apiName string) {
	delete(cb.failures, apiName)
	delete(cb.lastFailTime, apiName)
	cb.states[apiName] = StateClosed
}

// GetStatus returns the current status of an API.
func (cb *CircuitBreaker) GetStatus(apiName string) (failures int, state State, cooldownEndsAt time.Time) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	failures = cb.failures[apiName]
	state = cb.states[apiName]

	if lastFail, ok := cb.lastFailTime[apiName]; ok {
		cooldownEndsAt = lastFail.Add(cb.cooldownTime)
	}

	return
}

// GetAllStatus returns a snapshot of all tracked APIs.
func (cb *CircuitBreaker) GetAllStatus() map[string]struct {
	Failures     int
	State        State
	CooldownEnds time.Time
} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	result := make(map[string]struct {
		Failures     int
		State        State
		CooldownEnds time.Time
	}, len(cb.states))

	for name := range cb.states {
		entry := struct {
			Failures     int
			State        State
			CooldownEnds time.Time
		}{
			Failures: cb.failures[name],
			State:    cb.states[name],
		}
		if lastFail, ok := cb.lastFailTime[name]; ok {
			entry.CooldownEnds = lastFail.Add(cb.cooldownTime)
		}
		result[name] = entry
	}

	return result
}
