package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type storyCandidate struct {
	Height          int64  `json:"height"`
	Width           int64  `json:"width"`
	URL             string `json:"url"`
	URLDownloadable string `json:"url_downloadable"`
}

type storyResponse struct {
	Result []struct {
		PK             string           `json:"pk"`
		VideoVersions  []storyCandidate `json:"video_versions"`
		ImageVersions2 *struct {
			Candidates []storyCandidate `json:"candidates"`
		} `json:"image_versions2"`
		User *struct {
			Username string `json:"username"`
		} `json:"user"`
	} `json:"result"`
}

func FetchIgramStory(targetURL string) (*UniversalInstagramData, error) {
	if !strings.Contains(targetURL, "/stories/") {
		return nil, fmt.Errorf("invalid Instagram Story URL")
	}
	return fetchIGramStory(targetURL)
}

func fetchIGramStory(targetURL string) (*UniversalInstagramData, error) {
	payload, err := buildIGramStoryPayload(targetURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/api/v1/instagram/story", igramHostname), payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://igram.world/")
	req.Header.Set("Origin", "https://igram.world")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return parseStoryResponse(body, targetURL)
}

func buildIGramStoryPayload(targetURL string) (io.Reader, error) {
	nowMs := time.Now().UnixMilli()
	serverMs := getIGramServerTime()
	drift := serverMs - nowMs

	var correction int64
	if drift >= 60000 || drift <= -60000 {
		correction = drift
	}
	ts := nowMs + correction

	keys := []string{"_df", "_ef", "_sc", "url"}
	vals := []interface{}{0, 0, 0, targetURL}

	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(&buf, "%q:", k)
		switch v := vals[i].(type) {
		case string:
			fmt.Fprintf(&buf, "%q", v)
		case int:
			fmt.Fprintf(&buf, "%d", v)
		}
	}
	buf.WriteByte('}')
	dataToSign := buf.String() + strconv.FormatInt(ts, 10)

	keyBytes, _ := hex.DecodeString(igramHMACKey)
	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(dataToSign))
	sig := hex.EncodeToString(mac.Sum(nil))

	final := map[string]interface{}{
		"url":  targetURL,
		"_sc":  0,
		"_ef":  0,
		"_df":  0,
		"ts":   ts,
		"_ts":  igramStaticTS,
		"_tsc": correction,
		"_sv":  2,
		"_s":   sig,
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.Encode(final)
	return bytes.NewReader(bytes.TrimSpace(out.Bytes())), nil
}

func parseStoryResponse(body []byte, targetURL string) (*UniversalInstagramData, error) {
	var resp storyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse story response: %w", err)
	}
	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("no story items found")
	}

	var imageURLs []string
	var videoURL, coverURL, username string

	for _, item := range resp.Result {
		if username == "" && item.User != nil {
			username = item.User.Username
		}

		// VIDEO STORY
		if len(item.VideoVersions) > 0 {
			sorted := make([]storyCandidate, len(item.VideoVersions))
			copy(sorted, item.VideoVersions)
			sortStoryCandidates(sorted)

			if videoURL == "" {
				if clean, err := getCleanURL(bestStoryURL(sorted[0])); err == nil && clean != "" {
					videoURL = clean
				}
			}
			// image_versions2 jadi cover
			if item.ImageVersions2 != nil && coverURL == "" {
				imgs := make([]storyCandidate, len(item.ImageVersions2.Candidates))
				copy(imgs, item.ImageVersions2.Candidates)
				sortStoryCandidates(imgs)
				if clean, err := getCleanURL(bestStoryURL(imgs[0])); err == nil && clean != "" {
					coverURL = clean
				}
			}
			continue
		}

		// IMAGE STORY - ambil 1 kualitas terbaik saja
		if item.ImageVersions2 != nil && len(item.ImageVersions2.Candidates) > 0 {
			imgs := make([]storyCandidate, len(item.ImageVersions2.Candidates))
			copy(imgs, item.ImageVersions2.Candidates)

			// Urutkan dari resolusi tertinggi ke terendah
			sortStoryCandidates(imgs)

			// Langsung ambil indeks [0] (kualitas paling jernih)
			if clean, err := getCleanURL(bestStoryURL(imgs[0])); err == nil && clean != "" {
				imageURLs = append(imageURLs, clean)
			}
		}
	}

	id := ""
	if m := regexp.MustCompile(`/stories/[^/]+/(\d+)`).FindStringSubmatch(targetURL); len(m) > 1 {
		id = m[1]
	} else if len(resp.Result) > 0 {
		id = resp.Result[0].PK
	}

	title := "Story dari @" + username
	if username == "" {
		title = "Instagram Story"
	}

	isAlbum := len(resp.Result) > 1 || len(imageURLs) > 1

	return &UniversalInstagramData{
		ID:        id,
		Title:     title,
		VideoURL:  videoURL,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
		IsAlbum:   isAlbum,
	}, nil
}

func sortStoryCandidates(c []storyCandidate) {
	sort.Slice(c, func(i, j int) bool {
		pi := c[i].Height * c[i].Width
		pj := c[j].Height * c[j].Width
		if pi != pj {
			return pi > pj
		}
		return c[i].Height > c[j].Height
	})
}

func bestStoryURL(c storyCandidate) string {
	if c.URLDownloadable != "" {
		return c.URLDownloadable
	}
	return c.URL
}
