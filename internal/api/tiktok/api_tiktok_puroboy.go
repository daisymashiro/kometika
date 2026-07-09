package tiktok

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type puruboyBaseResponse struct {
	Success bool            `json:"success"`
	Author  string          `json:"author"`
	Result  json.RawMessage `json:"result"`
}

type puruboyContent struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	Downloads []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"downloads"`
}

func FetchPuruboyTikTok(videoUrl string) (*UniversalTikTokData, error) {
	endpoint := "https://puruboy-api.vercel.app/api/downloader/tiktok-v2"
	reqBody, _ := json.Marshal(map[string]string{"url": videoUrl})

	client := &http.Client{Timeout: 40 * time.Second}
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("gagal buat request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi API gagal: %v", err)
	}
	defer resp.Body.Close()

	var base puruboyBaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&base); err != nil {
		return nil, fmt.Errorf("decode JSON gagal: %v", err)
	}

	if !base.Success {
		return nil, fmt.Errorf("API mengembalikan success=false")
	}

	var content puruboyContent
	if err := json.Unmarshal(base.Result, &content); err != nil {
		return nil, fmt.Errorf("gagal parse result: %v", err)
	}

	data := &UniversalTikTokData{
		ID:       extractIDFromURL(videoUrl),
		Title:    content.Title,
		CoverURL: content.Thumbnail,
		IsAlbum:  content.Type == "photo",
	}

	for _, dl := range content.Downloads {
		switch {
		case content.Type == "video" && strings.Contains(dl.Type, "MP4"):
			if data.VideoURL == "" || strings.Contains(dl.Type, "HD") {
				data.VideoURL = dl.URL
			}
		case strings.Contains(dl.Type, "MP3"):
			data.AudioURL = dl.URL
		case content.Type == "photo" && strings.HasPrefix(dl.Type, "Image"):
			data.ImageURLs = append(data.ImageURLs, dl.URL)
		}
	}

	if content.Type == "video" && data.VideoURL == "" {
		return nil, fmt.Errorf("tidak ditemukan URL video dalam respon")
	}

	if content.Type == "photo" && len(data.ImageURLs) == 0 {
		return nil, fmt.Errorf("tidak ditemukan gambar dalam respon")
	}

	return data, nil
}

func extractIDFromURL(url string) string {
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if part == "video" && i+1 < len(parts) {
			id := strings.TrimSpace(parts[i+1])
			if idx := strings.Index(id, "?"); idx != -1 {
				id = id[:idx]
			}
			return id
		}
	}

	url = strings.TrimSuffix(url, "/")
	if lastSlash := strings.LastIndex(url, "/"); lastSlash != -1 {
		id := url[lastSlash+1:]
		if idx := strings.Index(id, "?"); idx != -1 {
			id = id[:idx]
		}
		return id
	}

	return url
}
