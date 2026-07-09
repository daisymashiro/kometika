package tiktok

import (
	"errors"
	"github.com/PuerkitoBio/goquery"
	"hash/crc32"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const baseURL = "https://musicaldown.com"

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{Jar: jar},
	}
}

// generateNumericID menghasilkan ID numerik (string) dari shortcode menggunakan CRC32
func generateNumericID(shortcode string) string {
	hash := crc32.ChecksumIEEE([]byte(shortcode))
	return strconv.FormatUint(uint64(hash), 10)
}

// extractShortcodeFromURL mengambil shortcode dari berbagai format URL TikTok
func extractShortcodeFromURL(tiktokURL string) string {
	reShort := regexp.MustCompile(`tiktok\.com/(?:@[\w]+/video/|t/|v/)?(\d+)|vm\.tiktok\.com/([a-zA-Z0-9_-]+)`)
	matches := reShort.FindStringSubmatch(tiktokURL)
	if len(matches) > 1 {
		if matches[1] != "" {
			return matches[1]
		}
		if matches[2] != "" {
			return matches[2]
		}
	}
	// Fallback: ambil bagian terakhir path setelah slash terakhir
	parts := strings.Split(strings.TrimRight(tiktokURL, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if strings.Contains(last, "?") {
			last = strings.Split(last, "?")[0]
		}
		return last
	}
	return ""
}

// GetUniversalData melakukan request ke musicaldown dan mengembalikan UniversalTikTokData
func (c *Client) GetUniversalData(tiktokURL string) (*UniversalTikTokData, error) {
	// Step 1: GET halaman utama
	resp, err := c.httpClient.Get(baseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Step 2: kumpulkan input hidden
	formData := url.Values{}
	doc.Find("input[type='hidden']").Each(func(i int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		value, _ := s.Attr("value")
		if name != "" {
			formData.Set(name, value)
		}
	})

	// Step 3: cari input text
	textInput := doc.Find("input[type='text']").First()
	if textInput.Length() == 0 {
		return nil, errors.New("input text tidak ditemukan")
	}
	textName, _ := textInput.Attr("name")
	if textName == "" {
		return nil, errors.New("nama input text tidak ditemukan")
	}
	formData.Set(textName, tiktokURL)

	// Step 4: POST ke /download
	postURL := baseURL + "/download"
	req, err := http.NewRequest("POST", postURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL+"/en")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", "Linux")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if strings.Contains(resp.Request.URL.Path, "err") {
		return nil, errors.New("URL TikTok tidak valid atau error")
	}

	doc, err = goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Generate ID dari shortcode
	shortcode := extractShortcodeFromURL(tiktokURL)
	id := generateNumericID(shortcode)

	data := &UniversalTikTokData{
		ID: id,
	}

	// Deteksi apakah video biasa atau album (slideshow)
	isSlideshow := doc.Find("div.video-header.bg-overlay").Length() == 0

	if !isSlideshow {
		// ========== VIDEO ==========
		data.IsAlbum = false

		// Cover/Thumbnail
		bgDiv := doc.Find("div.video-header.bg-overlay").First()
		if style, exists := bgDiv.Attr("style"); exists {
			re := regexp.MustCompile(`background-image:\s*url\(([^)]+)\)`)
			if matches := re.FindStringSubmatch(style); len(matches) > 1 {
				data.CoverURL = matches[1]
			}
		}
		if data.CoverURL == "" {
			if src, exists := doc.Find("div.img-area img").Attr("src"); exists {
				data.CoverURL = src
			}
		}

		// Title (caption)
		data.Title = strings.TrimSpace(doc.Find("p.video-desc").Text())

		// Video URL (no watermark) dan Audio URL
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists || !strings.Contains(href, "fastdl.muscdn.app") {
				return
			}
			text := strings.TrimSpace(s.Text())
			event, _ := s.Attr("data-event")

			switch {
			case strings.Contains(text, "MP4") && !strings.Contains(text, "HD") && !strings.Contains(text, "Watermark"):
				if data.VideoURL == "" {
					data.VideoURL = href
				}
			case strings.Contains(text, "MP3") || event == "mp3_download_click":
				if data.AudioURL == "" {
					data.AudioURL = href
				}
			}
		})

		if data.VideoURL == "" {
			return nil, errors.New("tidak ditemukan URL video (no watermark)")
		}
	} else {
		// ========== SLIDESHOW / ALBUM ==========
		data.IsAlbum = true
		data.ImageURLs = []string{}

		// Title dari meta description atau title
		if desc, exists := doc.Find("meta[name='description']").Attr("content"); exists {
			data.Title = desc
		}
		if data.Title == "" {
			data.Title = strings.TrimSpace(doc.Find("title").Text())
		}

		// Audio URL
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists || !strings.Contains(href, "fastdl.muscdn.app") {
				return
			}
			text := strings.TrimSpace(s.Text())
			event, _ := s.Attr("data-event")
			if strings.Contains(text, "MP3") || event == "mp3_download_click" {
				if data.AudioURL == "" {
					data.AudioURL = href
				}
			}
		})

		doc.Find("div.card-action a.btn").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists && strings.Contains(href, "fastdl.muscdn.app") {
				data.ImageURLs = append(data.ImageURLs, href)
			}
		})

		// Jika tidak ada gambar dari tombol, coba ambil dari img src (cuman preview, bukan download)
		if len(data.ImageURLs) == 0 {
			doc.Find("div.card-image img").Each(func(i int, s *goquery.Selection) {
				src, exists := s.Attr("src")
				if exists && strings.Contains(src, "fastdl.muscdn.app") {
					data.ImageURLs = append(data.ImageURLs, src)
				}
			})
		}

		if len(data.ImageURLs) == 0 {
			return nil, errors.New("tidak ditemukan gambar untuk slideshow")
		}
	}

	return data, nil
}

// MusicallDown adalah fungsi utama yang mengembalikan UniversalTikTokData
func FetchMusicallDown(tiktokURL string) (*UniversalTikTokData, error) {
	client := NewClient()
	return client.GetUniversalData(tiktokURL)
}
