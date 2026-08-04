package facebook

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- Helper Functions ---
func extractFacebookID(url string) string {
	// Pattern: /watch/?v=ID atau /video/ID atau /share/v/ID
	reFb := regexp.MustCompile(`(?:/watch/?\?v=|/video/|/share/v/)([^/?#&]+)`)
	matches := reFb.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func baseConvert(s string, fromBase int) int {
	charset := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/"
	fromChars := charset[:fromBase]
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	result := 0
	multiplier := 1
	for _, ch := range runes {
		idx := strings.IndexRune(fromChars, ch)
		if idx != -1 {
			result += idx * multiplier
		}
		multiplier *= fromBase
	}
	return result
}

func decodeObfuscated(html string) string {
	re := regexp.MustCompile(`\}\("([^"]+)",(\d+),"([^"]+)",(\d+),(\d+),(\d+)\)\)`)
	m := re.FindStringSubmatch(html)
	if len(m) < 7 {
		return ""
	}

	payload, n := m[1], m[3]
	t, _ := strconv.Atoi(m[4])
	e, _ := strconv.Atoi(m[5])
	runesN := []rune(n)

	if e >= len(runesN) {
		return ""
	}
	d := runesN[e]

	var res, chunk strings.Builder
	for _, ch := range payload {
		if ch == d {
			var mappedStr strings.Builder
			for _, c := range chunk.String() {
				idx := strings.IndexRune(n, c)
				if idx != -1 {
					mappedStr.WriteString(fmt.Sprintf("%d", idx))
				} else {
					mappedStr.WriteRune(c)
				}
			}
			val := baseConvert(mappedStr.String(), e) - t
			res.WriteRune(rune(val))
			chunk.Reset()
		} else {
			chunk.WriteRune(ch)
		}
	}
	return res.String()
}

// --- Main Parser untuk Facebook ---
func parseFacebookVideoData(decodedJS string, originalURL string) *FacebookUniversalVideoData {
	result := &FacebookUniversalVideoData{}

	// Generate ID dari URL
	fbID := extractFacebookID(originalURL)
	if fbID != "" {
		checksum := crc32.ChecksumIEEE([]byte(fbID))
		result.ID = fmt.Sprintf("%d", checksum)
	}
	cleanJS := strings.ReplaceAll(decodedJS, `\"`, `"`)
	cleanJS = strings.ReplaceAll(cleanJS, `\\/`, `/`)

	// 1. Ambil thumbnail dari rapidcdn
	reThumbnail := regexp.MustCompile(`<img[^>]*src="(https://d\.rapidcdn\.app/thumb[^"]*)"`)
	thumbMatches := reThumbnail.FindAllStringSubmatch(cleanJS, -1)
	if len(thumbMatches) > 0 {
		result.CoverURL = thumbMatches[0][1]
	}

	// 2. Ambil link download video
	reDownload := regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*class="[^"]*button\s+is-success[^"]*"[^>]*>(?:\s*<span>([^<]+)</span>)?`)
	dlMatches := reDownload.FindAllStringSubmatch(cleanJS, -1)

	// Ambil video URL (prioritas: contains "video" dalam button text)
	for _, m := range dlMatches {
		dlURL := m[1]
		dlURL = strings.ReplaceAll(dlURL, "&amp;", "&")
		buttonText := ""
		if len(m) > 2 {
			buttonText = strings.ToLower(strings.TrimSpace(m[2]))
		}

		// Jika text button mengandung "video", gunakan URL ini
		if strings.Contains(buttonText, "video") {
			if result.VidioURL == "" {
				result.VidioURL = dlURL
			}
		}
	}

	// Fallback: jika tidak ada yang dengan text "video", gunakan link pertama
	if result.VidioURL == "" && len(dlMatches) > 0 {
		result.VidioURL = strings.ReplaceAll(dlMatches[0][1], "&amp;", "&")
	}

	// 3. Set Hardcode Title
	result.Title = "Facebook"

	// 4. Set Hardcode Audio/Music URL
	result.AudioURL = "https://files.catbox.moe/boh523.mp3"

	return result
}

// --- Main Scraping Function ---
func FetchFacebookSnapsave(facebookURL string) (*FacebookUniversalVideoData, error) {
	// Persiapan request ke Snapsave
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("url", facebookURL)
	writer.Close()

	req, err := http.NewRequest("POST", "https://snapsave.app/action.php?lang=id", body)
	if err != nil {
		return nil, fmt.Errorf("gagal buat request: %v", err)
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://snapsave.app")
	req.Header.Set("Referer", "https://snapsave.app/id")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Timeout 40 detik
	client := &http.Client{Timeout: 40 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request gagal: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code %d", res.StatusCode)
	}

	resBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal baca response: %v", err)
	}

	// Decode obfuscated HTML
	decoded := decodeObfuscated(string(resBytes))
	if decoded == "" {
		return nil, fmt.Errorf("gagal mendekode obfuscation (mungkin link tidak valid atau diblokir)")
	}

	// Parse data
	data := parseFacebookVideoData(decoded, facebookURL)

	// Validasi
	if data.VidioURL == "" && data.CoverURL == "" {
		return nil, fmt.Errorf("tidak ada video atau thumbnail yang ditemukan")
	}

	return data, nil
}

