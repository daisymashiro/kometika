package facebook

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FetchFacebookGetVidFB mengambil video dari getvidfb.com
// Mendukung ekstraksi HD/SD Video, MP3 Audio, dan Thumbnail.
func FetchFacebookGetVidFB(videoURL string) (*FacebookUniversalVideoData, error) {
	validURL := regexp.MustCompile(`(?:https?:\/\/(web\.|www\.|m\.)?(facebook|fb)\.(com|watch|gg)\S+)?$`)
	if !validURL.MatchString(videoURL) {
		return nil, fmt.Errorf("URL Facebook tidak valid")
	}

	encodedURL := url.QueryEscape(videoURL)
	formData := fmt.Sprintf("url=%s&lang=en&type=redirect", encodedURL)

	req, err := http.NewRequest("POST", "https://getvidfb.com/", strings.NewReader(formData))
	if err != nil {
		return nil, fmt.Errorf("gagal buat request GetVidFB: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Mobile Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Origin", "https://getvidfb.com")
	req.Header.Set("Referer", "https://getvidfb.com/")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request GetVidFB gagal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetVidFB HTTP error: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal parse HTML GetVidFB: %v", err)
	}

	videoContainer := doc.Find("#snaptik-video")
	if videoContainer.Length() == 0 {
		return nil, fmt.Errorf("video container tidak ditemukan di GetVidFB")
	}

	thumbnail := videoContainer.Find(".snaptik-left img").AttrOr("src", "")

	// Bersihkan title: ganti newline dengan spasi, lalu rapikan spasi berlebih
	rawTitle := strings.TrimSpace(videoContainer.Find(".snaptik-middle h3").Text())
	cleanTitle := strings.Join(strings.Fields(strings.ReplaceAll(rawTitle, "\n", " ")), " ")
	if cleanTitle == "" {
		cleanTitle = "Facebook Video"
	}

	var videoHD, videoSD, audioMP3 string

	// Cari semua link download
	videoContainer.Find(".abuttons a").Each(func(i int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if !exists || link == "" {
			return
		}
		spanText := strings.TrimSpace(s.Find(".span-icon span").Last().Text())
		if spanText == "" {
			spanText = strings.TrimSpace(s.Text())
		}
		if strings.HasPrefix(link, "http") && spanText != "" {
			if strings.Contains(spanText, "HD") && videoHD == "" {
				videoHD = link
			} else if strings.Contains(spanText, "SD") && videoSD == "" {
				videoSD = link
			} else if (strings.Contains(spanText, "Mp3") || strings.Contains(spanText, "Audio")) && audioMP3 == "" {
				audioMP3 = link
			}
		}
	})

	// Prioritaskan HD, jika tidak ada gunakan SD
	vidURL := videoHD
	if vidURL == "" {
		vidURL = videoSD
	}

	if vidURL == "" {
		return nil, fmt.Errorf("tidak ditemukan link video di GetVidFB")
	}

	// Buat ID unik
	uniqueID := GenerateUniqueID(videoURL)

	return &FacebookUniversalVideoData{
		ID:       uniqueID,
		Title:    cleanTitle,
		VidioURL: vidURL,
		AudioURL: audioMP3,
		CoverURL: thumbnail,
	}, nil
}
