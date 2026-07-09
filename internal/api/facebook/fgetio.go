package facebook

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func FetchFacebookFGet(videoURL string) (*FacebookUniversalVideoData, error) {
	// Siapkan form data
	formData := url.Values{}
	formData.Set("id", videoURL)
	formData.Set("locale", "en")

	req, err := http.NewRequest("POST", "https://fget.io/process", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gagal buat request FGet: %v", err)
	}

	// Header penting untuk HTMX
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", "https://fget.io/")
	req.Header.Set("Origin", "https://fget.io")
	req.Header.Set("Referer", "https://fget.io/")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", "Linux")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request FGet gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FGet HTTP error: %d", resp.StatusCode)
	}

	// Parse HTML response
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal parse HTML FGet: %v", err)
	}

	// Thumbnail dari div.result-thumbnail img
	thumbnail := doc.Find(".result-thumbnail img").AttrOr("src", "")

	// Title dari h3.result-title
	title := strings.TrimSpace(doc.Find(".result-title").Text())
	if title == "" {
		title = "Facebook Video"
	}

	var video720p, video360p, audioMp3 string

	// Cari link download 720p, 360p, MP3
	doc.Find(".download-result").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}
		// Cek kelas: .hd, .sd, .mp3
		if s.HasClass("hd") {
			video720p = href
		} else if s.HasClass("sd") {
			video360p = href
		} else if s.HasClass("mp3") {
			audioMp3 = href
		}
	})

	vidURL := video720p
	if vidURL == "" {
		vidURL = video360p
	}

	if vidURL == "" {
		return nil, fmt.Errorf("tidak ditemukan link video di FGet")
	}

	// Generate ID unik menggunakan fungsi yang sudah ada di package ini
	uniqueID := GenerateUniqueID(videoURL)

	return &FacebookUniversalVideoData{
		ID:       uniqueID,
		Title:    title,
		VidioURL: vidURL,
		AudioURL: audioMp3,  // FGet.io mendukung audio terpisah!
		CoverURL: thumbnail, // Masukkan thumbnail di sini
	}, nil
}
