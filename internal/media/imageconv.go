package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"strings"

	"go.uber.org/zap"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	telegramPhotoMaxBytes = 9 * 1024 * 1024 // aman di bawah 10 MB
	telegramPhotoMaxSide  = 2560            // aman untuk photo Telegram
	telegramMaxAspect     = 20.0
)

// ProcessAndValidateImage mengambil stream mentah, mendeteksi MIME,
// mengonversi ke JPEG, resize bila perlu, dan memastikan hasil ramah MTProto.
//
// Zero disk: tidak tulis file ke disk.
// Catatan: hasil akhir tetap disimpan di memory sebagai []byte.
func ProcessAndValidateImage(body io.Reader, logger *zap.Logger) (io.Reader, error) {
	out, err := ProcessAndValidateImageBytes(body, logger)
	if err != nil {
		return nil, err
	}

	// bytes.NewReader lebih aman daripada &bytes.Buffer
	// karena punya Size/Len/Seek dan posisi baca mulai dari awal.
	return bytes.NewReader(out), nil
}

// ProcessAndValidateImageBytes versi bytes.
// Ini berguna kalau nanti SendPhotoStream ingin upload pakai FromBytes.
func ProcessAndValidateImageBytes(body io.Reader, logger *zap.Logger) ([]byte, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	header := make([]byte, 512)

	n, err := io.ReadFull(body, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("gagal membaca header stream: %w", err)
	}

	if n == 0 {
		return nil, fmt.Errorf("stream gambar kosong")
	}

	mimeType := http.DetectContentType(header[:n])

	logger.Info("Mendeteksi tipe MIME image dari CDN", zap.String("mime_type", mimeType))

	fullStream := io.MultiReader(bytes.NewReader(header[:n]), body)

	var img image.Image

	switch mimeType {
	case "image/jpeg":
		img, err = jpeg.Decode(fullStream)
		if err != nil {
			return nil, fmt.Errorf("gagal decode data JPEG: %w", err)
		}

	case "image/webp":
		logger.Debug("Mengonversi WebP ke JPEG on-the-fly")

		img, err = webp.Decode(fullStream)
		if err != nil {
			return nil, fmt.Errorf("gagal decode data WebP: %w", err)
		}

	case "image/png":
		logger.Debug("Mengonversi PNG ke JPEG on-the-fly")

		img, err = png.Decode(fullStream)
		if err != nil {
			return nil, fmt.Errorf("gagal decode data PNG: %w", err)
		}

	case "image/gif":
		logger.Debug("Mengonversi GIF frame pertama ke JPEG on-the-fly")

		img, err = gif.Decode(fullStream)
		if err != nil {
			return nil, fmt.Errorf("gagal decode data GIF: %w", err)
		}

	default:
		if strings.HasPrefix(mimeType, "text/") ||
			mimeType == "application/json" ||
			mimeType == "application/xml" {
			return nil, fmt.Errorf("CDN mengembalikan konten non-gambar (%s)", mimeType)
		}

		return nil, fmt.Errorf("tipe MIME gambar tidak didukung untuk photo Telegram: %s", mimeType)
	}

	out, err := encodeTelegramSafeJPEG(img)
	if err != nil {
		return nil, err
	}

	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("hasil JPEG tidak valid setelah encode: %w", err)
	}

	logger.Info(
		"JPEG image siap dikirim ke Telegram",
		zap.Int("width", cfg.Width),
		zap.Int("height", cfg.Height),
		zap.Int("bytes", len(out)),
	)

	return out, nil
}

func encodeTelegramSafeJPEG(img image.Image) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("image nil")
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("dimensi image tidak valid: %dx%d", width, height)
	}

	// 1. Flatten alpha ke putih dan normalize bounds ke Rect(0,0,w,h).
	normalized := flattenOnWhite(img)

	// 2. Jika aspect ratio terlalu ekstrem, beri padding putih.
	normalized = padToSafeAspectRatio(normalized, telegramMaxAspect)

	// 3. Coba beberapa ukuran dan quality sampai aman.
	maxSides := []int{
		telegramPhotoMaxSide, // 2560
		2048,
		1600,
		1280,
		1024,
	}

	qualities := []int{
		90,
		85,
		80,
		75,
		70,
		65,
		60,
	}

	var lastSize int
	var lastErr error

	for _, maxSide := range maxSides {
		resized := resizeMax(normalized, maxSide)

		for _, quality := range qualities {
			var buf bytes.Buffer

			err := jpeg.Encode(&buf, resized, &jpeg.Options{
				Quality: quality,
			})
			if err != nil {
				return nil, fmt.Errorf("gagal encode ke JPEG: %w", err)
			}

			lastSize = buf.Len()

			if buf.Len() > telegramPhotoMaxBytes {
				continue
			}

			// Validasi hasil encode.
			_, err = jpeg.DecodeConfig(bytes.NewReader(buf.Bytes()))
			if err != nil {
				lastErr = err
				continue
			}

			return buf.Bytes(), nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("hasil JPEG gagal validasi: %w", lastErr)
	}

	return nil, fmt.Errorf("hasil JPEG terlalu besar untuk photo Telegram: %d bytes", lastSize)
}

func flattenOnWhite(src image.Image) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	stddraw.Draw(
		dst,
		dst.Bounds(),
		&image.Uniform{C: color.White},
		image.Point{},
		stddraw.Src,
	)

	stddraw.Draw(
		dst,
		dst.Bounds(),
		src,
		bounds.Min,
		stddraw.Over,
	)

	return dst
}

func resizeMax(src image.Image, maxSide int) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width <= 0 || height <= 0 {
		return src
	}

	if width <= maxSide && height <= maxSide {
		return src
	}

	var newWidth int
	var newHeight int

	if width >= height {
		newWidth = maxSide
		newHeight = int(float64(height) * float64(maxSide) / float64(width))
	} else {
		newHeight = maxSide
		newWidth = int(float64(width) * float64(maxSide) / float64(height))
	}

	if newWidth < 1 {
		newWidth = 1
	}

	if newHeight < 1 {
		newHeight = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	xdraw.CatmullRom.Scale(
		dst,
		dst.Bounds(),
		src,
		src.Bounds(),
		xdraw.Over,
		nil,
	)

	return dst
}

func padToSafeAspectRatio(src image.Image, maxAspect float64) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width <= 0 || height <= 0 {
		return src
	}

	ratio := float64(width) / float64(height)
	if ratio < 1 {
		ratio = 1 / ratio
	}

	if ratio <= maxAspect {
		return src
	}

	newWidth := width
	newHeight := height

	if width > height {
		newHeight = int(math.Ceil(float64(width) / maxAspect))
	} else {
		newWidth = int(math.Ceil(float64(height) / maxAspect))
	}

	if newWidth < width {
		newWidth = width
	}

	if newHeight < height {
		newHeight = height
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	stddraw.Draw(
		dst,
		dst.Bounds(),
		&image.Uniform{C: color.White},
		image.Point{},
		stddraw.Src,
	)

	offsetX := (newWidth - width) / 2
	offsetY := (newHeight - height) / 2

	targetRect := image.Rect(
		offsetX,
		offsetY,
		offsetX+width,
		offsetY+height,
	)

	stddraw.Draw(
		dst,
		targetRect,
		src,
		bounds.Min,
		stddraw.Over,
	)

	return dst
}

