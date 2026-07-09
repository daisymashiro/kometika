package instagram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

//UniversalInstagramData ada di api danzyy

// nexrayResponse struktur respons dari API NexRay
type nexrayResponse struct {
	Status bool `json:"status"`
	Result struct {
		Title     string `json:"title"`
		Thumbnail string `json:"thumbnail"`
		Media     []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"media"`
	} `json:"result"`
}

func FetchInstagramFromNexRay(instaURL string) (*UniversalInstagramData, error) {
	apiURL := "https://api.nexray.eu.cc/downloader/v2/instagram?url=" + url.QueryEscape(instaURL)
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke NexRay gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons NexRay: %v", err)
	}

	var raw nexrayResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gagal parsing JSON NexRay: %v", err)
	}
	if !raw.Status {
		return nil, fmt.Errorf("NexRay API mengembalikan status false")
	}
	if len(raw.Result.Media) == 0 {
		return nil, fmt.Errorf("tidak ada media dalam respons")
	}

	shortcode := extractShortcode(instaURL)
	if shortcode == "" {
		return nil, fmt.Errorf("tidak dapat mengekstrak shortcode dari URL")
	}
	id := generateNumericID(shortcode)

	var videoURL string
	var imageURLs []string
	for _, m := range raw.Result.Media {
		switch m.Type {
		case "mp4":
			videoURL = m.URL
		case "jpg", "png", "heic":
			imageURLs = append(imageURLs, m.URL)
		}
	}

	isAlbum := len(imageURLs) > 1
	coverURL := raw.Result.Thumbnail

	return &UniversalInstagramData{
		ID:        id,
		Title:     cleanText(raw.Result.Title),
		AudioURL:  "",
		VideoURL:  videoURL,
		IsAlbum:   isAlbum,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

// cleanText extractShortcode dan generateNumericID ada di api danzyy
