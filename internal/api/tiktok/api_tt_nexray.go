package tiktok

import (
	"encoding/json"
	"fmt"
	"io"
	"mybot/internal/api"
	"net/http"
)

type NexRayResponse struct {
	Status bool `json:"status"`
	Result struct {
		Title     string      `json:"title"`
		Cover     string      `json:"cover"`
		ID        string      `json:"id"`
		Data      interface{} `json:"data"` // bisa string atau array
		MusicInfo struct {
			Title string `json:"title"`
			Url   string `json:"url"`
		} `json:"music_info"`
	} `json:"result"`
}

func FetchNexRay(videoUrl string) (*UniversalTikTokData, error) {
	apiURL := "https://api.nexray.web.id/downloader/tiktok?url=" + videoUrl

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke NexRay gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons NexRay: %v", err)
	}

	var data NexRayResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("gagal parsing JSON NexRay: %v", err)
	}
	if !data.Status {
		return nil, fmt.Errorf("NexRay status false")
	}

	videoID := data.Result.ID
	if videoID == "" {
		videoID = api.ShortID(videoUrl)
	}

	var videoURL string
	var imageURLs []string
	isAlbum := false

	switch d := data.Result.Data.(type) {
	case string:
		videoURL = d
	case []interface{}:
		isAlbum = true
		for _, item := range d {
			if urlStr, ok := item.(string); ok {
				imageURLs = append(imageURLs, urlStr)
			}
		}
		if len(imageURLs) == 0 {
			return nil, fmt.Errorf("tidak ada URL gambar di album")
		}
	default:
		return nil, fmt.Errorf("tipe data tidak dikenal")
	}

	// Jika bukan album dan videoURL kosong, anggap gagal
	if !isAlbum && videoURL == "" {
		return nil, fmt.Errorf("tidak ada video URL atau album")
	}

	return &UniversalTikTokData{
		ID:        videoID,
		Title:     data.Result.Title,
		CoverURL:  data.Result.Cover,
		VideoURL:  videoURL,
		AudioURL:  data.Result.MusicInfo.Url,
		IsAlbum:   isAlbum,
		ImageURLs: imageURLs,
	}, nil
}
