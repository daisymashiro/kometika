package terabox

import (
	"fmt"
	"go.uber.org/zap"
)

// log adalah logger global untuk package terabox
var log *zap.Logger

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

// FetchTeraboxUniversal mencoba semua API secara berurutan
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
	for _, api := range apis {
		logInfo("Mencoba API", zap.String("api", api.name), zap.String("url", teraboxURL))
		data, err := api.fn(teraboxURL)
		if err == nil && len(data) > 0 {
			logInfo("API berhasil", zap.String("api", api.name), zap.Int("total_files", len(data)))
			return data, nil
		}
		lastErr = err
		logWarn("API gagal", zap.String("api", api.name), zap.Error(err))
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
