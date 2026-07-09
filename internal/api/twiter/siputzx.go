package twiter

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// TwitterResponse mencerminkan respons dari API siputzx
type TwitterResponse struct {
	Status bool `json:"status"`
	Data   struct {
		ImgURL           string `json:"imgUrl"`
		DownloadLink     string `json:"downloadLink"`
		VideoTitle       string `json:"videoTitle"`
		VideoDescription string `json:"videoDescription"`
	} `json:"data"`
	Timestamp string `json:"timestamp"`
}

const twitterAPI = "https://api.siputzx.my.id/api/d/twitter"

// ---- Helper Functions ----

// GenerateNumericID menghasilkan ID numerik dari string menggunakan CRC32
func GenerateNumericID(shortcode string) string {
	hash := crc32.ChecksumIEEE([]byte(shortcode))
	return strconv.FormatUint(uint64(hash), 10)
}

// extractTweetID mengambil ID numerik dari URL tweet (bagian setelah /status/)
func extractTweetID(tweetURL string) string {
	parts := strings.Split(tweetURL, "/")
	for i, part := range parts {
		if part == "status" && i+1 < len(parts) {
			// Ambil bagian setelah "status", potong query string jika ada
			idPart := strings.Split(parts[i+1], "?")[0]
			return idPart
		}
	}
	return ""
}

// ---- Fungsi Scraping Siputzx ----

// ScrapeTwitter mengambil data tweet dari Siputzx API
// Siputzx API hanya mengembalikan single video/photo (tidak support album)
func ScrapeTwitter(ctx context.Context, tweetURL string) (*TwitterUniversalData, error) {
	if tweetURL == "" {
		return nil, fmt.Errorf("URL tweet kosong")
	}

	// Build request URL dengan query parameter
	base, _ := url.Parse(twitterAPI)
	params := url.Values{}
	params.Add("url", tweetURL)
	base.RawQuery = params.Encode()
	fullURL := base.String()

	logInfo("Siputzx API request",
		zap.String("url", fullURL),
	)

	// Buat HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("gagal buat request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal panggil API Siputzx: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API mengembalikan status %d: %s", resp.StatusCode, string(body))
	}

	// Decode JSON
	var twResp TwitterResponse
	if err := json.NewDecoder(resp.Body).Decode(&twResp); err != nil {
		return nil, fmt.Errorf("gagal parse JSON: %w", err)
	}

	if !twResp.Status {
		return nil, fmt.Errorf("API menyatakan status false")
	}

	// Validasi data minimal
	if twResp.Data.DownloadLink == "" {
		return nil, fmt.Errorf("API tidak mengembalikan download link")
	}

	// Ekstrak ID tweet dari URL
	tweetID := extractTweetID(tweetURL)
	if tweetID == "" {
		// Fallback: gunakan timestamp jika ekstraksi gagal
		tweetID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	numericID := GenerateNumericID(tweetID)

	// Bersihkan title sebelum mengembalikan
	cleanedTitle := CleanText(twResp.Data.VideoTitle)

	// Tentukan media type berdasarkan download link
	mediaType := "video" // Default: Siputzx mostly returns video
	downloadURL := twResp.Data.DownloadLink
	imageURLs := []string{}

	// Heuristic: jika URL mengandung "jpg", "png", "webp" -> photo
	lowerURL := strings.ToLower(downloadURL)
	if strings.Contains(lowerURL, ".jpg") ||
		strings.Contains(lowerURL, ".png") ||
		strings.Contains(lowerURL, ".webp") ||
		strings.Contains(lowerURL, ".jpeg") {
		mediaType = "photo"
		imageURLs = append(imageURLs, downloadURL)
	}

	// Kembalikan TwitterUniversalData
	data := &TwitterUniversalData{
		ID:          numericID,
		Title:       cleanedTitle,
		DownloadURL: downloadURL,
		CoverURL:    twResp.Data.ImgURL,
		MediaType:   mediaType,
		VideoURL:    "", // Kosong untuk photo
		ImageURLs:   imageURLs,
		IsAlbum:     false, // Siputzx tidak support album
	}

	// Set video URL jika video
	if mediaType == "video" {
		data.VideoURL = downloadURL
		logInfo("Siputzx: Video found",
			zap.String("id", numericID),
			zap.String("video_url", downloadURL),
		)
	} else {
		logInfo("Siputzx: Photo found",
			zap.String("id", numericID),
			zap.String("photo_url", downloadURL),
		)
	}

	return data, nil
}
