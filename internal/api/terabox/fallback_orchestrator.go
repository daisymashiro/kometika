package terabox

import (
	"fmt"
	"go.uber.org/zap"
	"mybot/internal/api"
)

// log adalah logger global untuk package terabox
var log *zap.Logger

// Circuit breaker global untuk Terabox APIs
var teraboxBreaker = api.NewCircuitBreaker(3, 5*60) // 3 kegagalan, cooldown 5 menit

// SetLogger digunakan oleh main untuk menginjeksi logger ke package ini
func SetLogger(l *zap.Logger) {
	log = l
}

// helper logging internal (aman jika log nil)
func logInfo(msg string, fields ...zap.Field) {
	if log != nil {
		log.Info(msg, fields...)
	}
}

func logWarn(msg string, fields ...zap.Field) {
	if log != nil {
		log.Warn(msg, fields...)
	}
}

func logError(msg string, fields ...zap.Field) {
	if log != nil {
		log.Error(msg, fields...)
	}
}

// FetchTeraboxUniversal mencoba semua API dengan circuit breaker
func FetchTeraboxUniversal(teraboxURL string) ([]TeraboxUniversalData, error) {
	// Daftar API dalam urutan prioritas
	apis := []struct {
		name string
		fn   func(string) ([]TeraboxUniversalData, error)
	}{
		{"Iteraplay", FetchIteraMediaUniversal},
		{"Terabox Mayumi", FetchTeraboxDirectUniversal},
		{"FlowVideoPlayer", fetchFlowVideoPlayerUniversal},
		{"Netlify API", FetchTeraboxAPI2},
	}

	var lastErr error
	for _, apiItem := range apis {
		// Cek circuit breaker
		if !teraboxBreaker.CanAttempt(apiItem.name) {
			failures, inCooldown := teraboxBreaker.GetStatus(apiItem.name)
			logWarn("API Terabox di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Bool("in_cooldown", inCooldown),
			)
			continue
		}

		logInfo("Mencoba API", zap.String("api", apiItem.name), zap.String("url", teraboxURL))
		data, err := apiItem.fn(teraboxURL)
		
		if err == nil && len(data) > 0 {
			logInfo("API berhasil", zap.String("api", apiItem.name), zap.Int("total_files", len(data)))
			teraboxBreaker.RecordSuccess(apiItem.name)
			return data, nil
		}
		
		lastErr = err
		teraboxBreaker.RecordFailure(apiItem.name)
		logWarn("API gagal", zap.String("api", apiItem.name), zap.Error(err))
	}
	return nil, fmt.Errorf("semua API gagal: %v", lastErr)
}

// fetchFlowVideoPlayerUniversal adalah adapter untuk mengkonversi Result ke TeraboxUniversalData
func fetchFlowVideoPlayerUniversal(teraboxURL string) ([]TeraboxUniversalData, error) {
	results, err := FetchMedia(teraboxURL) // dari flowvideoplayer.go
	if err != nil {
		return nil, err
	}
	var universal []TeraboxUniversalData
	for _, res := range results {
		universal = append(universal, ResultToUniversal(res))
	}
	return universal, nil
}

// ResultToUniversal mengkonversi tipe Result ke TeraboxUniversalData
func ResultToUniversal(r Result) TeraboxUniversalData {
	return TeraboxUniversalData{
		ID:          r.ID,
		FileName:    r.FileName,
		FileSize:    r.FileSize,
		Thumbnail:   r.CoverURL,
		StreamURL:   r.StreamURL,
		DownloadURL: r.FileURL,
	}
}

// ResetCircuitBreaker mereset circuit breaker untuk API tertentu (untuk debugging)
func ResetCircuitBreaker(apiName string) {
	teraboxBreaker.Reset(apiName)
}
