package config

import (
	"sync"
)

// FeatureManager mengelola status aktif/nonaktif fitur downloader
type FeatureManager struct {
	mu       sync.RWMutex
	features map[string]bool
}

var (
	globalFeatureManager *FeatureManager
	once                 sync.Once
)

// GetFeatureManager mengembalikan instance global FeatureManager (singleton)
func GetFeatureManager() *FeatureManager {
	once.Do(func() {
		globalFeatureManager = &FeatureManager{
			features: map[string]bool{
				"tiktok":    true,
				"instagram": true,
				"facebook":  true,
				"terabox":   true,
				"twitter":   true,
				"mediafire": true,
				"aceimg":     true,
				"lulustream": true,
			},
		}
	})
	return globalFeatureManager
}

// IsEnabled memeriksa apakah fitur sedang aktif
func (fm *FeatureManager) IsEnabled(feature string) bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	enabled, exists := fm.features[feature]
	if !exists {
		return false
	}
	return enabled
}

// Enable mengaktifkan fitur
func (fm *FeatureManager) Enable(feature string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.features[feature] = true
}

// Disable menonaktifkan fitur
func (fm *FeatureManager) Disable(feature string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.features[feature] = false
}

// GetAll mengembalikan semua status fitur
func (fm *FeatureManager) GetAll() map[string]bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	result := make(map[string]bool, len(fm.features))
	for k, v := range fm.features {
		result[k] = v
	}
	return result
}
