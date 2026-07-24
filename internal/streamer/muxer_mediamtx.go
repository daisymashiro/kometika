package streamer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

// PushToMediaMTX menggabungkan Video dan Audio secara In-Memory lalu push ke MediaMTX lokal
// MediaMTX kemudian akan re-stream ke Telegram RTMP endpoint
func PushToMediaMTX(ctx context.Context, videoURL, audioURL, mediamtxURL string, logger *zap.Logger) error {
	// User-Agent untuk bypass 403 Forbidden
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	args := []string{
		"-re", // Baca input secara real-time
		"-user_agent", userAgent,
		"-i", videoURL,
		"-user_agent", userAgent,
		"-i", audioURL,
		"-map", "0:v:0", // Map video dari input pertama
		"-map", "1:a:0", // Map audio dari input kedua
		"-c:v", "copy",  // Copy video tanpa re-encode (sangat efisien)
		"-c:a", "aac",   // Encode audio ke AAC
		"-b:a", "128k",  // Audio bitrate
		"-ar", "44100",  // Sample rate 44.1kHz (standar untuk streaming)
		"-shortest",     // Stop jika salah satu input selesai
		"-f", "flv",     // Format FLV untuk RTMP
		"-flvflags", "no_duration_filesize",
		mediamtxURL, // Push ke MediaMTX lokal (misal: rtmp://localhost:1935/live/youtube123)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	logger.Info("Starting FFmpeg → MediaMTX muxing...", 
		zap.String("destination", mediamtxURL),
		zap.String("video_source", videoURL[:50]+"..."),
		zap.String("audio_source", audioURL[:50]+"..."),
	)

	err := cmd.Run()
	if err != nil {
		// Jika context di-cancel (user stop), bukan error
		if ctx.Err() != nil {
			logger.Info("Streaming dihentikan oleh user (Context Cancelled).")
			return nil
		}

		// Log error FFmpeg untuk debugging
		logger.Error("FFmpeg error",
			zap.Error(err),
			zap.String("ffmpeg_stderr", stderrBuf.String()),
		)
		return fmt.Errorf("ffmpeg streaming error: %w", err)
	}

	logger.Info("Streaming selesai dengan sukses.")
	return nil
}

// PushToMediaMTXWithRetry melakukan retry jika FFmpeg gagal
func PushToMediaMTXWithRetry(ctx context.Context, videoURL, audioURL, mediamtxURL string, logger *zap.Logger, maxRetries int) error {
	var lastErr error
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Info("FFmpeg attempt", zap.Int("attempt", attempt), zap.Int("max", maxRetries))
		
		err := PushToMediaMTX(ctx, videoURL, audioURL, mediamtxURL, logger)
		
		if err == nil {
			return nil // Sukses
		}
		
		// Jika user cancel, langsung return
		if ctx.Err() != nil {
			return ctx.Err()
		}
		
		lastErr = err
		logger.Warn("FFmpeg failed, will retry...", 
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		
		// Jangan retry jika sudah attempt terakhir
		if attempt < maxRetries {
			// Tunggu sebentar sebelum retry (exponential backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
				// Lanjut ke retry berikutnya
			}
		}
	}
	
	return fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, lastErr)
}
