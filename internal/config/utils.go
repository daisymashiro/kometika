package config

import (
	"bytes"
	"fmt"
	_ "golang.org/x/image/webp" // Wajib untuk decode WebP otomatis
	"image"
	"image/jpeg"
	_ "image/png" // Wajib untuk decode PNG otomatis
)

// ConvertImageToJPEG mendeteksi gambar (WebP/PNG) dan mengubahnya ke JPEG
func ConvertImageToJPEG(inputBytes []byte) ([]byte, error) {
	// 1. Decode byte mentah. Go otomatis mengenali WebP, PNG, atau JPEG
	img, format, err := image.Decode(bytes.NewReader(inputBytes))
	if err != nil {
		return nil, fmt.Errorf("gagal mendecode gambar (mungkin format tidak didukung): %w", err)
	}

	// 2. Jika formatnya sudah JPEG, langsung kembalikan aslinya (Hemat CPU)
	if format == "jpeg" {
		return inputBytes, nil
	}

	// 3. Jika format lain (seperti webp atau png), konversi ke JPEG
	var buf bytes.Buffer
	// Quality 90 sudah cukup bagus dan tidak memakan size besar
	err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	if err != nil {
		return nil, fmt.Errorf("gagal encode gambar ke JPEG: %w", err)
	}

	return buf.Bytes(), nil
}
