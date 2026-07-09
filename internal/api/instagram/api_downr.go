package instagram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
)

// DownrResponse ...
type DownrResponse struct {
	URL       string      `json:"url"`
	Source    string      `json:"source"`
	Title     string      `json:"title"`
	Author    string      `json:"author"`
	Shortcode string      `json:"shortcode"`
	ViewCount *int        `json:"view_count"`
	LikeCount int         `json:"like_count"`
	Thumbnail string      `json:"thumbnail"`
	Duration  int         `json:"duration"`
	Owner     Owner       `json:"owner"`
	Location  interface{} `json:"location"`
	Medias    []Media     `json:"medias"`
	Type      string      `json:"type"`
	Error     bool        `json:"error"`
	TimeEnd   int         `json:"time_end"`
}

// Owner ...
type Owner struct {
	Username         string      `json:"username"`
	ProfilePicURL    string      `json:"profile_pic_url"`
	IsUnpublished    bool        `json:"is_unpublished"`
	FullName         string      `json:"full_name"`
	ID               string      `json:"id"`
	Pk               string      `json:"pk"`
	FriendshipStatus interface{} `json:"friendship_status"`
	IsVerified       bool        `json:"is_verified"`
	IsPrivate        bool        `json:"is_private"`
	ProfilePicURLHD  string      `json:"profile_pic_url_hd"`
	__typename       string      `json:"__typename"`
	IsEmbedsDisabled bool        `json:"is_embeds_disabled"`
}

// Media ...
type Media struct {
	ID         string      `json:"id"`
	URL        string      `json:"url"`
	Thumbnail  string      `json:"thumbnail,omitempty"`
	Quality    string      `json:"quality,omitempty"`
	Resolution string      `json:"resolution,omitempty"`
	Duration   int         `json:"duration,omitempty"`
	IsAudio    bool        `json:"is_audio,omitempty"`
	Type       string      `json:"type"`
	Extension  string      `json:"extension,omitempty"`
	MimeType   string      `json:"mimeType,omitempty"`
	Codec      string      `json:"codec,omitempty"`
	Bandwidth  int         `json:"bandwidth,omitempty"`
	Width      interface{} `json:"width,omitempty"`
	Height     interface{} `json:"height,omitempty"`
	FrameRate  interface{} `json:"frameRate,omitempty"`
}

// GenerateNumericID ...
func GenerateNumericID(shortcode string) string {
	hash := crc32.ChecksumIEEE([]byte(shortcode))
	return strconv.FormatUint(uint64(hash), 10)
}

// ensureSession dengan header lengkap
func ensureSession(client *http.Client) error {
	req, err := http.NewRequest("GET", "https://downr.org/.netlify/functions/analytics", nil)
	if err != nil {
		return err
	}

	// Header lengkap (mirip browser)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Linux"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Debug cookie
	cookies := client.Jar.Cookies(req.URL)
	if len(cookies) == 0 {
		return fmt.Errorf("tidak ada cookie yang diterima dari /analytics")
	}
	fmt.Printf("[DEBUG] Cookie diterima: %d buah\n", len(cookies))
	for _, c := range cookies {
		fmt.Printf("  %s = %s\n", c.Name, c.Value[:50]+"...")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("analytics endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func FetchDownr(igURL string) (*UniversalInstagramData, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 40 * time.Second, // <-- Batas waktu 30 detik
	}

	fmt.Println("[DEBUG] Init session via /analytics...")
	if err := ensureSession(client); err != nil {
		return nil, fmt.Errorf("session init failed: %v", err)
	}

	fmt.Println("[DEBUG] Session ready, sending POST to /nyt...")
	payload := map[string]string{"url": igURL}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("gagal marshaling payload: %v", err)
	}

	req, err := http.NewRequest("POST", "https://downr.org/.netlify/functions/nyt", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("gagal buat request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://downr.org")
	req.Header.Set("Referer", "https://downr.org/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status code %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal baca response: %v", err)
	}

	var downrResp DownrResponse
	if err := json.Unmarshal(body, &downrResp); err != nil {
		return nil, fmt.Errorf("gagal parse JSON: %v, body: %s", err, string(body))
	}

	if downrResp.Error {
		return nil, fmt.Errorf("downr.org mengembalikan error: %+v", downrResp)
	}

	// 🔁 KONVERSI ke UniversalInstagramData
	data := MapToUniversal(&downrResp)
	return &data, nil
}

// MapToUniversal ...
func MapToUniversal(downr *DownrResponse) UniversalInstagramData {
	result := UniversalInstagramData{
		ImageURLs: []string{},
	}

	if downr.Shortcode != "" {
		result.ID = GenerateNumericID(downr.Shortcode)
	}

	result.Title = downr.Title
	result.CoverURL = downr.Thumbnail

	var videoURL, audioURL string
	var imageURLs []string

	for _, media := range downr.Medias {
		switch media.Type {
		case "video":
			if videoURL == "" {
				videoURL = media.URL
			}
		case "audio":
			if audioURL == "" {
				audioURL = media.URL
			}
		case "image":
			imageURLs = append(imageURLs, media.URL)
		default:
			lowerURL := strings.ToLower(media.URL)
			if strings.Contains(lowerURL, ".mp4") || strings.Contains(lowerURL, "video") {
				if videoURL == "" {
					videoURL = media.URL
				}
			} else if strings.Contains(lowerURL, ".mp3") || strings.Contains(lowerURL, "audio") {
				if audioURL == "" {
					audioURL = media.URL
				}
			} else {
				imageURLs = append(imageURLs, media.URL)
			}
		}
	}

	result.VideoURL = videoURL
	result.AudioURL = audioURL
	result.ImageURLs = imageURLs

	// Hitung visual count
	visualCount := 0
	if videoURL != "" {
		visualCount++
	}
	visualCount += len(imageURLs)
	result.IsAlbum = visualCount > 1

	return result
}
