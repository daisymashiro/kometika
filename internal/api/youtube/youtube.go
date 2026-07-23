package youtube

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Resource struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Format  string  `json:"format"`
	Quality string  `json:"quality"`
	URL     string  `json:"url"`
	SizeMB  float64 `json:"sizeMB"`
}

type VideoData struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Thumbnail string     `json:"thumbnail"`
	Duration  int        `json:"duration"`
	Videos    []Resource `json:"videos"`
	Audios    []Resource `json:"audios"`
}

type vidssaveMediaResource struct {
	Format      string `json:"format"`
	Quality     string `json:"quality"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

type vidssaveMediaItem struct {
	MediaID   string                  `json:"media_id"`
	Type      string                  `json:"type"`
	Resources []vidssaveMediaResource `json:"resources"`
	Thumbnail string                  `json:"thumbnail"`
}

type vidssaveData struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	Thumbnail string              `json:"thumbnail"`
	Duration  int                 `json:"duration"`
	Resources []vidssaveResource  `json:"resources"`
	Media     []vidssaveMediaItem `json:"media"`
}

type vidssaveResource struct {
	ResourceID  string `json:"resource_id"`
	Format      string `json:"format"`
	Quality     string `json:"quality"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
}

type vidssaveResponse struct {
	Status     int           `json:"status"`
	StatusCode string        `json:"status_code"`
	Data       *vidssaveData `json:"data"`
}

// FetchYouTubeData mengambil data dari vidssave.com
func FetchYouTubeData(videoURL string) (*VideoData, error) {
	if videoURL == "" {
		return nil, errors.New("URL YouTube harus diisi")
	}

	form := url.Values{}
	form.Set("auth", "20250901majwlqo")
	form.Set("domain", "api-ak.vidssave.com")
	form.Set("origin", "source")
	form.Set("link", videoURL)

	req, err := http.NewRequest("POST", "https://api.vidssave.com/api/contentsite_api/media/parse", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://vidssave.com")
	req.Header.Set("Referer", "https://vidssave.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request gagal: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status HTTP %d", resp.StatusCode)
	}

	var apiResp vidssaveResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("gagal parse JSON: %w", err)
	}

	if apiResp.Status != 1 || apiResp.Data == nil {
		return nil, fmt.Errorf("respons tidak valid, code: %s", apiResp.StatusCode)
	}

	info := apiResp.Data
	result := &VideoData{
		ID:        info.ID,
		Title:     info.Title,
		Thumbnail: info.Thumbnail,
		Duration:  info.Duration,
	}

	addResources := func(resources []vidssaveMediaResource, mediaType string) {
		for _, r := range resources {
			if r.DownloadURL == "" {
				continue
			}
			item := Resource{
				Type:    mediaType,
				Format:  r.Format,
				Quality: r.Quality,
				URL:     r.DownloadURL,
				SizeMB:  float64(int((float64(r.Size)/1024/1024)*100+0.5)) / 100,
			}
			if mediaType == "video" {
				result.Videos = append(result.Videos, item)
			} else if mediaType == "audio" {
				result.Audios = append(result.Audios, item)
			}
		}
	}

	if len(info.Media) > 0 {
		for _, mediaItem := range info.Media {
			if mediaItem.Type == "video" || mediaItem.Type == "audio" {
				addResources(mediaItem.Resources, mediaItem.Type)
			}
		}
	} else {
		for _, r := range info.Resources {
			if r.DownloadURL == "" {
				continue
			}
			item := Resource{
				ID:      r.ResourceID,
				Type:    r.Type,
				Format:  r.Format,
				Quality: r.Quality,
				URL:     r.DownloadURL,
				SizeMB:  float64(int((float64(r.Size)/1024/1024)*100+0.5)) / 100,
			}
			if r.Type == "video" {
				result.Videos = append(result.Videos, item)
			} else if r.Type == "audio" {
				result.Audios = append(result.Audios, item)
			}
		}
	}
	return result, nil
}
