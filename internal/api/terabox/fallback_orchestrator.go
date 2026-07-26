package terabox

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"mybot/internal/api"
	"mybot/internal/log"
)

var loggerPtr *zap.Logger
var teraboxBreaker = api.NewCircuitBreaker(3, 5*time.Minute)

func SetLogger(l *zap.Logger) {
	loggerPtr = l
}

func logInfo(msg string, fields ...zap.Field) {
	if loggerPtr != nil {
		loggerPtr.Info(msg, fields...)
	}
}

func logWarn(msg string, fields ...zap.Field) {
	if loggerPtr != nil {
		loggerPtr.Warn(msg, fields...)
	}
}

func logError(msg string, fields ...zap.Field) {
	if loggerPtr != nil {
		loggerPtr.Error(msg, fields...)
	}
}

func FetchTeraboxUniversal(teraboxURL string) ([]TeraboxUniversalData, error) {
	ctx := context.Background()

	apis := []struct {
		name string
		fn   func(string) ([]TeraboxUniversalData, error)
	}{
		{"Iteraplay", FetchIteraMediaUniversal},
		{"FlowVideoPlayer", fetchFlowVideoPlayerUniversal},
		{"Netlify API", FetchTeraboxAPI2},
		{"Terabox Mayumi", FetchTeraboxDirectUniversal},
	}

	var lastErr error
	for _, apiItem := range apis {
		if !teraboxBreaker.CanAttempt(apiItem.name) {
			failures, state, cooldownEnds := teraboxBreaker.GetStatus(apiItem.name)
			sisaWaktu := time.Until(cooldownEnds).Round(time.Second)

			logWarn("API Terabox di-skip (circuit breaker)",
				zap.String("api", apiItem.name),
				zap.Int("failures", failures),
				zap.Any("state", state),
				zap.Duration("sisa_waktu", sisaWaktu),
			)

			logMsg := fmt.Sprintf("API Terabox %s sedang cooldown. State: %d. Sisa waktu: %s", apiItem.name, state, sisaWaktu)
			log.LogWarn(ctx, "Terabox_CircuitBreaker", logMsg, fmt.Sprintf("failures=%d", failures))
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

		log.LogError(ctx, "Terabox_API_Gagal", err, "api="+apiItem.name, "url="+teraboxURL)
	}

	log.LogError(ctx, "Terabox_Semua_API_Gagal", lastErr, "url="+teraboxURL)
	return nil, fmt.Errorf("semua API gagal: %v", lastErr)
}

func fetchFlowVideoPlayerUniversal(teraboxURL string) ([]TeraboxUniversalData, error) {
	results, err := FetchMedia(teraboxURL)
	if err != nil {
		return nil, err
	}
	var universal []TeraboxUniversalData
	for _, res := range results {
		universal = append(universal, ResultToUniversal(res))
	}
	return universal, nil
}

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

func ResetCircuitBreaker(apiName string) {
	teraboxBreaker.Reset(apiName)
}

