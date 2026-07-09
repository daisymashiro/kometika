package facebook

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

type saveFromResponse struct {
	Status bool `json:"status"`
	Data   []struct {
		Type string `json:"type"`
		Data []struct {
			ID   string `json:"id"`
			Meta struct {
				Title string `json:"title"`
			} `json:"meta"`
			HD struct {
				URL string `json:"url"`
			} `json:"hd"`
			SD struct {
				URL string `json:"url"`
			} `json:"sd"`
			Thumb string `json:"thumb"` // Diperbaiki: berupa string langsung, bukan struct
		} `json:"data"`
	} `json:"data"`
}

func init() {
	rand.Seed(time.Now().UnixNano()) // Diperbaiki: menambahkan rand.Seed
}

func GenerateUniqueID(facebookURL string) string {
	hash := md5.Sum([]byte(facebookURL))
	hexHash := hex.EncodeToString(hash[:]) // 32 karakter hex

	mainPart := hexHash[:10]

	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	randomPart := string(b)
	return mainPart + randomPart
}

// GetFacebookVideoData adalah fungsi utama untuk mengambil data video dari API SaveFrom.
func GetFacebookVideoData(videoURL string) (*FacebookUniversalVideoData, error) {
	uniqueID := GenerateUniqueID(videoURL)
	apiURL := "https://api.siputzx.my.id/api/d/savefrom"
	payload := map[string]string{"url": videoURL}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat payload JSON: %v", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gagal menghubungi API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API mengembalikan HTTP error %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca response: %v", err)
	}

	var apiResp saveFromResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("gagal parse JSON response: %v", err)
	}

	if !apiResp.Status {
		return nil, fmt.Errorf("API mengembalikan status false")
	}

	if len(apiResp.Data) == 0 || len(apiResp.Data[0].Data) == 0 {
		return nil, fmt.Errorf("tidak ada data video ditemukan")
	}

	videoData := apiResp.Data[0].Data[0]
	title := videoData.Meta.Title
	if title == "" {
		title = "Facebook Vidio" // Kosongkan jika tidak ada
	}

	videoURLResult := videoData.HD.URL
	if videoURLResult == "" {
		videoURLResult = videoData.SD.URL
	}
	if videoURLResult == "" {
		return nil, fmt.Errorf("tidak ada URL video (HD/SD) yang ditemukan")
	}

	result := &FacebookUniversalVideoData{
		ID:       uniqueID,
		Title:    title,
		VidioURL: videoURLResult,
		AudioURL: "",
		CoverURL: videoData.Thumb, // Masukkan data Thumb ke CoverURL
	}

	return result, nil
}
