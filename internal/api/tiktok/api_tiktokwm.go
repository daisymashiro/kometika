package tiktok

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Struktur lengkap (unexported) untuk menangkap data dari API TikWM
type tikWMResponse struct {
	Code int       `json:"code"`
	Msg  string    `json:"msg"`
	Data tikWMData `json:"data"`
}

type tikWMData struct {
	ID            string   `json:"id"`
	Region        string   `json:"region"`
	Title         string   `json:"title"`
	Cover         string   `json:"cover"`
	Duration      int      `json:"duration"`
	Play          string   `json:"play"`
	WmPlay        string   `json:"wmplay"`
	HdPlay        string   `json:"hdplay"`
	Size          int      `json:"size"`
	WmSize        int      `json:"wm_size"`
	HdSize        int      `json:"hd_size"`
	Music         string   `json:"music"`
	PlayCount     int      `json:"play_count"`
	DiggCount     int      `json:"digg_count"`
	CommentCount  int      `json:"comment_count"`
	ShareCount    int      `json:"share_count"`
	DownloadCount int      `json:"download_count"`
	CreateTime    int      `json:"create_time"`
	Images        []string `json:"images,omitempty"`

	Author struct {
		ID       string `json:"id"`
		UniqueID string `json:"unique_id"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	} `json:"author"`

	MusicInfo struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Play     string `json:"play"`
		Author   string `json:"author"`
		Original bool   `json:"original"`
		Duration int    `json:"duration"`
	} `json:"music_info"`
}

// fixTikWMUrl memastikan URL yang direturn adalah absolut,
// karena TikWM sering memberikan path relatif (misal: /video/...)
func fixTikWMUrl(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	return "https://www.tikwm.com" + path
}

// FetchTikWM adalah fungsi scraper menggantikan Snaptik
func FetchTikWM(videoUrl string) (*UniversalTikTokData, error) {
	apiURL := "https://www.tikwm.com/api/"

	formData := url.Values{}
	formData.Set("url", videoUrl)
	formData.Set("count", "12")
	formData.Set("cursor", "0")
	formData.Set("web", "1")
	formData.Set("hd", "1")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat request TikWM: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke TikWM gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons TikWM: %v", err)
	}

	var result tikWMResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("gagal parsing JSON TikWM: %v", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API TikWM merespons error: %s", result.Msg)
	}

	data := result.Data

	// Prioritaskan video tanpa watermark versi HD
	videoLink := data.HdPlay
	if videoLink == "" {
		videoLink = data.Play
	}

	isAlbum := len(data.Images) > 0

	// Map data ke UniversalTikTokData
	return &UniversalTikTokData{
		ID:        data.ID,
		Title:     data.Title,
		CoverURL:  fixTikWMUrl(data.Cover),
		VideoURL:  fixTikWMUrl(videoLink),
		AudioURL:  fixTikWMUrl(data.Music),
		IsAlbum:   isAlbum,
		ImageURLs: data.Images,
	}, nil
}
