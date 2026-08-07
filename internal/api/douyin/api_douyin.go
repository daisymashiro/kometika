package douyin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// UniversalDouyinData adalah struktur data standar hasil parsing konten:
// video, foto/album, judul, deskripsi, dan cover. Audio/musik tidak
// disertakan karena hanya konten visual yang didukung.
type UniversalDouyinData struct {
	ID        string   // ID unik konten (random)
	Title     string   // judul konten
	Desc      string   // deskripsi konten
	VideoURL  string   // URL video tanpa watermark (khusus video)
	IsAlbum   bool     // true jika konten berupa foto (album)
	ImageURLs []string // daftar URL foto (jika IsAlbum true)
	CoverURL  string   // thumbnail / cover (video maupun foto)
}

// FetchDouyinData mengambil konten dari URL dan memetakannya ke
// UniversalDouyinData. Parser internal sudah punya rantai fallback
// (INITIAL_STATE -> og:meta -> proxy), jadi cukup satu panggilan.
// Cookie diambil dari env REDNOTE_COOKIE (opsional, untuk post private).
func FetchDouyinData(ctx context.Context, rawURL string) (*UniversalDouyinData, error) {
	r, err := (&API{Cookie: os.Getenv("REDNOTE_COOKIE")}).Parse(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	data := &UniversalDouyinData{
		ID:    randomID(),
		Title: r.Title,
		Desc:  r.Desc,
	}

	switch r.Type {
	case "video":
		data.VideoURL = upgradeHTTPS(r.Video)
		data.CoverURL = upgradeHTTPS(r.Cover)
	case "image":
		for _, p := range r.Photos {
			data.ImageURLs = append(data.ImageURLs, upgradeHTTPS(p))
		}
		data.IsAlbum = len(data.ImageURLs) > 1
		if len(data.ImageURLs) > 0 {
			data.CoverURL = data.ImageURLs[0]
		}
	}

	if data.VideoURL == "" && len(data.ImageURLs) == 0 {
		return nil, fmt.Errorf("konten tidak memiliki media")
	}
	return data, nil
}

// upgradeHTTPS memaksa skema https karena pipeline download (dan Telegram)
// hanya menerima koneksi HTTPS. Beberapa sumber CDN kembali http saja.
func upgradeHTTPS(raw string) string {
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

// randomID menghasilkan ID acak untuk penamaan file dan referensi konten.
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
