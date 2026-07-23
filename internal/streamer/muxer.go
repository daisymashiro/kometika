package streamer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"go.uber.org/zap"
)

// PushToRTMP menggabungkan Video dan Audio secara In-Memory lalu memancarkannya ke RTMP Telegram
func PushToRTMP(ctx context.Context, videoURL, audioURL, rtmpURL string, logger *zap.Logger) error {
	// Menambahkan User-Agent agar tidak diblokir (403 Forbidden) oleh server video/audio
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	args := []string{
		"-re", // Baca input secara real-time
		"-user_agent", userAgent,
		"-i", videoURL,
		"-user_agent", userAgent,
		"-i", audioURL,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "copy", // Zero CPU: Copy video H264 murni
		"-c:a", "aac", // Ubah audio ke AAC (Sangat ringan untuk STB)
		"-b:a", "128k", // Bitrate standar
		"-ar", "44100", // Wajib 44.1 kHz agar diterima oleh server RTMP Telegram
		"-shortest", // Berhenti jika video/audio selesai
		"-f", "flv", // Format RTMP
		"-flvflags", "no_duration_filesize",
		rtmpURL,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	logger.Info("Memulai In-Memory Muxing & RTMP Push...", zap.String("rtmp", rtmpURL))

	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			logger.Info("Livestream dihentikan secara manual (Context Cancelled).")
			return nil
		}
		// Log error FFmpeg akan sangat membantu kita melihat masalah aslinya
		logger.Error("FFmpeg RTMP Push terhenti",
			zap.Error(err),
			zap.String("ffmpeg_stderr", stderrBuf.String()),
		)
		return fmt.Errorf("ffmpeg streaming error: %w", err)
	}

	logger.Info("Livestream selesai.")
	return nil
}
