package media

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
)

// TestProcessAndValidateImageExtremeAspect memastikan gambar dengan rasio
// ekstrem (mirip screenshot panjang XHS) tidak lolos apa adanya — harus jadi
// JPEG valid dengan rasio aman, supaya Telegram tidak balas
// PHOTO_INVALID_DIMENSIONS.
func TestProcessAndValidateImageExtremeAspect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 600, 12000)) // rasio 20:1 lebih
	draw.Draw(src, src.Bounds(), &image.Uniform{C: color.Gray{Y: 128}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode input: %v", err)
	}

	out, err := ProcessAndValidateImageBytes(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("ProcessAndValidateImageBytes: %v", err)
	}

	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("hasil bukan JPEG valid: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("dimensi hasil tidak valid: %dx%d", cfg.Width, cfg.Height)
	}

	ratio := float64(cfg.Width) / float64(cfg.Height)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	if ratio > telegramMaxAspect {
		t.Fatalf("rasio masih ekstrem: %dx%d (%.1f:1)", cfg.Width, cfg.Height, ratio)
	}

	// Pastikan hasil memang < ukuran maks Telegram
	if len(out) > telegramPhotoMaxBytes {
		t.Fatalf("hasil terlalu besar: %d bytes", len(out))
	}
}

// TestProcessAndValidateImageNonImage memastikan konten non-gambar bikin error,
// bukan lolos ke Telegram.
func TestProcessAndValidateImageNonImage(t *testing.T) {
	_, err := ProcessAndValidateImageBytes(bytes.NewReader([]byte("<html>error</html>")), nil)
	if err == nil {
		t.Fatal("konten non-gambar seharusnya ditolak")
	}
}