package instagram

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchInstagramFromDownloadGram mengadaptasi logika DownloadGram ke struktur UniversalInstagramData
func FetchInstagramFromDownloadGram(instaURL string) (*UniversalInstagramData, error) {
	apiURL := "https://api.downloadgram.org/media"

	// 1. Siapkan Payload (Form Data)
	formData := url.Values{}
	formData.Set("url", instaURL)
	formData.Set("v", "3")
	formData.Set("lang", "en")

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat request DownloadGram: %v", err)
	}

	// 2. Set Headers (Menyamar sebagai Browser Android)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Android 10; Mobile; rv:131.0) Gecko/131.0 Firefox/131.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "id-ID")
	req.Header.Set("Referer", "https://downloadgram.org/")
	req.Header.Set("Origin", "https://downloadgram.org")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")

	// 3. Eksekusi Request
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke DownloadGram gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DownloadGram mengembalikan status HTTP %d", resp.StatusCode)
	}

	// 4. BACA DAN BERSIHKAN RAW RESPONSE
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca body: %v", err)
	}
	rawStr := string(bodyBytes)

	// Pembersihan Hex-Encoding dari Javascript Server DownloadGram
	rawStr = strings.ReplaceAll(rawStr, `\x20`, " ")
	rawStr = strings.ReplaceAll(rawStr, `\x22`, `"`)
	rawStr = strings.ReplaceAll(rawStr, `\x27`, `'`)
	rawStr = strings.ReplaceAll(rawStr, `\"`, `"`)

	// 5. Parsing HTML (Menggunakan goquery)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawStr))
	if err != nil {
		return nil, fmt.Errorf("gagal parsing HTML DownloadGram: %v", err)
	}

	shortcode := extractShortcode(instaURL)
	if shortcode == "" {
		shortcode = "unknown"
	}
	numericID := generateNumericID(shortcode)

	var videoURL, coverURL string
	var imageURLs []string

	// 6. Ekstraksi berdasarkan Tag HTML yang sudah normal
	if doc.Find("video").Length() > 0 {
		// Menemukan Video
		videoTag := doc.Find("video").First()
		coverURL = cleanQuotesAndSpaces(videoTag.AttrOr("poster", ""))

		sourceTag := videoTag.Find("source").First()
		videoURL = cleanQuotesAndSpaces(sourceTag.AttrOr("src", ""))

		// Fallback jika <source> kosong
		if videoURL == "" {
			videoURL = cleanQuotesAndSpaces(doc.Find("a[download]").AttrOr("href", ""))
		}
	} else if doc.Find("img").Length() > 0 {
		// Menemukan Gambar (Mendukung Album/Carousel)
		doc.Find("img").Each(func(i int, s *goquery.Selection) {
			imgURL := cleanQuotesAndSpaces(s.AttrOr("src", ""))

			// Validasi dasar: Pastikan URL tidak kosong.
			// Opsional: Tambahkan filter string jika DownloadGram menyertakan logo web mereka sendiri di tag <img>
			if imgURL != "" && strings.HasPrefix(imgURL, "http") {
				imageURLs = append(imageURLs, imgURL)
			}
		})

		// Set gambar pertama di array sebagai cover
		if len(imageURLs) > 0 {
			coverURL = imageURLs[0]
		}
	} else {
		return nil, fmt.Errorf("media tidak ditemukan dari HTML yang dibersihkan")
	}

	if videoURL == "" && len(imageURLs) == 0 {
		return nil, fmt.Errorf("media gagal di-parsing dari DownloadGram")
	}

	// 7. Kembalikan ke format Universal
	return &UniversalInstagramData{
		ID:        numericID,
		Title:     "Instagram Media (DownloadGram)",
		AudioURL:  "",
		VideoURL:  videoURL,
		IsAlbum:   len(imageURLs) > 1,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

// Helper untuk membersihkan kutip sisa dan spasi yang merusak link (misal: "token= eyJ...")
func cleanQuotesAndSpaces(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, `'`, "")
	// Hapus semua spasi di dalam URL karena URL tidak boleh memiliki spasi mentah
	s = strings.ReplaceAll(s, " ", "")
	return strings.TrimSpace(s)
}
