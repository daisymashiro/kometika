package facebook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// ==================== FLY.DEV API ====================
type FlyDevResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Links   struct {
		HighQuality string `json:"Download High Quality"`
		LowQuality  string `json:"Download Low Quality"`
	} `json:"links"`
}

// FetchFacebookFlyDev menggunakan API fly.dev untuk mendapatkan video URL
// Mengembalikan FacebookUniversalVideoData dengan ID dari GenerateUniqueID, dan AudioURL kosong.
func FetchFacebookFlyDev(videoURL string) (*FacebookUniversalVideoData, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("url", videoURL)
	writer.Close()

	req, _ := http.NewRequest("POST", "https://facebook-video-downloader.fly.dev/app/main.php", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FlyDev gagal: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("baca FlyDev gagal: %v", err)
	}

	var data FlyDevResponse
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("JSON FlyDev invalid: %v", err)
	}
	if !data.Success {
		return nil, fmt.Errorf("FlyDev success false")
	}

	hd := data.Links.HighQuality
	sd := data.Links.LowQuality
	if hd == "" && sd == "" {
		return nil, fmt.Errorf("tidak ada link")
	}
	if hd == "" {
		hd = sd
	}
	title := data.Title
	if title == "" {
		title = "Facebook Video"
	}
	// Gunakan GenerateUniqueID dari package yang sama (sudah ada)
	uniqueID := GenerateUniqueID(videoURL)

	return &FacebookUniversalVideoData{
		ID:       uniqueID,
		Title:    title,
		VidioURL: hd,
		AudioURL: "", // kosong karena tidak ada audio
		CoverURL: "", // kosong karena API ini tidak menyediakan thumbnail
	}, nil
}
