package instagram

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/url"
	"strconv"

	"github.com/sardanioss/httpcloak/client"
)

// ------------------- private helpers -------------------

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

func encryptECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	padded := pkcs7Pad(plaintext, blockSize)
	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += blockSize {
		block.Encrypt(ciphertext[i:i+blockSize], padded[i:i+blockSize])
	}
	return ciphertext, nil
}

func encryptURL(urlStr string) (string, error) {
	key := []byte("qwertyuioplkjhgf")
	plaintext := []byte(urlStr)
	ciphertext, err := encryptECB(plaintext, key)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ciphertext), nil
}

// response structures
type responseAllinone struct {
	Video []struct {
		Video     string `json:"video"`
		Thumbnail string `json:"thumbnail"`
	} `json:"video"`
	Image []string `json:"image"`
	Fetch bool     `json:"fetch"`
}

type responseAudio struct {
	Media []struct {
		URL    string `json:"url"`
		Poster string `json:"poster"`
	} `json:"media"`
	Audio bool `json:"audio"`
}

func fetchAllinone(ctx context.Context, c *client.Client, encryptedURL string) (*responseAllinone, error) {
	headers := map[string][]string{
		"url":        {encryptedURL},
		"Origin":     {"https://fastvideosave.net"},
		"Referer":    {"https://fastvideosave.net/"},
		"User-Agent": {"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"},
	}

	resp, err := c.Get(ctx, "https://api.videodropper.app/allinone", headers)
	if err != nil {
		return nil, fmt.Errorf("request /allinone gagal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status /allinone: %d", resp.StatusCode)
	}

	var data responseAllinone
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode /allinone gagal: %w", err)
	}
	return &data, nil
}

func fetchAudio(ctx context.Context, c *client.Client, encryptedURL string) (*responseAudio, error) {
	headers := map[string][]string{
		"url":        {encryptedURL},
		"Origin":     {"https://fastvideosave.net"},
		"Referer":    {"https://fastvideosave.net/"},
		"User-Agent": {"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"},
	}

	resp, err := c.Get(ctx, "https://api.videodropper.app/audio", headers)
	if err != nil {
		return nil, fmt.Errorf("request /audio gagal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status /audio: %d", resp.StatusCode)
	}

	var data responseAudio
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode /audio gagal: %w", err)
	}
	return &data, nil
}

func getMP3URL(videoURL string) string {
	encoded := url.QueryEscape(videoURL)
	return fmt.Sprintf("https://mp3.videodropper.app/api?url=%s", encoded)
}

func generateIDFromURL(rawURL string) string {
	hash := crc32.ChecksumIEEE([]byte(rawURL))
	return strconv.FormatUint(uint64(hash), 10)
}

// ------------------- exported function -------------------

// FetchFastVidioSave mengambil data dari API fastvidio (videodropper) dan mengembalikan UniversalInstagramData.
// Title diisi dengan "Tiktok Download" sesuai permintaan.
func FetchFastVidioSave(rawURL string) (*UniversalInstagramData, error) {
	encrypted, err := encryptURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("enkripsi URL gagal: %w", err)
	}

	// buat client httpcloak
	c := client.NewClient("chrome-143")
	defer c.Close()

	// ambil data allinone
	dataAll, err := fetchAllinone(context.Background(), c, encrypted)
	if err != nil {
		return nil, fmt.Errorf("fetch /allinone gagal: %w", err)
	}

	// ambil audio (opsional, error diabaikan)
	dataAudio, _ := fetchAudio(context.Background(), c, encrypted)

	// bangun hasil
	result := &UniversalInstagramData{
		ID:        generateIDFromURL(rawURL),
		Title:     "Tiktok Download", // statis
		ImageURLs: []string{},
	}

	if len(dataAll.Video) > 0 {
		result.VideoURL = dataAll.Video[0].Video
		result.CoverURL = dataAll.Video[0].Thumbnail
	}
	if len(dataAll.Image) > 0 {
		result.ImageURLs = dataAll.Image
	}

	totalItems := len(dataAll.Video) + len(dataAll.Image)
	result.IsAlbum = totalItems > 1

	if dataAudio != nil && dataAudio.Audio && len(dataAudio.Media) > 0 {
		result.AudioURL = dataAudio.Media[0].URL
	} else if result.VideoURL != "" {
		// fallback: generate dari video URL
		result.AudioURL = getMP3URL(result.VideoURL)
	}

	return result, nil
}
