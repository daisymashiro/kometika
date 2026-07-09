package facebook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ==================== VERCEL API ====================
type VercelFacebookResponse struct {
	Creator string `json:"creator"`
	Status  bool   `json:"status"`
	Data    struct {
		Title string `json:"title"`
		Sd    string `json:"sd"`
		Hd    string `json:"hd"`
		// Description dihapus
	} `json:"data"`
}

func FetchFacebookVercel(videoURL string) (*FacebookUniversalVideoData, error) {
	apiURL := "https://api-tiktokdl.vercel.app/api/download/facebook?url=" + url.QueryEscape(videoURL)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("Vercel gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("baca Vercel gagal: %v", err)
	}

	var data VercelFacebookResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("JSON Vercel invalid: %v", err)
	}
	if !data.Status {
		return nil, fmt.Errorf("Vercel status false")
	}

	hd := data.Data.Hd
	sd := data.Data.Sd
	if hd == "" && sd == "" {
		return nil, fmt.Errorf("tidak ada video")
	}
	if hd == "" {
		hd = sd
	}
	title := data.Data.Title
	if title == "" || title == "No video title" {
		title = "Facebook Video"
	}
	uniqueID := GenerateUniqueID(videoURL)

	return &FacebookUniversalVideoData{
		ID:       uniqueID,
		Title:    title,
		VidioURL: hd,
		AudioURL: "", // kosong karena tidak ada audio
		CoverURL: "", // kosong karena API Vercel tidak menyediakan thumbnail
	}, nil
}
