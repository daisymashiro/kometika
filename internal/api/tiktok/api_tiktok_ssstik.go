package tiktok

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SsstikData (internal, tidak diekspor)
type ssstikData struct {
	Author    string
	Title     string
	Thumbnail string
	MP4NoWM   string
	MP3       string
	IsImage   bool
	Images    []string
}

type ssstikScraper struct {
	Client *http.Client
}

func newSsstikScraper() *ssstikScraper {
	jar, _ := cookiejar.New(nil)
	return &ssstikScraper{
		Client: &http.Client{
			Jar:     jar,
			Timeout: 45 * time.Second,
		},
	}
}

func setHeaders(req *http.Request) {
	req.Header.Set("hx-current-url", "https://ssstik.io/id")
	req.Header.Set("hx-request", "true")
	req.Header.Set("hx-target", "target")
	req.Header.Set("hx-trigger", "_gcaptcha_pt")
	req.Header.Set("origin", "https://ssstik.io")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("referer", "https://ssstik.io/id")
	req.Header.Set("sec-ch-ua", `" Not A;Brand";v="99", "Chromium";v="102", "Google Chrome";v="102"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Linux"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.5059.159 Safari/537.36")
}

func decodeSsscdnLink(href string) string {
	if !strings.Contains(href, "ssscdn.io") {
		return href
	}
	parts := strings.Split(href, "/")
	if len(parts) > 5 {
		b64Str := strings.Join(parts[5:], "/")
		b64Str = strings.TrimRight(b64Str, "/")
		b64Str = strings.ReplaceAll(b64Str, "-", "+")
		b64Str = strings.ReplaceAll(b64Str, "_", "/")

		pad := len(b64Str) % 4
		if pad != 0 {
			b64Str += strings.Repeat("=", 4-pad)
		}

		decoded, err := base64.StdEncoding.DecodeString(b64Str)
		if err == nil {
			return string(decoded)
		}
	}
	return href
}

// getMediaFromSsstik (sama persis seperti GetMedia Anda, hanya saya rename internal)
func (s *ssstikScraper) getMedia(targetURL string) (*ssstikData, error) {
	// 1. GET ambil token
	reqGet, err := http.NewRequest("GET", "https://ssstik.io", nil)
	if err != nil {
		return nil, err
	}
	setHeaders(reqGet)

	respGet, err := s.Client.Do(reqGet)
	if err != nil {
		return nil, err
	}
	defer respGet.Body.Close()

	bodyGet, _ := io.ReadAll(respGet.Body)
	ttRegex := regexp.MustCompile(`s_tt\s*=\s*'([^']+)'`)
	ttMatches := ttRegex.FindStringSubmatch(string(bodyGet))
	if len(ttMatches) < 2 {
		return nil, fmt.Errorf("token 's_tt' tidak ditemukan")
	}
	token := ttMatches[1]

	// 2. POST
	formData := url.Values{}
	formData.Set("id", targetURL)
	formData.Set("locale", "en")
	formData.Set("tt", token)

	reqPost, err := http.NewRequest("POST", "https://ssstik.io/abc?url=dl", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	setHeaders(reqPost)
	reqPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	respPost, err := s.Client.Do(reqPost)
	if err != nil {
		return nil, err
	}
	defer respPost.Body.Close()

	bodyBytes, _ := io.ReadAll(respPost.Body)
	rawHTML := string(bodyBytes)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return nil, err
	}

	var data ssstikData

	data.Author = strings.TrimSpace(doc.Find(".pd-lr h2").Text())
	titleText := strings.TrimSpace(doc.Find("p.maintext").Text())
	if titleText != "" {
		data.Title = titleText
	} else {
		data.Title = ""
	}

	mp3Link, exists := doc.Find("a.music").Attr("href")
	if exists {
		data.MP3 = decodeSsscdnLink(strings.TrimSpace(mp3Link))
	}

	slidesDataVal, hasSlidesData := doc.Find("input[name='slides_data']").Attr("value")

	if hasSlidesData && slidesDataVal != "" {
		data.IsImage = true
		doc.Find("a.slide").Each(func(i int, sel *goquery.Selection) {
			href, ok := sel.Attr("href")
			if ok {
				data.Images = append(data.Images, strings.TrimSpace(href))
			}
		})
		if len(data.Images) > 0 {
			data.Thumbnail = data.Images[0]
		}

		renderPayload := url.Values{}
		renderPayload.Set("slides_data", slidesDataVal)
		reqRender, err := http.NewRequest("POST", "https://r.ssstik.top/b/index.sh", strings.NewReader(renderPayload.Encode()))
		if err == nil {
			reqRender.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			reqRender.Header.Set("Origin", "https://ssstik.io")
			reqRender.Header.Set("Referer", "https://ssstik.io/")
			reqRender.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.5059.159 Safari/537.36")
			respRender, err := s.Client.Do(reqRender)
			if err == nil {
				defer respRender.Body.Close()
				renderHTML, _ := goquery.NewDocumentFromReader(respRender.Body)
				renderedLink, ok := renderHTML.Find("a").Attr("href")
				if ok {
					data.MP4NoWM = strings.TrimSpace(renderedLink)
				}
			}
		}
	} else {
		data.IsImage = false
		mp4Link, exists := doc.Find("a.without_watermark").Attr("href")
		if exists {
			data.MP4NoWM = decodeSsscdnLink(strings.TrimSpace(mp4Link))
		}
		reThumb := regexp.MustCompile(`background-image:\s*url\((.*?)\)`)
		thumbMatch := reThumb.FindStringSubmatch(rawHTML)
		if len(thumbMatch) > 1 {
			data.Thumbnail = strings.TrimSpace(thumbMatch[1])
		}
	}

	return &data, nil
}

// extractTikTokID mengambil 19 digit angka dari URL TikTok
func extractTikTokID(rawURL string) (string, error) {
	re := regexp.MustCompile(`\b\d{19}\b`)
	matches := re.FindStringSubmatch(rawURL)
	if len(matches) == 0 {
		return "", fmt.Errorf("tidak ditemukan ID 19 digit pada URL: %s", rawURL)
	}
	return matches[0], nil
}

// ScrapeTikTokUniversal adalah fungsi utama yang Anda minta.
// Ia mengembalikan *api.UniversalTikTokData (struct dari package api Anda)
// tanpa menambahkan field apapun ke struct tersebut.
func ScrapeTikTokUniversal(tiktokURL string) (*UniversalTikTokData, error) {
	// 1. Ekstrak ID 19 digit
	videoID, err := extractTikTokID(tiktokURL)
	if err != nil {
		return nil, err
	}

	// 2. Scrape dengan ssstik.io
	scraper := newSsstikScraper()
	sData, err := scraper.getMedia(tiktokURL)
	if err != nil {
		return nil, fmt.Errorf("gagal scrape ssstik: %w", err)
	}

	// 3. Konversi ke api.UniversalTikTokData
	//    MusicName dan Duration diisi kosong/0 karena tidak tersedia.
	universal := &UniversalTikTokData{
		ID:        videoID,
		Title:     sData.Title,
		VideoURL:  sData.MP4NoWM,
		AudioURL:  sData.MP3,
		IsAlbum:   sData.IsImage,
		ImageURLs: sData.Images,
		CoverURL:  sData.Thumbnail,
	}

	return universal, nil
}
