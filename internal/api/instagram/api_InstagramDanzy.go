package instagram

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// DanzyIGResponse struktur respons dari API Danzy
type danzyIGResponse struct {
	Creator string `json:"creator"`
	Status  bool   `json:"status"`
	Result  struct {
		Type     string   `json:"type"`
		Username string   `json:"username"`
		Thumb    string   `json:"thumb"`
		Videos   []string `json:"videos"`
		Images   []string `json:"images"`
		Mp3      []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"mp3"`
	} `json:"result"`
}

// FetchInstagramFromDanzy mengambil data Instagram dari API Danzy dan mengembalikan UniversalInstagramData
func FetchInstagramFromDanzy(instaURL string) (*UniversalInstagramData, error) {
	apiURL := "https://api.danzy.web.id/api/download/instagram?url=" + url.QueryEscape(instaURL)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke Danzy gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons Danzy: %v", err)
	}

	var raw danzyIGResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gagal parsing JSON Danzy: %v", err)
	}
	if !raw.Status {
		return nil, fmt.Errorf("Danzy API mengembalikan status false")
	}

	shortcode := extractShortcode(instaURL)
	if shortcode == "" {
		return nil, fmt.Errorf("tidak dapat mengekstrak shortcode dari URL")
	}

	// Generate ID numerik dari shortcode
	id := generateNumericID(shortcode)

	// Ambil audio jika ada
	audioURL := ""
	if len(raw.Result.Mp3) > 0 {
		audioURL = raw.Result.Mp3[0].URL
	}

	// Siapkan result
	result := &UniversalInstagramData{
		ID:        id,
		Title:     "", // caption tidak disediakan oleh Danzy
		AudioURL:  audioURL,
		IsAlbum:   false,
		ImageURLs: []string{},
	}

	switch raw.Result.Type {
	case "video":
		if len(raw.Result.Videos) == 0 {
			return nil, fmt.Errorf("tidak ada video dalam respons")
		}
		result.VideoURL = raw.Result.Videos[0]
		result.CoverURL = raw.Result.Thumb

	case "photo":
		if len(raw.Result.Images) == 0 {
			return nil, fmt.Errorf("tidak ada gambar dalam respons")
		}
		result.ImageURLs = raw.Result.Images
		result.CoverURL = raw.Result.Thumb
		result.IsAlbum = len(raw.Result.Images) > 1
		// VideoURL tetap kosong

	default:
		return nil, fmt.Errorf("tipe media tidak dikenal: %s", raw.Result.Type)
	}

	return result, nil
}

// extractShortcode mengambil shortcode Instagram dari URL (supports /p/, /reel/, /tv/)
func extractShortcode(rawURL string) string {
	re := regexp.MustCompile(`(?:/p/|/reel/|/tv/)([A-Za-z0-9_-]+)`)
	matches := re.FindStringSubmatch(rawURL)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// generateNumericID menghasilkan ID numerik (string) dari shortcode menggunakan CRC32
func generateNumericID(shortcode string) string {
	hash := crc32.ChecksumIEEE([]byte(shortcode))
	return strconv.FormatUint(uint64(hash), 10)
}
