package config

import (
	"mybot/internal/db"
	"sync"
)

type FeatureManager struct {
	mu       sync.RWMutex
	features map[string]bool
}

var (
	instance *FeatureManager
	once     sync.Once
)

func GetFeatureManager() *FeatureManager {
	once.Do(func() {
		instance = &FeatureManager{
			features: make(map[string]bool),
		}
	})
	return instance
}

func (fm *FeatureManager) LoadFromDB() {
	dbFeatures, err := db.GetAllFeatures()
	if err == nil {
		fm.mu.Lock()
		defer fm.mu.Unlock()
		for k, v := range dbFeatures {
			fm.features[k] = v
		}
	}
}

func (fm *FeatureManager) Enable(feature string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.features[feature] = true
	go db.SetFeatureStatus(feature, true)
}

func (fm *FeatureManager) Disable(feature string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.features[feature] = false
	go db.SetFeatureStatus(feature, false)
}

// IsEnabled mengecek apakah suatu fitur sedang aktif
func (fm *FeatureManager) IsEnabled(feature string) bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	if status, exists := fm.features[feature]; exists {
		return status
	}

	return true
}

func (fm *FeatureManager) GetAll() map[string]bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	copyMap := make(map[string]bool)
	for k, v := range fm.features {
		copyMap[k] = v
	}
	return copyMap
}
