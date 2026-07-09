package twiter

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---- Struct untuk response dari fxtwitter.com ----

type TweetResponse struct {
	Tweet Tweet `json:"tweet"`
}

type Tweet struct {
	ID               string    `json:"id"`
	Text             string    `json:"text"`
	CreatedAt        string    `json:"created_at"`
	CreatedTimestamp int64     `json:"created_timestamp"`
	Author           Author    `json:"author"`
	Media            MediaData `json:"media"`
}

type Author struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	AvatarURL  string `json:"avatar_url"`
}

type MediaData struct {
	All []MediaItem `json:"all"`
}

type MediaItem struct {
	Type    string        `json:"type"`    // "photo" atau "video"
	URL     string        `json:"url"`     // untuk foto
	Formats []VideoFormat `json:"formats"` // untuk video
}

type VideoFormat struct {
	URL     string `json:"url"`
	Bitrate int    `json:"bitrate"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// ---- Helper Functions ----

func CleanText(text string) string {
	// Hapus escape characters \n dan \n\n
	text = strings.ReplaceAll(text, "\\n", " ")
	text = strings.ReplaceAll(text, "\\r", " ")
	text = strings.ReplaceAll(text, "\\t", " ")

	// Hapus multiple spaces menjadi single space
	re := regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")

	// Trim whitespace
	text = strings.TrimSpace(text)

	return text
}

func extractPath(tweetURL string) string {
	// Hilangkan protokol
	if strings.HasPrefix(tweetURL, "http://") {
		tweetURL = strings.TrimPrefix(tweetURL, "http://")
	} else if strings.HasPrefix(tweetURL, "https://") {
		tweetURL = strings.TrimPrefix(tweetURL, "https://")
	}

	// Potong domain
	parts := strings.SplitN(tweetURL, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	path := "/" + parts[1]

	// Pastikan path mengandung "/status/"
	if !strings.Contains(path, "/status/") {
		return ""
	}
	return path
}

// ---- Fungsi utama scraping FXTwitter ----

// FetchTweetData mengambil data tweet dari FXTwitter API
func FetchTweetData(ctx context.Context, tweetURL string) (*TwitterUniversalData, error) {
	path := extractPath(tweetURL)
	if path == "" {
		return nil, fmt.Errorf("invalid tweet URL: %s", tweetURL)
	}

	apiURL := "https://api.fxtwitter.com" + path

	logInfo("FXTwitter API request",
		zap.String("url", apiURL),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call fxtwitter API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var tweetResp TweetResponse
	if err := json.NewDecoder(resp.Body).Decode(&tweetResp); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	tweet := tweetResp.Tweet

	// Buat data dengan format universal
	cleanedText := CleanText(tweet.Text)
	data := &TwitterUniversalData{
		ID:    tweet.ID,
		Title: fmt.Sprintf("%s: %s", tweet.Author.ScreenName, cleanedText),
	}

	if len(tweet.Media.All) == 0 {
		// Tidak ada media, set cover URL ke avatar
		logWarn("FXTwitter: No media found", zap.String("id", tweet.ID))
		data.CoverURL = tweet.Author.AvatarURL
		data.MediaType = "text"
		return data, nil
	}

	var photoURLs []string
	var videoFormats []VideoFormat

	// Kategorisasi media
	for _, media := range tweet.Media.All {
		switch media.Type {
		case "photo":
			photoURLs = append(photoURLs, media.URL)
		case "video":
			if len(media.Formats) > 0 {
				videoFormats = append(videoFormats, media.Formats...)
			}
		}
	}

	// PRIORITAS 1: Jika ada video, pilih bitrate tertinggi
	if len(videoFormats) > 0 {
		best := videoFormats[0]
		for _, vf := range videoFormats[1:] {
			if vf.Bitrate > best.Bitrate {
				best = vf
			}
		}
		data.VideoURL = best.URL
		data.DownloadURL = best.URL // Set utama untuk compatibility
		data.CoverURL = tweet.Author.AvatarURL
		data.MediaType = "video"

		logInfo("FXTwitter: Video found",
			zap.String("id", tweet.ID),
			zap.String("video_url", best.URL),
			zap.Int("bitrate", best.Bitrate),
		)

	} else if len(photoURLs) > 0 {
		// PRIORITAS 2: Jika ada foto
		data.ImageURLs = photoURLs
		data.DownloadURL = photoURLs[0] // Set utama untuk compatibility
		data.CoverURL = photoURLs[0]

		if len(photoURLs) == 1 {
			data.MediaType = "photo"
			logInfo("FXTwitter: Single photo found",
				zap.String("id", tweet.ID),
				zap.String("photo_url", photoURLs[0]),
			)
		} else {
			data.MediaType = "album"
			data.IsAlbum = true
			logInfo("FXTwitter: Album found",
				zap.String("id", tweet.ID),
				zap.Int("photo_count", len(photoURLs)),
			)
		}
	}

	return data, nil
}
