package mediafire

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type MediaFireData struct {
	ID         string
	Title      string
	Size       string
	DirectLink string
}

func FetchMediaFireData(rawURL string) (*MediaFireData, error) {
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil halaman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal parsing HTML: %w", err)
	}

	info := &MediaFireData{}

	// 1. Ambil ID dari URL
	parsedURL, _ := url.Parse(rawURL)
	pathParts := strings.Split(parsedURL.Path, "/")
	for i, part := range pathParts {
		if part == "file" && i+1 < len(pathParts) {
			info.ID = pathParts[i+1]
			break
		}
	}
	if info.ID == "" {
		info.ID = parsedURL.Query().Get("id")
	}

	// 2. Ambil judul
	title := doc.Find("title").Text()
	if title != "" {
		title = strings.TrimSpace(title)
		title = strings.ReplaceAll(title, " - MediaFire", "")
		title = strings.ReplaceAll(title, " | MediaFire", "")
		title = strings.ReplaceAll(title, " - mediafires.co", "")
		info.Title = title
	}
	if info.Title == "" {
		doc.Find(".filename, .file-name, .download-file-name, .file_info .name").Each(func(i int, s *goquery.Selection) {
			if info.Title == "" {
				info.Title = strings.TrimSpace(s.Text())
			}
		})
	}
	if info.Title == "" {
		info.Title = filepath.Base(parsedURL.Path)
	}

	// 3. Ambil ukuran file
	doc.Find(".file-size, .download-file-size, .file_info .size, .size").Each(func(i int, s *goquery.Selection) {
		if info.Size == "" {
			info.Size = strings.TrimSpace(s.Text())
		}
	})
	if info.Size == "" {
		html, _ := doc.Html()
		re := regexp.MustCompile(`(\d+(\.\d+)?\s*(KB|MB|GB|TB))`)
		if match := re.FindString(html); match != "" {
			info.Size = match
		}
	}

	// 4. Ambil tautan unduhan langsung
	var directLink string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		if directLink != "" {
			return
		}
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		class, _ := s.Attr("class")
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if strings.Contains(href, "download") ||
			strings.Contains(strings.ToLower(class), "download") ||
			strings.Contains(text, "download") {
			if strings.HasPrefix(href, "//") {
				href = "https:" + href
			} else if strings.HasPrefix(href, "/") {
				u, _ := url.Parse(rawURL)
				if u != nil {
					href = u.Scheme + "://" + u.Host + href
				}
			}
			if strings.HasPrefix(href, "http") {
				directLink = href
			}
		}
	})

	if directLink == "" {
		doc.Find("[data-download], [data-link]").Each(func(i int, s *goquery.Selection) {
			if directLink == "" {
				if link, ok := s.Attr("data-download"); ok && link != "" {
					directLink = link
				} else if link, ok := s.Attr("data-link"); ok && link != "" {
					directLink = link
				}
			}
		})
	}

	if directLink == "" {
		html, _ := doc.Html()
		re := regexp.MustCompile(`"(https?://download[^\"]+)"`)
		if match := re.FindStringSubmatch(html); len(match) > 1 {
			directLink = match[1]
		}
	}
	info.DirectLink = directLink

	return info, nil
}
