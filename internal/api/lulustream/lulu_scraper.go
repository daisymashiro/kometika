package lulustream

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// FileDownloadResult menampung data hasil ekstraksi
type FileDownloadResult struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Thumbnail   string `json:"thumbnail"`
	Filesize    string `json:"filesize"`
	ExactSize   string `json:"exact_size"`
	DownloadURL string `json:"download_url"`
}

// Scrape mengekstrak direct link dan metadata dari LuluStream berdasarkan Video ID
func Scrape(videoID string) (*FileDownloadResult, *http.Client, error) {
	if videoID == "" {
		return nil, nil, errors.New("videoID tidak boleh kosong")
	}

	result := &FileDownloadResult{ID: videoID}
	client := newHTTPClient()

	urlMain := fmt.Sprintf("https://luluvid.com/%s", videoID)
	urlHash := fmt.Sprintf("https://luluvid.com/d/%s_h", videoID)

	// ==========================================
	// TAHAP 1: Ekstrak Metadata (Halaman Utama)
	// ==========================================
	req1, _ := http.NewRequest("GET", urlMain, nil)
	setStandardHeaders(req1)

	resp1, err := client.Do(req1)
	if err != nil {
		return nil, nil, fmt.Errorf("gagal GET halaman utama: %w", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode == 404 {
		return nil, nil, errors.New("video tidak ditemukan (404)")
	}

	doc1, err := goquery.NewDocumentFromReader(resp1.Body)
	if err == nil {
		title := strings.TrimSpace(doc1.Find("title").Text())
		result.Title = strings.TrimSpace(strings.ReplaceAll(title, "- LuluStream", ""))

		if content, exists := doc1.Find("meta[name='og:image']").Attr("content"); exists {
			result.Thumbnail = content
		} else if content, exists := doc1.Find("meta[property='og:image']").Attr("content"); exists {
			result.Thumbnail = content
		}
	}

	// ==========================================
	// DELAY: Simulasi manusia
	// ==========================================
	time.Sleep(3 * time.Second)

	// ==========================================
	// TAHAP 2: Ekstrak Hash (Halaman /d/{id}_h)
	// ==========================================
	req2, _ := http.NewRequest("GET", urlHash, nil)
	setStandardHeaders(req2)
	req2.Header.Set("Referer", urlMain)

	resp2, err := client.Do(req2)
	if err != nil {
		return nil, nil, fmt.Errorf("gagal GET halaman hash: %w", err)
	}
	defer resp2.Body.Close()

	doc2, err := goquery.NewDocumentFromReader(resp2.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("gagal parsing DOM halaman hash: %w", err)
	}

	op := doc2.Find("input[name='op']").AttrOr("value", "")
	mode := doc2.Find("input[name='mode']").AttrOr("value", "")
	hash := doc2.Find("input[name='hash']").AttrOr("value", "")

	if hash == "" {
		return nil, nil, errors.New("token hash tidak ditemukan, server mungkin memblokir request atau struktur HTML berubah")
	}

	// ==========================================
	// DELAY: Simulasi manusia sebelum klik
	// ==========================================
	time.Sleep(4 * time.Second)

	// ==========================================
	// TAHAP 3: POST dengan RETRY (maks 3x)
	// ==========================================
	var downloadURL string
	var lastError error

	for attempt := 1; attempt <= 3; attempt++ {
		// TAMBAHKAN PARAMETER "result" DI SINI
		downloadURL, lastError = tryPostDownload(client, result, urlHash, urlMain, videoID, op, mode, hash)

		if downloadURL != "" {
			break // Berhasil!
		}

		// Cek apakah error karena security (bisa di-retry)
		if strings.Contains(lastError.Error(), "Security error") {
			fmt.Printf("[RETRY] Attempt %d/3 - Security error, mencoba lagi...\n", attempt)

			// Regenerate hash (GET ulang halaman hash)
			time.Sleep(3 * time.Second)

			reqRefresh, _ := http.NewRequest("GET", urlHash, nil)
			setStandardHeaders(reqRefresh)
			reqRefresh.Header.Set("Referer", urlMain)

			respRefresh, err := client.Do(reqRefresh)
			if err != nil {
				continue
			}

			docRefresh, err := goquery.NewDocumentFromReader(respRefresh.Body)
			respRefresh.Body.Close()
			if err != nil {
				continue
			}

			hash = docRefresh.Find("input[name='hash']").AttrOr("value", "")
			op = docRefresh.Find("input[name='op']").AttrOr("value", "")
			mode = docRefresh.Find("input[name='mode']").AttrOr("value", "")

			if hash == "" {
				continue
			}

			time.Sleep(5 * time.Second)
			continue
		}

		// Error lain, langsung break
		break
	}

	if downloadURL == "" {
		return nil, client, lastError
	}

	result.DownloadURL = downloadURL
	return result, client, nil
}

// tryPostDownload melakukan satu kali percobaan POST
// TAMBAHKAN "result *FileDownloadResult" DI PARAMETER
func tryPostDownload(client *http.Client, result *FileDownloadResult, urlHash, urlMain, videoID, op, mode, hash string) (string, error) {
	formData := url.Values{}
	formData.Set("op", op)
	formData.Set("id", videoID)
	formData.Set("mode", mode)
	formData.Set("hash", hash)

	req3, _ := http.NewRequest("POST", urlHash, strings.NewReader(formData.Encode()))
	setStandardHeaders(req3)
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.Header.Set("Origin", "https://luluvid.com")
	req3.Header.Set("Referer", urlHash)

	resp3, err := client.Do(req3)
	if err != nil {
		return "", fmt.Errorf("gagal POST request untuk direct link: %w", err)
	}
	defer resp3.Body.Close()

	doc3, err := goquery.NewDocumentFromReader(resp3.Body)
	if err != nil {
		return "", fmt.Errorf("gagal parsing DOM halaman hasil: %w", err)
	}

	// Cek Security Error
	if doc3.Find(".text-danger").Length() > 0 {
		errorText := strings.TrimSpace(doc3.Find(".text-danger").First().Text())
		if strings.Contains(errorText, "Security error") {
			return "", fmt.Errorf("Security error dari server")
		}
	}

	// Parsing Filesize & Exact Size
	doc3.Find("table tr").Each(func(i int, s *goquery.Selection) {
		label := strings.ToLower(strings.TrimSpace(s.Find("td").Eq(0).Text()))
		value := strings.TrimSpace(s.Find("td").Eq(1).Text()) // <- SEKARANG TIDAK AKAN ERROR

		if strings.Contains(label, "filesize") {
			result.Filesize = value // <- VALUE DIGUNAKAN
		} else if strings.Contains(label, "exact size") {
			result.ExactSize = value // <- VALUE DIGUNAKAN
		}
	})

	// Cari direct link - Method 1: class submit-btn
	if aBtn := doc3.Find("a.submit-btn").First(); aBtn.Length() > 0 {
		href := aBtn.AttrOr("href", "")
		if href != "" && strings.HasPrefix(href, "http") {
			return href, nil
		}
	}

	// Cari direct link - Method 2: teks "Direct Download"
	var foundURL string
	doc3.Find("a").Each(func(i int, s *goquery.Selection) {
		if foundURL != "" {
			return
		}
		href := s.AttrOr("href", "")
		text := strings.ToLower(s.Text())
		if (strings.Contains(text, "direct download") || strings.Contains(text, "download link")) &&
			strings.HasPrefix(href, "http") {
			foundURL = href
		}
	})
	if foundURL != "" {
		return foundURL, nil
	}

	// Cari direct link - Method 3: CDN URL pattern
	doc3.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		if foundURL != "" {
			return
		}
		href := s.AttrOr("href", "")
		if strings.Contains(href, ".mp4") ||
			strings.Contains(href, "tnmr.org") ||
			strings.Contains(href, "/v/") {
			foundURL = href
		}
	})
	if foundURL != "" {
		return foundURL, nil
	}

	// Debug: simpan HTML
	htmlBody, _ := doc3.Html()
	_ = os.WriteFile("error_lulu_bot.html", []byte(htmlBody), 0644)

	return "", errors.New("direct link tidak ditemukan. Cek file error_lulu_bot.html di direktori bot untuk melihat halaman asli yang diblokir")
}

// ==========================================
// Helper Functions
// ==========================================

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout: 120 * time.Second,
		Jar:     jar,
	}
}

func setStandardHeaders(req *http.Request) {
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="120", "Not:A-Brand";v="8", "Chromium";v="120"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
}

// PERUBAHAN: Terima client dan referer, tambahkan header Range
func LuluStreamDown(ctx context.Context, client *http.Client, videoURL string, refererURL string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", videoURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", refererURL)
	req.Header.Set("Range", "bytes=0-") // RAHASIA: Wajib untuk nginx CDN
	req.Header.Set("Sec-Fetch-Dest", "video")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// ==========================================
	// RAHASIA DI SINI: Buat Client Khusus Download
	// ==========================================
	// Client ini TANPA Timeout (0), jadi tidak akan mati sendiri saat streaming file besar.
	// Tapi tetap memakai "Jar" (Cookie) dari client sebelumnya agar tidak kena 403.
	downloadClient := &http.Client{
		Jar: client.Jar,
	}

	// Gunakan downloadClient, BUKAN client
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gagal request stream HTTP: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("unexpected status %d for %q", resp.StatusCode, videoURL)
	}

	return resp.Body, resp.ContentLength, nil
}
