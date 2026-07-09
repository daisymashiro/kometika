package instagram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// UniversalInstagramData ada di api danzyy

type fastdlVideoResponse struct {
	Status bool `json:"status"`
	Data   struct {
		URL []struct {
			URL     string `json:"url"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Ext     string `json:"ext"`
			Quality int    `json:"quality"`
			Subname string `json:"subname"`
		} `json:"url"`
		Meta struct {
			Title        string `json:"title"`
			Source       string `json:"source"`
			Shortcode    string `json:"shortcode"`
			Comments     []any  `json:"comments"`
			CommentCount int    `json:"comment_count"`
			LikeCount    int    `json:"like_count"`
			TakenAt      int64  `json:"taken_at"`
			DashManifest string `json:"dash_manifest"`
			Username     string `json:"username"`
		} `json:"meta"`
		Thumb   string `json:"thumb"`
		Sd      *any   `json:"sd"`
		Hosting string `json:"hosting"`
		Hd      *any   `json:"hd"`
	} `json:"data"`
	Timestamp string `json:"timestamp"`
}

type fastdlAlbumResponse struct {
	Status bool `json:"status"`
	Data   []struct {
		URL []struct {
			URL     string `json:"url"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Ext     string `json:"ext"`
			Quality int    `json:"quality"`
			Subname string `json:"subname"`
		} `json:"url"`
		Meta struct {
			Title        string `json:"title"`
			Source       string `json:"source"`
			Shortcode    string `json:"shortcode"`
			Comments     []any  `json:"comments"`
			CommentCount int    `json:"comment_count"`
			LikeCount    int    `json:"like_count"`
			TakenAt      int64  `json:"taken_at"`
			Username     string `json:"username"`
		} `json:"meta"`
		Thumb string `json:"thumb"`
	} `json:"data"`
	Timestamp string `json:"timestamp"`
}

// cleanText membersihkan newline menjadi spasi dan menghapus spasi berlebih
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	// collapse multiple spaces
	re := regexp.MustCompile(`\s+`)
	s = re.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// FetchInstagramFromFastdl menangani video, foto, album dari api.siputzx.my.id
func FetchInstagramFromFastdl(instaURL string) (*UniversalInstagramData, error) {
	apiURL := "https://api.siputzx.my.id/api/d/fastdl?url=" + url.QueryEscape(instaURL)
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("koneksi gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("baca respons gagal: %v", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(body, &rawMap); err != nil {
		return nil, fmt.Errorf("parse JSON gagal: %v", err)
	}

	status, ok := rawMap["status"].(bool)
	if !ok || !status {
		return nil, fmt.Errorf("status false atau tidak valid")
	}

	switch rawMap["data"].(type) {
	case map[string]interface{}:
		return parseVideoResponse(body, instaURL)
	case []interface{}:
		return parseAlbumResponse(body, instaURL)
	default:
		return nil, fmt.Errorf("format data tidak dikenal")
	}
}

func parseVideoResponse(body []byte, instaURL string) (*UniversalInstagramData, error) {
	var raw fastdlVideoResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if !raw.Status {
		return nil, fmt.Errorf("status video false")
	}

	shortcode := raw.Data.Meta.Shortcode
	if shortcode == "" {
		shortcode = extractShortcode(instaURL)
	}
	if shortcode == "" {
		return nil, fmt.Errorf("shortcode tidak ditemukan")
	}
	id := generateNumericID(shortcode)

	// Cari video kualitas tertinggi
	var videoURL string
	best := -1
	for _, v := range raw.Data.URL {
		if v.Quality > best && v.Type == "mp4" {
			best = v.Quality
			videoURL = v.URL
		}
	}
	if videoURL == "" && len(raw.Data.URL) > 0 {
		videoURL = raw.Data.URL[0].URL
	}
	if videoURL == "" {
		return nil, fmt.Errorf("tidak ada URL video")
	}

	audioURL := extractAudioFromManifestIgram(raw.Data.Meta.DashManifest)

	return &UniversalInstagramData{
		ID:        id,
		Title:     cleanText(raw.Data.Meta.Title),
		AudioURL:  audioURL,
		VideoURL:  videoURL,
		IsAlbum:   false,
		ImageURLs: []string{},
		CoverURL:  raw.Data.Thumb,
	}, nil
}

func parseAlbumResponse(body []byte, instaURL string) (*UniversalInstagramData, error) {
	var raw fastdlAlbumResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if !raw.Status || len(raw.Data) == 0 {
		return nil, fmt.Errorf("album kosong atau status false")
	}

	first := raw.Data[0]
	shortcode := first.Meta.Shortcode
	if shortcode == "" {
		shortcode = extractShortcode(instaURL)
	}
	if shortcode == "" {
		return nil, fmt.Errorf("shortcode tidak ditemukan")
	}
	id := generateNumericID(shortcode)

	var images []string
	for _, item := range raw.Data {
		if len(item.URL) == 0 {
			continue
		}
		// ambil kualitas tertinggi
		bestURL := item.URL[0].URL
		bestQ := item.URL[0].Quality
		for _, u := range item.URL {
			if u.Quality > bestQ {
				bestQ = u.Quality
				bestURL = u.URL
			}
		}
		images = append(images, bestURL)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("tidak ada gambar")
	}

	return &UniversalInstagramData{
		ID:        id,
		Title:     cleanText(first.Meta.Title),
		AudioURL:  "",
		VideoURL:  "",
		IsAlbum:   len(images) > 1,
		ImageURLs: images,
		CoverURL:  first.Thumb,
	}, nil
}

// fungsi generateNumericID dan extractShortcode ada di api danzyy
// (extractAudioFromManifestIgram ) ada di Igram
